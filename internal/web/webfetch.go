package web

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Reading web pages for the AI assistants. A link pasted in a message is opened
// by the server (never the browser), reduced to readable text and given to the
// model as clearly marked reference material.

const (
	maxURLsPerMessage = 3
	maxFetchBytes     = 2 << 20 // 2 MiB of HTML is plenty for text extraction
	maxPageChars      = 18000   // text kept per page (~4.5k tokens)
	fetchTimeout      = 15 * time.Second
)

// Page is the readable result of opening one link.
type Page struct {
	URL         string // what the user pasted
	FinalURL    string // where redirects ended
	Title       string
	Description string
	Headings    []string
	Links       []string
	Text        string
	Truncated   bool
	Thin        bool // almost no text: probably a JavaScript-only page
}

var webClient = newSafeClient(fetchTimeout)

// webAccessEnabled lets an administrator switch link reading and screenshots off.
func webAccessEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WEB_ACCESS"))) {
	case "off", "false", "0", "no", "disabled":
		return false
	}
	return true
}

var urlCandidate = regexp.MustCompile(`(?i)(?:https?://|\bwww\.)[^\s<>"'` + "`" + `]+`)

// extractURLs finds http(s) and www. links in text, in order, without duplicates.
// It returns at most max links and how many extra links were ignored.
func extractURLs(text string, max int) (urls []string, ignored int) {
	seen := map[string]bool{}
	for _, m := range urlCandidate.FindAllString(text, -1) {
		m = trimURLTail(m)
		if len(m) < 8 {
			continue
		}
		if strings.HasPrefix(strings.ToLower(m), "www.") {
			m = "https://" + m
		}
		key := strings.ToLower(m)
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(urls) >= max {
			ignored++
			continue
		}
		urls = append(urls, m)
	}
	return
}

// trimURLTail removes punctuation that belongs to the sentence, not the link.
func trimURLTail(s string) string {
	for {
		s = strings.TrimRight(s, ".,;:!?\"'*_")
		if strings.HasSuffix(s, ")") && strings.Count(s, "(") < strings.Count(s, ")") {
			s = s[:len(s)-1]
			continue
		}
		if strings.HasSuffix(s, "]") && strings.Count(s, "[") < strings.Count(s, "]") {
			s = s[:len(s)-1]
			continue
		}
		return s
	}
}

// fetchPage opens a public web page and returns readable text.
func fetchPage(ctx context.Context, raw string) (*Page, error) {
	u, err := validatePublicURL(raw)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.New("the link is not a valid web address")
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ConnectAIHubBot/1.0; +https://connectai-hub.example)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,application/json;q=0.8,*/*;q=0.3")
	req.Header.Set("Accept-Language", "en")
	resp, err := webClient.Do(req)
	if err != nil {
		return nil, friendlyFetchError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("the site answered with an error (HTTP %d); it may block automated readers or need a login", resp.StatusCode)
	}
	ctype := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	switch {
	case ctype == "application/pdf":
		return nil, errors.New("this link is a PDF; download it and upload it in the Documents assistant instead")
	case strings.HasPrefix(ctype, "image/"), strings.HasPrefix(ctype, "video/"), strings.HasPrefix(ctype, "audio/"),
		strings.HasPrefix(ctype, "font/"), ctype == "application/zip", ctype == "application/octet-stream":
		return nil, fmt.Errorf("this link is a %s file, which cannot be read as text", ctype)
	}
	raw8, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		return nil, friendlyFetchError(err)
	}
	truncated := len(raw8) > maxFetchBytes
	if truncated {
		raw8 = raw8[:maxFetchBytes]
	}
	body := decodeBody(raw8, resp.Header.Get("Content-Type"))
	page := &Page{URL: raw, FinalURL: resp.Request.URL.String(), Truncated: truncated}
	switch ctype {
	case "", "text/html", "application/xhtml+xml":
		extractHTML(body, resp.Request.URL, page)
	default:
		page.Text = strings.TrimSpace(body)
	}
	page.Text, page.Truncated = capText(page.Text, maxPageChars, page.Truncated)
	page.Thin = len(strings.TrimSpace(page.Text)) < 200
	return page, nil
}

func friendlyFetchError(err error) error {
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(msg, "Client.Timeout") || strings.Contains(msg, "timeout"):
		return errors.New("the site took too long to respond")
	case strings.Contains(msg, errBlockedAddress.Error()):
		return errBlockedAddress
	case strings.Contains(msg, "could not be found") || strings.Contains(msg, "no such host"):
		return errors.New("the website name could not be found")
	case strings.Contains(msg, "redirect"):
		return errors.New(msg[strings.Index(msg, "redirect"):])
	case strings.Contains(msg, "standard web ports"):
		return errors.New("only standard web ports (80 and 443) are allowed")
	case strings.Contains(msg, "certificate"):
		return errors.New("the site's security certificate is not valid")
	}
	return errors.New("the site could not be reached")
}

func capText(s string, max int, already bool) (string, bool) {
	if len(s) <= max {
		return s, already
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n…[page text shortened]", true
}

// decodeBody turns the bytes into valid UTF-8. UTF-8 and Latin-1/Windows-1252 are
// handled; anything else falls back to dropping invalid sequences.
func decodeBody(b []byte, contentType string) string {
	ct := strings.ToLower(contentType)
	head := strings.ToLower(string(b[:min(len(b), 2048)]))
	if (strings.Contains(ct, "iso-8859-1") || strings.Contains(ct, "windows-1252") ||
		strings.Contains(head, "charset=iso-8859-1") || strings.Contains(head, "charset=windows-1252")) && !utf8.Valid(b) {
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r)
	}
	return strings.ToValidUTF8(string(b), "")
}

// ---------------------------------------------------------------------------
// HTML -> text (standard library only)
// ---------------------------------------------------------------------------

var (
	reComment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	reTitle    = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reMeta     = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	reAttr     = regexp.MustCompile(`(?is)([a-z][a-z0-9:_-]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	reHeading  = regexp.MustCompile(`(?is)<h[1-3]\b[^>]*>(.*?)</h[1-3]>`)
	reAnchor   = regexp.MustCompile(`(?is)<a\s[^>]*?href\s*=\s*(?:"([^"]+)"|'([^']+)')[^>]*>(.*?)</a>`)
	reBlock    = regexp.MustCompile(`(?i)</?(?:p|div|br|li|tr|h[1-6]|section|article|header|footer|nav|ul|ol|table|blockquote|pre|form|main|aside|figure|figcaption|dd|dt|dl)\b[^>]*>`)
	reCell     = regexp.MustCompile(`(?i)<(?:td|th)\b[^>]*>`)
	reTag      = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces   = regexp.MustCompile(`[ \t\f\v\x{00a0}]+`)
	reNewlines = regexp.MustCompile(`\n{3,}`)
	dropTags   = func() []*regexp.Regexp {
		var out []*regexp.Regexp
		for _, t := range []string{"script", "style", "noscript", "svg", "template", "iframe", "head", "canvas", "object", "embed"} {
			out = append(out, regexp.MustCompile(`(?is)<`+t+`\b[^>]*>.*?</`+t+`\s*>`))
		}
		return out
	}()
)

func cleanInline(s string) string {
	s = reTag.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(reSpaces.ReplaceAllString(strings.ReplaceAll(s, "\n", " "), " "))
}

// extractHTML fills page with the title, description, headings, links and visible text.
func extractHTML(src string, base *url.URL, page *Page) {
	src = reComment.ReplaceAllString(src, "")
	if m := reTitle.FindStringSubmatch(src); m != nil {
		page.Title = cleanInline(m[1])
	}
	for _, tag := range reMeta.FindAllString(src, -1) {
		attrs := map[string]string{}
		for _, a := range reAttr.FindAllStringSubmatch(tag, -1) {
			v := a[2]
			if v == "" {
				v = a[3]
			}
			attrs[strings.ToLower(a[1])] = v
		}
		key := strings.ToLower(attrs["name"] + attrs["property"])
		if (key == "description" || key == "ogdescription" || key == "og:description") && page.Description == "" {
			page.Description = strings.TrimSpace(html.UnescapeString(attrs["content"]))
		}
		if page.Title == "" && (key == "og:title") {
			page.Title = strings.TrimSpace(html.UnescapeString(attrs["content"]))
		}
	}
	for _, t := range dropTags {
		src = t.ReplaceAllString(src, " ")
	}
	for _, m := range reHeading.FindAllStringSubmatch(src, 24) {
		if h := cleanInline(m[1]); h != "" && len(h) < 200 {
			page.Headings = append(page.Headings, h)
		}
	}
	seen := map[string]bool{}
	for _, m := range reAnchor.FindAllStringSubmatch(src, 400) {
		href := m[1]
		if href == "" {
			href = m[2]
		}
		label := cleanInline(m[3])
		if label == "" || len(label) > 80 || strings.HasPrefix(href, "#") ||
			strings.HasPrefix(strings.ToLower(href), "javascript:") || strings.HasPrefix(strings.ToLower(href), "mailto:") {
			continue
		}
		ref, err := url.Parse(html.UnescapeString(href))
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref)
		if abs.Scheme != "http" && abs.Scheme != "https" {
			continue
		}
		key := abs.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		page.Links = append(page.Links, label+" → "+key)
		if len(page.Links) >= 20 {
			break
		}
	}
	src = reBlock.ReplaceAllString(src, "\n")
	src = reCell.ReplaceAllString(src, " | ")
	src = reTag.ReplaceAllString(src, "")
	src = html.UnescapeString(src)
	var lines []string
	last := ""
	for _, ln := range strings.Split(src, "\n") {
		ln = strings.TrimSpace(reSpaces.ReplaceAllString(ln, " "))
		if ln == "" || ln == last {
			if ln == "" && len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			continue
		}
		lines = append(lines, ln)
		last = ln
	}
	page.Text = strings.TrimSpace(reNewlines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
