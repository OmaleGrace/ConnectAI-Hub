package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Fingerprinted static assets: {{asset "css/app.css"}} renders
// /static/css/app.css?v=<hash of the file>. The hash changes only when the file
// does, so browsers keep it for a year yet every deploy shows up immediately,
// with no manual ?v= numbers to bump.
// ---------------------------------------------------------------------------

type assetEntry struct {
	mod  time.Time
	size int64
	ver  string
}

var (
	assetMu    sync.Mutex
	assetCache = map[string]assetEntry{}
)

func assetURL(path string) string {
	path = strings.TrimPrefix(path, "/")
	full := filepath.Join("static", filepath.FromSlash(path))
	st, err := os.Stat(full)
	if err != nil {
		return "/static/" + path
	}
	assetMu.Lock()
	defer assetMu.Unlock()
	if e, ok := assetCache[full]; ok && e.mod.Equal(st.ModTime()) && e.size == st.Size() {
		return "/static/" + path + "?v=" + e.ver
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "/static/" + path
	}
	sum := sha256.Sum256(data)
	ver := hex.EncodeToString(sum[:])[:10]
	assetCache[full] = assetEntry{mod: st.ModTime(), size: st.Size(), ver: ver}
	return "/static/" + path + "?v=" + ver
}

// noListingFS stops http.FileServer from showing directory listings.
type noListingFS struct{ fs http.FileSystem }

func (n noListingFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	if st, err := f.Stat(); err != nil || st.IsDir() {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

// ---------------------------------------------------------------------------
// Background cleanup: expired sessions and tokens used to pile up forever.
// ---------------------------------------------------------------------------

// StartJanitor prunes expired rows and stale rate-limit entries every hour
// until ctx is cancelled.
func (s *Server) StartJanitor(ctx context.Context) {
	go func() {
		s.sweep(ctx)
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.sweep(ctx)
			}
		}
	}()
}

func (s *Server) sweep(ctx context.Context) {
	for _, q := range []string{
		`DELETE FROM sessions WHERE expires_at < NOW() - INTERVAL '1 day'`,
		`DELETE FROM password_resets WHERE expires_at < NOW() - INTERVAL '1 day'`,
		`DELETE FROM phone_verification_codes WHERE expires_at < NOW()`,
		`DELETE FROM web_screenshots WHERE created_at < NOW() - INTERVAL '30 days'`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			log.Printf("connectai: cleanup failed: %v", err)
		}
	}
	s.reconcilePayments(ctx)
	s.plusLifecycle(ctx)
	now := time.Now()
	s.rateMu.Lock()
	for k, v := range s.rates {
		if now.After(v.Reset) {
			delete(s.rates, k)
		}
	}
	s.rateMu.Unlock()
}

// outboundClient is shared by calls to payment providers. http.DefaultClient
// has no timeout, so a slow provider could hang a request forever.
var outboundClient = &http.Client{Timeout: 20 * time.Second}
