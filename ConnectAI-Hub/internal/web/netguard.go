package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Server-side requests made for users (reading a pasted link, taking a
// screenshot) are a classic SSRF risk: a user could paste http://169.254.169.254/
// or http://localhost:5432/ and make the server talk to its own private network.
// Everything that fetches a user-supplied address goes through this file.
//
// The check happens where it cannot be bypassed: at connect time, on the IP the
// connection is really made to. That covers DNS tricks (a public name that
// resolves to a private address, or changes between lookups), redirects, and
// numeric host forms such as http://2130706433/.

// Only standard web ports are allowed, so a link cannot be used to probe other services.
// Tests replace these two variables.
var (
	allowedPorts = map[string]bool{"80": true, "443": true}
	ipAllowed    = isPublicIP
)

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, c := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32",
		"2001:db8::/32", "2002::/16", "fc00::/7", "fe80::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}()

// isPublicIP reports whether ip is a normal, publicly routable internet address.
func isPublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

var errBlockedAddress = errors.New("that address is not a public website")

// validatePublicURL checks the shape of a user-supplied URL. It does not resolve
// DNS; the dialer does that and checks the real IP.
func validatePublicURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 {
		return nil, errors.New("the link is empty or too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("the link is not a valid web address")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("only http and https links can be opened")
	}
	if u.User != nil {
		return nil, errors.New("links that contain a username or password are not allowed")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return nil, errors.New("the link has no website name")
	}
	if p := u.Port(); p != "" && !allowedPorts[p] {
		return nil, errors.New("only standard web ports (80 and 443) are allowed")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !ipAllowed(ip) {
			return nil, errBlockedAddress
		}
		return u, nil
	}
	// Odd numeric spellings of an IP (0x7f.0.0.1, 127.1, 017.0.0.1) are never real site names.
	if last := host[strings.LastIndex(host, ".")+1:]; last != "" && (strings.HasPrefix(last, "0x") || strings.Trim(last, "0123456789") == "") {
		return nil, errBlockedAddress
	}
	if host == "localhost" || !strings.Contains(host, ".") ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".lan") ||
		strings.HasSuffix(host, ".home.arpa") || strings.HasSuffix(host, ".intranet") {
		return nil, errBlockedAddress
	}
	return u, nil
}

// resolvePublic looks the host up and returns its addresses only if EVERY one is
// public. A mixed answer is rejected, because the attacker could be the one
// choosing which address we end up using.
func resolvePublic(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("the website name could not be found")
	}
	for _, ip := range ips {
		if !ipAllowed(ip) {
			return nil, errBlockedAddress
		}
	}
	return ips, nil
}

// safeDialContext connects to a host:port only when it is a public address on an
// allowed port. It dials the checked IP directly (never the name again), and a
// second check runs inside the socket, right before connect.
func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if !allowedPorts[port] {
		return nil, errors.New("only standard web ports (80 and 443) are allowed")
	}
	ips, err := resolvePublic(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve public host %q: %w", host, err)
	}
	d := net.Dialer{
		Timeout: 8 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			h, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(h)
			if err != nil || !ipAllowed(ip) {
				return errBlockedAddress
			}
			return nil
		},
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, fmt.Errorf("connect to public host %q: %w", host, lastErr)
	}
	return nil, errors.New("no public address available")
}

// newSafeTransport returns an HTTP transport that can only reach public sites.
// Proxy is nil on purpose: an environment proxy must never bypass these checks.
func newSafeTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           safeDialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
}

// newSafeClient follows at most 5 redirects, and re-validates every one.
func newSafeClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: newSafeTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("the site redirected too many times")
			}
			if _, err := validatePublicURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect blocked: %w", err)
			}
			return nil
		},
	}
}
