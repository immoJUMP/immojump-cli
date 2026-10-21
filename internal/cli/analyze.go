package cli

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/immoJUMP/immojump-cli/internal/api"
	"github.com/immoJUMP/immojump-cli/internal/config"
	"github.com/immoJUMP/immojump-cli/internal/output"
)

// analyzeSummary ist die Ausgabe von documents analyze (mehrere IDs) und
// analyze-all — Reihenfolge fest.
type analyzeSummary struct {
	Listed  *int             `json:"listed,omitempty"`
	Queued  []string         `json:"queued"`
	Skipped []map[string]any `json:"skipped"`
	Failed  []map[string]any `json:"failed"`
}

// queueAnalyses stößt die Analyse je Dokument an, eins nach dem anderen.
//
// Wiederholt wird bei 429/502/503/504 und Netzwerkfehlern (Retry-After hat
// Vorrang): Ein doppelter Aufruf ist harmlos, das Backend vergibt die Analyse
// je Dokument nur einmal und antwortet sonst 409. Bleibt das Rate Limit nach
// allen Versuchen, hört der Befehl auf — die übrigen Dokumente stehen unter
// skipped mit reason rate_limited, ein späterer Lauf setzt fort. 401 und 402
// (Kontingent) treffen jedes weitere Dokument genauso und beenden ebenfalls.
func (r *runner) queueAnalyses(client *api.Client, ids []string, body []byte, summary *analyzeSummary) error {
	for i, id := range ids {
		_, err := client.Do(api.Request{
			Method: "POST", Path: "/api/documents/documents/" + url.PathEscape(id) + "/analyze",
			Body: body, ContentType: "application/json", Idempotent: true,
		})
		if err == nil {
			summary.Queued = append(summary.Queued, id)
			continue
		}
		var apiErr *api.Error
		if !errors.As(err, &apiErr) {
			return err
		}
		switch {
		case apiErr.Status == 409:
			skip := map[string]any{"id": id, "reason": apiErr.Message}
			if apiErr.Code != "" {
				skip["code"] = apiErr.Code
			}
			summary.Skipped = append(summary.Skipped, skip)
		case apiErr.Status == 429:
			for _, rest := range ids[i:] {
				summary.Skipped = append(summary.Skipped, map[string]any{"id": rest, "reason": "rate_limited"})
			}
			stop := &api.Error{
				Status:  429,
				Message: fmt.Sprintf("Rate Limit nach %d von %d Dokumenten — die übrigen stehen unter skipped; später erneut starten.", i, len(ids)),
				Code:    "RATE_LIMITED",
				Details: map[string]any{"backend_message": apiErr.Message},
			}
			if apiErr.RetryAfter > 0 {
				stop.Details["retry_after_seconds"] = int(apiErr.RetryAfter.Seconds())
			}
			return stop
		case apiErr.Status == 401 || apiErr.Status == 402 || apiErr.Exit != 0:
			summary.Failed = append(summary.Failed, failedAnalysis(id, apiErr))
			for _, rest := range ids[i+1:] {
				summary.Skipped = append(summary.Skipped, map[string]any{
					"id": rest, "reason": fmt.Sprintf("abgebrochen nach HTTP %d: %s", apiErr.Status, apiErr.Message),
				})
			}
			return apiErr
		default:
			summary.Failed = append(summary.Failed, failedAnalysis(id, apiErr))
		}
	}
	return nil
}

func failedAnalysis(id string, err *api.Error) map[string]any {
	entry := map[string]any{"id": id, "status": err.Status, "message": err.Message}
	if err.Code != "" {
		entry["code"] = err.Code
	}
	return entry
}

// finishAnalyses schreibt die Zusammenfassung und wählt den Exit-Code: der
// Abbruchgrund, sonst Exit 1 bei Fehlschlägen, sonst 0.
func (r *runner) finishAnalyses(summary *analyzeSummary, stop error, flags *flagValues) int {
	raw, err := output.Marshal(summary)
	if err != nil {
		return r.fail(err)
	}
	if err := r.render(raw, "application/json", flags); err != nil {
		return r.fail(err)
	}
	if stop != nil {
		return r.fail(stop)
	}
	if len(summary.Failed) > 0 {
		return r.fail(&api.Error{
			Message: fmt.Sprintf("%d Analysen nicht gestartet — Details unter failed auf stdout.", len(summary.Failed)),
			Code:    "ANALYSIS_INCOMPLETE",
			Exit:    1,
		})
	}
	return 0
}

func newAnalyzeSummary() *analyzeSummary {
	return &analyzeSummary{Queued: []string{}, Skipped: []map[string]any{}, Failed: []map[string]any{}}
}

// runDocumentsAnalyzeMany: documents analyze mit mehreren IDs.
func (r *runner) runDocumentsAnalyzeMany(spec Spec, ids []string, flags *flagValues, resolved config.Resolved) int {
	body, err := r.buildJSONBody(spec, "POST", flags)
	if err != nil {
		return r.fail(err)
	}
	client, err := r.newClient(resolved, flags)
	if err != nil {
		return r.fail(err)
	}
	summary := newAnalyzeSummary()
	stop := r.queueAnalyses(client, ids, body, summary)
	return r.finishAnalyses(summary, stop, flags)
}

// runDocumentsAnalyzeAll: passende Dokumente listen (alle Seiten), dann je
// Dokument die Analyse anstoßen.
func (r *runner) runDocumentsAnalyzeAll(flags *flagValues, resolved config.Resolved) int {
	client, err := r.newClient(resolved, flags)
	if err != nil {
		return r.fail(err)
	}
	status := strings.TrimSpace(flags.get("analysis-status"))
	if status == "" {
		status = "not_started"
	}
	query := url.Values{
		"immobilien_id":   {strings.TrimSpace(flags.get("immobilie-id"))},
		"analysis_status": {status},
	}
	if category := strings.TrimSpace(flags.get("category")); category != "" {
		query.Set("category", category)
	}
	if unit := strings.TrimSpace(flags.get("unit-id")); unit != "" {
		query.Set("unit_id", unit)
	}
	items, isArray, err := fetchAllPages(client, &api.Request{Method: "GET", Path: "/api/documents/documents", Query: query}, maxPageSize, 0)
	if err != nil {
		return r.fail(err)
	}
	if isArray {
		// Ohne Umschlag hat die Instanz die Filter nicht gelesen: Die Liste
		// enthält auch längst analysierte Dokumente.
		return r.fail(&api.Error{
			Message: "Die Dokumentliste dieser Instanz kennt die Filter nicht (Antwort ohne Umschlag) — abgebrochen, " +
				"damit nicht alle Dokumente auf Kosten des KI-Kontingents analysiert werden.",
			Code: "FILTER_UNSUPPORTED",
			Exit: 1,
		})
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		var doc map[string]any
		if err := decodeJSONExact(item, &doc); err != nil {
			return r.fail(&api.Error{Message: "Dokumentliste enthält kein JSON-Objekt: " + err.Error(), Exit: 1})
		}
		if id := stringID(doc["id"]); id != "" {
			ids = append(ids, id)
		}
	}
	summary := newAnalyzeSummary()
	listed := len(ids)
	summary.Listed = &listed
	stop := r.queueAnalyses(client, ids, []byte(`{}`), summary)
	return r.finishAnalyses(summary, stop, flags)
}
