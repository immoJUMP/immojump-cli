package cli

import (
	"strings"
	"testing"
)

func TestDocumentsListAllPages(t *testing.T) {
	h := newHarness(t)
	h.handle = pagedUnits(730)
	code, stdout, stderr := h.run("documents", "list", "--immobilie-id", "5", "--analysis-status", "not_started", "--all")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(h.requests) != 2 {
		t.Errorf("zwei Seiten erwartet, got %d", len(h.requests))
	}
	for _, rec := range h.requests {
		if rec.Query.Get("immobilien_id") != "5" || rec.Query.Get("analysis_status") != "not_started" {
			t.Errorf("Filter auf jeder Seite erwartet, got %v", rec.Query)
		}
	}
	if items := decodeArray(t, stdout); len(items) != 730 {
		t.Errorf("730 Dokumente erwartet, got %d", len(items))
	}
}

func TestDocumentsSetUnitNeedsExactlyOneTarget(t *testing.T) {
	for _, args := range [][]string{
		{"documents", "set-unit", "11"},
		{"documents", "set-unit", "11", "--unit-id", "u-1", "--clear"},
	} {
		h := newHarness(t)
		code, _, stderr := h.run(args...)
		if code != 2 {
			t.Errorf("%v: Exit 2 erwartet, got %d (%s)", args, code, stderr)
		}
		if h.last != nil {
			t.Errorf("%v: kein Request erwartet", args)
		}
	}
}

func TestDocumentsSetUnitIsWrite(t *testing.T) {
	spec, ok := Lookup("documents", "set-unit")
	if !ok || spec.Risk != RiskWrite {
		t.Fatalf("documents set-unit als write erwartet, got %v %q", ok, spec.Risk)
	}
	h := newHarness(t)
	if code, _, _ := h.run("--readonly", "documents", "set-unit", "11", "--clear"); code != 6 {
		t.Errorf("--readonly soll set-unit sperren, got %d", code)
	}
}

func TestDocumentsListNamesCategories(t *testing.T) {
	spec, _ := Lookup("documents", "list")
	flag, ok := findFlag(spec, "category")
	if !ok || !strings.Contains(flag.Desc, "mietvertrag") {
		t.Errorf("--category soll die Dokumentarten nennen, got %q", flag.Desc)
	}
}

// TestDocumentsSetUnitProjectsFields: Die Route antwortet mit dem Dokument
// selbst (document_schema.dump) — ein Agent, der 900 Mietverträge zuordnet,
// liest nur id und unit_id und bekommt keine Warnung.
func TestDocumentsSetUnitProjectsFields(t *testing.T) {
	h := newHarness(t)
	h.respond = `{"id":"d-1","dateiname":"Mietvertrag.pdf","unit_id":"u-1","classification":{"category":"mietvertrag"}}`
	code, stdout, stderr := h.run("documents", "set-unit", "d-1", "--unit-id", "u-1", "--fields", "id,unit_id")
	if code != 0 || stderr != "" {
		t.Fatalf("Exit 0 und leeres stderr erwartet, got %d (%s)", code, stderr)
	}
	if strings.TrimSpace(stdout) != `{"id":"d-1","unit_id":"u-1"}` {
		t.Errorf("Projektion auf id,unit_id erwartet, got %q", stdout)
	}
}
