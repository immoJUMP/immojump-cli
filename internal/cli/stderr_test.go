package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// stderrLines liest jede JSON-Zeile von stderr — Fehler wie Hinweise.
func stderrLines(t *testing.T, stderr string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("stderr-Zeile ist kein JSON: %q (%v)", line, err)
		}
		out = append(out, parsed)
	}
	return out
}

// TestValidationErrorCarriesFieldsAndValidValues ist der Kern von Befund 1:
// Das Backend liefert dem Aufrufer die Lösung frei Haus. Landet davon nur
// "Validierungsfehler." auf stderr, rät ein Agent weiter.
func TestValidationErrorCarriesFieldsAndValidValues(t *testing.T) {
	h := newHarness(t)
	h.status = 400
	// Wortlaut wie gegen die Produktion gemessen.
	h.respond = `{"errors":{"type":["Invalid enum value task"]},"message":"Validierungsfehler.",` +
		`"valid_values":{"type":["ANRUF","BESICHTIGUNG","BRIEF","E-MAIL","MEETING","NOTIZ","SONSTIGES"]}}`

	code, stdout, stderr := h.run("activities", "create", "--set", "title=Test", "--set", "type=task")
	if code != 11 {
		t.Fatalf("Exit 11 erwartet, got %d (%s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("kein stdout bei Fehlern erwartet, got %q", stdout)
	}

	line := errorLine(t, stderr)
	if line["error"] != true || line["message"] != "Validierungsfehler." {
		t.Errorf("unveränderte Backend-Meldung erwartet, got %#v", line)
	}
	errors, ok := line["errors"].(map[string]any)
	if !ok {
		t.Fatalf("errors erwartet, got %#v", line)
	}
	if fields, _ := errors["type"].([]any); len(fields) != 1 || fields[0] != "Invalid enum value task" {
		t.Errorf("errors unverändert erwartet, got %#v", errors)
	}
	valid, ok := line["valid_values"].(map[string]any)
	if !ok {
		t.Fatalf("valid_values erwartet, got %#v", line)
	}
	if values, _ := valid["type"].([]any); len(values) != 7 || values[0] != "ANRUF" {
		t.Errorf("valid_values unverändert erwartet, got %#v", valid)
	}
}

// TestUnknownFieldErrorIsVisible: „Unknown field" ist der zweite häufige Fall
// (contact_id statt contact_ids) — auch er steht in errors.
func TestUnknownFieldErrorIsVisible(t *testing.T) {
	h := newHarness(t)
	h.status = 400
	h.respond = `{"errors":{"contact_id":["Unknown field."]},"message":"Validierungsfehler."}`

	_, _, stderr := h.run("activities", "create", "--set", "contact_id=5")
	line := errorLine(t, stderr)
	errors, _ := line["errors"].(map[string]any)
	if _, ok := errors["contact_id"]; !ok {
		t.Errorf("das abgelehnte Feld soll sichtbar sein, got %#v", line)
	}
}

// TestPlanLimitKeepsExtraFieldsExact: 402-Antworten tragen den
// Kontingentstand — beliebige Zusatzfelder, unverfälschte Zahlen.
func TestPlanLimitKeepsExtraFieldsExact(t *testing.T) {
	h := newHarness(t)
	h.status = 402
	h.respond = `{"message":"Plan-Limit erreicht.","code":"PLAN_LIMIT","limit":25,"used":25,` +
		`"upgrade_url":"https://immojump.de/tarife"}`

	code, _, stderr := h.run("contacts", "create", "--set", "first_name=Ada")
	if code != 1 {
		t.Fatalf("Exit 1 erwartet, got %d", code)
	}
	if !strings.Contains(stderr, `"limit":25`) || !strings.Contains(stderr, `"used":25`) {
		t.Errorf("Kontingentstand unverfälscht erwartet, got %s", stderr)
	}
	line := errorLine(t, stderr)
	if line["code"] != "PLAN_LIMIT" || line["upgrade_url"] != "https://immojump.de/tarife" {
		t.Errorf("code und Zusatzfeld erwartet, got %#v", line)
	}
}

// TestPlainErrorLineStaysLean: Ohne Zusatzfelder sieht die Zeile aus wie
// bisher — der Fix darf nichts aufblähen, was schlank war.
func TestPlainErrorLineStaysLean(t *testing.T) {
	h := newHarness(t)
	h.status = 403
	h.respond = `{"message":"Kein Zugriff auf diese Organisation"}`

	_, _, stderr := h.run("contacts", "list")
	want := `{"error":true,"status":403,"message":"Kein Zugriff auf diese Organisation"}` + "\n"
	if stderr != want {
		t.Errorf("schlanke Fehlerzeile erwartet\n got: %q\nwant: %q", stderr, want)
	}
}

// TestHTMLErrorPageBecomesReadableMessage ist Befund 2: Eine 404-HTML-Seite
// als Meldung ist unbrauchbar; die Einordnung („Route gibt es hier nicht")
// spart dem Agenten das Durchprobieren geratener Pfade.
func TestHTMLErrorPageBecomesReadableMessage(t *testing.T) {
	h := newHarness(t)
	h.status = 404
	h.respCT = "text/html; charset=utf-8"
	h.respond = "<!doctype html>\n<html lang=en>\n<title>404 Not Found</title>\n" +
		"<h1>Not Found</h1>\n<p>The requested URL was not found on the server.</p>\n"

	code, _, stderr := h.run("api", "GET", "/api/gibtsnicht")
	if code != 5 {
		t.Fatalf("Exit 5 erwartet, got %d (%s)", code, stderr)
	}
	line := errorLine(t, stderr)
	message, _ := line["message"].(string)
	if strings.Contains(message, "<") {
		t.Errorf("keine HTML-Wüste in der Meldung erwartet, got %q", message)
	}
	if !strings.Contains(message, "HTTP 404") || !strings.Contains(message, "Route") {
		t.Errorf("Status und Einordnung erwartet, got %q", message)
	}
	raw, ok := line["raw"].(string)
	if !ok || !strings.Contains(raw, "Not Found") {
		t.Errorf("gekürzten Rohtext als raw erwartet, got %#v", line)
	}
}

// --- Befund 3: --fields, das ins Leere zeigt -----------------------------

// TestFieldsWarnsWhenNothingMatched: `{}` und Exit 0 sind für einen Agenten
// kein Signal — im Zweifel legt er denselben Datensatz ein zweites Mal an.
func TestFieldsWarnsWhenNothingMatched(t *testing.T) {
	h := newHarness(t)
	h.status = 201
	h.respond = `{"contact":{"id":42,"first_name":"Ada"},"success":true}`

	code, stdout, stderr := h.run("contacts", "create",
		"--set", "first_name=Ada", "--fields", "id,first_name")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet — eine Warnung ist kein Fehler, got %d (%s)", code, stderr)
	}
	if stdout != "{}\n" {
		t.Errorf("stdout bleibt reines JSON, got %q", stdout)
	}

	lines := stderrLines(t, stderr)
	if len(lines) != 1 {
		t.Fatalf("genau eine Hinweiszeile erwartet, got %q", stderr)
	}
	warning := lines[0]
	if warning["warning"] != true {
		t.Errorf("warning:true erwartet (kein error), got %#v", warning)
	}
	if warning["error"] != nil {
		t.Errorf("eine Warnung darf nicht als Fehler auftreten, got %#v", warning)
	}
	missing, _ := warning["fields_missing"].([]any)
	if len(missing) != 2 {
		t.Errorf("beide Felder als fehlend erwartet, got %#v", warning["fields_missing"])
	}
	keys, _ := warning["top_level_keys"].([]any)
	if len(keys) != 2 || keys[0] != "contact" {
		t.Errorf("vorhandene Top-Level-Schlüssel erwartet, got %#v", warning["top_level_keys"])
	}
}

func TestFieldsWarnsOnlyAboutTheMissingHalf(t *testing.T) {
	h := newHarness(t)
	h.respond = `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`

	code, stdout, stderr := h.run("immobilien", "list", "--fields", "id,name,gibtsnicht")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if stdout != `[{"id":1,"name":"A"},{"id":2,"name":"B"}]`+"\n" {
		t.Errorf("gefundene Felder auf stdout erwartet, got %q", stdout)
	}
	warning := stderrLines(t, stderr)[0]
	missing, _ := warning["fields_missing"].([]any)
	if len(missing) != 1 || missing[0] != "gibtsnicht" {
		t.Errorf("nur das fehlende Feld erwartet, got %#v", warning["fields_missing"])
	}
}

func TestFieldsStayQuietWhenEverythingMatched(t *testing.T) {
	h := newHarness(t)
	h.respond = `[{"id":1,"name":"A"}]`
	code, _, stderr := h.run("immobilien", "list", "--fields", "id,name")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if stderr != "" {
		t.Errorf("stderr bleibt bei Erfolg leer, got %q", stderr)
	}
}

// TestFieldsWarningWorksOnLocalOutput: auth und context laufen über dieselbe
// Ausgabeschicht — der Hinweis darf dort nicht fehlen.
func TestFieldsWarningWorksOnLocalOutput(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("context", "list", "--fields", "gibtsnicht")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if stdout != "{}\n" {
		t.Errorf("leeres Objekt erwartet, got %q", stdout)
	}
	if len(stderrLines(t, stderr)) != 1 {
		t.Errorf("Hinweis erwartet, got %q", stderr)
	}
}

// --- Befund 8: die tatsächlich aufgerufene URL ----------------------------

func TestVerbosePrintsMethodAndURL(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("immobilien", "list", "-q", "slim=true", "--verbose")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if stdout != `{"ok":true}`+"\n" {
		t.Errorf("stdout bleibt reines JSON, got %q", stdout)
	}
	line := stderrLines(t, stderr)[0]
	if line["method"] != "GET" {
		t.Errorf("Methode erwartet, got %#v", line)
	}
	url, _ := line["url"].(string)
	if url != h.server.URL+"/api/v2/immobilien?slim=true" {
		t.Errorf("vollständige URL erwartet, got %q", url)
	}
	if strings.Contains(stderr, "tok-test") {
		t.Error("der Token darf auch mit --verbose nirgends auftauchen")
	}
}

func TestVerboseAlsoTracesFailedCalls(t *testing.T) {
	h := newHarness(t)
	h.status = 404
	h.respCT = "text/html"
	h.respond = "<html><title>404 Not Found</title></html>"

	code, _, stderr := h.run("api", "GET", "/api/deals", "--verbose")
	if code != 5 {
		t.Fatalf("Exit 5 erwartet, got %d", code)
	}
	lines := stderrLines(t, stderr)
	if len(lines) != 2 {
		t.Fatalf("Trace plus Fehlerzeile erwartet, got %q", stderr)
	}
	if lines[0]["url"] != h.server.URL+"/api/deals" {
		t.Errorf("aufgerufene URL erwartet, got %#v", lines[0])
	}
	if lines[1]["error"] != true {
		t.Errorf("Fehlerzeile erwartet, got %#v", lines[1])
	}
}

func TestWithoutVerboseNothingIsTraced(t *testing.T) {
	h := newHarness(t)
	if _, _, stderr := h.run("immobilien", "list"); stderr != "" {
		t.Errorf("ohne --verbose bleibt stderr leer, got %q", stderr)
	}
}
