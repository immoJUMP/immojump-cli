package output

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func render(t *testing.T, body string, opts Options) string {
	t.Helper()
	out, _ := renderWithReport(t, body, opts)
	return out
}

func renderWithReport(t *testing.T, body string, opts Options) (string, Report) {
	t.Helper()
	buf := &bytes.Buffer{}
	report, err := Render(buf, []byte(body), "application/json", opts)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return buf.String(), report
}

func TestCompactKeepsFieldOrderOnOneLine(t *testing.T) {
	got := render(t, "{\n  \"id\": 1,\n  \"adresse\": {\"stadt\": \"Köln\"}\n}", Options{})
	want := `{"id":1,"adresse":{"stadt":"Köln"}}` + "\n"
	if got != want {
		t.Errorf("kompakte Ausgabe erwartet\n got: %q\nwant: %q", got, want)
	}
}

func TestPrettyIndents(t *testing.T) {
	got := render(t, `{"id":1,"titel":"Haus"}`, Options{Pretty: true})
	if !strings.Contains(got, "\n  \"id\": 1") {
		t.Errorf("eingerückte Ausgabe erwartet, got %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Error("abschließenden Zeilenumbruch erwartet")
	}
}

func TestFieldsProjectObject(t *testing.T) {
	body := `{"id":7,"titel":"Haus","kaufpreis":225000,"intern":{"x":1}}`
	got := render(t, body, Options{Fields: []string{"id", "kaufpreis"}})
	want := `{"id":7,"kaufpreis":225000}` + "\n"
	if got != want {
		t.Errorf("Projektion erwartet\n got: %q\nwant: %q", got, want)
	}
}

func TestFieldsFollowRequestedOrder(t *testing.T) {
	body := `{"a":1,"b":2,"c":3}`
	got := render(t, body, Options{Fields: []string{"c", "a"}})
	if got != `{"c":3,"a":1}`+"\n" {
		t.Errorf("Reihenfolge der Flags erwartet, got %q", got)
	}
}

func TestFieldsProjectArrayElements(t *testing.T) {
	body := `[{"id":1,"titel":"A","x":9},{"id":2,"titel":"B","x":8}]`
	got := render(t, body, Options{Fields: []string{"id", "titel"}})
	want := `[{"id":1,"titel":"A"},{"id":2,"titel":"B"}]` + "\n"
	if got != want {
		t.Errorf("Array-Projektion erwartet\n got: %q\nwant: %q", got, want)
	}
}

func TestFieldsDottedPathKeepsShape(t *testing.T) {
	body := `{"id":1,"adresse":{"stadt":"Köln","plz":"50667","land":"DE"}}`
	got := render(t, body, Options{Fields: []string{"id", "adresse.stadt", "adresse.plz"}})
	want := `{"id":1,"adresse":{"stadt":"Köln","plz":"50667"}}` + "\n"
	if got != want {
		t.Errorf("verschachtelte Projektion erwartet\n got: %q\nwant: %q", got, want)
	}
}

// TestFieldsBroaderPathWinsRegardlessOfOrder: "adresse" und "adresse.stadt"
// zusammen sollen dasselbe liefern, egal in welcher Reihenfolge sie stehen —
// alles andere ist eine Falle, die von der Tippreihenfolge abhängt.
func TestFieldsBroaderPathWinsRegardlessOfOrder(t *testing.T) {
	body := `{"id":1,"adresse":{"stadt":"Köln","plz":"50667"}}`
	want := `{"adresse":{"stadt":"Köln","plz":"50667"}}` + "\n"

	if got := render(t, body, Options{Fields: []string{"adresse", "adresse.stadt"}}); got != want {
		t.Errorf("breit zuerst\n got: %q\nwant: %q", got, want)
	}
	if got := render(t, body, Options{Fields: []string{"adresse.stadt", "adresse"}}); got != want {
		t.Errorf("schmal zuerst\n got: %q\nwant: %q", got, want)
	}
}

func TestFieldsDeepBroaderPathWinsRegardlessOfOrder(t *testing.T) {
	body := `{"a":{"b":{"c":1,"d":2},"e":3}}`
	want := `{"a":{"b":{"c":1,"d":2}}}` + "\n"

	if got := render(t, body, Options{Fields: []string{"a.b", "a.b.c"}}); got != want {
		t.Errorf("breit zuerst\n got: %q\nwant: %q", got, want)
	}
	if got := render(t, body, Options{Fields: []string{"a.b.c", "a.b"}}); got != want {
		t.Errorf("schmal zuerst\n got: %q\nwant: %q", got, want)
	}
}

func TestFieldsIgnoresUnknownPaths(t *testing.T) {
	got := render(t, `{"id":1}`, Options{Fields: []string{"id", "gibtsnicht", "a.b.c"}})
	if got != `{"id":1}`+"\n" {
		t.Errorf("unbekannte Felder werden ausgelassen, got %q", got)
	}
}

// TestReportNamesMissingFieldsAndAvailableKeys: `{}` als einzige Antwort ist
// für einen Agenten kein Signal — er hält den Aufruf für gescheitert. Der
// Report sagt, was fehlte und welche Schlüssel es stattdessen gibt.
func TestReportNamesMissingFieldsAndAvailableKeys(t *testing.T) {
	body := `{"contact":{"id":42,"first_name":"Ada"},"success":true}`
	got, report := renderWithReport(t, body, Options{Fields: []string{"id", "first_name"}})
	if got != "{}\n" {
		t.Errorf("leeres Objekt auf stdout erwartet, got %q", got)
	}
	if !reflect.DeepEqual(report.Missing, []string{"id", "first_name"}) {
		t.Errorf("beide Felder als fehlend erwartet, got %#v", report.Missing)
	}
	if !reflect.DeepEqual(report.Requested, []string{"id", "first_name"}) {
		t.Errorf("angeforderte Felder erwartet, got %#v", report.Requested)
	}
	if !reflect.DeepEqual(report.Keys, []string{"contact", "success"}) {
		t.Errorf("Top-Level-Schlüssel erwartet, got %#v", report.Keys)
	}
}

func TestReportNamesOnlyTheMissingHalf(t *testing.T) {
	body := `{"id":42,"success":true}`
	_, report := renderWithReport(t, body, Options{Fields: []string{"id", "first_name"}})
	if !reflect.DeepEqual(report.Missing, []string{"first_name"}) {
		t.Errorf("nur das fehlende Feld erwartet, got %#v", report.Missing)
	}
}

func TestReportStaysQuietWhenEverythingMatched(t *testing.T) {
	_, report := renderWithReport(t, `{"id":42,"name":"A"}`, Options{Fields: []string{"id", "name"}})
	if len(report.Missing) != 0 {
		t.Errorf("keine Meldung erwartet, got %#v", report.Missing)
	}
}

// TestReportForListsCountsAnyElement: Ein Feld gilt als vorhanden, sobald ein
// einziges Element es trägt — sonst warnte jede Liste mit Lücken.
func TestReportForListsCountsAnyElement(t *testing.T) {
	body := `[{"id":1},{"id":2,"name":"B"}]`
	_, report := renderWithReport(t, body, Options{Fields: []string{"id", "name"}})
	if len(report.Missing) != 0 {
		t.Errorf("keine Meldung erwartet, got %#v", report.Missing)
	}

	_, report = renderWithReport(t, body, Options{Fields: []string{"titel"}})
	if !reflect.DeepEqual(report.Missing, []string{"titel"}) {
		t.Errorf("titel als fehlend erwartet, got %#v", report.Missing)
	}
	if !reflect.DeepEqual(report.Keys, []string{"id", "name"}) {
		t.Errorf("Schlüssel aller Elemente erwartet, got %#v", report.Keys)
	}
}

// TestReportIgnoresEmptyList: Eine leere Liste ist keine Fehlbedienung —
// dort ist schlicht nichts zu finden.
func TestReportIgnoresEmptyList(t *testing.T) {
	_, report := renderWithReport(t, `[]`, Options{Fields: []string{"id"}})
	if len(report.Missing) != 0 {
		t.Errorf("bei leerer Liste keine Meldung erwartet, got %#v", report.Missing)
	}
}

func TestReportTruncatesLongKeyLists(t *testing.T) {
	body := `{"a":1,"b":1,"c":1,"d":1,"e":1,"f":1,"g":1,"h":1,"i":1,"j":1}`
	_, report := renderWithReport(t, body, Options{Fields: []string{"gibtsnicht"}})
	if len(report.Keys) != 8 {
		t.Errorf("auf 8 Schlüssel gekürzt erwartet, got %#v", report.Keys)
	}
	if !report.KeysTruncated {
		t.Error("KeysTruncated erwartet")
	}
}

// TestReportCountsNestedPaths: --fields contact.id trifft, obwohl id nicht
// oben liegt.
func TestReportCountsNestedPaths(t *testing.T) {
	body := `{"contact":{"id":42},"success":true}`
	_, report := renderWithReport(t, body, Options{Fields: []string{"contact.id", "contact.gibtsnicht"}})
	if !reflect.DeepEqual(report.Missing, []string{"contact.gibtsnicht"}) {
		t.Errorf("nur den unbekannten Unterpfad erwartet, got %#v", report.Missing)
	}
}

func TestFieldsWithPretty(t *testing.T) {
	got := render(t, `{"id":1,"titel":"Haus"}`, Options{Fields: []string{"titel"}, Pretty: true})
	if !strings.Contains(got, "\"titel\": \"Haus\"") || strings.Contains(got, "\"id\"") {
		t.Errorf("projizierte und eingerückte Ausgabe erwartet, got %q", got)
	}
}

func TestNonJSONIsPassedThroughRaw(t *testing.T) {
	yaml := "name: Ankauf\nstatuses:\n  - Neu\n"
	buf := &bytes.Buffer{}
	if _, err := Render(buf, []byte(yaml), "application/x-yaml", Options{Fields: []string{"name"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if buf.String() != yaml {
		t.Errorf("YAML unverändert erwartet, got %q", buf.String())
	}
}

func TestEmptyBodyProducesNoOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	if _, err := Render(buf, nil, "application/json", Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if buf.String() != "" {
		t.Errorf("keine Ausgabe erwartet, got %q", buf.String())
	}
}

func TestWriteErrorShape(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := WriteError(buf, 403, "Kein Zugriff", "ORG_FORBIDDEN", nil); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	want := `{"error":true,"status":403,"message":"Kein Zugriff","code":"ORG_FORBIDDEN"}` + "\n"
	if buf.String() != want {
		t.Errorf("Fehlerzeile erwartet\n got: %q\nwant: %q", buf.String(), want)
	}
}

// TestWriteErrorKeepsEveryBackendField: api_error() erlaubt beliebige
// Zusatzfelder — genau darin steckt die Lösung für den Agenten (welches Feld,
// welche erlaubten Werte). Nichts davon darf unterwegs verloren gehen.
func TestWriteErrorKeepsEveryBackendField(t *testing.T) {
	details := map[string]any{
		"message":      "Validierungsfehler.",
		"errors":       map[string]any{"type": []any{"Invalid enum value task"}},
		"valid_values": map[string]any{"type": []any{"ANRUF", "BESICHTIGUNG"}},
	}
	buf := &bytes.Buffer{}
	if err := WriteError(buf, 400, "Validierungsfehler.", "", details); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	want := `{"error":true,"status":400,"message":"Validierungsfehler.",` +
		`"errors":{"type":["Invalid enum value task"]},` +
		`"valid_values":{"type":["ANRUF","BESICHTIGUNG"]}}` + "\n"
	if buf.String() != want {
		t.Errorf("vollständiges Fehler-Payload erwartet\n got: %q\nwant: %q", buf.String(), want)
	}
}

// TestWriteErrorKeepsNumbersExact: Ein Kontingentstand aus einer
// 402-Plan-Limit-Antwort darf nicht durch float64 laufen.
func TestWriteErrorKeepsNumbersExact(t *testing.T) {
	details := map[string]any{
		"message": "Plan-Limit erreicht.",
		"code":    "PLAN_LIMIT",
		"limit":   json.Number("25"),
		"used":    json.Number("25"),
		"upgrade": "https://immojump.de/tarife",
	}
	buf := &bytes.Buffer{}
	if err := WriteError(buf, 402, "Plan-Limit erreicht.", "PLAN_LIMIT", details); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	want := `{"error":true,"status":402,"message":"Plan-Limit erreicht.","code":"PLAN_LIMIT",` +
		`"limit":25,"upgrade":"https://immojump.de/tarife","used":25}` + "\n"
	if buf.String() != want {
		t.Errorf("Zusatzfelder erwartet\n got: %q\nwant: %q", buf.String(), want)
	}
}

// TestWriteErrorMovesCollidingKeysAside: Die CLI-eigenen Schlüssel error und
// status behalten ihre Bedeutung — der Backend-Wert geht trotzdem nicht
// verloren, sondern steht daneben.
func TestWriteErrorMovesCollidingKeysAside(t *testing.T) {
	details := map[string]any{
		"error":  "Feld kaufpreis fehlt",
		"status": "failed",
	}
	buf := &bytes.Buffer{}
	if err := WriteError(buf, 400, "Feld kaufpreis fehlt", "", details); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	want := `{"error":true,"status":400,"message":"Feld kaufpreis fehlt",` +
		`"backend_error":"Feld kaufpreis fehlt","backend_status":"failed"}` + "\n"
	if buf.String() != want {
		t.Errorf("kollidierende Felder erwartet\n got: %q\nwant: %q", buf.String(), want)
	}
}

// TestWriteErrorSkipsEchoedStatus: Ein Backend, das den Status nur spiegelt,
// soll die Zeile nicht mit backend_status:400 aufblähen.
func TestWriteErrorSkipsEchoedStatus(t *testing.T) {
	buf := &bytes.Buffer{}
	details := map[string]any{"message": "Nicht gefunden", "status": json.Number("404")}
	if err := WriteError(buf, 404, "Nicht gefunden", "", details); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	want := `{"error":true,"status":404,"message":"Nicht gefunden"}` + "\n"
	if buf.String() != want {
		t.Errorf("gespiegelten Status weglassen\n got: %q\nwant: %q", buf.String(), want)
	}
}

func TestWriteWarningShape(t *testing.T) {
	buf := &bytes.Buffer{}
	err := WriteWarning(buf, "--fields hat nichts getroffen.", map[string]any{
		"fields_missing": []string{"id"},
		"top_level_keys": []string{"contact", "success"},
	})
	if err != nil {
		t.Fatalf("WriteWarning: %v", err)
	}
	want := `{"warning":true,"message":"--fields hat nichts getroffen.",` +
		`"fields_missing":["id"],"top_level_keys":["contact","success"]}` + "\n"
	if buf.String() != want {
		t.Errorf("Warnzeile erwartet\n got: %q\nwant: %q", buf.String(), want)
	}
}

func TestWriteErrorDoesNotHTMLEscape(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := WriteError(buf, 0, "Nutze --context <name>", "USAGE", nil); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	if strings.Contains(buf.String(), `\u003c`) {
		t.Errorf("kein HTML-Escaping erwartet, got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "<name>") {
		t.Errorf("<name> lesbar erwartet, got %q", buf.String())
	}
}

func TestMarshalWithoutHTMLEscaping(t *testing.T) {
	raw, err := Marshal(map[string]string{"hinweis": "a < b & c"})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(raw) != `{"hinweis":"a < b & c"}` {
		t.Errorf("unescapte Ausgabe erwartet, got %s", raw)
	}
}

func TestWriteErrorOmitsEmptyStatusAndCode(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := WriteError(buf, 0, "Unbekannter Befehl \"foo\"", "", nil); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	want := `{"error":true,"message":"Unbekannter Befehl \"foo\""}` + "\n"
	if buf.String() != want {
		t.Errorf("schlanke Fehlerzeile erwartet\n got: %q\nwant: %q", buf.String(), want)
	}
}

func TestCompactExposesErrors(t *testing.T) {
	got, err := Compact([]byte("{\n  \"a\": 1\n}"))
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if string(got) != `{"a":1}` {
		t.Errorf(`{"a":1} erwartet, got %s`, got)
	}
	if _, err := Compact([]byte("{kaputt")); err == nil {
		t.Error("Compact soll kaputtes JSON melden statt es zu verschlucken")
	}
}

// TestFieldsProjectsIntoLists: Paginierte Routen antworten als Envelope
// ({items: [...], pagination: {...}}). lookup() folgte einem Pfad bisher nur
// durch Objekte und brach bei einem Array ab — `--fields items.id` traf damit
// NICHTS, und die Ausgabe war `{}` plus eine Warnung.
//
// Gemessen gegen Produktion am 01.09.2026 mit `immobilien search`,
// `contacts list` und `email list`. Betroffen war ausgerechnet der Fall, für
// den --fields gebaut wurde: Bei kleinen Antworten lohnt die Projektion nicht,
// bei den grossen (paginierten) wirkte sie nicht.
func TestFieldsProjectsIntoLists(t *testing.T) {
	body := `{"items":[{"id":1,"name":"A","gross":"weg"},{"id":2,"name":"B","gross":"weg"}],` +
		`"pagination":{"page":1,"total":2}}`

	got, report := renderWithReport(t, body, Options{Fields: []string{"items.id", "items.name"}})
	want := `{"items":[{"id":1,"name":"A"},{"id":2,"name":"B"}]}` + "\n"
	if got != want {
		t.Errorf("Projektion in die Liste erwartet\n got: %s\nwant: %s", got, want)
	}
	if len(report.Missing) != 0 {
		t.Errorf("keine fehlenden Felder erwartet, got %v", report.Missing)
	}
}

// Ein Pfad, den es in den Listenelementen nicht gibt, muss weiterhin als
// fehlend gemeldet werden — sonst sieht ein Tippfehler wie ein leeres Ergebnis aus.
func TestFieldsInListsReportMissingPaths(t *testing.T) {
	body := `{"items":[{"id":1},{"id":2}]}`
	_, report := renderWithReport(t, body, Options{Fields: []string{"items.id", "items.tippfehler"}})
	if len(report.Missing) != 1 || report.Missing[0] != "items.tippfehler" {
		t.Errorf("items.tippfehler als fehlend erwartet, got %v", report.Missing)
	}
}

// Eine leere Liste ist kein Tippfehler: items.id ist ein gültiger Pfad, auch
// wenn gerade nichts drin steht. Sonst warnt ein leeres Postfach so, als wäre
// der Befehl falsch gewesen.
func TestFieldsInEmptyListIsNotMissing(t *testing.T) {
	body := `{"items":[],"total":0}`
	got, report := renderWithReport(t, body, Options{Fields: []string{"items.id"}})
	if len(report.Missing) != 0 {
		t.Errorf("leere Liste darf items.id nicht als fehlend melden, got %v", report.Missing)
	}
	if strings.TrimSpace(got) != `{"items":[]}` {
		t.Errorf("leeres Array erwartet, got %s", got)
	}
}

// Verschachtelte Objekte müssen weiter funktionieren wie bisher.
func TestFieldsStillProjectNestedObjects(t *testing.T) {
	body := `{"id":1,"adresse":{"stadt":"Köln","plz":"50667"}}`
	got := render(t, body, Options{Fields: []string{"id", "adresse.stadt"}})
	want := `{"id":1,"adresse":{"stadt":"Köln"}}` + "\n"
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
