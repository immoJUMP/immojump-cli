package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fieldsBackend simuliert Einheitenliste, Definitionen und Feldwerte.
type fieldsBackend struct {
	units  []string // IDs; extern_id ist "RH-<i>"
	values map[string]map[string]any
	puts   []string
	putFn  func(n int, body string) reply
}

func (b *fieldsBackend) handle(rec *recorded) reply {
	switch {
	case strings.HasPrefix(rec.Path, "/api/units/immobilie/"):
		items := []string{}
		for i, id := range b.units {
			items = append(items, fmt.Sprintf(`{"id":%q,"extern_id":"RH-%d","einheit":"WE %d","ist_rent":500}`, id, i, i))
		}
		return reply{body: fmt.Sprintf(`{"items":[%s],"total":%d,"limit":500,"offset":0}`, strings.Join(items, ","), len(b.units))}
	case rec.Path == "/api/custom-fields/definitions":
		return reply{body: `{"definitions":[{"id":"d1","name":"Marktmiete","type":"number"},{"id":"d2","name":"Sanierung","type":"boolean"}]}`}
	case rec.Path == "/api/custom-fields/values/batch/read" && rec.Method == "POST":
		var payload struct {
			Model     string   `json:"model"`
			TargetIDs []string `json:"target_ids"`
		}
		_ = json.Unmarshal([]byte(rec.Body), &payload)
		if payload.Model != "unit" || len(payload.TargetIDs) == 0 || len(payload.TargetIDs) > 1000 {
			return reply{status: 400, body: `{"message":"falscher Lese-Body"}`}
		}
		out := map[string]any{}
		for _, id := range payload.TargetIDs {
			if v, ok := b.values[id]; ok {
				out[id] = v
			} else {
				out[id] = map[string]any{}
			}
		}
		raw, _ := json.Marshal(map[string]any{"values": out})
		return reply{body: string(raw)}
	case rec.Path == "/api/custom-fields/values/batch" && rec.Method == "PUT":
		b.puts = append(b.puts, rec.Body)
		if b.putFn != nil {
			return b.putFn(len(b.puts), rec.Body)
		}
		var payload struct {
			Items []any `json:"items"`
		}
		_ = json.Unmarshal([]byte(rec.Body), &payload)
		return reply{body: fmt.Sprintf(`{"success":true,"updated":%d}`, len(payload.Items))}
	}
	return reply{status: 404, body: `{"message":"unbekannt"}`}
}

func requestsTo(h *harness, method, path string) []*recorded {
	var out []*recorded
	for _, rec := range h.requests {
		if rec.Method == method && rec.Path == path {
			out = append(out, rec)
		}
	}
	return out
}

func TestUnitsFieldsGetMapsDefinitionIDsToNames(t *testing.T) {
	h := newHarness(t)
	b := &fieldsBackend{
		units: []string{"u0", "u1", "u2"},
		values: map[string]map[string]any{
			"u0": {"d1": 12.5},
			"u2": {"d2": true, "d9": "verwaist"},
		},
	}
	h.handle = b.handle
	code, stdout, stderr := h.run("units", "fields-get", "5")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	want := `[{"id":"u0","extern_id":"RH-0","einheit":"WE 0","values":{"Marktmiete":12.5}},` +
		`{"id":"u1","extern_id":"RH-1","einheit":"WE 1","values":{}},` +
		`{"id":"u2","extern_id":"RH-2","einheit":"WE 2","values":{"Sanierung":true,"d9":"verwaist"}}]`
	if !sameJSON(t, stdout, want) {
		t.Errorf("Ausgabe\n got: %s\nwant: %s", stdout, want)
	}
	defs := requestsTo(h, "GET", "/api/custom-fields/definitions")
	if len(defs) != 1 || defs[0].Query.Get("organisation_id") != "org-test" || defs[0].Query.Get("model") != "unit" {
		t.Errorf("Definitionen mit organisation_id und model=unit erwartet, got %v", defs)
	}
	vals := requestsTo(h, "POST", "/api/custom-fields/values/batch/read")
	if len(vals) != 1 || !sameJSON(t, vals[0].Body, `{"model":"unit","target_ids":["u0","u1","u2"]}`) {
		t.Errorf("ein Lese-POST mit model und target_ids im Body erwartet, got %v", vals)
	}
	if len(vals) == 1 && len(vals[0].Query) != 0 {
		t.Errorf("die IDs gehören in den Body, nicht in die Query: %v", vals[0].Query)
	}
}

// TestUnitsFieldsGetChunksByThousand: Die Lese-Route nimmt höchstens 1000
// IDs je Aufruf — im Body, damit die Request-Zeile kurz bleibt.
func TestUnitsFieldsGetChunksByThousand(t *testing.T) {
	h := newHarness(t)
	b := &fieldsBackend{}
	for i := 0; i < 2500; i++ {
		b.units = append(b.units, fmt.Sprintf("00000000-0000-4000-8000-%012d", i))
	}
	h.handle = b.handle
	code, stdout, stderr := h.run("units", "fields-get", "5")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	sizes := []int{}
	for _, rec := range requestsTo(h, "POST", "/api/custom-fields/values/batch/read") {
		var payload struct {
			TargetIDs []string `json:"target_ids"`
		}
		_ = json.Unmarshal([]byte(rec.Body), &payload)
		sizes = append(sizes, len(payload.TargetIDs))
	}
	if fmt.Sprint(sizes) != "[1000 1000 500]" {
		t.Errorf("Stapel 1000/1000/500 erwartet, got %v", sizes)
	}
	if items := decodeArray(t, stdout); len(items) != 2500 {
		t.Errorf("2500 Zeilen erwartet, got %d", len(items))
	}
}

// TestUnitsFieldsGetRetriesTheReadPOST: Der POST liest nur — Wiederholen ist
// sicher, und als read darf er auch unter --readonly laufen.
func TestUnitsFieldsGetRetriesTheReadPOST(t *testing.T) {
	h := newHarness(t)
	b := &fieldsBackend{units: []string{"u0"}}
	calls := 0
	h.handle = func(rec *recorded) reply {
		if rec.Path == "/api/custom-fields/values/batch/read" {
			calls++
			if calls == 1 {
				return reply{status: 503, body: `{"message":"kurz weg"}`}
			}
		}
		return b.handle(rec)
	}
	if code, _, stderr := h.run("--readonly", "units", "fields-get", "5"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if calls != 2 {
		t.Errorf("zwei Lese-Versuche erwartet, got %d", calls)
	}
	if risk := riskForAPI("POST", "/api/custom-fields/values/batch/read"); risk != RiskRead {
		t.Errorf("api POST …/values/batch/read soll read sein, got %q", risk)
	}
}

func TestUnitsFieldsGetEmptyHouse(t *testing.T) {
	h := newHarness(t)
	b := &fieldsBackend{}
	h.handle = b.handle
	code, stdout, _ := h.run("units", "fields-get", "5")
	if code != 0 || strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("leeres Array erwartet, got %d %q", code, stdout)
	}
	if len(requestsTo(h, "POST", "/api/custom-fields/values/batch/read")) != 0 {
		t.Error("ohne Einheiten kein Werte-Aufruf erwartet")
	}
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestUnitsFieldsSetResolvesExternIDs(t *testing.T) {
	h := newHarness(t)
	b := &fieldsBackend{units: []string{"u0", "u1", "u2"}}
	h.handle = b.handle
	file := writeTemp(t, "werte.ndjson",
		`{"extern_id":"RH-0","values":{"Marktmiete":13.50}}`+"\n\n"+
			`{"id":"u2","values":{"Sanierung":null}}`+"\n")
	code, stdout, stderr := h.run("units", "fields-set", "5", "--file", file)
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(b.puts) != 1 {
		t.Fatalf("ein PUT erwartet, got %d", len(b.puts))
	}
	want := `{"model":"unit","items":[{"target_id":"u0","values":{"Marktmiete":13.50}},{"target_id":"u2","values":{"Sanierung":null}}]}`
	if !sameJSON(t, b.puts[0], want) {
		t.Errorf("PUT-Body\n got: %s\nwant: %s", b.puts[0], want)
	}
	if !strings.Contains(b.puts[0], "13.50") {
		t.Errorf("Zahl unverfälscht erwartet, got %s", b.puts[0])
	}
	if !sameJSON(t, stdout, `{"updated":2,"batches":1}`) {
		t.Errorf("Summe erwartet, got %s", stdout)
	}
}

func TestUnitsFieldsSetJSONArray(t *testing.T) {
	h := newHarness(t)
	b := &fieldsBackend{units: []string{"u0"}}
	h.handle = b.handle
	h.stdin = `[{"extern_id":"RH-0","values":{"d1":1}}]`
	code, _, stderr := h.run("units", "fields-set", "5", "--file", "-")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(b.puts) != 1 || !strings.Contains(b.puts[0], `"target_id":"u0"`) {
		t.Errorf("PUT mit u0 erwartet, got %v", b.puts)
	}
}

func TestUnitsFieldsSetRejectsUnknownUnitsBeforeSending(t *testing.T) {
	h := newHarness(t)
	b := &fieldsBackend{units: []string{"u0"}}
	h.handle = b.handle
	file := writeTemp(t, "werte.ndjson",
		`{"extern_id":"RH-0","values":{"a":1}}`+"\n"+
			`{"extern_id":"RH-77","values":{"a":1}}`+"\n"+
			`{"id":"fremd","values":{"a":1}}`+"\n")
	code, _, stderr := h.run("units", "fields-set", "5", "--file", file)
	if code != 11 {
		t.Fatalf("Exit 11 erwartet, got %d (%s)", code, stderr)
	}
	if len(b.puts) != 0 {
		t.Error("bei unbekannten Einheiten darf nichts geschrieben werden")
	}
	line := errorLine(t, stderr)
	if fmt.Sprint(line["unknown_extern_ids"]) != "[RH-77]" || fmt.Sprint(line["unknown_ids"]) != "[fremd]" {
		t.Errorf("unbekannte extern_id und id sollen genannt werden, got %v", line)
	}
}

func TestUnitsFieldsSetRejectsMalformedRecords(t *testing.T) {
	cases := map[string]string{
		"ohne Schlüssel":     `{"values":{"a":1}}`,
		"ohne values":        `{"extern_id":"RH-0"}`,
		"values kein Objekt": `{"extern_id":"RH-0","values":[1]}`,
		"kein JSON":          `{kaputt`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			b := &fieldsBackend{units: []string{"u0"}}
			h.handle = b.handle
			file := writeTemp(t, "w.ndjson", `{"extern_id":"RH-0","values":{"a":1}}`+"\n"+content+"\n")
			code, _, stderr := h.run("units", "fields-set", "5", "--file", file)
			if code != 2 {
				t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
			}
			if len(h.requests) != 0 {
				t.Error("bei kaputter Eingabe kein Request erwartet")
			}
			if msg, _ := errorLine(t, stderr)["message"].(string); !strings.Contains(msg, "Zeile 2") {
				t.Errorf("Meldung soll die Zeile nennen, got %q", msg)
			}
		})
	}
}

func TestUnitsFieldsSetBatchesAndShiftsErrorIndex(t *testing.T) {
	h := newHarness(t)
	b := &fieldsBackend{}
	lines := []string{}
	for i := 0; i < 600; i++ {
		b.units = append(b.units, fmt.Sprintf("u%d", i))
		lines = append(lines, fmt.Sprintf(`{"extern_id":"RH-%d","values":{"Marktmiete":%d}}`, i, i))
	}
	b.putFn = func(n int, body string) reply {
		if n == 2 {
			return reply{status: 400, body: `{"message":"1 von 100 Einträgen sind ungültig, es wurde nichts gespeichert. Eintrag 3: Ungültiger Wert.",` +
				`"errors":[{"index":3,"target_id":"u503","message":"Ungültiger Wert."}]}`}
		}
		return reply{body: `{"success":true,"updated":500}`}
	}
	h.handle = b.handle
	file := writeTemp(t, "w.ndjson", strings.Join(lines, "\n"))
	code, stdout, stderr := h.run("units", "fields-set", "5", "--file", file)
	if code != 11 {
		t.Fatalf("Exit 11 erwartet, got %d (%s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout soll leer bleiben, got %q", stdout)
	}
	if len(b.puts) != 2 {
		t.Fatalf("zwei Stapel erwartet, got %d", len(b.puts))
	}
	line := errorLine(t, stderr)
	errs, _ := line["errors"].([]any)
	first, _ := errs[0].(map[string]any)
	if first["index"] != float64(503) || first["batch_index"] != float64(3) || first["line"] != float64(504) {
		t.Errorf("index 503 (Datei), batch_index 3, line 504 erwartet, got %v", first)
	}
	if line["committed_batches"] != float64(1) || line["batch"] != float64(2) || line["batch_offset"] != float64(500) {
		t.Errorf("committed_batches/batch/batch_offset erwartet, got %v", line)
	}
	if !strings.HasPrefix(line["message"].(string), "1 von 100 Einträgen") {
		t.Errorf("Backend-Meldung unverändert erwartet, got %q", line["message"])
	}
}

func TestUnitsFieldsSetRetriesBatches(t *testing.T) {
	h := newHarness(t)
	b := &fieldsBackend{units: []string{"u0"}}
	b.putFn = func(n int, body string) reply {
		if n == 1 {
			return reply{status: 503, body: `{"message":"kurz weg"}`}
		}
		return reply{body: `{"success":true,"updated":1}`}
	}
	h.handle = b.handle
	file := writeTemp(t, "w.ndjson", `{"extern_id":"RH-0","values":{"a":1}}`)
	code, stdout, stderr := h.run("units", "fields-set", "5", "--file", file)
	if code != 0 {
		t.Fatalf("Exit 0 nach Wiederholung erwartet, got %d (%s)", code, stderr)
	}
	if len(b.puts) != 2 || !sameJSON(t, stdout, `{"updated":1,"batches":1}`) {
		t.Errorf("zwei PUT-Versuche und Summe erwartet, got %d / %s", len(b.puts), stdout)
	}
}

func TestUnitsFieldsPolicy(t *testing.T) {
	get, _ := Lookup("units", "fields-get")
	set, _ := Lookup("units", "fields-set")
	if get.Risk != RiskRead || set.Risk != RiskWrite {
		t.Errorf("fields-get read und fields-set write erwartet, got %q/%q", get.Risk, set.Risk)
	}
	h := newHarness(t)
	file := writeTemp(t, "w.ndjson", `{"extern_id":"RH-0","values":{"a":1}}`)
	if code, _, _ := h.run("--readonly", "units", "fields-set", "5", "--file", file); code != 6 {
		t.Errorf("--readonly soll fields-set sperren, got %d", code)
	}
	// Über den Escape-Hatch bleibt der Stapel-PUT write, nicht external.
	if risk := riskForAPI("PUT", "/api/custom-fields/values/batch"); risk != RiskWrite {
		t.Errorf("api PUT values/batch soll write sein, got %q", risk)
	}
}
