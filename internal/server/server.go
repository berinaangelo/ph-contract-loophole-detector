// Package server is the HTTP layer wrapping the primary flow (PLAN.md):
// extract → language gate → clause.Split →
// severity/checklist/retrieve+generate, merged into one shared Finding
// shape and served as one synchronous JSON response, plus the single-page
// UI (web/) that consumes it. No new pipeline logic here — this composes
// what those packages already do behind one route.
package server

import (
	"encoding/json"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ollama/ollama/api"

	"ph-contract-loophole-detector/internal/checklist"
	"ph-contract-loophole-detector/internal/clause"
	"ph-contract-loophole-detector/internal/extract"
	"ph-contract-loophole-detector/internal/generate"
	"ph-contract-loophole-detector/internal/language"
	"ph-contract-loophole-detector/internal/retrieve"
	"ph-contract-loophole-detector/internal/severity"
	"ph-contract-loophole-detector/internal/store"
	"ph-contract-loophole-detector/web"
)

// Status values for analyzeResponse. All user-facing copy for each of
// these lives once in web/index.html, keyed by this string — the server
// never transmits prose, same "fixed, never-regenerated string" instinct
// PLAN.md already applies to language.UnsupportedMessage.
const (
	statusResults         = "results"
	statusClean           = "clean"
	statusNoMatchLanguage = "no_match_language"
	statusNoMatchOffTopic = "no_match_offtopic"
	statusFileError       = "file_error"
)

// maxUploadBytes caps the whole request body (form fields + uploaded
// file) — generous for a lease DOCX/PDF, defensive against an unbounded
// body per CLAUDE.md's "always generate with a fallback" instinct applied
// to external input.
const maxUploadBytes = 10 << 20 // 10 MiB

// requestTimeout is sized from measured local timing, not guessed: a live
// end-to-end run of the real 15-clause testdata/sample_lease.txt fixture
// (45 candidates, 5 sequential generate.BatchCap-sized batches) measured
// retrieve.Query at ~1s total but generate.Answer at 5m38s against
// qwen2.5:7b — an initial 5-minute timeout, extrapolated from one
// 10-candidate batch alone, was too tight and was caught live by this
// same measurement. 15 minutes gives a realistic lease real headroom
// above that measured run, not just the old single-LLM-call app's 90s.
const requestTimeout = 15 * time.Minute

// Deps are the live dependencies handleAnalyze calls into. The
// short-circuit paths (bad request, file error, language rejection) never
// touch these — see the tests, which pass nil deps to prove it.
type Deps struct {
	Ollama *api.Client
	Qdrant *store.QdrantStore
	Bleve  *store.BleveStore
}

// NewRouter wires the one primary-flow route plus a health check.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Logger)
	r.Use(middleware.Timeout(requestTimeout))

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Post("/api/analyze", deps.handleAnalyze)

	// Serves web/index.html same-origin as /api/analyze — the browser
	// fetch needs no CORS headers this way.
	r.Handle("/", http.FileServer(http.FS(web.FS)))

	return r
}

// Finding is PLAN.md's shared shape that severity.Finding,
// checklist.Finding, and generate.Finding all get mapped into — that
// unification is this package's job specifically, the three source
// packages deliberately don't share a struct directly (Design decisions,
// PLAN.md). ClauseText/Citation are empty for checklist-sourced (Gaps)
// entries: a missing clause isn't tied to a specific statute or quoted
// text, by design, not by omission.
type Finding struct {
	Severity    string `json:"severity"`
	Issue       string `json:"issue"`
	ClauseText  string `json:"clauseText,omitempty"`
	Explanation string `json:"explanation"`
	Citation    string `json:"citation,omitempty"`
	Action      string `json:"action"`
}

// Scorecard tallies every Finding (both Findings and Gaps) by severity —
// "2 HIGH, 4 MEDIUM, 1 LOW" rendered before any clause detail.
type Scorecard struct {
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
}

// analyzeResponse covers every primary-flow outcome with one shape;
// Scorecard/Findings/Gaps are only populated when Status is
// statusResults — every other status is fully described by the status
// string alone, its copy owned client-side.
type analyzeResponse struct {
	Status    string     `json:"status"`
	Scorecard *Scorecard `json:"scorecard,omitempty"`
	Findings  []Finding  `json:"findings,omitempty"`
	Gaps      []Finding  `json:"gaps,omitempty"`
}

func (d Deps) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form data")
		return
	}

	text, err := resolveText(r)
	if err != nil {
		log.Printf("server: extract uploaded file: %v", err)
		writeJSON(w, http.StatusOK, analyzeResponse{Status: statusFileError})
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text or file is required")
		return
	}

	// Language gate. Nothing past this point runs if it fails.
	if !language.IsEnglish(text) {
		writeJSON(w, http.StatusOK, analyzeResponse{Status: statusNoMatchLanguage})
		return
	}

	ctx := r.Context()
	clauses := clause.Split(text)

	// checklist.LooksLikeLease gates only whether checklist runs — severity
	// and retrieve+generate below run regardless (Design decisions,
	// PLAN.md) — but it's also this handler's tiebreaker between the
	// "looks clean" and "no match" zero-findings outcomes below, since a
	// zero-findings result means something different depending on whether
	// checklist ran and validated the doc, or never ran at all.
	looksLikeLease := checklist.LooksLikeLease(text)
	var gaps []Finding
	if looksLikeLease {
		for _, f := range checklist.Check(text) {
			gaps = append(gaps, Finding{
				Severity:    f.Severity,
				Issue:       f.Topic,
				Explanation: f.Explanation,
				Action:      f.Action,
			})
		}
	}

	var findings []Finding
	var candidates []generate.Candidate
	for _, c := range clauses {
		for _, f := range severity.Check(c.Text) {
			findings = append(findings, Finding{
				Severity:    f.Severity,
				Issue:       f.Issue,
				ClauseText:  f.ClauseText,
				Explanation: f.Explanation,
				Citation:    f.Statute,
				Action:      f.Action,
			})
		}

		results, err := retrieve.Query(ctx, d.Ollama, d.Qdrant, d.Bleve, c.Text)
		if err != nil {
			log.Printf("server: retrieve.Query clause %d: %v", c.Index, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for _, res := range results {
			candidates = append(candidates, generate.Candidate{
				ClauseIndex: c.Index,
				ClauseText:  c.Text,
				Issue:       res.Issue,
				Chunk:       res.Chunk,
			})
		}
	}

	generated, err := generate.Answer(ctx, d.Ollama, candidates)
	if err != nil {
		log.Printf("server: generate.Answer: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	for _, f := range generated {
		findings = append(findings, Finding{
			Severity:    f.Severity,
			Issue:       issueLabel(f.Issue),
			ClauseText:  f.ClauseText,
			Explanation: f.Explanation,
			Citation:    f.Citation,
			Action:      f.Action,
		})
	}

	findings = sortBySeverity(findings)

	status := finalStatus(findings, gaps, looksLikeLease)
	if status != statusResults {
		writeJSON(w, http.StatusOK, analyzeResponse{Status: status})
		return
	}

	writeJSON(w, http.StatusOK, analyzeResponse{
		Status:    statusResults,
		Scorecard: scorecard(findings, gaps),
		Findings:  findings,
		Gaps:      gaps,
	})
}

// finalStatus decides between the three outcomes a completed pipeline run
// can produce. When nothing survived at all, looksLikeLease is the
// tiebreaker: checklist ran and validated the doc as gap-free (statusClean,
// a genuine reassuring result) vs. checklist never ran because the text
// wasn't lease-shaped to begin with (statusNoMatchOffTopic, nothing
// recognized). Pure — unit-testable without live services.
func finalStatus(findings, gaps []Finding, looksLikeLease bool) string {
	if len(findings)+len(gaps) > 0 {
		return statusResults
	}
	if looksLikeLease {
		return statusClean
	}
	return statusNoMatchOffTopic
}

// resolveText returns the request's contract text: an uploaded file's
// extracted text if a "file" form field was sent, otherwise the "text"
// field verbatim. A non-nil error always means "couldn't read this file"
// (unsupported extension or no extractable text) — the only case
// handleAnalyze maps to statusFileError.
func resolveText(r *http.Request) (string, error) {
	fh := formFile(r)
	if fh == nil {
		return r.FormValue("text"), nil
	}
	return extractUploadedText(fh)
}

func formFile(r *http.Request) *multipart.FileHeader {
	if r.MultipartForm == nil || len(r.MultipartForm.File["file"]) == 0 {
		return nil
	}
	return r.MultipartForm.File["file"][0]
}

func extractUploadedText(fh *multipart.FileHeader) (string, error) {
	format, err := extract.DetectFormat(fh.Filename)
	if err != nil {
		return "", err
	}
	f, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return extract.Text(data, format)
}

// sortBySeverity returns findings ordered HIGH → MEDIUM → LOW, stable so
// clause/candidate order (already document order) is preserved within
// each rank — the god-moment ordering PLAN.md requires: HIGH must never
// require scrolling to find.
func sortBySeverity(findings []Finding) []Finding {
	sort.SliceStable(findings, func(i, j int) bool {
		return severityRank(findings[i].Severity) < severityRank(findings[j].Severity)
	})
	return findings
}

func severityRank(s string) int {
	switch s {
	case "HIGH":
		return 0
	case "MEDIUM":
		return 1
	default:
		return 2
	}
}

// scorecard tallies every Finding across both findings and gaps — Gaps
// are LOW findings too (PLAN.md's own worked example: 2 HIGH + 2 MEDIUM +
// 1 clause-specific LOW + 3 gap LOW = a scorecard of 2/2/4).
func scorecard(findings, gaps []Finding) *Scorecard {
	sc := &Scorecard{}
	for _, f := range findings {
		tally(sc, f.Severity)
	}
	for _, f := range gaps {
		tally(sc, f.Severity)
	}
	return sc
}

func tally(sc *Scorecard, sev string) {
	switch sev {
	case "HIGH":
		sc.High++
	case "MEDIUM":
		sc.Medium++
	case "LOW":
		sc.Low++
	}
}

// issueLabels maps a generate.Finding's raw corpus issue_tags value to a
// display-ready label — severity.Finding.Issue and checklist.Finding.Topic
// are already display phrases, but this is the one source that isn't
// (data/corpus/README.md lists the 11 known tags this covers).
var issueLabels = map[string]string{
	"security_deposit":             "Security deposit",
	"eviction_process":             "Eviction process",
	"repairs_habitability":         "Repairs & habitability",
	"rent_increase":                "Rent increase",
	"penalty_interest":             "Penalty / interest",
	"termination_notice":           "Termination notice",
	"renewal_terms":                "Renewal terms",
	"dispute_resolution_venue":     "Dispute resolution / venue",
	"utilities":                    "Utilities",
	"subletting_assignment":        "Subletting / assignment",
	"house_rules_use_restrictions": "House rules / use restrictions",
}

// issueLabel looks up tag's display label, falling back to a formatted
// version of the raw tag for any future corpus tag this map doesn't know
// about yet — degrades gracefully instead of rendering (or crashing on)
// a raw snake_case string.
func issueLabel(tag string) string {
	if label, ok := issueLabels[tag]; ok {
		return label
	}
	return fallbackLabel(tag)
}

func fallbackLabel(tag string) string {
	label := strings.ReplaceAll(tag, "_", " ")
	if label == "" {
		return label
	}
	return strings.ToUpper(label[:1]) + label[1:]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("server: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
