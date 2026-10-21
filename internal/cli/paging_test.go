package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// pagedUnits simuliert den Umschlag {items,total,limit,offset} über total
// Einheiten — wie GET /api/units/... mit limit/offset.
func pagedUnits(total int) func(rec *recorded) reply {
	return func(rec *recorded) reply {
		limit, _ := strconv.Atoi(rec.Query.Get("limit"))
		offset, _ := strconv.Atoi(rec.Query.Get("offset"))
		if limit == 0 {
			limit = 100
		}
		items := []string{}
		for i := offset; i < offset+limit && i < total; i++ {
			items = append(items, fmt.Sprintf(`{"id":"u%d","einheit":"WE %d"}`, i, i))
		}
		return reply{body: fmt.Sprintf(`{"items":[%s],"total":%d,"limit":%d,"offset":%d}`,
			strings.Join(items, ","), total, limit, offset)}
	}
}

func decodeArray(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("JSON-Array auf stdout erwartet: %v (%q)", err, stdout)
	}
	return out
}

// TestUnitsListAllPagesThroughEverything: 1203 Einheiten in Seiten zu 500 —
// ein Aufruf, ein Array, keine Blätter-Schleife beim Agenten.
func TestUnitsListAllPagesThroughEverything(t *testing.T) {
	h := newHarness(t)
	h.handle = pagedUnits(1203)
	code, stdout, stderr := h.run("units", "list", "5", "--all", "--type", "wohnen")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(h.requests) != 3 {
		t.Fatalf("drei Seiten erwartet, got %d Requests", len(h.requests))
	}
	for i, rec := range h.requests {
		if rec.Query.Get("limit") != "500" || rec.Query.Get("offset") != strconv.Itoa(i*500) {
			t.Errorf("Seite %d: limit=500&offset=%d erwartet, got %v", i, i*500, rec.Query)
		}
		if rec.Query.Get("type") != "wohnen" {
			t.Errorf("Seite %d: Filter type=wohnen soll auf jeder Seite stehen, got %v", i, rec.Query)
		}
	}
	items := decodeArray(t, stdout)
	if len(items) != 1203 || items[1202]["id"] != "u1202" {
		t.Errorf("1203 Einheiten in Reihenfolge erwartet, got %d", len(items))
	}
	if stderr != "" {
		t.Errorf("stderr soll leer bleiben, got %q", stderr)
	}
}

func TestPagedAllHonoursLimitAndOffsetAsStart(t *testing.T) {
	h := newHarness(t)
	h.handle = pagedUnits(7)
	code, stdout, stderr := h.run("units", "list", "5", "--all", "--limit", "3", "--offset", "2")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if len(h.requests) != 2 {
		t.Errorf("Seiten ab Offset 2 zu je 3 → zwei Requests erwartet, got %d", len(h.requests))
	}
	if items := decodeArray(t, stdout); len(items) != 5 || items[0]["id"] != "u2" {
		t.Errorf("Einheiten u2…u6 erwartet, got %v", items)
	}
}

func TestPagedAllProjectsFields(t *testing.T) {
	h := newHarness(t)
	h.handle = pagedUnits(2)
	code, stdout, _ := h.run("units", "list", "5", "--all", "--fields", "id")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if strings.TrimSpace(stdout) != `[{"id":"u0"},{"id":"u1"}]` {
		t.Errorf("--fields auf das zusammengeführte Array erwartet, got %q", stdout)
	}
}

func TestPagedAllEmptyResult(t *testing.T) {
	h := newHarness(t)
	h.handle = pagedUnits(0)
	code, stdout, _ := h.run("units", "list-org", "--all", "--vacant")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if strings.TrimSpace(stdout) != `[]` || len(h.requests) != 1 {
		t.Errorf("leeres Array nach einem Request erwartet, got %q (%d Requests)", stdout, len(h.requests))
	}
}

// TestPagedAllAcceptsPlainArray: Ein Backend ohne Blättern antwortet mit dem
// kompletten Array — das ist dann bereits alles.
func TestPagedAllAcceptsPlainArray(t *testing.T) {
	h := newHarness(t)
	h.respond = `[{"id":"a"},{"id":"b"}]`
	code, stdout, _ := h.run("units", "list", "5", "--all")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if strings.TrimSpace(stdout) != `[{"id":"a"},{"id":"b"}]` || len(h.requests) != 1 {
		t.Errorf("Array unverändert nach einem Request erwartet, got %q", stdout)
	}
}

// TestPagedAllFailsWithoutPartialOutput: Eine halbe Liste auf stdout sähe aus
// wie die ganze.
func TestPagedAllFailsWithoutPartialOutput(t *testing.T) {
	h := newHarness(t)
	pages := pagedUnits(900)
	h.handle = func(rec *recorded) reply {
		if rec.Query.Get("offset") == "500" {
			return reply{status: 403, body: `{"message":"Keine Berechtigung."}`}
		}
		return pages(rec)
	}
	code, stdout, stderr := h.run("units", "list-org", "--all")
	if code != 6 {
		t.Fatalf("Exit 6 erwartet, got %d (%s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout soll leer bleiben, got %d Zeichen", len(stdout))
	}
}

func TestPagedAllRejectsBadLimit(t *testing.T) {
	for _, value := range []string{"0", "501", "viele"} {
		h := newHarness(t)
		code, _, stderr := h.run("units", "list", "5", "--all", "--limit", value)
		if code != 2 {
			t.Errorf("--limit %s mit --all: Exit 2 erwartet, got %d (%s)", value, code, stderr)
		}
		if h.last != nil {
			t.Errorf("--limit %s: kein Request erwartet", value)
		}
	}
}

// TestPagedRegistryInvariant: --all gibt es genau dort, wo der Befehl als
// blätterbar markiert ist — sonst wäre es ein Flag, das nichts tut.
func TestPagedRegistryInvariant(t *testing.T) {
	for _, spec := range Registry {
		flag, hasAll := findFlag(spec, "all")
		if spec.Paged != hasAll {
			t.Errorf("%q: Paged=%v, aber --all vorhanden=%v", spec.Name(), spec.Paged, hasAll)
		}
		if hasAll && flag.Kind != FlagBool {
			t.Errorf("%q: --all muss ein Schalter sein", spec.Name())
		}
		if spec.Paged && !strings.EqualFold(spec.Method, "GET") {
			t.Errorf("%q: nur GET-Befehle können blättern", spec.Name())
		}
	}
	for _, name := range [][2]string{{"units", "list"}, {"units", "list-org"}} {
		if spec, ok := Lookup(name[0], name[1]); !ok || !spec.Paged {
			t.Errorf("%s %s soll --all können", name[0], name[1])
		}
	}
}
