package web

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"connectai-hub/internal/ai"
)

const fullSite = "@@TITLE@@\nBloom Florist\n@@HTML@@\n<header><h1>Bloom</h1></header>\n<main id=\"about\"><p>We sell flowers.</p></main>\n@@CSS@@\nbody{margin:0;font-family:system-ui}\nh1{color:#a33}\n@@JS@@\ndocument.title='Bloom';\n@@END@@"

func TestParseSiteFilesComplete(t *testing.T) {
	f, complete, _ := parseSiteFiles(fullSite)
	if !complete || f.Title != "Bloom Florist" || !strings.Contains(f.HTML, "We sell flowers.") || !strings.Contains(f.CSS, "h1{color:#a33}") || !strings.Contains(f.JS, "Bloom") {
		t.Fatalf("bad parse: complete=%v %+v", complete, f)
	}
	// Models sometimes wrap sections in fences or add CRLF.
	fenced := strings.ReplaceAll("@@TITLE@@\nT\n@@HTML@@\n```html\n<p>x</p>\n```\n@@CSS@@\n```css\np{}\n```\n@@JS@@\n@@END@@", "\n", "\r\n")
	f2, c2, _ := parseSiteFiles(fenced)
	if !c2 || f2.HTML != "<p>x</p>" || f2.CSS != "p{}" || f2.JS != "" {
		t.Errorf("fenced parse failed: %v %+v", c2, f2)
	}
	// JSON answers (old style) are still accepted.
	f3, c3, _ := parseSiteFiles("```json\n{\"title\":\"J\",\"html\":\"<p>j</p>\",\"css\":\"p{}\",\"js\":\"\"}\n```")
	if !c3 || f3.Title != "J" || f3.HTML != "<p>j</p>" {
		t.Errorf("json fallback failed: %v %+v", c3, f3)
	}
	if _, c4, _ := parseSiteFiles("I'm sorry, I can't help with that."); c4 {
		t.Error("prose must not parse as a site")
	}
}

func TestSalvage(t *testing.T) {
	f, c, seen := parseSiteFiles("@@TITLE@@\nT\n@@HTML@@\n<p>ok</p>\n@@CSS@@\np{color:red}\nh2{color:bl")
	if c {
		t.Fatal("must be incomplete")
	}
	sv, ok := salvage(f, seen)
	if !ok || sv.HTML != "<p>ok</p>" || sv.CSS != "p{color:red}" || sv.JS != "" {
		t.Errorf("salvage: ok=%v %+v", ok, sv)
	}
	// Truncated inside the HTML: cannot be saved.
	f, _, seen = parseSiteFiles("@@TITLE@@\nT\n@@HTML@@\n<p>half")
	if _, ok := salvage(f, seen); ok {
		t.Error("a half-written page must not be salvaged")
	}
}

type fakeGen struct {
	mu      sync.Mutex
	answers []string
	calls   []string // prompts received
	hist    [][]ai.Message
	err     error
}

func (g *fakeGen) GenerateWithImages(_ context.Context, _ string, history []ai.Message, prompt string, _ float64, _ int, images []ai.Image) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, prompt)
	g.hist = append(g.hist, history)
	if g.err != nil {
		return "", g.err
	}
	if len(g.answers) == 0 {
		return "", errors.New("no more answers")
	}
	a := g.answers[0]
	g.answers = g.answers[1:]
	return a, nil
}

func TestGenerateSiteContinuesTruncatedAnswers(t *testing.T) {
	// The first answer stops in the middle of a CSS line; the continuation repeats that
	// line (as it was told to) and finishes.
	first := "@@TITLE@@\nBloom\n@@HTML@@\n<main><p>Hi there</p></main>\n@@CSS@@\nbody{margin:0}\nmain{padding:2"
	second := "main{padding:24px}\n.btn{color:#fff}\n@@JS@@\n@@END@@"
	g := &fakeGen{answers: []string{first, second}}
	var stages []string
	f, err := generateSite(context.Background(), g, "ins", "a florist", nil, func(s string) { stages = append(stages, s) })
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Bloom" || !strings.Contains(f.CSS, "main{padding:24px}") || strings.Contains(f.CSS, "padding:2\n") || !strings.Contains(f.CSS, ".btn{color:#fff}") {
		t.Errorf("continuation not merged cleanly:\n%s", f.CSS)
	}
	if len(g.calls) != 2 || !strings.Contains(g.calls[1], "cut off") {
		t.Errorf("expected a continuation call, got %v", g.calls)
	}
	if len(g.hist[1]) != 2 || g.hist[1][0].Text != "a florist" || g.hist[1][1].Role != "model" {
		t.Errorf("continuation must carry the conversation: %+v", g.hist[1])
	}
	if len(stages) < 2 || !strings.Contains(stages[1], "part 2") {
		t.Errorf("progress not reported: %v", stages)
	}
}

func TestGenerateSiteGivesUpGracefully(t *testing.T) {
	// Never finishes, but HTML + CSS exist: salvage instead of failing.
	chunk := "@@TITLE@@\nT\n@@HTML@@\n<p>x</p>\n@@CSS@@\np{a:b}\nq{c:d"
	g := &fakeGen{answers: []string{chunk, "q{c:d", "q{c:d", "q{c:d", "q{c:d", "q{c:d", "q{c:d"}}
	f, err := generateSite(context.Background(), g, "i", "p", nil, func(string) {})
	if err != nil || f.HTML != "<p>x</p>" || strings.Contains(f.CSS, "q{c:d") && !strings.HasSuffix(f.CSS, "}") {
		t.Errorf("salvage path: err=%v %+v", err, f)
	}
	if len(g.calls) != 1+maxContinuations {
		t.Errorf("should stop after %d continuations, made %d calls", maxContinuations, len(g.calls))
	}
	// Truncated inside the HTML and never recovers: an error, not a broken site.
	g2 := &fakeGen{answers: []string{"@@TITLE@@\nT\n@@HTML@@\n<p>half", "<p>half", "<p>half", "<p>half", "<p>half", "<p>half", "<p>half"}}
	if _, err := generateSite(context.Background(), g2, "i", "p", nil, func(string) {}); !errors.Is(err, errSiteIncomplete) {
		t.Errorf("want errSiteIncomplete, got %v", err)
	}
}

func TestGenerateSiteRetriesWithoutImagesAndPropagatesErrors(t *testing.T) {
	g := &imageRejectingGen{ok: fullSite}
	f, err := generateSite(context.Background(), g, "i", "p", []ai.Image{{MimeType: "image/jpeg", Data: []byte{1}}}, func(string) {})
	if err != nil || f.Title != "Bloom Florist" || g.calls != 2 {
		t.Errorf("image fallback: err=%v calls=%d %+v", err, g.calls, f)
	}
	g2 := &fakeGen{err: errors.New("provider down")}
	if _, err := generateSite(context.Background(), g2, "i", "p", nil, func(string) {}); err == nil {
		t.Error("provider errors must be returned")
	}
}

type imageRejectingGen struct {
	ok    string
	calls int
}

func (g *imageRejectingGen) GenerateWithImages(_ context.Context, _ string, _ []ai.Message, _ string, _ float64, _ int, images []ai.Image) (string, error) {
	g.calls++
	if len(images) > 0 {
		return "", errors.New("this model does not accept images")
	}
	return g.ok, nil
}

func TestFinishSiteLimits(t *testing.T) {
	if _, err := finishSite(siteFiles{Title: "x", HTML: ""}); err == nil {
		t.Error("empty html must fail")
	}
	if _, err := finishSite(siteFiles{Title: "x", HTML: strings.Repeat("a", maxSiteHTML+1)}); err == nil {
		t.Error("oversized html must fail")
	}
	f, err := finishSite(siteFiles{Title: "\"Quoted\"", HTML: "<p>x</p>"})
	if err != nil || f.Title != "Quoted" {
		t.Errorf("%v %+v", err, f)
	}
}

func TestBuildSiteZip(t *testing.T) {
	p := WebsiteProject{ID: 5, Title: `Bloom & "Co" <Florist>`, Prompt: "A florist site, see https://example.com for ideas", HTML: "<h1>Hi</h1>", CSS: "h1{color:red}", JS: "console.log(1)"}
	data, name, err := buildSiteZip(p)
	if err != nil {
		t.Fatal(err)
	}
	if name != "bloom-co-florist-website.zip" {
		t.Errorf("filename = %q", name)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	var order []string
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(b)
		order = append(order, f.Name)
	}
	for _, want := range []string{"index.html", "css/style.css", "js/script.js", "favicon.svg", "404.html", "robots.txt", "README.md"} {
		if _, ok := got[want]; !ok {
			t.Errorf("zip is missing %s (has %v)", want, order)
		}
	}
	if order[0] != "index.html" {
		t.Errorf("index.html should be first, order=%v", order)
	}
	idx := got["index.html"]
	for _, want := range []string{"<!doctype html>", "<title>Bloom &amp; &#34;Co&#34; &lt;Florist&gt;</title>", `href="css/style.css"`, `src="js/script.js"`, "<h1>Hi</h1>", `name="viewport"`} {
		if !strings.Contains(idx, want) {
			t.Errorf("index.html missing %q:\n%s", want, idx)
		}
	}
	if strings.Contains(idx, "example.com") {
		t.Error("the meta description must not include links from the brief")
	}
	if !strings.Contains(got["README.md"], "Netlify") || !strings.Contains(got["README.md"], "public_html") {
		t.Error("README should explain how to deploy")
	}
	// No JS → no script tag, no js file.
	p.JS = ""
	data, _, _ = buildSiteZip(p)
	zr, _ = zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, f := range zr.File {
		if f.Name == "js/script.js" {
			t.Error("empty JS should not produce a file")
		}
	}
}

func TestSiteTitleAndSlug(t *testing.T) {
	if got := siteTitleFromBrief("Make a site like https://x.com/abc for my bakery"); got != "Make a site like for my bakery" {
		t.Errorf("title = %q", got)
	}
	if got := siteTitleFromBrief("https://x.com"); got != "New website" {
		t.Errorf("title = %q", got)
	}
	if siteSlug("Ünï çødé!!") == "" || siteSlug("   ") != "my" {
		t.Errorf("slug fallback broken: %q / %q", siteSlug("Ünï çødé!!"), siteSlug("   "))
	}
}

func TestSiteFailMessage(t *testing.T) {
	if !strings.Contains(siteFailMessage(context.DeadlineExceeded), "ran out of time") {
		t.Error("timeout message")
	}
	if !strings.Contains(siteFailMessage(errSiteIncomplete), "could not finish") {
		t.Error("incomplete message")
	}
}
