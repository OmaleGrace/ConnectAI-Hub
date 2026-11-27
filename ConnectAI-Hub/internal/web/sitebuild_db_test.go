package web

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"connectai-hub/internal/ai"
	"connectai-hub/internal/database"
)

// End-to-end tests of the website builder and the screenshot route. They drive the real
// HTTP handlers against a real PostgreSQL database (TEST_DATABASE_URL) with a scripted AI.

type scriptedGen struct {
	mu   sync.Mutex
	fn   func(prompt string, history []ai.Message) (string, error)
	seen []string
}

func (g *scriptedGen) GenerateWithImages(_ context.Context, _ string, history []ai.Message, prompt string, _ float64, _ int, _ []ai.Image) (string, error) {
	g.mu.Lock()
	g.seen = append(g.seen, prompt)
	fn := g.fn
	g.mu.Unlock()
	return fn(prompt, history)
}

type e2e struct {
	t      *testing.T
	s      *Server
	srv    *httptest.Server
	client *http.Client
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run database tests")
	}
	wd, _ := os.Getwd()
	if err := os.Chdir("../.."); err != nil { // templates/ and static/ are read from the working directory
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	db, err := database.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil { // migrations must be repeatable
		t.Fatalf("second migrate: %v", err)
	}
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &e2e{t: t, s: s, srv: srv, client: client}
}

// login creates a user plus session and returns the cookies to send.
func (e *e2e) login(email string) (int64, []*http.Cookie) {
	e.t.Helper()
	var id int64
	if err := e.s.db.QueryRow(`INSERT INTO users(name,email,password_hash) VALUES('Test User',$1,'x') RETURNING id`, email).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { e.s.db.Exec(`DELETE FROM users WHERE id=$1`, id) })
	rec := httptest.NewRecorder()
	if err := e.s.createSession(rec, id); err != nil {
		e.t.Fatal(err)
	}
	cookies := append(rec.Result().Cookies(), &http.Cookie{Name: "connectai_csrf", Value: "tok"})
	return id, cookies
}

func (e *e2e) do(method, path string, cookies []*http.Cookie, form url.Values) (*http.Response, string) {
	e.t.Helper()
	var body io.Reader
	if form != nil {
		form.Set("csrf_token", "tok")
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (e *e2e) waitStatus(cookies []*http.Cookie, id, want string) string {
	e.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		_, body := e.do("GET", "/builder/projects/"+id+"/status", cookies, nil)
		if strings.Contains(body, `"status":"`+want+`"`) {
			return body
		}
		time.Sleep(60 * time.Millisecond)
	}
	e.t.Fatalf("project %s never reached status %q", id, want)
	return ""
}

func projectID(t *testing.T, resp *http.Response) string {
	t.Helper()
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/builder/projects/") {
		t.Fatalf("expected redirect to the project page, got %d %q", resp.StatusCode, loc)
	}
	return strings.TrimPrefix(loc, "/builder/projects/")
}

func TestWebsiteBuilderEndToEnd(t *testing.T) {
	e := newE2E(t)
	_, alice := e.login(fmt.Sprintf("alice-%d@example.test", time.Now().UnixNano()))
	_, bob := e.login(fmt.Sprintf("bob-%d@example.test", time.Now().UnixNano()))

	release := make(chan struct{})
	gen := &scriptedGen{}
	e.s.siteGen = gen
	// The first build is slow, so the "already building" guard can be observed. The AI cuts
	// its first answer off mid-CSS, so the automatic continuation is exercised too.
	gen.fn = func(prompt string, history []ai.Message) (string, error) {
		if len(history) > 0 {
			return "h1{color:#a33}\n@@JS@@\ndocument.title='Bloom';\n@@END@@", nil
		}
		<-release
		return "@@TITLE@@\nBloom Florist\n@@HTML@@\n<header><h1>Bloom</h1></header><main id=\"about\"><p>Fresh flowers daily.</p></main>\n@@CSS@@\nbody{margin:0}\nh1{col", nil
	}

	resp, _ := e.do("POST", "/builder/generate", alice, url.Values{"prompt": {"A website for a flower shop called Bloom with an about section and contact details"}})
	id := projectID(t, resp)

	// While it builds: progress page, status endpoint, and no second build for the same user.
	_, page := e.do("GET", "/builder/projects/"+id, alice, nil)
	if !strings.Contains(page, "Building your website") || strings.Contains(page, "Download ZIP") {
		t.Errorf("building page wrong:\n%.600s", page)
	}
	resp2, _ := e.do("POST", "/builder/generate", alice, url.Values{"prompt": {"Another site for a bakery with a menu and opening hours"}})
	if loc := resp2.Header.Get("Location"); !strings.Contains(loc, "already+being+built") {
		t.Errorf("a second concurrent build must be refused, got %q", loc)
	}
	// Other people cannot see this project or its status.
	if r, _ := e.do("GET", "/builder/projects/"+id+"/status", bob, nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("bob saw alice's status: %d", r.StatusCode)
	}
	if r, _ := e.do("GET", "/builder/projects/"+id+"/download", bob, nil); r.StatusCode != http.StatusNotFound {
		t.Errorf("bob could download alice's site: %d", r.StatusCode)
	}

	close(release)
	e.waitStatus(alice, id, "ready")

	_, page = e.do("GET", "/builder/projects/"+id, alice, nil)
	for _, want := range []string{"Bloom Florist", "Download ZIP", "Fresh flowers daily.", "<iframe", `href="/builder/projects/` + id + `/download"`} {
		if !strings.Contains(page, want) {
			t.Errorf("ready page missing %q", want)
		}
	}
	var css string
	var used int
	e.s.db.QueryRow(`SELECT css FROM website_projects WHERE id=$1`, id).Scan(&css)
	e.s.db.QueryRow(`SELECT COUNT(*) FROM ai_usage WHERE request_type='website_build' AND module_slug='website' AND user_id=(SELECT user_id FROM website_projects WHERE id=$1)`, id).Scan(&used)
	if !strings.Contains(css, "h1{color:#a33}") || strings.Contains(css, "h1{col\n") {
		t.Errorf("continuation was not merged: %q", css)
	}
	if used != 1 {
		t.Errorf("a successful build should be counted once, got %d", used)
	}

	// The ZIP.
	resp, body := e.do("GET", "/builder/projects/"+id+"/download", alice, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/zip" || !strings.Contains(resp.Header.Get("Content-Disposition"), "bloom-florist-website.zip") {
		t.Fatalf("download headers: %d %v", resp.StatusCode, resp.Header)
	}
	zr, err := zip.NewReader(bytes.NewReader([]byte(body)), int64(len(body)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(b)
	}
	if !strings.Contains(files["index.html"], "Fresh flowers daily.") || !strings.Contains(files["css/style.css"], "h1{color:#a33}") || files["js/script.js"] == "" || files["README.md"] == "" {
		t.Errorf("zip contents wrong: %v", keys(files))
	}

	// An AI edit: the old version is preserved, the new one saved.
	gen.fn = func(prompt string, history []ai.Message) (string, error) {
		if !strings.Contains(prompt, "Fresh flowers daily.") || !strings.Contains(prompt, "add a pricing section") {
			return "", fmt.Errorf("edit prompt lacks the existing site or the request: %.200s", prompt)
		}
		return "@@TITLE@@\nBloom Florist\n@@HTML@@\n<header><h1>Bloom</h1></header><main><p>Fresh flowers daily.</p></main><section id=\"pricing\">Pricing</section>\n@@CSS@@\nbody{margin:0}\n@@JS@@\n@@END@@", nil
	}
	resp, _ = e.do("POST", "/builder/projects/"+id+"/edit", alice, url.Values{"instruction": {"please add a pricing section"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit: %d", resp.StatusCode)
	}
	e.waitStatus(alice, id, "ready")
	var html string
	var versions int
	e.s.db.QueryRow(`SELECT html FROM website_projects WHERE id=$1`, id).Scan(&html)
	e.s.db.QueryRow(`SELECT COUNT(*) FROM website_project_versions WHERE project_id=$1`, id).Scan(&versions)
	if !strings.Contains(html, "pricing") || versions != 1 {
		t.Errorf("edit not applied/versioned: versions=%d html=%q", versions, html)
	}

	// A failing edit leaves the site untouched and reports why.
	gen.fn = func(string, []ai.Message) (string, error) { return "", fmt.Errorf("provider down") }
	e.do("POST", "/builder/projects/"+id+"/edit", alice, url.Values{"instruction": {"make everything purple"}})
	e.waitStatus(alice, id, "ready")
	var berr string
	e.s.db.QueryRow(`SELECT html, build_error FROM website_projects WHERE id=$1`, id).Scan(&html, &berr)
	if !strings.Contains(html, "pricing") || berr == "" {
		t.Errorf("failed edit should keep the site and record an error: err=%q", berr)
	}
	_, page = e.do("GET", "/builder/projects/"+id, alice, nil)
	if !strings.Contains(page, "error-notice") || !strings.Contains(page, "Download ZIP") {
		t.Error("project page should show the edit error and still offer the ZIP")
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestWebsiteBuilderFailureAndRecovery(t *testing.T) {
	e := newE2E(t)
	uid, alice := e.login(fmt.Sprintf("carol-%d@example.test", time.Now().UnixNano()))
	e.s.siteGen = &scriptedGen{fn: func(string, []ai.Message) (string, error) { return "I cannot do that.", nil }}

	resp, _ := e.do("POST", "/builder/generate", alice, url.Values{"prompt": {"A site for my plumbing business with services and a phone number"}})
	id := projectID(t, resp)
	e.waitStatus(alice, id, "failed")
	_, page := e.do("GET", "/builder/projects/"+id, alice, nil)
	if !strings.Contains(page, "did not finish") || !strings.Contains(page, "Try again") || strings.Contains(page, "Download ZIP") {
		t.Errorf("failed page wrong:\n%.500s", page)
	}
	var n int
	e.s.db.QueryRow(`SELECT COUNT(*) FROM ai_usage WHERE user_id=$1 AND request_type='website_build'`, uid).Scan(&n)
	if n != 0 {
		t.Errorf("a failed build must not use up the daily allowance, counted %d", n)
	}
	// "Try again" works: the same brief builds once the AI behaves.
	e.s.siteGen = &scriptedGen{fn: func(string, []ai.Message) (string, error) { return fullSite, nil }}
	resp, _ = e.do("POST", "/builder/generate", alice, url.Values{"prompt": {"A site for my plumbing business with services and a phone number"}})
	id2 := projectID(t, resp)
	e.waitStatus(alice, id2, "ready")

	// The list page shows both states.
	_, list := e.do("GET", "/builder", alice, nil)
	if !strings.Contains(list, "Did not finish") || !strings.Contains(list, "Ready · ZIP available") {
		t.Error("builder list should show status pills")
	}
}

func TestInterruptedBuildsAreCleanedUp(t *testing.T) {
	e := newE2E(t)
	uid, alice := e.login(fmt.Sprintf("dave-%d@example.test", time.Now().UnixNano()))
	var stuck, editing int64
	e.s.db.QueryRow(`INSERT INTO website_projects(user_id,title,prompt,html,status,updated_at) VALUES($1,'t','p','','building',NOW()-INTERVAL '10 minutes') RETURNING id`, uid).Scan(&stuck)
	e.s.db.QueryRow(`INSERT INTO website_projects(user_id,title,prompt,html,status,updated_at) VALUES($1,'t2','p','<p>x</p>','editing',NOW()-INTERVAL '10 minutes') RETURNING id`, uid).Scan(&editing)
	body := e.waitStatus(alice, fmt.Sprint(stuck), "failed")
	if !strings.Contains(body, "interrupted") {
		t.Errorf("status body: %s", body)
	}
	e.waitStatus(alice, fmt.Sprint(editing), "ready")
	var html, msg string
	e.s.db.QueryRow(`SELECT html, build_error FROM website_projects WHERE id=$1`, editing).Scan(&html, &msg)
	if html != "<p>x</p>" || msg == "" {
		t.Errorf("interrupted edit must keep the site: %q %q", html, msg)
	}
	// A live build (fresh heartbeat) is left alone.
	var live int64
	e.s.db.QueryRow(`INSERT INTO website_projects(user_id,title,prompt,html,status) VALUES($1,'t3','p','','building') RETURNING id`, uid).Scan(&live)
	e.s.failInterruptedSiteJobs()
	var st string
	e.s.db.QueryRow(`SELECT status FROM website_projects WHERE id=$1`, live).Scan(&st)
	if st != "building" {
		t.Errorf("a running build was wrongly failed: %s", st)
	}
}

func TestScreenshotRouteIsPrivate(t *testing.T) {
	e := newE2E(t)
	uid, alice := e.login(fmt.Sprintf("erin-%d@example.test", time.Now().UnixNano()))
	_, mallory := e.login(fmt.Sprintf("mal-%d@example.test", time.Now().UnixNano()))
	id, err := e.s.saveScreenshot(uid, "study", "https://example.com/", []byte("\xff\xd8\xffjpegbytes"))
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/screenshots/%d", id)
	resp, body := e.do("GET", path, alice, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" || body != "\xff\xd8\xffjpegbytes" {
		t.Errorf("owner: %d %v", resp.StatusCode, resp.Header)
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") || resp.Header.Get("Cache-Control") != "private, max-age=86400" {
		t.Errorf("screenshots must be served privately and sandboxed: %v", resp.Header)
	}
	if resp, _ := e.do("GET", path, mallory, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("another user got status %d for a screenshot they do not own", resp.StatusCode)
	}
	if resp, _ := e.do("GET", path, nil, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous got status %d", resp.StatusCode)
	}
	if resp, _ := e.do("GET", path+"?download=1", alice, nil); !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Error("?download=1 should force a download")
	}
	// Per-user cap: only the newest screenshots are kept.
	for i := 0; i < maxScreenshotsPerUser+5; i++ {
		e.s.saveScreenshot(uid, "study", "https://example.com/", []byte("x"))
	}
	var n int
	e.s.db.QueryRow(`SELECT COUNT(*) FROM web_screenshots WHERE user_id=$1`, uid).Scan(&n)
	if n != maxScreenshotsPerUser {
		t.Errorf("kept %d screenshots, want %d", n, maxScreenshotsPerUser)
	}
	// Clearing a conversation removes its screenshots; deleting the account removes the rest.
	_, _ = e.s.db.Exec(`DELETE FROM web_screenshots WHERE user_id=$1 AND module_slug='study'`, uid)
	var _ sql.NullInt64
}
