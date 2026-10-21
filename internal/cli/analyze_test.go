package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// analyzeBackend listet Dokumente und nimmt Analysen an; per Dokument lässt
// sich eine Antwortfolge vorgeben (z. B. erst 429, dann 202).
type analyzeBackend struct {
	docs     []string
	legacy   bool
	replies  map[string][]reply
	analyzed []string
	lists    []*recorded
}

func (b *analyzeBackend) handle(rec *recorded) reply {
	if rec.Method == "GET" && rec.Path == "/api/documents/documents" {
		b.lists = append(b.lists, rec)
		items := []string{}
		for _, id := range b.docs {
			items = append(items, fmt.Sprintf(`{"id":%q,"dateiname":"%s.pdf","analysis_status":"not_started"}`, id, id))
		}
		if b.legacy {
			return reply{body: "[" + strings.Join(items, ",") + "]"}
		}
		return reply{body: fmt.Sprintf(`{"items":[%s],"total":%d,"limit":500,"offset":0}`, strings.Join(items, ","), len(b.docs))}
	}
	if rec.Method == "POST" && strings.HasSuffix(rec.Path, "/analyze") {
		id := strings.TrimSuffix(strings.TrimPrefix(rec.Path, "/api/documents/documents/"), "/analyze")
		b.analyzed = append(b.analyzed, id)
		if queue := b.replies[id]; len(queue) > 0 {
			next := queue[0]
			if len(queue) > 1 {
				b.replies[id] = queue[1:]
			}
			return next
		}
		return reply{status: 202, body: fmt.Sprintf(`{"id":%q,"analysis_status":"queued"}`, id)}
	}
	return reply{status: 404, body: `{"message":"unbekannt"}`}
}

type analyzeOut struct {
	Queued  []string         `json:"queued"`
	Skipped []map[string]any `json:"skipped"`
	Failed  []map[string]any `json:"failed"`
	Listed  *int             `json:"listed"`
}

func decodeAnalyze(t *testing.T, stdout string) analyzeOut {
	t.Helper()
	var out analyzeOut
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("Zusammenfassung als JSON erwartet: %v (%q)", err, stdout)
	}
	return out
}

func TestAnalyzeSeveralIDs(t *testing.T) {
	h := newHarness(t)
	b := &analyzeBackend{replies: map[string][]reply{
		"12": {{status: 409, body: `{"error":"Document analysis already in progress","code":"DOCUMENT_ANALYSIS_IN_PROGRESS","message":"Dieses Dokument ist bereits zur Analyse vorgemerkt oder wird analysiert."}`}},
	}}
	h.handle = b.handle
	code, stdout, stderr := h.run("documents", "analyze", "11", "12", "13")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if fmt.Sprint(b.analyzed) != "[11 12 13]" {
		t.Errorf("drei Analysen in Reihenfolge erwartet, got %v", b.analyzed)
	}
	out := decodeAnalyze(t, stdout)
	if fmt.Sprint(out.Queued) != "[11 13]" || len(out.Skipped) != 1 || out.Skipped[0]["id"] != "12" {
		t.Errorf("11/13 vorgemerkt, 12 übersprungen erwartet, got %+v", out)
	}
	if !strings.Contains(fmt.Sprint(out.Skipped[0]["reason"]), "bereits") {
		t.Errorf("Grund des Backends erwartet, got %v", out.Skipped[0])
	}
	if out.Failed == nil {
		t.Error("failed soll [] sein, nicht null")
	}
}

// TestAnalyzeSingleIDStaysARequest: Mit einer ID bleibt alles wie bisher —
// die Antwort des Backends unverändert, sein Exit-Code.
func TestAnalyzeSingleIDStaysARequest(t *testing.T) {
	h := newHarness(t)
	h.status = 409
	h.respond = `{"message":"läuft schon"}`
	code, stdout, _ := h.run("documents", "analyze", "11")
	if code != 9 || stdout != "" {
		t.Errorf("Exit 9 und leeres stdout erwartet, got %d %q", code, stdout)
	}
}

func TestAnalyzeAllListsThenQueues(t *testing.T) {
	h := newHarness(t)
	b := &analyzeBackend{docs: []string{"d1", "d2", "d3"}}
	h.handle = b.handle
	code, stdout, stderr := h.run("documents", "analyze-all", "--immobilie-id", "5")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(b.lists) != 1 {
		t.Fatalf("eine Listen-Seite erwartet, got %d", len(b.lists))
	}
	q := b.lists[0].Query
	if q.Get("immobilien_id") != "5" || q.Get("analysis_status") != "not_started" || q.Get("limit") != "500" {
		t.Errorf("Liste mit immobilien_id=5, analysis_status=not_started, limit=500 erwartet, got %v", q)
	}
	out := decodeAnalyze(t, stdout)
	if fmt.Sprint(out.Queued) != "[d1 d2 d3]" || out.Listed == nil || *out.Listed != 3 {
		t.Errorf("drei vorgemerkt, listed 3 erwartet, got %+v", out)
	}
}

func TestAnalyzeAllFilters(t *testing.T) {
	h := newHarness(t)
	b := &analyzeBackend{}
	h.handle = b.handle
	code, _, stderr := h.run("documents", "analyze-all", "--immobilie-id", "5",
		"--analysis-status", "failed", "--category", "mietvertrag", "--unit-id", "u-1")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	q := b.lists[0].Query
	if q.Get("analysis_status") != "failed" || q.Get("category") != "mietvertrag" || q.Get("unit_id") != "u-1" {
		t.Errorf("Filter erwartet, got %v", q)
	}
	if len(b.analyzed) != 0 {
		t.Error("ohne Treffer keine Analyse")
	}
}

func TestAnalyzeAllPacesOnRetryAfter(t *testing.T) {
	h := newHarness(t)
	b := &analyzeBackend{docs: []string{"d1", "d2"}, replies: map[string][]reply{
		"d2": {
			{status: 429, body: `{"message":"Rate limit"}`, header: http.Header{"Retry-After": {"5"}}},
			{status: 202, body: `{"id":"d2"}`},
		},
	}}
	h.handle = b.handle
	code, stdout, stderr := h.run("documents", "analyze-all", "--immobilie-id", "5")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(h.sleeps) != 1 || h.sleeps[0] != 5*time.Second {
		t.Errorf("Pause aus Retry-After erwartet, got %v", h.sleeps)
	}
	if out := decodeAnalyze(t, stdout); fmt.Sprint(out.Queued) != "[d1 d2]" {
		t.Errorf("beide vorgemerkt erwartet, got %+v", out)
	}
}

// TestAnalyzeAllStopsWhenRateLimitPersists: Bleibt das Limit, hört der Befehl
// auf, statt jedes weitere Dokument ins 429 laufen zu lassen. Ein zweiter Lauf
// später setzt fort (analysis_status=not_started).
func TestAnalyzeAllStopsWhenRateLimitPersists(t *testing.T) {
	h := newHarness(t)
	limited := reply{status: 429, body: `{"message":"Rate limit"}`, header: http.Header{"Retry-After": {"600"}}}
	b := &analyzeBackend{docs: []string{"d1", "d2", "d3"}, replies: map[string][]reply{"d2": {limited}}}
	h.handle = b.handle
	code, stdout, stderr := h.run("documents", "analyze-all", "--immobilie-id", "5")
	if code != 7 {
		t.Fatalf("Exit 7 erwartet, got %d (%s)", code, stderr)
	}
	out := decodeAnalyze(t, stdout)
	if fmt.Sprint(out.Queued) != "[d1]" || len(out.Skipped) != 2 {
		t.Fatalf("d1 vorgemerkt, d2/d3 übersprungen erwartet, got %+v", out)
	}
	if out.Skipped[0]["reason"] != "rate_limited" || out.Skipped[1]["id"] != "d3" {
		t.Errorf("Grund rate_limited erwartet, got %v", out.Skipped)
	}
	if fmt.Sprint(b.analyzed) != "[d1 d2 d2 d2]" {
		t.Errorf("d2 dreimal versucht, d3 nie, got %v", b.analyzed)
	}
	line := errorLine(t, stderr)
	if line["retry_after_seconds"] != float64(600) {
		t.Errorf("retry_after_seconds 600 erwartet, got %v", line)
	}
}

func TestAnalyzeAllStopsOnQuota(t *testing.T) {
	h := newHarness(t)
	b := &analyzeBackend{docs: []string{"d1", "d2", "d3"}, replies: map[string][]reply{
		"d2": {{status: 402, body: `{"message":"KI-Kontingent erschöpft","used":100,"limit":100}`}},
	}}
	h.handle = b.handle
	code, stdout, _ := h.run("documents", "analyze-all", "--immobilie-id", "5")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	out := decodeAnalyze(t, stdout)
	if len(out.Failed) != 1 || out.Failed[0]["status"] != float64(402) || len(out.Skipped) != 1 || out.Skipped[0]["id"] != "d3" {
		t.Errorf("d2 fehlgeschlagen (402), d3 übersprungen erwartet, got %+v", out)
	}
	if fmt.Sprint(b.analyzed) != "[d1 d2]" {
		t.Errorf("nach 402 kein weiterer Versuch erwartet, got %v", b.analyzed)
	}
}

func TestAnalyzeAllContinuesAfterSingleFailure(t *testing.T) {
	h := newHarness(t)
	b := &analyzeBackend{docs: []string{"d1", "d2", "d3"}, replies: map[string][]reply{
		"d2": {{status: 500, body: `{"message":"Document analysis could not be queued"}`}},
	}}
	h.handle = b.handle
	code, stdout, _ := h.run("documents", "analyze-all", "--immobilie-id", "5")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	out := decodeAnalyze(t, stdout)
	if fmt.Sprint(out.Queued) != "[d1 d3]" || len(out.Failed) != 1 || out.Failed[0]["message"] != "Document analysis could not be queued" {
		t.Errorf("d2 fehlgeschlagen, Rest vorgemerkt erwartet, got %+v", out)
	}
}

// TestAnalyzeAllRefusesUnfilteredList: Eine Instanz ohne Filter liefert ALLE
// Dokumente als Array — analysiert würde auch längst Analysiertes, auf
// Kosten des KI-Kontingents.
func TestAnalyzeAllRefusesUnfilteredList(t *testing.T) {
	h := newHarness(t)
	b := &analyzeBackend{docs: []string{"d1"}, legacy: true}
	h.handle = b.handle
	code, stdout, stderr := h.run("documents", "analyze-all", "--immobilie-id", "5")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d (%s)", code, stderr)
	}
	if len(b.analyzed) != 0 || stdout != "" {
		t.Errorf("keine Analyse und kein stdout erwartet, got %v %q", b.analyzed, stdout)
	}
}

func TestAnalyzeAllPolicyAndRequiredFlag(t *testing.T) {
	h := newHarness(t)
	if code, _, _ := h.run("--readonly", "documents", "analyze-all", "--immobilie-id", "5"); code != 6 {
		t.Errorf("--readonly soll analyze-all sperren, got %d", code)
	}
	h2 := newHarness(t)
	if code, _, _ := h2.run("documents", "analyze-all"); code != 2 {
		t.Errorf("ohne --immobilie-id Exit 2 erwartet, got %d", code)
	}
	if risk := riskForAPI("POST", "/api/documents/documents/d1/analyze"); risk != RiskWrite {
		t.Errorf("api POST …/analyze bleibt write, got %q", risk)
	}
}
