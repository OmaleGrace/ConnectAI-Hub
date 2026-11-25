package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

// gzipPool reuses gzip writers so compression does not allocate on every request.
var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

// gzipResponseWriter compresses text-like responses on the fly. The decision is
// taken when the status line is written, once the handler has set Content-Type.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
}

func compressibleType(contentType string) bool {
	ct := strings.ToLower(contentType)
	switch {
	case strings.HasPrefix(ct, "text/"),
		strings.HasPrefix(ct, "application/json"),
		strings.HasPrefix(ct, "application/javascript"),
		strings.HasPrefix(ct, "application/xml"),
		strings.HasPrefix(ct, "image/svg+xml"),
		strings.Contains(ct, "+json"),
		strings.Contains(ct, "+xml"):
		return true
	}
	return false
}

func (g *gzipResponseWriter) WriteHeader(code int) {
	if g.wroteHeader {
		return
	}
	g.wroteHeader = true
	h := g.Header()
	if code != http.StatusNoContent && code != http.StatusNotModified && h.Get("Content-Encoding") == "" && compressibleType(h.Get("Content-Type")) {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		g.gz = gzipPool.Get().(*gzip.Writer)
		g.gz.Reset(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.gz != nil {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (g *gzipResponseWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func (g *gzipResponseWriter) finish() {
	if g.gz != nil {
		_ = g.gz.Close()
		gzipPool.Put(g.gz)
		g.gz = nil
	}
}

// serveWithCompression serves the request through next, gzip-compressing text
// responses for clients that accept it. Range and HEAD requests are untouched.
func serveWithCompression(next http.Handler, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead || r.Header.Get("Range") != "" || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		next.ServeHTTP(w, r)
		return
	}
	gw := &gzipResponseWriter{ResponseWriter: w}
	defer gw.finish()
	next.ServeHTTP(gw, r)
}

// staticHandler serves ./static with cache headers. Files requested with a ?v=
// version query are immutable; other files are revalidated. In development
// everything is revalidated so edits show up immediately.
func staticHandler() http.Handler {
	files := http.StripPrefix("/static/", http.FileServer(noListingFS{http.Dir("./static")}))
	production := os.Getenv("APP_ENV") == "production"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Query().Get("v") != "":
			// Fingerprinted by {{asset}}: the URL changes whenever the file does.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case !production:
			w.Header().Set("Cache-Control", "no-cache")
		default:
			w.Header().Set("Cache-Control", "public, max-age=3600, must-revalidate")
		}
		files.ServeHTTP(w, r)
	})
}
