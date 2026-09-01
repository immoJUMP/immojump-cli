package output

import (
	"strings"
	"testing"
)

// Die Tabelle ist für Menschen. Für Agenten bleibt NDJSON der Default —
// deshalb schaltet sie ausschliesslich --table ein, nie eine TTY-Erkennung:
// Agenten-Runtimes (Claude Code, Codex, n8n) starten Prozesse häufig in einem
// PTY und bekämen sonst unangekündigt Tabellen statt JSON.

func TestTableRendersListOfObjects(t *testing.T) {
	body := `[{"id":1,"name":"Ada"},{"id":2,"name":"Linus"}]`
	got := render(t, body, Options{Table: true})
	for _, want := range []string{"id", "name", "Ada", "Linus"} {
		if !strings.Contains(got, want) {
			t.Errorf("Tabelle soll %q enthalten:\n%s", want, got)
		}
	}
	if strings.Contains(got, `{"id"`) {
		t.Errorf("kein rohes JSON erwartet:\n%s", got)
	}
}

// Paginierte Antworten sind der Hauptfall: {items:[…], total, page}. Die
// Tabelle zeigt die items, der Rest wird zur Fusszeile — sonst müsste ein
// Mensch erst --fields items.… tippen, um überhaupt etwas zu sehen.
func TestTableUnwrapsEnvelope(t *testing.T) {
	body := `{"items":[{"id":1,"subject":"Notartermin"}],"total":1,"page":1,"per_page":50}`
	got := render(t, body, Options{Table: true})
	if !strings.Contains(got, "Notartermin") {
		t.Errorf("items sollen tabelliert werden:\n%s", got)
	}
	if !strings.Contains(got, "total") || !strings.Contains(got, "1") {
		t.Errorf("Envelope-Rest soll als Fusszeile erscheinen:\n%s", got)
	}
}

// Ein einzelnes Objekt hat keine sinnvollen Spalten — untereinander ist
// lesbarer als eine Zeile mit 40 Spalten.
func TestTableRendersSingleObjectAsPairs(t *testing.T) {
	body := `{"id":7,"name":"Haus","stadt":"Köln"}`
	got := render(t, body, Options{Table: true})
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) < 3 {
		t.Fatalf("drei Zeilen erwartet (eine je Feld):\n%s", got)
	}
	if !strings.Contains(got, "name") || !strings.Contains(got, "Haus") {
		t.Errorf("Feld und Wert erwartet:\n%s", got)
	}
}

// Spalten müssen ausgerichtet sein, sonst ist die Tabelle nutzlos. Geprüft
// über die Position des Trenners in Kopf- und Datenzeile.
func TestTableAlignsColumns(t *testing.T) {
	body := `[{"id":1,"name":"kurz"},{"id":222222,"name":"deutlich länger"}]`
	got := render(t, body, Options{Table: true})
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) < 3 {
		t.Fatalf("Kopfzeile plus zwei Datenzeilen erwartet:\n%s", got)
	}
	// lines[1] ist die Trennlinie; die Datenzeilen beginnen bei lines[2].
	first := strings.Index(lines[0], "name")
	for i, line := range lines[2:] {
		if !strings.HasPrefix(line[first:], "kurz") && !strings.HasPrefix(line[first:], "deutlich") {
			t.Errorf("Spalte 2 in Zeile %d nicht bei Spalte %d ausgerichtet:\n%s", i, first, got)
		}
	}
}

// Verschachtelte Werte würden die Tabelle sprengen — sie werden kompakt
// eingesetzt, nicht ausgeklappt.
func TestTableCompactsNestedValues(t *testing.T) {
	body := `[{"id":1,"adresse":{"stadt":"Köln"}}]`
	got := render(t, body, Options{Table: true})
	if !strings.Contains(got, `{"stadt":"Köln"}`) {
		t.Errorf("verschachtelter Wert soll kompakt erscheinen:\n%s", got)
	}
}

// Eine leere Liste ist ein gültiges Ergebnis und darf nicht wie ein Fehler
// aussehen.
func TestTableHandlesEmptyList(t *testing.T) {
	got := render(t, `{"items":[],"total":0}`, Options{Table: true})
	if strings.TrimSpace(got) == "" {
		t.Error("leere Liste soll eine erkennbare Meldung zeigen, nicht nichts")
	}
}

// --table und --fields müssen zusammen funktionieren: erst projizieren,
// dann tabellieren.
func TestTableAppliesFieldsFirst(t *testing.T) {
	body := `{"items":[{"id":1,"name":"Ada","gross":"weg"}]}`
	got := render(t, body, Options{Table: true, Fields: []string{"items.id", "items.name"}})
	if strings.Contains(got, "gross") || strings.Contains(got, "weg") {
		t.Errorf("--fields soll vor der Tabelle greifen:\n%s", got)
	}
	if !strings.Contains(got, "Ada") {
		t.Errorf("projizierte Spalte fehlt:\n%s", got)
	}
}

// Nicht-JSON (YAML-Export) geht unverändert durch — eine Tabelle daraus zu
// bauen hiesse, den Inhalt zu erfinden.
func TestTableLeavesNonJSONAlone(t *testing.T) {
	buf, _ := renderRaw(t, "name: Ankauf\n", "application/x-yaml", Options{Table: true})
	if buf != "name: Ankauf\n" {
		t.Errorf("YAML soll unverändert durchgehen, got %q", buf)
	}
}
