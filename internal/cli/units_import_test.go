package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// bulkBackend beantwortet POST …/units/bulk wie das Backend: jede Einheit mit
// extern_id gilt als neu angelegt.
type bulkBackend struct {
	bodies []string
	fail   func(n int) *reply
}

func (b *bulkBackend) handle(rec *recorded) reply {
	if rec.Method != "POST" || rec.Path != "/api/units/immobilie/5/units/bulk" {
		return reply{status: 404, body: `{"message":"falsche Route"}`}
	}
	b.bodies = append(b.bodies, rec.Body)
	if b.fail != nil {
		if out := b.fail(len(b.bodies)); out != nil {
			return *out
		}
	}
	var payload struct {
		Units  []map[string]any `json:"units"`
		DryRun bool             `json:"dry_run"`
	}
	_ = json.Unmarshal([]byte(rec.Body), &payload)
	items := []string{}
	for i, unit := range payload.Units {
		items = append(items, fmt.Sprintf(`{"index":%d,"id":"id-%v","extern_id":%q,"action":"created"}`, i, unit["extern_id"], fmt.Sprint(unit["extern_id"])))
	}
	return reply{body: fmt.Sprintf(`{"created":%d,"updated":0,"unchanged":0,"placeholder_removed":%v,"dry_run":%v,"items":[%s]}`,
		len(payload.Units), len(b.bodies) == 1, payload.DryRun, strings.Join(items, ","))}
}

func ndjsonUnits(n int) string {
	lines := make([]string, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, fmt.Sprintf(`{"extern_id":"RH-01-WE-%03d","einheit":"WE %d","livingspace":62.5,"ist_rent":540}`, i, i))
	}
	return strings.Join(lines, "\n") + "\n"
}

func TestUnitsImportSendsOneBatch(t *testing.T) {
	h := newHarness(t)
	b := &bulkBackend{}
	h.handle = b.handle
	file := writeTemp(t, "mieter.ndjson", ndjsonUnits(3))
	code, stdout, stderr := h.run("units", "import", "5", "--file", file)
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(b.bodies) != 1 {
		t.Fatalf("ein Stapel erwartet, got %d", len(b.bodies))
	}
	want := `{"units":[` +
		`{"extern_id":"RH-01-WE-000","einheit":"WE 0","livingspace":62.5,"ist_rent":540},` +
		`{"extern_id":"RH-01-WE-001","einheit":"WE 1","livingspace":62.5,"ist_rent":540},` +
		`{"extern_id":"RH-01-WE-002","einheit":"WE 2","livingspace":62.5,"ist_rent":540}],"dry_run":false}`
	if !sameJSON(t, b.bodies[0], want) {
		t.Errorf("Body\n got: %s\nwant: %s", b.bodies[0], want)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("JSON erwartet: %v", err)
	}
	if out["created"] != float64(3) || out["batches"] != float64(1) || out["placeholder_removed"] != true || out["dry_run"] != false {
		t.Errorf("Summe falsch: %v", out)
	}
	if !strings.HasPrefix(stdout, `{"created":3,"updated":0,"unchanged":0,"placeholder_removed":true,"dry_run":false,"batches":1,"items":[{"index":0,`) {
		t.Errorf("feste Feldreihenfolge erwartet, got %s", stdout)
	}
	if stderr != "" {
		t.Errorf("stderr soll leer bleiben, got %q", stderr)
	}
}

func TestUnitsImportBatchesAndAggregates(t *testing.T) {
	h := newHarness(t)
	b := &bulkBackend{}
	h.handle = b.handle
	file := writeTemp(t, "mieter.ndjson", ndjsonUnits(1203))
	code, stdout, stderr := h.run("units", "import", "5", "--file", file, "--dry-run")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	sizes := []int{}
	for _, body := range b.bodies {
		var payload struct {
			Units  []any `json:"units"`
			DryRun bool  `json:"dry_run"`
		}
		_ = json.Unmarshal([]byte(body), &payload)
		sizes = append(sizes, len(payload.Units))
		if !payload.DryRun {
			t.Error("--dry-run soll in jedem Stapel stehen")
		}
	}
	if fmt.Sprint(sizes) != "[500 500 203]" {
		t.Errorf("Stapel 500/500/203 erwartet, got %v", sizes)
	}
	var out struct {
		Created int  `json:"created"`
		Batches int  `json:"batches"`
		DryRun  bool `json:"dry_run"`
		Items   []struct {
			Index    int    `json:"index"`
			ExternID string `json:"extern_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("JSON erwartet: %v", err)
	}
	if out.Created != 1203 || out.Batches != 3 || !out.DryRun || len(out.Items) != 1203 {
		t.Errorf("Summe über alle Stapel erwartet, got %+v", out)
	}
	if out.Items[1202].Index != 1202 || out.Items[1202].ExternID != "RH-01-WE-1202" {
		t.Errorf("index soll in die Datei zeigen, got %+v", out.Items[1202])
	}
}

func TestUnitsImportBatchSize(t *testing.T) {
	h := newHarness(t)
	b := &bulkBackend{}
	h.handle = b.handle
	file := writeTemp(t, "mieter.ndjson", ndjsonUnits(5))
	if code, _, stderr := h.run("units", "import", "5", "--file", file, "--batch-size", "2"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(b.bodies) != 3 {
		t.Errorf("drei Stapel erwartet, got %d", len(b.bodies))
	}
	for _, value := range []string{"0", "501", "viele"} {
		h := newHarness(t)
		if code, _, _ := h.run("units", "import", "5", "--file", file, "--batch-size", value); code != 2 {
			t.Errorf("--batch-size %s: Exit 2 erwartet, got %d", value, code)
		}
		if h.last != nil {
			t.Errorf("--batch-size %s: kein Request erwartet", value)
		}
	}
}

func TestUnitsImportJSONInputs(t *testing.T) {
	for name, content := range map[string]string{
		"array":    `[{"extern_id":"A"},{"extern_id":"B"}]`,
		"umschlag": `{"units":[{"extern_id":"A"},{"extern_id":"B"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			b := &bulkBackend{}
			h.handle = b.handle
			file := writeTemp(t, "mieter.json", content)
			if code, _, stderr := h.run("units", "import", "5", "--file", file); code != 0 {
				t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
			}
			if !sameJSON(t, b.bodies[0], `{"units":[{"extern_id":"A"},{"extern_id":"B"}],"dry_run":false}`) {
				t.Errorf("Body falsch: %s", b.bodies[0])
			}
		})
	}
}

func TestUnitsImportFromStdinSniffsFormat(t *testing.T) {
	h := newHarness(t)
	b := &bulkBackend{}
	h.handle = b.handle
	h.stdin = ndjsonUnits(2)
	if code, _, stderr := h.run("units", "import", "5", "--file", "-"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	h2 := newHarness(t)
	h2.stdin = "extern_id,einheit\nA,WE 1\n"
	if code, _, _ := h2.run("units", "import", "5", "--file", "-"); code != 2 {
		t.Errorf("CSV über stdin ohne --format: Exit 2 erwartet, got %d", code)
	}
	h3 := newHarness(t)
	b3 := &bulkBackend{}
	h3.handle = b3.handle
	h3.stdin = "extern_id,einheit\nA,WE 1\n"
	if code, _, stderr := h3.run("units", "import", "5", "--file", "-", "--format", "csv"); code != 0 {
		t.Fatalf("--format csv: Exit 0 erwartet, got %d (%s)", code, stderr)
	}
}

func TestUnitsImportCSV(t *testing.T) {
	h := newHarness(t)
	b := &bulkBackend{}
	h.handle = b.handle
	csv := "extern_id,einheit,type,livingspace,rooms,ist_rent,soll_rent,note,lease_start_date\n" +
		"001,WE 1 EG links,wohnen,62.5,2,540,610,\"Müller, Anna\",01.04.2019\n" +
		"002,WE 2,wohnen,48,,0,,Leerstand,\n"
	file := writeTemp(t, "mieter.csv", csv)
	if code, _, stderr := h.run("units", "import", "5", "--file", file); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	want := `{"units":[` +
		`{"extern_id":"001","einheit":"WE 1 EG links","type":"wohnen","livingspace":62.5,"rooms":2,"ist_rent":540,"soll_rent":610,"note":"Müller, Anna","lease_start_date":"01.04.2019"},` +
		`{"extern_id":"002","einheit":"WE 2","type":"wohnen","livingspace":48,"ist_rent":0,"note":"Leerstand"}` +
		`],"dry_run":false}`
	if !sameJSON(t, b.bodies[0], want) {
		t.Errorf("Body\n got: %s\nwant: %s", b.bodies[0], want)
	}
}

func TestUnitsImportCSVGerman(t *testing.T) {
	h := newHarness(t)
	b := &bulkBackend{}
	h.handle = b.handle
	csv := "\uFEFFextern_id;einheit;livingspace;ist_rent\nRH-1;WE 1;62,5;1.250,50\n"
	file := writeTemp(t, "mieter.csv", csv)
	if code, _, stderr := h.run("units", "import", "5", "--file", file, "--decimal-comma"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if !sameJSON(t, b.bodies[0], `{"units":[{"extern_id":"RH-1","einheit":"WE 1","livingspace":62.5,"ist_rent":1250.5}],"dry_run":false}`) {
		t.Errorf("Semikolon und Dezimalkomma erwartet, got %s", b.bodies[0])
	}
	if !strings.Contains(b.bodies[0], `"ist_rent":1250.50`) && !strings.Contains(b.bodies[0], `"ist_rent":1250.5`) {
		t.Errorf("ist_rent als Zahl erwartet, got %s", b.bodies[0])
	}

	h2 := newHarness(t)
	b2 := &bulkBackend{}
	h2.handle = b2.handle
	file2 := writeTemp(t, "m.csv", "extern_id|einheit\nA|WE 1\n")
	if code, _, _ := h2.run("units", "import", "5", "--file", file2, "--delimiter", "|"); code != 2 {
		t.Errorf("--delimiter | ist nicht vorgesehen: Exit 2 erwartet, got %d", code)
	}
	file3 := writeTemp(t, "m.csv", "extern_id,einheit;x\nA,WE 1;y\n")
	if code, _, stderr := h2.run("units", "import", "5", "--file", file3, "--delimiter", ","); code != 0 {
		t.Fatalf("--delimiter , : Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if !sameJSON(t, b2.bodies[0], `{"units":[{"extern_id":"A","einheit;x":"WE 1;y"}],"dry_run":false}`) {
		t.Errorf("explizites Trennzeichen soll gelten, got %s", b2.bodies[0])
	}
}

// TestUnitsImportCSVNumbersAreStrict: Ein Zahlenformat, das nicht zum
// Schalter passt, wird abgelehnt statt umgedeutet — „62.5“ mit
// --decimal-comma war 625, „1.250“ in einer deutschen CSV ohne Schalter 1,25.
func TestUnitsImportCSVNumbersAreStrict(t *testing.T) {
	cases := []struct {
		name, csv string
		args      []string
		ok        bool
		want      string
	}{
		{"Dezimalkomma", "extern_id;ist_rent\nA;62,5\n", []string{"--decimal-comma"}, true, `"ist_rent":62.5`},
		{"Tausenderpunkt mit Komma", "extern_id;ist_rent\nA;1.250,50\n", []string{"--decimal-comma"}, true, `"ist_rent":1250.50`},
		{"Tausenderpunkt ohne Komma", "extern_id;ist_rent\nA;12.500\n", []string{"--decimal-comma"}, true, `"ist_rent":12500`},
		{"negativ mit Komma", "extern_id;ist_rent\nA;-3,5\n", []string{"--decimal-comma"}, true, `"ist_rent":-3.5`},
		{"Punkt mit --decimal-comma", "extern_id;ist_rent\nA;62.5\n", []string{"--decimal-comma"}, false, ""},
		{"zwei Punkte", "extern_id;ist_rent\nA;1.2.3\n", []string{"--decimal-comma"}, false, ""},
		{"Leerzeichen in der Zahl", "extern_id;ist_rent\nA;1 250,50\n", []string{"--decimal-comma"}, false, ""},
		{"Punkt ohne Schalter", "extern_id,ist_rent\nA,62.5\n", nil, true, `"ist_rent":62.5`},
		{"Komma ohne Schalter", "extern_id;ist_rent\nA;62,5\n", nil, false, ""},
		{"deutscher Tausender ohne Schalter", "extern_id;ist_rent\nA;1.250\n", nil, false, ""},
		{"negativer deutscher Tausender ohne Schalter", "extern_id;ist_rent\nA;-1.250\n", nil, false, ""},
		{"negativer Tausender mit Schalter", "extern_id;ist_rent\nA;-1.250\n", []string{"--decimal-comma"}, true, `"ist_rent":-1250`},
		{"Tausender-Form mit Komma-Trenner bleibt Dezimalpunkt", "extern_id,ist_rent\nA,1.250\n", nil, true, `"ist_rent":1.250`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			b := &bulkBackend{}
			h.handle = b.handle
			file := writeTemp(t, "mieter.csv", tc.csv)
			code, _, stderr := h.run(append([]string{"units", "import", "5", "--file", file}, tc.args...)...)
			if tc.ok {
				if code != 0 {
					t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
				}
				if !strings.Contains(b.bodies[0], tc.want) {
					t.Errorf("%s erwartet, got %s", tc.want, b.bodies[0])
				}
				return
			}
			if code != 11 {
				t.Fatalf("Exit 11 erwartet, got %d (%s)", code, stderr)
			}
			if len(b.bodies) != 0 {
				t.Error("vor dem ersten Stapel abbrechen")
			}
			msg, _ := errorLine(t, stderr)["message"].(string)
			if !strings.Contains(msg, "Zeile 2") || !strings.Contains(msg, "ist_rent") {
				t.Errorf("Meldung soll Zeile und Spalte nennen, got %q", msg)
			}
		})
	}
}

func TestUnitsImportCSVGermanThousandsHintsAtDecimalComma(t *testing.T) {
	h := newHarness(t)
	file := writeTemp(t, "mieter.csv", "extern_id;ist_rent\nA;1.250\n")
	_, _, stderr := h.run("units", "import", "5", "--file", file)
	if msg, _ := errorLine(t, stderr)["message"].(string); !strings.Contains(msg, "--decimal-comma") {
		t.Errorf("Hinweis auf --decimal-comma erwartet, got %q", msg)
	}
}

// TestUnitsImportRejectsDuplicateExternIDsAcrossBatches: Das Backend erkennt
// doppelte extern_id nur innerhalb eines Stapels. Über Stapel hinweg würde
// der zweite Eintrag den ersten still überschreiben.
func TestUnitsImportRejectsDuplicateExternIDsAcrossBatches(t *testing.T) {
	h := newHarness(t)
	content := ndjsonUnits(3) + `{"extern_id":"RH-01-WE-001","einheit":"doppelt"}` + "\n"
	file := writeTemp(t, "mieter.ndjson", content)
	code, _, stderr := h.run("units", "import", "5", "--file", file, "--batch-size", "2")
	if code != 11 {
		t.Fatalf("Exit 11 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("vor dem ersten Stapel abbrechen")
	}
	line := errorLine(t, stderr)
	errs, _ := line["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("ein Fehler je Duplikat erwartet, got %v", line)
	}
	entry := errs[0].(map[string]any)
	if entry["index"] != float64(3) || entry["extern_id"] != "RH-01-WE-001" || entry["line"] != float64(4) {
		t.Errorf("Duplikat mit index 3, Zeile 4 erwartet, got %v", entry)
	}
}

func TestUnitsImportReportsFailedBatch(t *testing.T) {
	h := newHarness(t)
	b := &bulkBackend{fail: func(n int) *reply {
		if n == 2 {
			return &reply{status: 400, body: `{"message":"1 von 2 Einheiten sind ungültig, es wurde nichts gespeichert. Eintrag 1: livingspace: keine Zahl",` +
				`"errors":[{"index":1,"extern_id":"RH-01-WE-003","message":"livingspace: keine Zahl","errors":{"livingspace":["Not a valid number."]}}]}`}
		}
		return nil
	}}
	h.handle = b.handle
	file := writeTemp(t, "mieter.ndjson", ndjsonUnits(5))
	code, stdout, stderr := h.run("units", "import", "5", "--file", file, "--batch-size", "2")
	if code != 11 {
		t.Fatalf("Exit 11 erwartet, got %d (%s)", code, stderr)
	}
	// Stapel 1 ist gespeichert — seine Summe gehört auf stdout.
	var partial struct {
		Created  int   `json:"created"`
		Batches  int   `json:"batches"`
		Complete bool  `json:"complete"`
		Items    []any `json:"items"`
	}
	if err := json.Unmarshal([]byte(stdout), &partial); err != nil {
		t.Fatalf("Teilsumme auf stdout erwartet: %v (%q)", err, stdout)
	}
	if partial.Created != 2 || partial.Batches != 1 || partial.Complete || len(partial.Items) != 2 {
		t.Errorf("Teilsumme von Stapel 1 mit complete=false erwartet, got %+v", partial)
	}
	if len(b.bodies) != 2 {
		t.Errorf("nach dem Fehler keine weiteren Stapel, got %d", len(b.bodies))
	}
	line := errorLine(t, stderr)
	if line["committed_batches"] != float64(1) || line["batch"] != float64(2) || line["batch_offset"] != float64(2) {
		t.Errorf("Stapel-Lage erwartet, got %v", line)
	}
	entry := line["errors"].([]any)[0].(map[string]any)
	if entry["index"] != float64(3) || entry["batch_index"] != float64(1) || entry["line"] != float64(4) {
		t.Errorf("index 3 in der Datei erwartet, got %v", entry)
	}
	if _, ok := entry["errors"]; !ok {
		t.Error("Feldfehler des Backends sollen erhalten bleiben")
	}
}

func TestUnitsImportRetriesIdempotentBatches(t *testing.T) {
	h := newHarness(t)
	b := &bulkBackend{fail: func(n int) *reply {
		if n == 1 {
			return &reply{status: 503, body: `{"message":"kurz weg"}`}
		}
		return nil
	}}
	h.handle = b.handle
	file := writeTemp(t, "mieter.ndjson", ndjsonUnits(2))
	code, _, stderr := h.run("units", "import", "5", "--file", file)
	if code != 0 {
		t.Fatalf("Exit 0 nach Wiederholung erwartet, got %d (%s)", code, stderr)
	}
	if len(b.bodies) != 2 || b.bodies[0] != b.bodies[1] {
		t.Errorf("derselbe Stapel zweimal erwartet, got %d", len(b.bodies))
	}
}

// TestUnitsImportWithoutKeysIsNotRetried: Ohne extern_id/id legt jeder
// Versuch neu an — eine Wiederholung nach verlorener Antwort verdoppelte.
func TestUnitsImportWithoutKeysIsNotRetried(t *testing.T) {
	h := newHarness(t)
	b := &bulkBackend{fail: func(n int) *reply {
		if n == 1 {
			return &reply{status: 503, body: `{"message":"kurz weg"}`}
		}
		return nil
	}}
	h.handle = b.handle
	file := writeTemp(t, "mieter.ndjson", `{"extern_id":"A"}`+"\n"+`{"einheit":"ohne Schlüssel"}`+"\n")
	code, _, stderr := h.run("units", "import", "5", "--file", file)
	if code != 8 {
		t.Fatalf("Exit 8 erwartet, got %d (%s)", code, stderr)
	}
	if len(b.bodies) != 1 {
		t.Errorf("keine Wiederholung erwartet, got %d Versuche", len(b.bodies))
	}
	lines := stderrLines(t, stderr)
	if lines[0]["warning"] != true || !strings.Contains(lines[0]["message"].(string), "extern_id") {
		t.Errorf("Warnung zu Zeilen ohne extern_id erwartet, got %v", lines[0])
	}
}

func TestUnitsImportRejectsEmptyInputAndPolicy(t *testing.T) {
	h := newHarness(t)
	file := writeTemp(t, "leer.ndjson", "\n\n")
	if code, _, _ := h.run("units", "import", "5", "--file", file); code != 2 {
		t.Errorf("leere Datei: Exit 2 erwartet, got %d", code)
	}
	h2 := newHarness(t)
	file2 := writeTemp(t, "m.ndjson", ndjsonUnits(1))
	if code, _, _ := h2.run("--readonly", "units", "import", "5", "--file", file2); code != 6 {
		t.Errorf("--readonly soll units import sperren, got %d", code)
	}
	if risk := riskForAPI("POST", "/api/units/immobilie/5/units/bulk"); risk != RiskWrite {
		t.Errorf("api POST …/units/bulk soll write sein, got %q", risk)
	}
}

func TestUnitsImportHelpSaysRerunIsSafe(t *testing.T) {
	h := newHarness(t)
	_, help, _ := h.run("units", "import", "--help")
	for _, want := range []string{"extern_id", "erneut", "Stapel"} {
		if !strings.Contains(help, want) {
			t.Errorf("Hilfe soll %q erklären:\n%s", want, help)
		}
	}
}

// TestUnitsImportRejectsDuplicateIDsAcrossBatches: Dieselbe id in zwei
// Stapeln hieße, die Einheit zweimal zu überschreiben — der zweite Eintrag
// gewönne still.
func TestUnitsImportRejectsDuplicateIDsAcrossBatches(t *testing.T) {
	h := newHarness(t)
	content := `{"id":"u-1","ist_rent":500}` + "\n" + `{"id":"u-2","ist_rent":600}` + "\n" + `{"id":"u-1","ist_rent":700}` + "\n"
	file := writeTemp(t, "mieter.ndjson", content)
	code, _, stderr := h.run("units", "import", "5", "--file", file, "--batch-size", "1")
	if code != 11 {
		t.Fatalf("Exit 11 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("vor dem ersten Stapel abbrechen")
	}
	line := errorLine(t, stderr)
	errs, _ := line["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("ein Fehler erwartet, got %v", line)
	}
	entry := errs[0].(map[string]any)
	if entry["index"] != float64(2) || entry["id"] != "u-1" || entry["line"] != float64(3) {
		t.Errorf("Duplikat id u-1 an index 2, Zeile 3 erwartet, got %v", entry)
	}
}

// TestUnitsImportFlagsAmbiguousRetry: Nach 502/503/504 oder Netzwerkfehler
// kann der erste Versuch gespeichert haben — der zweite meldet dann
// „unchanged“ statt „created“. Die Summe sagt, dass die Zahlen unsicher sind.
// Nach 429 lief nichts: keine Markierung.
func TestUnitsImportFlagsAmbiguousRetry(t *testing.T) {
	for status, ambiguous := range map[int]bool{502: true, 429: false} {
		h := newHarness(t)
		b := &bulkBackend{fail: func(n int) *reply {
			if n == 1 {
				return &reply{status: status, body: `{"message":"kurz weg"}`}
			}
			return nil
		}}
		h.handle = b.handle
		file := writeTemp(t, "mieter.ndjson", ndjsonUnits(3))
		code, stdout, stderr := h.run("units", "import", "5", "--file", file, "--batch-size", "2")
		if code != 0 {
			t.Fatalf("HTTP %d: Exit 0 erwartet, got %d (%s)", status, code, stderr)
		}
		var out struct {
			AmbiguousRetry   bool  `json:"ambiguous_retry"`
			AmbiguousBatches []int `json:"ambiguous_batches"`
			Complete         bool  `json:"complete"`
		}
		_ = json.Unmarshal([]byte(stdout), &out)
		if out.AmbiguousRetry != ambiguous || !out.Complete {
			t.Errorf("HTTP %d: ambiguous_retry=%v und complete erwartet, got %+v", status, ambiguous, out)
		}
		if ambiguous && fmt.Sprint(out.AmbiguousBatches) != "[1]" {
			t.Errorf("HTTP %d: Stapel 1 als unsicher erwartet, got %v", status, out.AmbiguousBatches)
		}
	}
}

// TestUnitsImportWrapperRejectsOtherKeys: {"units":[...],"dry_run":true} ohne
// --dry-run schrieb echt — das dry_run aus der Datei galt nie. Im Umschlag
// ist deshalb nur der Schlüssel units erlaubt.
func TestUnitsImportWrapperRejectsOtherKeys(t *testing.T) {
	for _, content := range []string{
		`{"units":[{"extern_id":"A"}],"dry_run":true}`,
		`{"units":[{"extern_id":"A"}],"dry_run":false}`,
		`{"units":[{"extern_id":"A"}],"immobilie_id":"7"}`,
	} {
		h := newHarness(t)
		file := writeTemp(t, "m.json", content)
		code, _, stderr := h.run("units", "import", "5", "--file", file)
		if code != 2 || h.last != nil {
			t.Errorf("%s: Exit 2 ohne Request erwartet, got %d (%s)", content, code, stderr)
			continue
		}
		if msg, _ := errorLine(t, stderr)["message"].(string); !strings.Contains(msg, "--dry-run") {
			t.Errorf("Meldung soll auf --dry-run verweisen, got %q", msg)
		}
	}
	h := newHarness(t)
	file := writeTemp(t, "w.json", `{"model":"unit","items":[{"extern_id":"RH-0","values":{"a":1}}]}`)
	if code, _, stderr := h.run("units", "fields-set", "5", "--file", file); code != 2 || h.last != nil {
		t.Errorf("fields-set-Umschlag mit model: Exit 2 ohne Request erwartet, got %d (%s)", code, stderr)
	}
}
