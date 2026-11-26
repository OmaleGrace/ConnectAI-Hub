package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"
)

// This file holds the payment logic shared by the three ways a Paystack
// payment can reach us: the browser callback, Paystack's webhook, and the
// hourly reconciliation job. All three call the same two functions
// (verifyPaystackTransaction and activatePlusPayment), so a payment can only
// ever be activated once and only after Paystack itself confirms it.

const (
	plusDays            = 30
	plusReminderDays    = 3
	maxWebhookBodyBytes = 1 << 20
)

var (
	errPaystackUnavailable = errors.New("paystack unavailable")
	errPaystackRejected    = errors.New("paystack rejected the lookup")
)

// verifyPaystackTransaction asks Paystack for the real state of a payment.
// errPaystackUnavailable means "try again later" (network, config, 5xx);
// errPaystackRejected means Paystack answered but does not know the payment.
func (s *Server) verifyPaystackTransaction(ctx context.Context, reference string) (*paystackVerifyResponse, error) {
	secret := strings.TrimSpace(os.Getenv("PAYSTACK_SECRET_KEY"))
	if secret == "" {
		return nil, errPaystackUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.paystack.co/transaction/verify/"+reference, nil)
	if err != nil {
		return nil, errPaystackUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := outboundClient.Do(req)
	if err != nil {
		return nil, errPaystackUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return nil, errPaystackUnavailable
	}
	var v paystackVerifyResponse
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&v) != nil {
		return nil, errPaystackRejected
	}
	return &v, nil
}

// paymentConfirmed is true only when Paystack reports a successful payment
// for exactly this reference, amount (kobo) and currency.
func paymentConfirmed(v *paystackVerifyResponse, reference string, amountKobo int64) bool {
	return v != nil && v.Status && v.Data.Status == "success" && v.Data.Reference == reference &&
		v.Data.Amount == amountKobo && v.Data.Currency == "NGN"
}

// activatePlusPayment marks a pending payment as paid and extends Plus by 30
// days, in one transaction. It is safe to call many times for the same
// reference: only the call that flips pending->paid grants access and sends
// the notification, so a webhook and a callback arriving together cannot
// double-grant. It does NOT verify with Paystack; callers must do that first.
func (s *Server) activatePlusPayment(ctx context.Context, reference string) (bool, error) {
	var owner, amount int64
	if err := s.db.QueryRowContext(ctx, `SELECT user_id,amount FROM plan_payments WHERE reference=$1`, reference).Scan(&owner, &amount); err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE plan_payments SET status='paid',paid_at=NOW() WHERE reference=$1 AND status<>'paid'`, reference)
	if err != nil {
		return false, err
	}
	changed, _ := res.RowsAffected()
	if changed == 1 {
		if _, err = tx.ExecContext(ctx, `UPDATE users SET plan='plus',daily_limit=10000,plus_expires_at=GREATEST(COALESCE(plus_expires_at,NOW()),NOW())+make_interval(days => $2) WHERE id=$1`, owner, plusDays); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	if changed != 1 {
		return false, nil
	}
	var email, name string
	_ = s.db.QueryRowContext(ctx, `SELECT email,name FROM users WHERE id=$1`, owner).Scan(&email, &name)
	_, _ = s.db.ExecContext(ctx, `INSERT INTO notifications(user_id,title,message,kind) VALUES($1,$2,$3,'billing')`, owner, "Payment successful", fmt.Sprintf("Your ConnectAI Plus payment was verified. Plus access is now active for %d days.", plusDays))
	if err := s.sendPaymentSuccessEmail(name, email, reference, amount/100); err != nil {
		log.Printf("connectai: payment confirmation email could not be sent to %s: %v", email, err)
	}
	return true, nil
}

// validPaystackSignature checks the x-paystack-signature header: an
// HMAC-SHA512 of the raw request body keyed with the secret key.
func validPaystackSignature(body []byte, signature, secret string) bool {
	if secret == "" || signature == "" {
		return false
	}
	got, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(got, mac.Sum(nil))
}

// paystackWebhook receives server-to-server events from Paystack. This is what
// grants Plus when a customer pays but never returns to the site (closed tab,
// lost connection). Set the webhook URL in the Paystack dashboard to
// APP_BASE_URL + "/billing/webhook".
func (s *Server) paystackWebhook(w http.ResponseWriter, r *http.Request) {
	secret := strings.TrimSpace(os.Getenv("PAYSTACK_SECRET_KEY"))
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodyBytes))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !validPaystackSignature(body, r.Header.Get("x-paystack-signature"), secret) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	var event struct {
		Event string `json:"event"`
		Data  struct {
			Reference string `json:"reference"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &event) != nil || event.Event != "charge.success" || event.Data.Reference == "" || len(event.Data.Reference) > 100 {
		w.WriteHeader(http.StatusOK) // an event we do not act on: acknowledge so Paystack stops retrying
		return
	}
	if err := s.settlePayment(r.Context(), event.Data.Reference); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusOK) // not one of ours
			return
		}
		log.Printf("connectai: webhook for %s failed: %v", event.Data.Reference, err)
		http.Error(w, "try again", http.StatusInternalServerError) // Paystack retries on non-2xx
		return
	}
	w.WriteHeader(http.StatusOK)
}

// settlePayment verifies a known reference with Paystack and activates it when
// confirmed. Returns sql.ErrNoRows for unknown references.
func (s *Server) settlePayment(ctx context.Context, reference string) error {
	var amount int64
	var status string
	if err := s.db.QueryRowContext(ctx, `SELECT amount,status FROM plan_payments WHERE reference=$1`, reference).Scan(&amount, &status); err != nil {
		return err
	}
	if status == "paid" {
		return nil
	}
	tx, err := s.verifyPaystackTransaction(ctx, reference)
	if errors.Is(err, errPaystackRejected) {
		return nil // Paystack does not recognise it; nothing to grant
	}
	if err != nil {
		return err
	}
	if !paymentConfirmed(tx, reference, amount) {
		return nil
	}
	_, err = s.activatePlusPayment(ctx, reference)
	return err
}

// reconcilePayments is the safety net behind the webhook: every hour it
// re-checks payments that were started more than 15 minutes ago and are still
// pending, activates the ones Paystack says succeeded, and marks old
// abandoned ones as failed so the table does not fill with dead rows.
func (s *Server) reconcilePayments(ctx context.Context) {
	if strings.TrimSpace(os.Getenv("PAYSTACK_SECRET_KEY")) == "" {
		return
	}
	rows, err := s.db.QueryContext(ctx, `SELECT reference,created_at FROM plan_payments
		WHERE status='pending' AND created_at < NOW() - INTERVAL '15 minutes' AND created_at > NOW() - INTERVAL '7 days'
		ORDER BY created_at LIMIT 50`)
	if err != nil {
		log.Printf("connectai: reconcile query failed: %v", err)
		return
	}
	type pending struct {
		ref     string
		created time.Time
	}
	var list []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.ref, &p.created) == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	for _, p := range list {
		if ctx.Err() != nil {
			return
		}
		if err := s.settlePayment(ctx, p.ref); err != nil {
			log.Printf("connectai: reconcile %s: %v", p.ref, err)
			continue
		}
		if time.Since(p.created) > 24*time.Hour {
			_, _ = s.db.ExecContext(ctx, `UPDATE plan_payments SET status='failed' WHERE reference=$1 AND status='pending'`, p.ref)
		}
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE plan_payments SET status='failed' WHERE status='pending' AND created_at <= NOW() - INTERVAL '7 days'`)
}

// ---------------------------------------------------------------------------
// Plus lifecycle: expiry reminders and proactive downgrade
// ---------------------------------------------------------------------------

// plusLifecycle runs from the hourly sweep. It (1) tells Plus members their pass
// ends soon, once per expiry date, and (2) moves expired passes back to Free and
// says so, instead of waiting for the user's next page load.
func (s *Server) plusLifecycle(ctx context.Context) {
	// The UPDATE ... RETURNING claims each user atomically, so two app
	// instances (or two sweeps) can never send the same reminder twice.
	rows, err := s.db.QueryContext(ctx, `UPDATE users SET plus_reminder_for=plus_expires_at
		WHERE plan='plus' AND plus_expires_at > NOW() AND plus_expires_at <= NOW() + make_interval(days => $1)
		  AND plus_reminder_for IS DISTINCT FROM plus_expires_at
		RETURNING id,name,email,plus_expires_at`, plusReminderDays)
	if err != nil {
		log.Printf("connectai: reminder query failed: %v", err)
	} else {
		type who struct {
			id          int64
			name, email string
			expires     time.Time
		}
		var due []who
		for rows.Next() {
			var x who
			if rows.Scan(&x.id, &x.name, &x.email, &x.expires) == nil {
				due = append(due, x)
			}
		}
		rows.Close()
		for _, x := range due {
			msg := fmt.Sprintf("Your ConnectAI Plus pass ends on %s. Renew from Plans & billing to keep your higher daily allowance.", x.expires.UTC().Format("2 January 2006"))
			_, _ = s.db.ExecContext(ctx, `INSERT INTO notifications(user_id,title,message,kind) VALUES($1,'Plus ends soon',$2,'billing')`, x.id, msg)
			if s.emailsEnabled(x.id) {
				if err := sendPlainEmail(x.email, "Your ConnectAI Plus pass ends soon", "Hello "+cleanHeader(x.name)+",\r\n\r\n"+msg+"\r\n\r\nThe ConnectAI Hub Team\r\n"); err != nil {
					log.Printf("connectai: reminder email to user %d not sent: %v", x.id, err)
				}
			}
		}
	}
	rows, err = s.db.QueryContext(ctx, `UPDATE users SET plan='free',daily_limit=100,plus_expires_at=NULL,plus_reminder_for=NULL
		WHERE plan='plus' AND plus_expires_at IS NOT NULL AND plus_expires_at <= NOW() RETURNING id`)
	if err != nil {
		log.Printf("connectai: expiry query failed: %v", err)
		return
	}
	var expired []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			expired = append(expired, id)
		}
	}
	rows.Close()
	for _, id := range expired {
		_, _ = s.db.ExecContext(ctx, `INSERT INTO notifications(user_id,title,message,kind) VALUES($1,'Plus has ended','Your ConnectAI Plus pass has ended and your account is back on the Free allowance. You can renew any time from Plans & billing.','billing')`, id)
	}
}

func (s *Server) emailsEnabled(userID int64) bool {
	on := true // default when the user never saved a preference
	_ = s.db.QueryRow(`SELECT email_notifications FROM notification_preferences WHERE user_id=$1`, userID).Scan(&on)
	return on
}

func cleanHeader(v string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(v) }

// sendPlainEmail sends a text email through the configured SMTP account.
func sendPlainEmail(to, subject, body string) error {
	host, port, user, pass := os.Getenv("SMTP_HOST"), os.Getenv("SMTP_PORT"), os.Getenv("SMTP_USER"), os.Getenv("SMTP_PASSWORD")
	if host == "" || port == "" || user == "" || pass == "" {
		return fmt.Errorf("email delivery is not configured")
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = user
	}
	msg := []byte("From: ConnectAI Hub <" + cleanHeader(from) + ">\r\nTo: " + cleanHeader(to) + "\r\nSubject: " + cleanHeader(subject) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body)
	return smtp.SendMail(host+":"+port, smtp.PlainAuth("", user, pass, host), from, []string{to}, msg)
}
