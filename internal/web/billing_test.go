package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"connectai-hub/internal/database"
)

func sign(body, secret string) string {
	m := hmac.New(sha512.New, []byte(secret))
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil))
}

func TestValidPaystackSignature(t *testing.T) {
	body := []byte(`{"event":"charge.success"}`)
	good := sign(string(body), "sk_test_x")
	cases := []struct {
		name, sig, secret string
		want              bool
	}{
		{"correct", good, "sk_test_x", true},
		{"uppercase hex still decodes", strings.ToUpper(good), "sk_test_x", true},
		{"wrong secret", good, "other", false},
		{"empty signature", "", "sk_test_x", false},
		{"not hex", "zzzz", "sk_test_x", false},
		{"empty secret never matches", good, "", false},
	}
	for _, c := range cases {
		if got := validPaystackSignature(body, c.sig, c.secret); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
	if validPaystackSignature([]byte(`{"event":"tampered"}`), good, "sk_test_x") {
		t.Error("a changed body must not validate")
	}
}

func TestPaymentConfirmed(t *testing.T) {
	ok := &paystackVerifyResponse{Status: true}
	ok.Data.Status, ok.Data.Reference, ok.Data.Amount, ok.Data.Currency = "success", "cahabc", 300000, "NGN"
	if !paymentConfirmed(ok, "cahabc", 300000) {
		t.Fatal("exact match should be confirmed")
	}
	for name, mutate := range map[string]func(v paystackVerifyResponse) paystackVerifyResponse{
		"abandoned":    func(v paystackVerifyResponse) paystackVerifyResponse { v.Data.Status = "abandoned"; return v },
		"other ref":    func(v paystackVerifyResponse) paystackVerifyResponse { v.Data.Reference = "cahzzz"; return v },
		"short amount": func(v paystackVerifyResponse) paystackVerifyResponse { v.Data.Amount = 100; return v },
		"usd":          func(v paystackVerifyResponse) paystackVerifyResponse { v.Data.Currency = "USD"; return v },
		"status false": func(v paystackVerifyResponse) paystackVerifyResponse { v.Status = false; return v },
	} {
		v := mutate(*ok)
		if paymentConfirmed(&v, "cahabc", 300000) {
			t.Errorf("%s must not be confirmed", name)
		}
	}
	if paymentConfirmed(nil, "x", 1) {
		t.Error("nil response must not be confirmed")
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	t.Setenv("PAYSTACK_SECRET_KEY", "sk_test_x")
	s := &Server{} // no database needed: it must be rejected before any lookup
	body := `{"event":"charge.success","data":{"reference":"cahabc"}}`
	for name, sig := range map[string]string{"missing": "", "wrong": sign(body, "not-the-secret")} {
		req := httptest.NewRequest(http.MethodPost, "/billing/webhook", strings.NewReader(body))
		if sig != "" {
			req.Header.Set("x-paystack-signature", sig)
		}
		rec := httptest.NewRecorder()
		s.paystackWebhook(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s signature: got %d want 401", name, rec.Code)
		}
	}
}

func TestWebhookIgnoresOtherEvents(t *testing.T) {
	t.Setenv("PAYSTACK_SECRET_KEY", "sk_test_x")
	s := &Server{}
	body := `{"event":"transfer.success","data":{"reference":"cahabc"}}`
	req := httptest.NewRequest(http.MethodPost, "/billing/webhook", strings.NewReader(body))
	req.Header.Set("x-paystack-signature", sign(body, "sk_test_x"))
	rec := httptest.NewRecorder()
	s.paystackWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unrelated event should be acknowledged with 200, got %d", rec.Code)
	}
}

// ---- Database-backed tests: run with TEST_DATABASE_URL pointing at an empty PostgreSQL database ----

func testServer(t *testing.T) *Server {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run database tests")
	}
	db, err := database.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	return &Server{db: db, rates: map[string]rateEntry{}}
}

func newUser(t *testing.T, s *Server, email string) int64 {
	t.Helper()
	var id int64
	if err := s.db.QueryRow(`INSERT INTO users(name,email,password_hash) VALUES('Test User',$1,'x') RETURNING id`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	return id
}

func TestActivatePlusPaymentIsIdempotentAndConcurrencySafe(t *testing.T) {
	s := testServer(t)
	uid := newUser(t, s, "activate-"+t.Name()+"@example.test")
	ref := "test-" + hex.EncodeToString([]byte(t.Name()))
	if _, err := s.db.Exec(`INSERT INTO plan_payments(user_id,reference,amount,status) VALUES($1,$2,300000,'pending')`, uid, ref); err != nil {
		t.Fatal(err)
	}
	// Webhook, callback and reconciler can all arrive together.
	var wg sync.WaitGroup
	var mu sync.Mutex
	granted := 0
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := s.activatePlusPayment(context.Background(), ref)
			if err != nil {
				t.Error(err)
			}
			if ok {
				mu.Lock()
				granted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if granted != 1 {
		t.Fatalf("exactly one caller should grant access, got %d", granted)
	}
	var plan string
	var days float64
	if err := s.db.QueryRow(`SELECT plan, EXTRACT(EPOCH FROM (plus_expires_at-NOW()))/86400 FROM users WHERE id=$1`, uid).Scan(&plan, &days); err != nil {
		t.Fatal(err)
	}
	if plan != "plus" || days < 29.9 || days > 30.1 {
		t.Fatalf("want plus for ~30 days, got %s / %.2f days", plan, days)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND kind='billing'`, uid).Scan(&n)
	if n != 1 {
		t.Fatalf("want 1 notification, got %d", n)
	}
}

func TestWebhookUnknownReferenceIsAcknowledged(t *testing.T) {
	s := testServer(t)
	t.Setenv("PAYSTACK_SECRET_KEY", "sk_test_x")
	body := `{"event":"charge.success","data":{"reference":"not-ours-123"}}`
	req := httptest.NewRequest(http.MethodPost, "/billing/webhook", strings.NewReader(body))
	req.Header.Set("x-paystack-signature", sign(body, "sk_test_x"))
	rec := httptest.NewRecorder()
	s.paystackWebhook(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d want 200", rec.Code)
	}
}

func TestPlusLifecycleRemindsOnceThenExpires(t *testing.T) {
	s := testServer(t)
	soon := newUser(t, s, "soon-"+t.Name()+"@example.test")
	later := newUser(t, s, "later-"+t.Name()+"@example.test")
	gone := newUser(t, s, "gone-"+t.Name()+"@example.test")
	admin := newUser(t, s, "grant-"+t.Name()+"@example.test") // admin-granted Plus has no expiry and must be left alone
	mustExec := func(q string, a ...any) {
		t.Helper()
		if _, err := s.db.Exec(q, a...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`UPDATE users SET plan='plus',daily_limit=10000,plus_expires_at=NOW()+INTERVAL '2 days' WHERE id=$1`, soon)
	mustExec(`UPDATE users SET plan='plus',daily_limit=10000,plus_expires_at=NOW()+INTERVAL '20 days' WHERE id=$1`, later)
	mustExec(`UPDATE users SET plan='plus',daily_limit=10000,plus_expires_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, gone)
	mustExec(`UPDATE users SET plan='plus',daily_limit=10000 WHERE id=$1`, admin)

	count := func(uid int64, title string) (n int) {
		s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND title=$2`, uid, title).Scan(&n)
		return
	}
	s.plusLifecycle(context.Background())
	s.plusLifecycle(context.Background()) // second run must not duplicate anything
	if got := count(soon, "Plus ends soon"); got != 1 {
		t.Errorf("soon: want exactly 1 reminder, got %d", got)
	}
	if got := count(later, "Plus ends soon"); got != 0 {
		t.Errorf("later: want no reminder yet, got %d", got)
	}
	if got := count(gone, "Plus has ended"); got != 1 {
		t.Errorf("gone: want 1 ended notice, got %d", got)
	}
	var plan string
	var limit int64
	var exp sql.NullTime
	s.db.QueryRow(`SELECT plan,daily_limit,plus_expires_at FROM users WHERE id=$1`, gone).Scan(&plan, &limit, &exp)
	if plan != "free" || limit != 100 || exp.Valid {
		t.Errorf("gone: want free/100/no expiry, got %s/%d/%v", plan, limit, exp)
	}
	s.db.QueryRow(`SELECT plan FROM users WHERE id=$1`, admin).Scan(&plan)
	if plan != "plus" {
		t.Errorf("admin-granted Plus must not be downgraded, got %s", plan)
	}
	// Renewing moves the expiry, which re-arms the reminder.
	mustExec(`UPDATE users SET plus_expires_at=NOW()+INTERVAL '1 day' WHERE id=$1`, soon)
	s.plusLifecycle(context.Background())
	if got := count(soon, "Plus ends soon"); got != 2 {
		t.Errorf("soon after renewal change: want 2 reminders total, got %d", got)
	}
}
