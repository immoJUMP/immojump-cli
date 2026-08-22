package output

import (
	"bytes"
	"strings"
	"testing"
)

func render(t *testing.T, body string, opts Options) string {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := Render(buf, []byte(body), "application/json", opts); err != nil {
		t.Fatalf("Render: %v", err)
	}
	return buf.String()
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

func TestFieldsWithPretty(t *testing.T) {
	got := render(t, `{"id":1,"titel":"Haus"}`, Options{Fields: []string{"titel"}, Pretty: true})
	if !strings.Contains(got, "\"titel\": \"Haus\"") || strings.Contains(got, "\"id\"") {
		t.Errorf("projizierte und eingerückte Ausgabe erwartet, got %q", got)
	}
}

func TestNonJSONIsPassedThroughRaw(t *testing.T) {
	yaml := "name: Ankauf\nstatuses:\n  - Neu\n"
	buf := &bytes.Buffer{}
	if err := Render(buf, []byte(yaml), "application/x-yaml", Options{Fields: []string{"name"}}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if buf.String() != yaml {
		t.Errorf("YAML unverändert erwartet, got %q", buf.String())
	}
}

func TestEmptyBodyProducesNoOutput(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := Render(buf, nil, "application/json", Options{}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if buf.String() != "" {
		t.Errorf("keine Ausgabe erwartet, got %q", buf.String())
	}
}

func TestWriteErrorShape(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := WriteError(buf, 403, "Kein Zugriff", "ORG_FORBIDDEN"); err != nil {
		t.Fatalf("WriteError: %v", err)
	}
	want := `{"error":true,"status":403,"message":"Kein Zugriff","code":"ORG_FORBIDDEN"}` + "\n"
	if buf.String() != want {
		t.Errorf("Fehlerzeile erwartet\n got: %q\nwant: %q", buf.String(), want)
	}
}

func TestWriteErrorDoesNotHTMLEscape(t *testing.T) {
	buf := &bytes.Buffer{}
	if err := WriteError(buf, 0, "Nutze --context <name>", "USAGE"); err != nil {
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
	if err := WriteError(buf, 0, "Unbekannter Befehl \"foo\"", ""); err != nil {
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
