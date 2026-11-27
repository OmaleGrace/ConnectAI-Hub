package web

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// egressProxy is a tiny local HTTP/HTTPS proxy that headless Chrome is forced to
// use. Chrome loads untrusted pages, their redirects, images, scripts and frames;
// each of those connections passes through safeDialContext, so a page cannot
// make the browser reach localhost, the cloud metadata address or the private
// network, no matter how it tries.
type egressProxy struct {
	once sync.Once
	addr string
	err  error
	tr   *http.Transport
}

var screenshotProxy egressProxy

// start begins listening on a random loopback port (once) and returns host:port.
func (p *egressProxy) start() (string, error) {
	p.once.Do(func() {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			p.err = err
			return
		}
		p.addr = l.Addr().String()
		p.tr = newSafeTransport()
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				go p.handle(c)
			}
		}()
	})
	return p.addr, p.err
}

const (
	proxyConnLifetime = 45 * time.Second
	proxyMaxDownBytes = 40 << 20
)

func (p *egressProxy) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(proxyConnLifetime))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		log.Printf("connectai: screenshot proxy could not read browser request: %v", err)
		return
	}
	proxyDebugf("screenshot proxy received %s request for %s", req.Method, req.Host)
	if req.Method == http.MethodConnect {
		p.tunnel(c, br, req)
		return
	}
	p.forward(c, req)
}

// tunnel handles HTTPS: Chrome asks for host:port, we connect (publicly only), then
// copy bytes both ways. TLS stays end to end between Chrome and the site.
func (p *egressProxy) tunnel(c net.Conn, br *bufio.Reader, req *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	up, err := safeDialContext(ctx, "tcp", req.Host)
	if err != nil {
		log.Printf("connectai: screenshot proxy CONNECT to %q failed: %v", req.Host, err)
		// 403 = refused by the safety rules; 502 = allowed but could not be reached (DNS, timeout…).
		if strings.Contains(err.Error(), errBlockedAddress.Error()) || strings.Contains(err.Error(), "standard web ports") {
			_, _ = io.WriteString(c, "HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n")
		} else {
			_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n")
		}
		return
	}
	proxyDebugf("screenshot proxy CONNECT to %q established", req.Host)
	defer up.Close()
	_ = up.SetDeadline(time.Now().Add(proxyConnLifetime))
	if _, err := io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if n := br.Buffered(); n > 0 {
		buf, _ := br.Peek(n)
		if _, err := up.Write(buf); err != nil {
			return
		}
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, c); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, io.LimitReader(up, proxyMaxDownBytes)); done <- struct{}{} }()
	<-done
}

// forward handles plain http:// requests (absolute-form URLs).
func (p *egressProxy) forward(c net.Conn, req *http.Request) {
	if req.URL == nil || req.URL.Host == "" {
		_, _ = io.WriteString(c, "HTTP/1.1 400 Bad Request\r\nConnection: close\r\n\r\n")
		return
	}
	if _, err := validatePublicURL(req.URL.String()); err != nil {
		_, _ = io.WriteString(c, "HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n")
		return
	}
	req.RequestURI = ""
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resp, err := p.tr.RoundTrip(req.WithContext(ctx))
	if err != nil {
		log.Printf("connectai: screenshot proxy HTTP request to %q failed: %v", req.URL.Host, err)
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n")
		return
	}
	defer resp.Body.Close()
	resp.Body = io.NopCloser(io.LimitReader(resp.Body, proxyMaxDownBytes))
	resp.Close = true
	_ = resp.Write(c)
}

// proxyDebugf logs every proxied request. It is off by default (it would put the sites people
// look at into your logs); set WEB_DEBUG=on while diagnosing screenshot problems.
func proxyDebugf(format string, args ...any) {
	if strings.EqualFold(os.Getenv("WEB_DEBUG"), "on") {
		log.Printf("connectai: "+format, args...)
	}
}
