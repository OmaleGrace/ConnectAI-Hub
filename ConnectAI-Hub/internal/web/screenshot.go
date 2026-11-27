package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Website screenshots, taken by a headless Chrome/Chromium on the server.
//
// Safety model (see also netguard.go and egressproxy.go):
//   - the page address is validated, then every connection Chrome makes goes
//     through a local proxy that refuses non-public addresses;
//   - Chrome gets a throw-away profile and an almost empty environment (no API
//     keys, no database URL);
//   - at most two captures run at once, each is time limited and its whole
//     process group is killed on timeout;
//   - the result is decoded and re-encoded as a JPEG, so only pixels are kept.
//
// If no Chrome is installed the feature is simply off and link reading still works.

const (
	maxScreenshotsPerMessage = 2
	screenshotTimeout        = 28 * time.Second
	maxScreenshotsPerUser    = 60
	maxScreenshotPNGBytes    = 12 << 20
)

var (
	screenshotSlots = make(chan struct{}, 2)
	chromeOnce      sync.Once
	chromePath      string
)

// chromeBinary finds Chrome/Chromium: CHROME_BIN first, then common names.
func chromeBinary() string {
	chromeOnce.Do(func() {
		if v := strings.TrimSpace(os.Getenv("CHROME_BIN")); v != "" {
			if p, err := exec.LookPath(v); err == nil {
				chromePath = p
			}
			return
		}
		for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome", "chrome.exe"} {
			if p, err := exec.LookPath(name); err == nil {
				chromePath = p
				return
			}
		}
	})
	return chromePath
}

func screenshotsAvailable() bool { return webAccessEnabled() && chromeBinary() != "" }

type shotOptions struct {
	Mobile   bool // phone-sized viewport
	FullPage bool // taller capture (about three screens)
}

// chromeArgs builds the command line. It is separate so tests can inspect it.
func chromeArgs(dir, proxyAddr, target, out string, opt shotOptions) []string {
	width, height := 1280, 800
	if opt.Mobile {
		width, height = 390, 844
	}
	if opt.FullPage {
		height = 2400
		if opt.Mobile {
			height = 2600
		}
	}
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--hide-scrollbars",
		"--mute-audio",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-extensions",
		"--disable-sync",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-quic",
		"--disable-features=Translate,MediaRouter,OptimizationHints,InterestFeedContentSuggestions",
		"--force-webrtc-ip-handling-policy=disable_non_proxied_udp",
		"--user-data-dir=" + filepath.Join(dir, "profile"),
		// Every request, including loopback, must go through our filtering proxy,
		// and Chrome may not resolve names on its own.
		"--proxy-server=http=" + proxyAddr + ";https=" + proxyAddr,
		"--proxy-bypass-list=<-loopback>",
		"--disable-background-networking",
		"--window-size=" + strconv.Itoa(width) + "," + strconv.Itoa(height),
		"--force-device-scale-factor=1",
		"--virtual-time-budget=7000",
		"--run-all-compositor-stages-before-draw",
		"--screenshot=" + out,
	}
	if opt.Mobile {
		args = append(args, "--user-agent=Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Mobile Safari/537.36")
	}
	if strings.EqualFold(os.Getenv("CHROME_NO_SANDBOX"), "true") || os.Getenv("CHROME_NO_SANDBOX") == "1" {
		args = append(args, "--no-sandbox")
	}
	return append(args, target)
}

// captureScreenshot returns a JPEG of the page.
func captureScreenshot(ctx context.Context, rawURL string, opt shotOptions) ([]byte, error) {
	if !webAccessEnabled() {
		return nil, errors.New("web access is turned off on this server")
	}
	bin := chromeBinary()
	if bin == "" {
		return nil, errors.New("screenshots are not enabled on this server (no Chrome or Chromium is installed)")
	}
	u, err := validatePublicURL(rawURL)
	if err != nil {
		return nil, err
	}
	// Fail early and clearly if the name points at a private address.
	if _, err := resolvePublic(ctx, u.Hostname()); err != nil {
		return nil, err
	}
	proxyAddr, err := screenshotProxy.start()
	if err != nil {
		return nil, errors.New("the screenshot service could not start")
	}

	select {
	case screenshotSlots <- struct{}{}:
		defer func() { <-screenshotSlots }()
	case <-ctx.Done():
		return nil, errors.New("the screenshot service is busy; please try again in a moment")
	}

	dir, err := os.MkdirTemp("", "connectai-shot-*")
	if err != nil {
		return nil, errors.New("the screenshot service could not start")
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "shot.png")

	runCtx, cancel := context.WithTimeout(ctx, screenshotTimeout)
	defer cancel()
	args := chromeArgs(dir, proxyAddr, u.String(), out, opt)
	log.Printf("connectai: starting screenshot browser=%q proxy=%q target_host=%q", bin, proxyAddr, u.Host)
	cmd := exec.CommandContext(runCtx, bin, args...)
	var chromeStderr bytes.Buffer
	cmd.Stderr = &chromeStderr
	// A minimal environment: Chrome must never see the app's secrets.
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + dir, "LANG=C.UTF-8"}
	setProcessGroup(cmd)
	cmd.Cancel = func() error { killProcessTree(cmd); return nil }
	cmd.WaitDelay = 2 * time.Second
	runErr := cmd.Run()
	if diagnostic := strings.TrimSpace(chromeStderr.String()); diagnostic != "" {
		log.Printf("connectai: Chrome diagnostics: %s", diagnostic)
	}

	info, statErr := os.Stat(out)
	if statErr != nil || info.Size() == 0 {
		if runCtx.Err() != nil {
			return nil, errors.New("the page took too long to load for a screenshot")
		}
		log.Printf("connectai: screenshot failed: run_error=%v output_stat_error=%v", runErr, statErr)
		return nil, errors.New("the screenshot could not be captured")
	}
	if info.Size() > maxScreenshotPNGBytes {
		return nil, errors.New("the screenshot was too large")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, errors.New("the screenshot could not be read")
	}
	return pngToJPEG(data)
}

// pngToJPEG decodes the capture and re-encodes it as a compact JPEG on white.
func pngToJPEG(data []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("the screenshot was not a valid image")
	}
	b := img.Bounds()
	flat := image.NewRGBA(b)
	draw.Draw(flat, b, image.White, image.Point{}, draw.Src)
	draw.Draw(flat, b, img, b.Min, draw.Over)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 80}); err != nil {
		return nil, errors.New("the screenshot could not be compressed")
	}
	return buf.Bytes(), nil
}

// saveScreenshot stores the JPEG for the user and returns its id.
func (s *Server) saveScreenshot(userID int64, module, pageURL string, jpg []byte) (int64, error) {
	var id int64
	err := s.db.QueryRow(`INSERT INTO web_screenshots(user_id,module_slug,url,mime,data) VALUES($1,$2,$3,'image/jpeg',$4) RETURNING id`,
		userID, module, truncateRunes(pageURL, 500), jpg).Scan(&id)
	if err != nil {
		return 0, err
	}
	// Keep storage bounded: only the newest screenshots per user are kept.
	_, _ = s.db.Exec(`DELETE FROM web_screenshots WHERE user_id=$1 AND id NOT IN (SELECT id FROM web_screenshots WHERE user_id=$1 ORDER BY id DESC LIMIT $2)`, userID, maxScreenshotsPerUser)
	return id, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// serveScreenshot returns one of the signed-in user's own screenshots.
func (s *Server) serveScreenshot(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Error(w, "Please log in.", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	var mime string
	var data []byte
	if err := s.db.QueryRow(`SELECT mime,data FROM web_screenshots WHERE id=$1 AND user_id=$2`, id, u.ID).Scan(&mime, &data); err != nil {
		http.NotFound(w, r)
		return
	}
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("Content-Disposition", fmt.Sprintf(`%s; filename="screenshot-%d.jpg"`, disposition, id))
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// webToolsPage exposes URL reading and screenshot capture as a standalone tool,
// rather than requiring users to discover the capability inside an AI chat.
func (s *Server) webToolsPage(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	var unread int64
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND is_read=FALSE`, u.ID).Scan(&unread)
	s.render(w, "web-tools.html", PageData{
		Title: "URL Search & Screenshot", Active: "web-tools", User: u,
		CSRF: s.csrf(w, r), UnreadNotifications: unread,
		ScreenshotReady: screenshotsAvailable(),
	})
}

func (s *Server) openWebToolURL(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil || !s.validCSRF(r) {
		http.Error(w, "Your form expired. Refresh and try again.", http.StatusForbidden)
		return
	}
	raw := strings.TrimSpace(r.FormValue("url"))
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	if !s.allowRate(fmt.Sprintf("web-tool:%d", u.ID), 12, 10*time.Minute) {
		s.renderWebToolError(w, r, u, raw, "Please wait a few minutes before opening another URL.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	page, err := fetchPage(ctx, raw)
	if err != nil {
		s.renderWebToolError(w, r, u, raw, err.Error())
		return
	}
	data := PageData{
		Title: "URL Search & Screenshot", Active: "web-tools", User: u,
		CSRF: s.csrf(w, r), WebURL: raw, WebPage: page,
		ScreenshotReady: screenshotsAvailable(),
	}
	if r.FormValue("screenshot") == "on" {
		opt := shotOptions{Mobile: r.FormValue("viewport") == "mobile", FullPage: r.FormValue("full_page") == "on"}
		shotCtx, shotCancel := context.WithTimeout(r.Context(), screenshotTimeout+5*time.Second)
		jpg, shotErr := captureScreenshot(shotCtx, raw, opt)
		shotCancel()
		if shotErr != nil {
			data.ScreenshotError = shotErr.Error()
		} else if id, saveErr := s.saveScreenshot(u.ID, "web-tools", raw, jpg); saveErr != nil {
			data.ScreenshotError = "The screenshot was captured but could not be saved."
		} else {
			data.ScreenshotID = id
		}
	}
	s.render(w, "web-tools.html", data)
}

func (s *Server) renderWebToolError(w http.ResponseWriter, r *http.Request, u *User, raw, message string) {
	var unread int64
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND is_read=FALSE`, u.ID).Scan(&unread)
	s.render(w, "web-tools.html", PageData{
		Title: "URL Search & Screenshot", Active: "web-tools", User: u,
		CSRF: s.csrf(w, r), WebURL: raw, WebError: message,
		UnreadNotifications: unread, ScreenshotReady: screenshotsAvailable(),
	})
}
