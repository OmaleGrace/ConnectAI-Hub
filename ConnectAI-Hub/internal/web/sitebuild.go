package web

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"html"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"connectai-hub/internal/ai"
)

// The website builder.
//
// Why this file exists: the first version generated the whole site inside one web
// request, as one JSON document. That could not work reliably:
//   - the Gemini client gave up after 75 s and the web server closed the connection after
//     120 s, but a full site takes longer than that to write;
//   - answers were cut at 8,192 tokens, which truncates a real site and breaks the JSON;
//   - code inside JSON strings needs perfect escaping, and one slip fails the whole build.
//
// Now a build runs as a background job. The page shows progress and picks up the result,
// the model writes plain delimited sections (no escaping), a cut-off answer is continued
// automatically, and the result is always offered as a ready-to-deploy ZIP.

const (
	maxSiteHTML        = 220000
	maxSiteCSS         = 140000
	maxSiteJS          = 90000
	maxContinuations   = 5
	siteJobTimeout     = 10 * time.Minute
	siteStaleAfter     = "2 minutes" // no heartbeat for this long = the job died (e.g. server restart)
	siteChunkTokens    = 8192
	siteRateLimitBuild = 3
)

var siteJobSlots = make(chan struct{}, 3)

const siteFormat = `

OUTPUT FORMAT (strict). Write plain text sections, each marker alone on its own line, in exactly this order. No Markdown fences, no JSON, no commentary outside the sections:
@@TITLE@@
(the site name, one line)
@@HTML@@
(body markup only: no <html>, <head> or <body> tags, no <style> or <script> tags)
@@CSS@@
(all CSS)
@@JS@@
(all JavaScript; leave empty if none is needed)
@@END@@`

const siteInstruction = `You are a senior web designer and frontend developer. Build a polished, responsive, accessible, production-quality website from the user's brief. The result is a static site that the user will unzip and upload to any hosting, so it must work by opening index.html with no build step.

Rules:
- Pure HTML, CSS and vanilla JavaScript only. No frameworks, no CDN or remote scripts, no trackers, no external requests, no forms that post to unknown endpoints (use a mailto: link or a clearly marked placeholder for contact forms).
- Do not depend on external images. Draw visuals with CSS gradients, inline SVG, emoji or CSS shapes. Use system font stacks.
- Navigation uses in-page anchors (#about, #contact). Include a clear hero, the sections the brief implies, a footer, and calls to action.
- Semantic HTML, a mobile-first responsive layout, visible focus styles, sufficient colour contrast, alt text or aria-labels, and keyboard-friendly controls.
- Use a coherent colour palette with CSS variables, good spacing and typography, and realistic placeholder copy written for the business or purpose in the brief (no lorem ipsum).
- Keep it complete but efficient: full pages with real content, without padding the code. Never leave a section half written.` + siteFormat

const siteReferenceRules = `

Reference material: the brief includes web pages and/or screenshots of existing sites. Use them as inspiration for structure, layout, tone, colour direction and topics. Do not copy copyrighted text, logos, brand names or images; write original content and recreate visuals with CSS or SVG. If the user asked for a close layout match, mirror the layout, not the content.`

type siteFiles struct{ Title, HTML, CSS, JS string }

var siteMarkerRe = regexp.MustCompile(`(?m)^[ \t]*@@(TITLE|HTML|CSS|JS|END)@@[ \t]*\r?$`)

// stripFences removes a Markdown code fence a model sometimes wraps around a section.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

// parseSiteFiles reads the section format. complete is true only when @@END@@ was seen.
// seen reports which sections appeared at all, so a truncated answer can be salvaged.
func parseSiteFiles(raw string) (f siteFiles, complete bool, seen map[string]bool) {
	seen = map[string]bool{}
	locs := siteMarkerRe.FindAllStringSubmatchIndex(raw, -1)
	if len(locs) == 0 {
		// Some models ignore the format and answer with JSON; accept that too.
		var j struct{ Title, HTML, CSS, JS string }
		if json.Unmarshal([]byte(extractJSONObject(raw)), &j) == nil && strings.TrimSpace(j.HTML) != "" {
			return siteFiles{strings.TrimSpace(j.Title), strings.TrimSpace(j.HTML), strings.TrimSpace(j.CSS), strings.TrimSpace(j.JS)}, true,
				map[string]bool{"HTML": true, "CSS": true, "JS": true}
		}
		return siteFiles{}, false, seen
	}
	for i, loc := range locs {
		name := raw[loc[2]:loc[3]]
		seen[name] = true
		if name == "END" {
			complete = true
			break
		}
		end := len(raw)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		body := stripFences(raw[loc[1]:end])
		switch name {
		case "TITLE":
			f.Title = strings.TrimSpace(strings.SplitN(body, "\n", 2)[0])
		case "HTML":
			f.HTML = body
		case "CSS":
			f.CSS = body
		case "JS":
			f.JS = body
		}
	}
	return
}

// salvage makes the best of an answer that never reached @@END@@. The HTML must be
// finished (the CSS section started after it). A half-written stylesheet is cut at its
// last complete rule, and a half-written script is dropped, because a broken script is
// worse than none.
func salvage(f siteFiles, seen map[string]bool) (siteFiles, bool) {
	if !seen["HTML"] || !seen["CSS"] || strings.TrimSpace(f.HTML) == "" {
		return f, false
	}
	if i := strings.LastIndex(f.CSS, "}"); i >= 0 {
		f.CSS = strings.TrimSpace(f.CSS[:i+1])
	} else {
		f.CSS = ""
	}
	f.JS = ""
	return f, true
}

// textGenerator is what generateSite needs from the AI client (so tests can fake it).
type textGenerator interface {
	GenerateWithImages(ctx context.Context, instruction string, history []ai.Message, prompt string, temperature float64, maxOutputTokens int, images []ai.Image) (string, error)
}

const continuePrompt = "Your previous answer was cut off by the length limit. Continue it. Start by repeating the LAST LINE of your previous answer exactly as written, then carry on from there without repeating anything else, without any preamble and without code fences. Finish with @@END@@ on its own line."

// joinContinuation appends a continuation. The model was told to repeat the last line of
// what it had written, so the unfinished last line is replaced by the continuation. That
// also avoids whitespace being lost at the seam (the API trims each answer).
func joinContinuation(acc, more string) string {
	more = strings.TrimLeft(more, "\r\n")
	if i := strings.LastIndex(acc, "\n"); i >= 0 {
		return acc[:i+1] + more
	}
	return more
}

var errSiteIncomplete = errors.New("the AI did not return a complete website")

// generateSite asks the model for a site and keeps going until the answer ends properly.
func generateSite(ctx context.Context, gen textGenerator, instruction, prompt string, images []ai.Image, progress func(string)) (siteFiles, error) {
	progress("Designing your website…")
	out, err := gen.GenerateWithImages(ctx, instruction, nil, prompt, 0.45, siteChunkTokens, images)
	if err != nil && len(images) > 0 && ctx.Err() == nil {
		// The model may not accept images; build from the page text alone.
		out, err = gen.GenerateWithImages(ctx, instruction, nil, prompt+"\n\n[The screenshot could not be analysed; rely on the page text.]", 0.45, siteChunkTokens, nil)
	}
	if err != nil {
		return siteFiles{}, err
	}
	acc := out
	for i := 0; ; i++ {
		files, complete, seen := parseSiteFiles(acc)
		if complete {
			return finishSite(files)
		}
		if i >= maxContinuations {
			if sv, ok := salvage(files, seen); ok {
				log.Printf("connectai: website answer never reached @@END@@; salvaged HTML+CSS after %d continuations", i)
				return finishSite(sv)
			}
			return siteFiles{}, errSiteIncomplete
		}
		progress(fmt.Sprintf("Writing more of the site (part %d)…", i+2))
		hist := []ai.Message{{Role: "user", Text: prompt}, {Role: "model", Text: acc}}
		more, err := gen.GenerateWithImages(ctx, instruction, hist, continuePrompt, 0.2, siteChunkTokens, nil)
		if err != nil {
			if sv, ok := salvage(files, seen); ok && ctx.Err() == nil {
				return finishSite(sv)
			}
			return siteFiles{}, err
		}
		acc = joinContinuation(acc, more)
	}
}

func finishSite(f siteFiles) (siteFiles, error) {
	f.Title = strings.TrimSpace(strings.Trim(f.Title, "\"'"))
	f.HTML = strings.TrimSpace(f.HTML)
	f.CSS = strings.TrimSpace(f.CSS)
	f.JS = strings.TrimSpace(f.JS)
	if f.HTML == "" {
		return siteFiles{}, errSiteIncomplete
	}
	if len(f.HTML) > maxSiteHTML || len(f.CSS) > maxSiteCSS || len(f.JS) > maxSiteJS {
		return siteFiles{}, errors.New("the generated website was too large")
	}
	if len([]rune(f.Title)) > 100 {
		f.Title = string([]rune(f.Title)[:100])
	}
	return f, nil
}

// ---------------------------------------------------------------------------
// Background jobs
// ---------------------------------------------------------------------------

func (s *Server) setSiteStage(projectID int64, stage string) {
	_, _ = s.db.Exec(`UPDATE website_projects SET build_stage=$1, updated_at=NOW() WHERE id=$2 AND status IN ('building','editing')`, stage, projectID)
}

// siteFailMessage turns an error into something a person can act on.
func siteFailMessage(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "The builder ran out of time. Try a shorter or simpler brief, or build one page at a time."
	case errors.Is(err, errSiteIncomplete):
		return "The AI could not finish the website. Please try again, or shorten the brief."
	case strings.Contains(err.Error(), "too large"):
		return "The generated website was too large. Ask for fewer sections and try again."
	}
	return safeAIError(err)
}

// runSiteJob builds (or edits) a website in the background. It never touches the HTTP
// request that started it, so closing the browser does not cancel the build.
func (s *Server) runSiteJob(projectID, userID int64, kind, brief string, base *siteFiles) {
	ctx, cancel := context.WithTimeout(context.Background(), siteJobTimeout)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("connectai: website job panic project=%d: %v", projectID, r)
			s.finishSiteJob(projectID, userID, kind, nil, "Something went wrong while building. Please try again.", 0, 0)
		}
	}()
	// A heartbeat lets the cleanup job tell a running build from one that died with a restart.
	stopBeat := make(chan struct{})
	defer close(stopBeat)
	go func() {
		t := time.NewTicker(20 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_, _ = s.db.Exec(`UPDATE website_projects SET updated_at=NOW() WHERE id=$1 AND status IN ('building','editing')`, projectID)
			case <-stopBeat:
				return
			}
		}
	}()

	s.setSiteStage(projectID, "Waiting for a free builder…")
	select {
	case siteJobSlots <- struct{}{}:
		defer func() { <-siteJobSlots }()
	case <-ctx.Done():
		s.finishSiteJob(projectID, userID, kind, nil, siteFailMessage(ctx.Err()), 0, 0)
		return
	}

	instruction := siteInstruction
	prompt := brief
	var images []ai.Image
	if kind == "edit" && base != nil {
		instruction = "You are an expert frontend engineer making a focused change to an existing static website. Apply the user's change, preserve all unrelated content, structure and design, and return the COMPLETE updated website (every section, not a diff). Same rules as before: pure HTML/CSS/vanilla JS, no external requests or libraries." + siteFormat
		prompt = "Existing website:\n@@TITLE@@\n" + base.Title + "\n@@HTML@@\n" + base.HTML + "\n@@CSS@@\n" + base.CSS + "\n@@JS@@\n" + base.JS + "\n@@END@@\n\nChange requested by the user:\n" + brief
	}
	if links, _ := extractURLs(brief, maxURLsPerMessage); len(links) > 0 && webAccessEnabled() {
		s.setSiteStage(projectID, "Reading the links in your brief…")
		wc := s.buildWebContext(ctx, userID, "website", brief, shotOn)
		if wc.Used {
			if kind == "edit" && base != nil {
				prompt += strings.TrimPrefix(wc.Prompt, brief)
			} else {
				prompt = wc.Prompt
			}
			images = wc.Images
			instruction += webInstruction + siteReferenceRules
		}
	}

	var gen textGenerator = s.siteGen
	if gen == nil {
		gen = s.gemini.ForModule("website").Long()
	}
	files, err := generateSite(ctx, gen, instruction, prompt, images, func(stage string) { s.setSiteStage(projectID, stage) })
	if err != nil {
		log.Printf("connectai: website %s failed project=%d user=%d: %v", kind, projectID, userID, err)
		s.finishSiteJob(projectID, userID, kind, nil, siteFailMessage(err), len(brief), 0)
		return
	}
	s.setSiteStage(projectID, "Packaging your files…")
	s.finishSiteJob(projectID, userID, kind, &files, "", len(brief), len(files.HTML)+len(files.CSS)+len(files.JS))
}

// finishSiteJob stores the result. On failure a new build becomes "failed", while a failed
// edit keeps the existing site and just records the message.
func (s *Server) finishSiteJob(projectID, userID int64, kind string, f *siteFiles, failure string, inChars, outChars int) {
	if f == nil {
		if kind == "edit" {
			_, _ = s.db.Exec(`UPDATE website_projects SET status='ready', build_stage='', build_error=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`, failure, projectID, userID)
		} else {
			_, _ = s.db.Exec(`UPDATE website_projects SET status='failed', build_stage='', build_error=$1, updated_at=NOW() WHERE id=$2 AND user_id=$3`, failure, projectID, userID)
		}
		return
	}
	tx, err := s.db.Begin()
	if err != nil {
		log.Printf("connectai: website save begin: %v", err)
		return
	}
	defer tx.Rollback()
	if kind == "edit" {
		if _, err = tx.Exec(`INSERT INTO website_project_versions(project_id,user_id,title,html,css,js) SELECT id,user_id,title,html,css,js FROM website_projects WHERE id=$1 AND user_id=$2`, projectID, userID); err != nil {
			log.Printf("connectai: website version save: %v", err)
			return
		}
	}
	title := f.Title
	if title == "" {
		_ = tx.QueryRow(`SELECT title FROM website_projects WHERE id=$1`, projectID).Scan(&title)
	}
	if _, err = tx.Exec(`UPDATE website_projects SET title=$1,html=$2,css=$3,js=$4,status='ready',build_stage='',build_error='',updated_at=NOW() WHERE id=$5 AND user_id=$6`,
		title, f.HTML, f.CSS, f.JS, projectID, userID); err != nil {
		log.Printf("connectai: website save: %v", err)
		return
	}
	reqType := "website_build"
	if kind == "edit" {
		reqType = "edit"
	}
	_, _ = tx.Exec(`INSERT INTO ai_usage(user_id,module_slug,request_type,input_characters,output_characters) VALUES($1,'website',$2,$3,$4)`, userID, reqType, inChars, outChars)
	if err = tx.Commit(); err != nil {
		log.Printf("connectai: website commit: %v", err)
	}
}

// failInterruptedSiteJobs ends builds whose worker is gone (heartbeat stopped).
func (s *Server) failInterruptedSiteJobs() {
	_, _ = s.db.Exec(`UPDATE website_projects SET status='failed', build_stage='', build_error='The build was interrupted (the server restarted). Please try again.' WHERE status='building' AND updated_at < NOW() - INTERVAL '` + siteStaleAfter + `'`)
	_, _ = s.db.Exec(`UPDATE website_projects SET status='ready', build_stage='', build_error='The edit was interrupted (the server restarted). Your website is unchanged.' WHERE status='editing' AND updated_at < NOW() - INTERVAL '` + siteStaleAfter + `'`)
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

var urlStripRe = regexp.MustCompile(`(?i)(?:https?://|www\.)\S+`)

// siteTitleFromBrief is a readable working title until the AI names the site.
func siteTitleFromBrief(brief string) string {
	t := strings.Join(strings.Fields(urlStripRe.ReplaceAllString(brief, "")), " ")
	if t == "" {
		t = "New website"
	}
	r := []rune(t)
	if len(r) > 60 {
		t = strings.TrimSpace(string(r[:60])) + "…"
	}
	return t
}

func (s *Server) generateWebsite(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 12<<10)
	if err := r.ParseForm(); err != nil || !s.validCSRF(r) {
		http.Error(w, "Your form expired. Refresh and try again.", http.StatusForbidden)
		return
	}
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if len(prompt) < 5 || len(prompt) > 8000 {
		http.Redirect(w, r, "/builder?error=Please+write+a+website+description+between+5+and+8000+characters.", http.StatusSeeOther)
		return
	}
	if !s.allowRate("builder:"+fmt.Sprint(u.ID), siteRateLimitBuild, 10*time.Minute) {
		http.Redirect(w, r, "/builder?error=Please+wait+before+generating+another+website.", http.StatusSeeOther)
		return
	}
	if !s.withinDailyLimit(w, u.ID) {
		return
	}
	s.failInterruptedSiteJobs()
	var count, active int
	if err := s.db.QueryRow(`SELECT COUNT(*), COUNT(*) FILTER (WHERE status IN ('building','editing')) FROM website_projects WHERE user_id=$1`, u.ID).Scan(&count, &active); err != nil {
		http.Error(w, "Unable to check project limit.", 500)
		return
	}
	if count >= 30 {
		http.Redirect(w, r, "/builder?error=You+have+reached+the+30+website+project+limit.", http.StatusSeeOther)
		return
	}
	if active > 0 {
		http.Redirect(w, r, "/builder?error=A+website+is+already+being+built.+Open+it+below+and+wait+for+it+to+finish.", http.StatusSeeOther)
		return
	}

	// Only a genuinely empty brief is bounced back with questions; a brief that links to a
	// reference site is clear enough because the site itself is the reference.
	if hasLink, _ := extractURLs(prompt, 1); len(hasLink) == 0 {
		assessCtx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		assessPrompt := `Assess this website brief. If it is too vague to build a useful website, return exactly JSON {"ready":false,"questions":["..."],"reason":"..."}. Ask at most 3 short, essential questions. A brief is ready if its website type and purpose are reasonably inferable; do not demand optional details. If ready, return {"ready":true,"questions":[],"reason":""}. Brief: ` + prompt
		verdictText, aerr := s.gemini.ForModule("website").GenerateWithLimits(assessCtx, "You assess website briefs. Return JSON only.", nil, assessPrompt, 0.1, 500)
		cancel()
		if aerr == nil {
			var verdict struct {
				Ready     bool     `json:"ready"`
				Questions []string `json:"questions"`
				Reason    string   `json:"reason"`
			}
			if json.Unmarshal([]byte(extractJSONObject(verdictText)), &verdict) == nil && !verdict.Ready {
				message := strings.Join(verdict.Questions, "\n• ")
				if message == "" {
					message = verdict.Reason
				}
				http.Redirect(w, r, "/builder?clarify="+url.QueryEscape(message)+"&prompt="+url.QueryEscape(prompt), http.StatusSeeOther)
				return
			}
		}
	}

	var id int64
	err := s.db.QueryRow(`INSERT INTO website_projects(user_id,title,prompt,html,css,js,status,build_stage) VALUES($1,$2,$3,'','','','building','Starting…') RETURNING id`,
		u.ID, siteTitleFromBrief(prompt), prompt).Scan(&id)
	if err != nil {
		log.Printf("connectai: create website project: %v", err)
		http.Error(w, "Unable to start your website project.", 500)
		return
	}
	go s.runSiteJob(id, u.ID, "build", prompt, nil)
	http.Redirect(w, r, fmt.Sprintf("/builder/projects/%d", id), http.StatusSeeOther)
}

func (s *Server) editWebsiteProject(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Error(w, "Unauthorized", 401)
		return
	}
	if !s.validCSRF(r) {
		http.Error(w, "Invalid form token", 403)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid request", 400)
		return
	}
	instruction := strings.TrimSpace(r.FormValue("instruction"))
	if len(instruction) < 4 || len(instruction) > 4000 {
		http.Error(w, "Describe the change in 4–4,000 characters.", 400)
		return
	}
	if !s.allowRate("builder-edit:"+fmt.Sprint(u.ID), 8, 10*time.Minute) {
		http.Error(w, "Please wait before making more edits.", 429)
		return
	}
	if !s.withinDailyLimit(w, u.ID) {
		return
	}
	s.failInterruptedSiteJobs()
	p, err := s.loadOwnedWebsite(r, u)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if p.Status != "ready" {
		http.Redirect(w, r, fmt.Sprintf("/builder/projects/%d", p.ID), http.StatusSeeOther)
		return
	}
	res, err := s.db.Exec(`UPDATE website_projects SET status='editing', build_stage='Starting…', build_error='', updated_at=NOW() WHERE id=$1 AND user_id=$2 AND status='ready'`, p.ID, u.ID)
	if n, _ := res.RowsAffected(); err != nil || n == 0 {
		http.Redirect(w, r, fmt.Sprintf("/builder/projects/%d", p.ID), http.StatusSeeOther)
		return
	}
	base := siteFiles{Title: p.Title, HTML: p.HTML, CSS: p.CSS, JS: p.JS}
	go s.runSiteJob(p.ID, u.ID, "edit", instruction, &base)
	http.Redirect(w, r, fmt.Sprintf("/builder/projects/%d", p.ID), http.StatusSeeOther)
}

// websiteStatus is polled by the project page while a build or edit is running.
func (s *Server) websiteStatus(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Error(w, "Unauthorized", 401)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.failInterruptedSiteJobs()
	var status, stage, berr string
	var seconds float64
	if err := s.db.QueryRow(`SELECT status,build_stage,build_error,EXTRACT(EPOCH FROM (NOW()-created_at)) FROM website_projects WHERE id=$1 AND user_id=$2`, id, u.ID).Scan(&status, &stage, &berr, &seconds); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "stage": stage, "error": berr, "seconds": int(seconds)})
}

// ---------------------------------------------------------------------------
// ZIP packaging
// ---------------------------------------------------------------------------

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func siteSlug(title string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "my"
	}
	return s
}

// buildSiteZip packages a project as a folder-free ZIP that can be uploaded as it is.
func buildSiteZip(p WebsiteProject) ([]byte, string, error) {
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = "My website"
	}
	desc := strings.Join(strings.Fields(urlStripRe.ReplaceAllString(p.Prompt, "")), " ")
	if r := []rune(desc); len(r) > 155 {
		desc = string(r[:155])
	}
	var index strings.Builder
	index.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">\n")
	index.WriteString("<title>" + html.EscapeString(title) + "</title>\n")
	if desc != "" {
		index.WriteString("<meta name=\"description\" content=\"" + html.EscapeString(desc) + "\">\n")
	}
	index.WriteString("<link rel=\"icon\" href=\"favicon.svg\" type=\"image/svg+xml\">\n<link rel=\"stylesheet\" href=\"css/style.css\">\n</head>\n<body>\n")
	index.WriteString(p.HTML)
	index.WriteString("\n")
	if strings.TrimSpace(p.JS) != "" {
		index.WriteString("<script src=\"js/script.js\" defer></script>\n")
	}
	index.WriteString("</body>\n</html>\n")

	h := fnv.New32a()
	_, _ = h.Write([]byte(title))
	hue := int(h.Sum32() % 360)
	letter := "W"
	if r := []rune(strings.ToUpper(title)); len(r) > 0 {
		letter = html.EscapeString(string(r[0]))
	}
	favicon := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="hsl(%d 65%% 42%%)"/><text x="32" y="44" font-family="system-ui,sans-serif" font-size="36" font-weight="700" text-anchor="middle" fill="#fff">%s</text></svg>`+"\n", hue, letter)

	notFound := "<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>Page not found · " + html.EscapeString(title) + "</title>" +
		"<style>body{font-family:system-ui,sans-serif;display:grid;place-items:center;min-height:100vh;margin:0;text-align:center;padding:24px}a{color:inherit}</style></head><body><main><h1>404</h1><p>That page does not exist.</p><p><a href=\"/\">Back to " + html.EscapeString(title) + "</a></p></main></body></html>\n"

	readme := "# " + title + "\n\nA static website generated with ConnectAI Hub. There is nothing to install or build: it is plain HTML, CSS and JavaScript.\n\n" +
		"## Files\n\n- `index.html`: the page\n- `css/style.css`: styles\n" + func() string {
		if strings.TrimSpace(p.JS) != "" {
			return "- `js/script.js`: behaviour\n"
		}
		return ""
	}() + "- `favicon.svg`, `404.html`, `robots.txt`, `_headers`: standard extras (safe to edit or delete)\n\n" +
		"## Preview on your computer\n\nUnzip, then double-click `index.html`.\n\n" +
		"## Put it online (pick one)\n\n" +
		"- **Netlify Drop (easiest):** go to https://app.netlify.com/drop and drag the unzipped folder onto the page.\n" +
		"- **Cloudflare Pages:** Workers & Pages → Create → Pages → Upload assets, then upload the unzipped folder.\n" +
		"- **GitHub Pages:** create a repository, upload these files to the main branch, then Settings → Pages → Deploy from branch.\n" +
		"- **Vercel:** drag the folder into https://vercel.com/new, or run `vercel` inside it.\n" +
		"- **Any web host (cPanel, FTP):** upload everything into the `public_html` folder (or your site's root folder), keeping the folder structure.\n\n" +
		"## Before you publish\n\n- Read through the text and replace the placeholder details (names, phone numbers, email, addresses, prices) with your real ones.\n" +
		"- Contact forms are placeholders. Use a form service, or link to your email address or WhatsApp.\n- This code was written by AI: preview it on a phone and a computer and check that every link works.\n"

	type entry struct{ name, body string }
	files := []entry{{"index.html", index.String()}, {"css/style.css", p.CSS + "\n"}}
	if strings.TrimSpace(p.JS) != "" {
		files = append(files, entry{"js/script.js", p.JS + "\n"})
	}
	files = append(files, entry{"favicon.svg", favicon}, entry{"404.html", notFound}, entry{"robots.txt", "User-agent: *\nAllow: /\n"},
		entry{"_headers", "/*\n  X-Content-Type-Options: nosniff\n  Referrer-Policy: strict-origin-when-cross-origin\n  X-Frame-Options: SAMEORIGIN\n"},
		entry{".nojekyll", ""}, entry{"README.md", readme})

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	now := time.Now()
	for _, f := range files {
		hdr := &zip.FileHeader{Name: f.name, Method: zip.Deflate, Modified: now}
		hdr.SetMode(0o644)
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			return nil, "", err
		}
		if _, err = fw.Write([]byte(f.body)); err != nil {
			return nil, "", err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), siteSlug(title) + "-website.zip", nil
}

func (s *Server) downloadWebsiteProject(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if u == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	p, err := s.loadOwnedWebsite(r, u)
	if err != nil || strings.TrimSpace(p.HTML) == "" {
		http.NotFound(w, r)
		return
	}
	data, name, err := buildSiteZip(p)
	if err != nil {
		http.Error(w, "Unable to package website.", 500)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(data)
}
