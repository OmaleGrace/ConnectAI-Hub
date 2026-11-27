package web

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The standalone "URL Search & Screenshot" page, driven through the real handlers.
func TestWebToolsPage(t *testing.T) {
	e := newE2E(t)
	_, c := e.login(fmt.Sprintf("wt-%d@example.test", time.Now().UnixNano()))

	if r, _ := e.do("GET", "/web-tools", nil, nil); r.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous users must be sent to log in, got %d", r.StatusCode)
	}
	_, page := e.do("GET", "/web-tools", c, nil)
	for _, want := range []string{"URL Search", `action="/web-tools/open"`, `name="url"`, "Capture screenshot"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q", want)
		}
	}

	// Private targets are refused with a clear message, not an error page.
	for _, target := range []string{"http://127.0.0.1/", "169.254.169.254/latest/meta-data/", "http://localhost:5432/"} {
		resp, body := e.do("POST", "/web-tools/open", c, url.Values{"url": {target}})
		if resp.StatusCode != 200 || !strings.Contains(body, "error-notice") || !strings.Contains(body, "not a public website") && !strings.Contains(body, "standard web ports") {
			t.Errorf("%s should be refused politely: %d", target, resp.StatusCode)
		}
	}

	// A real (local test) site is read and shown. Screenshot is requested but Chrome is absent.
	allowLoopbackForTest(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, sampleHTML)
	}))
	defer site.Close()
	allowedPorts[portOf(t, site)] = true
	chromeOnce.Do(func() {})
	old := chromePath
	chromePath = ""
	defer func() { chromePath = old }()
	resp, body := e.do("POST", "/web-tools/open", c, url.Values{"url": {site.URL}, "screenshot": {"on"}})
	if resp.StatusCode != 200 || !strings.Contains(body, "Acme &amp; Co") || !strings.Contains(body, "Welcome to Acme") || strings.Contains(body, "should-not-appear") {
		t.Errorf("page result wrong: %d", resp.StatusCode)
	}
	if !strings.Contains(body, "Screenshot:") || !strings.Contains(body, "not enabled on this server") {
		t.Error("a missing Chrome should be reported next to the page text, not break the page")
	}

	// With a (fake) Chrome the screenshot is saved, shown and downloadable.
	if runtime.GOOS != "windows" {
		dir := t.TempDir()
		var pngBuf bytes.Buffer
		png.Encode(&pngBuf, image.NewNRGBA(image.Rect(0, 0, 32, 32)))
		fixture := filepath.Join(dir, "f.png")
		os.WriteFile(fixture, pngBuf.Bytes(), 0o600)
		fake := filepath.Join(dir, "chrome")
		os.WriteFile(fake, []byte("#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in --screenshot=*) cp "+fixture+" \"${a#--screenshot=}\";; esac; done\n"), 0o755)
		chromePath = fake
		resp, body = e.do("POST", "/web-tools/open", c, url.Values{"url": {site.URL}, "screenshot": {"on"}, "viewport": {"mobile"}})
		if resp.StatusCode != 200 || !strings.Contains(body, "Download image") || !strings.Contains(body, `src="/screenshots/`) {
			t.Fatalf("screenshot not shown: %d\n%s", resp.StatusCode, firstN(body, 300))
		}
		start := strings.Index(body, `src="/screenshots/`) + len(`src="`)
		img := body[start : start+strings.Index(body[start:], `"`)]
		r2, data := e.do("GET", img, c, nil)
		if r2.StatusCode != 200 || r2.Header.Get("Content-Type") != "image/jpeg" || len(data) < 100 {
			t.Errorf("screenshot image: %d %s", r2.StatusCode, r2.Header.Get("Content-Type"))
		}
	}

	// The kill switch also covers this page.
	t.Setenv("WEB_ACCESS", "off")
	_, body = e.do("POST", "/web-tools/open", c, url.Values{"url": {site.URL}})
	if !strings.Contains(body, "turned off") {
		t.Error("WEB_ACCESS=off must disable the page")
	}
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
