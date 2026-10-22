package cli

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/immoJUMP/immojump-cli/internal/api"
	"github.com/immoJUMP/immojump-cli/internal/config"
	"github.com/immoJUMP/immojump-cli/internal/output"
)

// maxUnitsPerBatch ist MAX_UNITS_PER_CALL der Bulk-Route.
const maxUnitsPerBatch = 500

// unitNumberColumns sind die CSV-Spalten, die als JSON-Zahl rausgehen. Alle
// anderen bleiben Strings — eine extern_id „001" darf nicht zu 1 werden.
var unitNumberColumns = map[string]bool{
	"livingspace": true, "rooms": true, "ist_rent": true, "soll_rent": true, "soll_rent2": true, "order": true,
}

// parseUnitsCSV liest eine CSV mit Kopfzeile aus Backend-Feldnamen. Eine
// leere Zelle lässt das Feld weg; Zahlenspalten werden zu JSON-Zahlen.
func parseUnitsCSV(raw []byte, delimiter string, decimalComma bool) ([]record, error) {
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	comma, err := csvDelimiter(raw, delimiter)
	if err != nil {
		return nil, err
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.Comma = comma
	reader.TrimLeadingSpace = true
	header, err := reader.Read()
	if err != nil {
		return nil, usageErr("CSV-Kopfzeile nicht lesbar: %v", err)
	}
	seen := map[string]bool{}
	for i, name := range header {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			return nil, usageErr("CSV-Kopfzeile: Spalte %d hat keinen Namen", i+1)
		}
		if seen[name] {
			return nil, usageErr("CSV-Kopfzeile: Spalte %q kommt doppelt vor", name)
		}
		seen[name] = true
		header[i] = name
	}
	var out []record
	for {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, usageErr("CSV nicht lesbar: %v", err)
		}
		line, _ := reader.FieldPos(0)
		object := map[string]any{}
		for i, cell := range row {
			value := strings.TrimSpace(cell)
			if value == "" {
				continue
			}
			column := header[i]
			if !unitNumberColumns[column] {
				object[column] = value
				continue
			}
			number, problem := parseCSVNumber(value, decimalComma, comma)
			if problem != "" {
				return nil, &api.Error{
					Message: fmt.Sprintf("Zeile %d, Spalte %s: %q %s — es wurde nichts gesendet.", line, column, value, problem),
					Code:    "CSV_INVALID_NUMBER",
					Exit:    11,
					Details: map[string]any{"line": line, "column": column, "value": value},
				}
			}
			object[column] = number
		}
		if len(object) == 0 {
			continue
		}
		out = append(out, record{data: object, line: line})
	}
	return out, nil
}

// csvDelimiter: --delimiter, sonst das häufigere von ; und , in der Kopfzeile.
func csvDelimiter(raw []byte, explicit string) (rune, error) {
	switch explicit {
	case ";":
		return ';', nil
	case ",":
		return ',', nil
	case "":
	default:
		return 0, usageErr("--delimiter kennt ';' oder ',', bekommen: %q", explicit)
	}
	first := raw
	if idx := bytes.IndexByte(raw, '\n'); idx >= 0 {
		first = raw[:idx]
	}
	if bytes.Count(first, []byte(";")) > bytes.Count(first, []byte(",")) {
		return ';', nil
	}
	return ',', nil
}

// Zahlenformate der CSV — streng: Was nicht genau passt, wird abgelehnt
// statt umgedeutet („62.5“ mit --decimal-comma wäre sonst 625).
var (
	plainNumber     = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	commaNumber     = regexp.MustCompile(`^-?\d+(,\d+)?$`)
	groupedComma    = regexp.MustCompile(`^-?\d{1,3}(\.\d{3})+(,\d+)?$`)
	germanThousands = regexp.MustCompile(`^\d{1,3}(\.\d{3})+$`)
)

// parseCSVNumber liefert die JSON-Zahl oder, warum der Wert keine ist.
//
// Mit decimalComma: „62,5“, „1250“ oder mit Tausenderpunkten „1.250,50“.
// Ohne: nur „62.5“. In einer ;-CSV ohne Schalter ist „1.250“ mehrdeutig
// (1,25 oder 1250?) und wird abgelehnt.
func parseCSVNumber(value string, decimalComma bool, comma rune) (json.Number, string) {
	if decimalComma {
		if !commaNumber.MatchString(value) && !groupedComma.MatchString(value) {
			return "", "ist keine Zahl im deutschen Format (erwartet z. B. 62,5 oder 1.250,50)"
		}
		normalized := strings.ReplaceAll(strings.ReplaceAll(value, ".", ""), ",", ".")
		number, _ := parseNumber(normalized)
		return number, ""
	}
	if comma == ';' && germanThousands.MatchString(value) {
		return "", "ist mehrdeutig (Tausenderpunkt oder Dezimalpunkt?) — deutsche Zahlen mit --decimal-comma lesen"
	}
	if !plainNumber.MatchString(value) {
		return "", "ist keine Zahl (Dezimalpunkt erwartet; deutsches Format mit --decimal-comma)"
	}
	number, _ := parseNumber(value)
	return number, ""
}

// importSummary ist die Ausgabe von units import — Reihenfolge fest.
type importSummary struct {
	Created            int          `json:"created"`
	Updated            int          `json:"updated"`
	Unchanged          int          `json:"unchanged"`
	PlaceholderRemoved bool         `json:"placeholder_removed"`
	DryRun             bool         `json:"dry_run"`
	Batches            int          `json:"batches"`
	Items              []importItem `json:"items"`
	// Complete ist false, wenn ein Stapel scheiterte — die Zahlen gelten dann
	// für die gespeicherten Stapel davor.
	Complete bool `json:"complete"`
	// AmbiguousRetry: Mindestens ein Stapel gelang erst nach einem Versuch,
	// der angekommen sein kann (5xx, Netzwerk). Dessen created/unchanged
	// beschreiben die Wiederholung und können vom ersten Versuch abweichen.
	AmbiguousRetry   bool  `json:"ambiguous_retry,omitempty"`
	AmbiguousBatches []int `json:"ambiguous_batches,omitempty"`
}

type importItem struct {
	Index    int    `json:"index"`
	ID       any    `json:"id"`
	ExternID any    `json:"extern_id"`
	Action   string `json:"action"`
}

type bulkResponse struct {
	Created            int  `json:"created"`
	Updated            int  `json:"updated"`
	Unchanged          int  `json:"unchanged"`
	PlaceholderRemoved bool `json:"placeholder_removed"`
	Items              []struct {
		Index    int    `json:"index"`
		ID       any    `json:"id"`
		ExternID any    `json:"extern_id"`
		Action   string `json:"action"`
	} `json:"items"`
}

func (r *runner) readUnitRecords(flags *flagValues) ([]record, error) {
	name, raw, err := r.readInput(flags)
	if err != nil {
		return nil, err
	}
	format, err := detectFormat(name, raw, flags.get("format"), "ndjson", "json", "csv")
	if err != nil {
		return nil, err
	}
	switch format {
	case "csv":
		return parseUnitsCSV(raw, flags.get("delimiter"), flags.bool("decimal-comma"))
	case "json":
		return parseJSONRecords(raw, "units")
	default:
		return parseNDJSON(raw)
	}
}

// duplicateKeys findet id und extern_id, die in der Eingabe mehrfach
// vorkommen. Das Backend sieht nur einen Stapel — über Stapel hinweg
// überschriebe der spätere Eintrag still den früheren.
func duplicateKeys(records []record) []any {
	var errs []any
	for _, key := range []string{"id", "extern_id"} {
		first := map[string]int{}
		for i, rec := range records {
			value := stringID(rec.data[key])
			if value == "" {
				continue
			}
			j, ok := first[value]
			if !ok {
				first[value] = i
				continue
			}
			entry := map[string]any{
				"index":   i,
				key:       value,
				"message": fmt.Sprintf("%s „%s“ steht schon in %s.", key, value, records[j].position(j)),
			}
			if rec.line > 0 {
				entry["line"] = rec.line
			}
			errs = append(errs, entry)
		}
	}
	return errs
}

func hasKey(rec record) bool {
	return stringID(rec.data["id"]) != "" || stringID(rec.data["extern_id"]) != ""
}

// runUnitsImport: Eingabe lesen, lokal prüfen, in Stapeln senden, summieren.
func (r *runner) runUnitsImport(immobilieID string, flags *flagValues, resolved config.Resolved) int {
	size := maxUnitsPerBatch
	if flags.has("batch-size") {
		raw := strings.TrimSpace(flags.get("batch-size"))
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxUnitsPerBatch {
			return r.fail(usageErr("--batch-size erwartet eine ganze Zahl von 1 bis %d, bekommen: %q", maxUnitsPerBatch, raw))
		}
		size = n
	}
	records, err := r.readUnitRecords(flags)
	if err != nil {
		return r.fail(err)
	}
	if len(records) == 0 {
		return r.fail(usageErr("Die Eingabe enthält keine Einheiten"))
	}
	if dups := duplicateKeys(records); len(dups) > 0 {
		return r.fail(&api.Error{
			Message: fmt.Sprintf("%d Einträge wiederholen eine id oder extern_id — es wurde nichts gesendet.", len(dups)),
			Code:    "DUPLICATE_KEY",
			Exit:    11,
			Details: map[string]any{"errors": dups},
		})
	}
	withoutKey := 0
	for _, rec := range records {
		if !hasKey(rec) {
			withoutKey++
		}
	}
	if withoutKey > 0 {
		_ = output.WriteWarning(r.stderr, fmt.Sprintf(
			"%d Einheiten ohne extern_id und id: ein erneuter Lauf legt sie noch einmal an, und ihr Stapel wird bei Netzwerkfehlern nicht wiederholt.",
			withoutKey), map[string]any{"without_key": withoutKey})
	}

	client, err := r.newClient(resolved, flags)
	if err != nil {
		return r.fail(err)
	}
	dryRun := flags.bool("dry-run")
	path := "/api/units/immobilie/" + url.PathEscape(immobilieID) + "/units/bulk"
	summary := importSummary{DryRun: dryRun, Items: []importItem{}}
	for offset := 0; offset < len(records); offset += size {
		end := offset + size
		if end > len(records) {
			end = len(records)
		}
		batch := records[offset:end]
		units := make([]map[string]any, 0, len(batch))
		idempotent := true
		for _, rec := range batch {
			units = append(units, rec.data)
			idempotent = idempotent && hasKey(rec)
		}
		body, err := output.Marshal(map[string]any{"units": units, "dry_run": dryRun})
		if err != nil {
			return r.fail(err)
		}
		// Idempotent nur, wenn jede Zeile über id/extern_id abgeglichen wird:
		// Dann findet ein zweiter Versuch dieselben Einheiten wieder.
		response, err := client.Do(api.Request{
			Method: "POST", Path: path, Body: body, ContentType: "application/json", Idempotent: idempotent,
			IdempotencyKey: derivedKey(flags, "b"+strconv.Itoa(summary.Batches+1)),
		})
		if err != nil {
			return r.failImport(summary, batchError(err, summary.Batches+1, offset, summary.Batches, records), flags)
		}
		var result bulkResponse
		if err := decodeJSONExact(response.Body, &result); err != nil {
			return r.failImport(summary, batchError(&api.Error{Message: "Antwort des Stapels ist kein JSON: " + err.Error(), Exit: 1},
				summary.Batches+1, offset, summary.Batches, records), flags)
		}
		if response.AmbiguousRetry {
			summary.AmbiguousRetry = true
			summary.AmbiguousBatches = append(summary.AmbiguousBatches, summary.Batches+1)
		}
		summary.Created += result.Created
		summary.Updated += result.Updated
		summary.Unchanged += result.Unchanged
		summary.PlaceholderRemoved = summary.PlaceholderRemoved || result.PlaceholderRemoved
		for _, item := range result.Items {
			summary.Items = append(summary.Items, importItem{
				Index: item.Index + offset, ID: item.ID, ExternID: item.ExternID, Action: item.Action,
			})
		}
		summary.Batches++
	}
	summary.Complete = true
	return r.emit(summary, flags)
}

// failImport schreibt die Summe der gespeicherten Stapel (complete: false)
// auf stdout und den Fehler auf stderr — sonst sähe der Agent nicht, was
// schon angelegt ist.
func (r *runner) failImport(summary importSummary, err error, flags *flagValues) int {
	summary.Complete = false
	if raw, marshalErr := output.Marshal(summary); marshalErr == nil {
		_ = r.render(raw, "application/json", flags)
	}
	return r.fail(err)
}
