package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestRouter builds a router with nil live deps — the short-circuit
// paths below never touch Deps.Qdrant/Ollama/Bleve, so passing nil
// doubles as proof those short-circuits truly happen before any live
// dependency is used.
func newTestRouter() http.Handler {
	return NewRouter(Deps{})
}

func postForm(t *testing.T, r http.Handler, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field %q: %v", k, err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/analyze", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) analyzeResponse {
	t.Helper()
	var resp analyzeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
	}
	return resp
}

func TestHandleAnalyze_MissingTextAndFile(t *testing.T) {
	rec := postForm(t, newTestRouter(), map[string]string{})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleAnalyze_WhitespaceOnlyText(t *testing.T) {
	rec := postForm(t, newTestRouter(), map[string]string{"text": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleAnalyze_NonEnglish(t *testing.T) {
	rec := postForm(t, newTestRouter(), map[string]string{
		"text": "Este es un contrato de arrendamiento escrito completamente en español para esta prueba.",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	resp := decode(t, rec)
	if resp.Status != statusNoMatchLanguage {
		t.Errorf("Status = %q, want %q", resp.Status, statusNoMatchLanguage)
	}
}

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	newTestRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestSortBySeverity_OrdersHighMediumLow(t *testing.T) {
	findings := []Finding{
		{Severity: "LOW", Issue: "a"},
		{Severity: "HIGH", Issue: "b"},
		{Severity: "MEDIUM", Issue: "c"},
		{Severity: "HIGH", Issue: "d"},
	}
	got := sortBySeverity(findings)

	wantOrder := []string{"HIGH", "HIGH", "MEDIUM", "LOW"}
	for i, want := range wantOrder {
		if got[i].Severity != want {
			t.Fatalf("got[%d].Severity = %q, want %q (full: %+v)", i, got[i].Severity, want, got)
		}
	}
	// Stable: "b" (first HIGH) must stay before "d" (second HIGH).
	if got[0].Issue != "b" || got[1].Issue != "d" {
		t.Errorf("HIGH order = %q, %q, want b, d (stable sort)", got[0].Issue, got[1].Issue)
	}
}

func TestScorecard_TalliesFindingsAndGapsTogether(t *testing.T) {
	findings := []Finding{
		{Severity: "HIGH"}, {Severity: "HIGH"},
		{Severity: "MEDIUM"}, {Severity: "MEDIUM"},
		{Severity: "LOW"},
	}
	gaps := []Finding{{Severity: "LOW"}, {Severity: "LOW"}, {Severity: "LOW"}}

	sc := scorecard(findings, gaps)

	// PLAN.md's own worked example: 2 HIGH, 2 MEDIUM, 1 clause-LOW + 3
	// gap-LOW = 4 LOW.
	if sc.High != 2 || sc.Medium != 2 || sc.Low != 4 {
		t.Errorf("scorecard = %+v, want {High:2 Medium:2 Low:4}", sc)
	}
}

func TestIssueLabel_KnownTag(t *testing.T) {
	if got := issueLabel("security_deposit"); got != "Security deposit" {
		t.Errorf("issueLabel(security_deposit) = %q, want %q", got, "Security deposit")
	}
}

func TestIssueLabel_UnknownTagFallsBackGracefully(t *testing.T) {
	got := issueLabel("some_future_tag")
	if got != "Some future tag" {
		t.Errorf("issueLabel(some_future_tag) = %q, want %q", got, "Some future tag")
	}
}

func TestFinalStatus_EmptyLooksLikeLeaseIsClean(t *testing.T) {
	if got := finalStatus(nil, nil, true); got != statusClean {
		t.Errorf("finalStatus(empty, empty, true) = %q, want %q", got, statusClean)
	}
}

func TestFinalStatus_EmptyNotLeaseShapedIsNoMatch(t *testing.T) {
	if got := finalStatus(nil, nil, false); got != statusNoMatchOffTopic {
		t.Errorf("finalStatus(empty, empty, false) = %q, want %q", got, statusNoMatchOffTopic)
	}
}

func TestFinalStatus_AnyFindingIsResults(t *testing.T) {
	findings := []Finding{{Severity: "HIGH"}}
	if got := finalStatus(findings, nil, false); got != statusResults {
		t.Errorf("finalStatus(1 finding, empty, false) = %q, want %q", got, statusResults)
	}
	if got := finalStatus(nil, []Finding{{Severity: "LOW"}}, false); got != statusResults {
		t.Errorf("finalStatus(empty, 1 gap, false) = %q, want %q", got, statusResults)
	}
}

func TestSeverityRank(t *testing.T) {
	if severityRank("HIGH") >= severityRank("MEDIUM") || severityRank("MEDIUM") >= severityRank("LOW") {
		t.Errorf("severityRank must order HIGH < MEDIUM < LOW")
	}
}
