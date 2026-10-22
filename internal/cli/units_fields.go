package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/immoJUMP/immojump-cli/internal/api"
	"github.com/immoJUMP/immojump-cli/internal/config"
	"github.com/immoJUMP/immojump-cli/internal/output"
)

// Grenzen der Stapel-Routen für Feldwerte (custom_field_routes.py). Gelesen
// wird per POST …/values/batch/read mit den IDs im Body — als Query wären
// 1000 UUIDs ~37 KB, gunicorn nimmt höchstens 4094 Bytes je Request-Zeile.
const (
	fieldValuesMaxRead  = 1000
	fieldValuesMaxWrite = 500
)

// listHouseUnits holt alle Einheiten einer Immobilie (alle Seiten).
func listHouseUnits(client *api.Client, immobilieID string) ([]map[string]any, error) {
	request := &api.Request{Method: "GET", Path: "/api/units/immobilie/" + url.PathEscape(immobilieID) + "/units"}
	raw, _, err := fetchAllPages(client, request, maxPageSize, 0)
	if err != nil {
		return nil, err
	}
	units := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		var unit map[string]any
		if err := decodeJSONExact(item, &unit); err != nil {
			return nil, &api.Error{Message: "Einheitenliste enthält kein JSON-Objekt: " + err.Error(), Exit: 1}
		}
		units = append(units, unit)
	}
	return units, nil
}

func stringID(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// chunkIDs teilt IDs in Stapel zu höchstens size.
func chunkIDs(ids []string, size int) [][]string {
	var chunks [][]string
	for len(ids) > size {
		chunks = append(chunks, ids[:size])
		ids = ids[size:]
	}
	if len(ids) > 0 {
		chunks = append(chunks, ids)
	}
	return chunks
}

func (r *runner) requireOrg(name string, resolved config.Resolved) error {
	if resolved.Org == "" {
		return configErr("%q braucht eine Organisation. Setze IMMOJUMP_ORGANISATION_ID, --org oder lege einen Context an.", name)
	}
	return nil
}

// fieldRow ist eine Zeile von units fields-get — Reihenfolge fest.
type fieldRow struct {
	ID       any            `json:"id"`
	ExternID any            `json:"extern_id"`
	Einheit  any            `json:"einheit"`
	Values   map[string]any `json:"values"`
}

// runUnitsFieldsGet: Einheiten, Definitionen und Werte holen, Feld-IDs in
// lesbare Namen übersetzen, ein Array ausgeben.
func (r *runner) runUnitsFieldsGet(immobilieID string, flags *flagValues, resolved config.Resolved) int {
	if err := r.requireOrg("units fields-get", resolved); err != nil {
		return r.fail(err)
	}
	client, err := r.newClient(resolved, flags)
	if err != nil {
		return r.fail(err)
	}
	units, err := listHouseUnits(client, immobilieID)
	if err != nil {
		return r.fail(err)
	}
	rows := make([]fieldRow, 0, len(units))
	if len(units) == 0 {
		return r.emit(rows, flags)
	}

	names, err := fieldNames(client, resolved.Org)
	if err != nil {
		return r.fail(err)
	}

	ids := make([]string, 0, len(units))
	for _, unit := range units {
		ids = append(ids, stringID(unit["id"]))
	}
	values := map[string]map[string]any{}
	for _, chunk := range chunkIDs(ids, fieldValuesMaxRead) {
		body, err := output.Marshal(map[string]any{"model": "unit", "target_ids": chunk})
		if err != nil {
			return r.fail(err)
		}
		// Ein POST, der nur liest: Wiederholen ist sicher.
		response, err := client.Do(api.Request{
			Method: "POST", Path: "/api/custom-fields/values/batch/read",
			Body: body, ContentType: "application/json", Idempotent: true,
		})
		if err != nil {
			return r.fail(err)
		}
		var payload struct {
			Values map[string]map[string]any `json:"values"`
		}
		if err := decodeJSONExact(response.Body, &payload); err != nil {
			return r.fail(&api.Error{Message: "Feldwerte sind kein JSON: " + err.Error(), Exit: 1})
		}
		for id, byField := range payload.Values {
			values[id] = byField
		}
	}

	for _, unit := range units {
		named := map[string]any{}
		for fieldID, value := range values[stringID(unit["id"])] {
			key := fieldID
			if name, ok := names[fieldID]; ok {
				key = name
			}
			named[key] = value
		}
		rows = append(rows, fieldRow{ID: unit["id"], ExternID: unit["extern_id"], Einheit: unit["einheit"], Values: named})
	}
	return r.emit(rows, flags)
}

// fieldNames liefert Feld-ID → Name für das Modell unit.
func fieldNames(client *api.Client, org string) (map[string]string, error) {
	response, err := client.Do(api.Request{
		Method: "GET", Path: "/api/custom-fields/definitions",
		Query: url.Values{"organisation_id": {org}, "model": {"unit"}},
	})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Definitions []struct {
			ID   any    `json:"id"`
			Name string `json:"name"`
		} `json:"definitions"`
	}
	if err := decodeJSONExact(response.Body, &payload); err != nil {
		return nil, &api.Error{Message: "Feld-Definitionen sind kein JSON: " + err.Error(), Exit: 1}
	}
	names := map[string]string{}
	for _, definition := range payload.Definitions {
		names[stringID(definition.ID)] = definition.Name
	}
	return names, nil
}

// fieldItem ist ein Eintrag im PUT-Body — Reihenfolge fest.
type fieldItem struct {
	TargetID string `json:"target_id"`
	Values   any    `json:"values"`
}

// runUnitsFieldsSet: Datei lesen, extern_id auflösen, in Stapeln schreiben.
func (r *runner) runUnitsFieldsSet(immobilieID string, flags *flagValues, resolved config.Resolved) int {
	// Ohne Organisation lassen sich Feldname und Feld-ID nicht auflösen —
	// doppelte Werte blieben unerkannt und überschrieben sich still.
	if err := r.requireOrg("units fields-set", resolved); err != nil {
		return r.fail(err)
	}
	name, raw, err := r.readInput(flags)
	if err != nil {
		return r.fail(err)
	}
	format, err := detectFormat(name, raw, flags.get("format"), "ndjson", "json")
	if err != nil {
		return r.fail(err)
	}
	var records []record
	if format == "json" {
		records, err = parseJSONRecords(raw, "items")
	} else {
		records, err = parseNDJSON(raw)
	}
	if err != nil {
		return r.fail(err)
	}
	if len(records) == 0 {
		return r.fail(usageErr("%s enthält keine Datensätze", name))
	}
	for i, rec := range records {
		if stringID(rec.data["id"]) == "" && stringID(rec.data["extern_id"]) == "" {
			return r.fail(usageErr("%s: Datensatz braucht extern_id oder id", rec.position(i)))
		}
		if _, ok := rec.data["values"].(map[string]any); !ok {
			return r.fail(usageErr("%s: values muss ein Objekt sein, z. B. {\"Marktmiete\": 12.5}", rec.position(i)))
		}
	}

	client, err := r.newClient(resolved, flags)
	if err != nil {
		return r.fail(err)
	}
	units, err := listHouseUnits(client, immobilieID)
	if err != nil {
		return r.fail(err)
	}
	byExtern := map[string]string{}
	known := map[string]bool{}
	for _, unit := range units {
		id := stringID(unit["id"])
		known[id] = true
		if extern := stringID(unit["extern_id"]); extern != "" {
			byExtern[extern] = id
		}
	}

	items := make([]fieldItem, 0, len(records))
	unknownExtern, unknownIDs := []string{}, []string{}
	seenExtern, seenID := map[string]bool{}, map[string]bool{}
	for _, rec := range records {
		id := stringID(rec.data["id"])
		if id != "" {
			if !known[id] && !seenID[id] {
				unknownIDs = append(unknownIDs, id)
				seenID[id] = true
			}
		} else {
			extern := stringID(rec.data["extern_id"])
			id = byExtern[extern]
			if id == "" && !seenExtern[extern] {
				unknownExtern = append(unknownExtern, extern)
				seenExtern[extern] = true
			}
		}
		items = append(items, fieldItem{TargetID: id, Values: rec.data["values"]})
	}
	if len(unknownExtern)+len(unknownIDs) > 0 {
		return r.fail(&api.Error{
			Message: fmt.Sprintf("%d Einheit(en) gibt es in Immobilie %s nicht — es wurde nichts gesendet.",
				len(unknownExtern)+len(unknownIDs), immobilieID),
			Code:    "UNKNOWN_UNITS",
			Exit:    11,
			Details: map[string]any{"unknown_extern_ids": unknownExtern, "unknown_ids": unknownIDs},
		})
	}

	// Feldnamen und Feld-IDs auflösen wie das Backend (ID oder Name nach
	// casefold). Scheitert der Abruf, wird nichts geschrieben.
	names, err := fieldNames(client, resolved.Org)
	if err != nil {
		return r.fail(err)
	}
	if dups := duplicateUnitFields(records, items, names); len(dups) > 0 {
		return r.fail(&api.Error{
			Message: fmt.Sprintf("%d Werte setzen dasselbe Feld derselben Einheit ein weiteres Mal — es wurde nichts gesendet.", len(dups)),
			Code:    "DUPLICATE_FIELD",
			Exit:    11,
			Details: map[string]any{"errors": dups},
		})
	}

	updated, batches := 0, 0
	for offset := 0; offset < len(items); offset += fieldValuesMaxWrite {
		end := offset + fieldValuesMaxWrite
		if end > len(items) {
			end = len(items)
		}
		body, err := output.Marshal(map[string]any{"model": "unit", "items": items[offset:end]})
		if err != nil {
			return r.fail(err)
		}
		// PUT mit denselben Werten ist idempotent — Wiederholen ist sicher.
		response, err := client.Do(api.Request{
			Method: "PUT", Path: "/api/custom-fields/values/batch",
			Body: body, ContentType: "application/json", Idempotent: true,
			IdempotencyKey: derivedKey(flags, "b"+strconv.Itoa(batches+1)),
		})
		if err != nil {
			return r.fail(batchError(err, batches+1, offset, batches, records))
		}
		var payload struct {
			Updated json.Number `json:"updated"`
		}
		if err := decodeJSONExact(response.Body, &payload); err == nil {
			if n, err := payload.Updated.Int64(); err == nil {
				updated += int(n)
			}
		}
		batches++
	}
	return r.emit(struct {
		Updated int `json:"updated"`
		Batches int `json:"batches"`
	}{updated, batches}, flags)
}

// duplicateUnitFields findet (Einheit, Feld)-Paare, die die Eingabe mehrfach
// setzt. Über Stapel hinweg überschriebe der spätere PUT den früheren still.
// names (Feld-ID → Name) löst Aliasse auf; ohne names zählt der Schlüssel
// ohne Groß-/Kleinschreibung.
func duplicateUnitFields(records []record, items []fieldItem, names map[string]string) []any {
	byName := map[string]string{}
	for id, name := range names {
		byName[casefold(name)] = id
	}
	canonical := func(key string) string {
		if _, ok := names[key]; ok {
			return "id:" + key
		}
		folded := casefold(strings.TrimSpace(key))
		if id, ok := byName[folded]; ok {
			return "id:" + id
		}
		return "name:" + folded
	}
	first := map[string]int{}
	var errs []any
	for i, item := range items {
		values, _ := item.Values.(map[string]any)
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			pair := item.TargetID + "|" + canonical(key)
			j, seen := first[pair]
			if !seen {
				first[pair] = i
				continue
			}
			entry := map[string]any{
				"index":     i,
				"target_id": item.TargetID,
				"field":     key,
				"message":   fmt.Sprintf("Feld „%s“ dieser Einheit steht schon in %s.", key, records[j].position(j)),
			}
			if records[i].line > 0 {
				entry["line"] = records[i].line
			}
			errs = append(errs, entry)
		}
	}
	return errs
}

// casefold bildet Pythons str.casefold() für die Fälle nach, die im
// Deutschen vorkommen: Kleinschreibung plus ß/ẞ → ss und ς → σ. Das Backend
// vergleicht Feldnamen so (custom_field_routes.py, nach_name).
func casefold(value string) string {
	folded := strings.ToLower(value)
	return strings.NewReplacer("ß", "ss", "ς", "σ").Replace(folded)
}
