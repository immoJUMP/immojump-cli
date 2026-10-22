package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/immoJUMP/immojump-cli/internal/api"
)

// record ist ein Datensatz aus einer Eingabedatei (units import, units
// fields-set). line ist die 1-basierte Zeile der Quelle — 0, wenn die Quelle
// keine Zeilen kennt (JSON-Array).
type record struct {
	data map[string]any
	line int
}

// position nennt den Datensatz so, wie ihn der Aufrufer in seiner Datei findet.
func (rec record) position(index int) string {
	if rec.line > 0 {
		return "Zeile " + strconv.Itoa(rec.line)
	}
	return "Eintrag " + strconv.Itoa(index) + " (0-basiert)"
}

// maxRecordLine begrenzt eine einzelne NDJSON-Zeile (bufio.Scanner kennt
// sonst nur 64 KB — eine Einheit mit langer Notiz sprengte das).
const maxRecordLine = 16 << 20

// readInput liest --file: einen Pfad oder - für stdin.
func (r *runner) readInput(flags *flagValues) (string, []byte, error) {
	source := strings.TrimSpace(flags.get("file"))
	if source == "" {
		return "", nil, usageErr("--file fehlt: Pfad zur Datei oder - für stdin")
	}
	if source == "-" {
		raw, err := io.ReadAll(r.stdin)
		if err != nil {
			return "", nil, usageErr("stdin nicht lesbar: %v", err)
		}
		return "-", raw, nil
	}
	raw, err := readFileOrErr(source)
	return source, raw, err
}

// detectFormat bestimmt das Eingabeformat: --format, sonst die Endung, bei
// stdin das erste Zeichen ('[' = JSON-Array, '{' = NDJSON).
func detectFormat(name string, raw []byte, explicit string, allowed ...string) (string, error) {
	isAllowed := func(format string) bool {
		for _, a := range allowed {
			if a == format {
				return true
			}
		}
		return false
	}
	if explicit = strings.ToLower(strings.TrimSpace(explicit)); explicit != "" {
		if !isAllowed(explicit) {
			return "", usageErr("--format kennt %s, bekommen: %q", strings.Join(allowed, ", "), explicit)
		}
		return explicit, nil
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".ndjson", ".jsonl":
		return "ndjson", nil
	case ".json":
		return "json", nil
	case ".csv":
		if isAllowed("csv") {
			return "csv", nil
		}
	}
	trimmed := bytes.TrimLeft(raw, " \t\r\n\uFEFF")
	switch {
	case len(trimmed) > 0 && trimmed[0] == '[':
		return "json", nil
	case len(trimmed) > 0 && trimmed[0] == '{':
		return "ndjson", nil
	}
	return "", usageErr("Format von %s nicht erkennbar — --format %s angeben", name, strings.Join(allowed, "|"))
}

// parseNDJSON liest ein JSON-Objekt je Zeile; Leerzeilen zählen nicht.
func parseNDJSON(raw []byte) ([]record, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), maxRecordLine)
	var out []record
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\uFEFF"))
		if text == "" {
			continue
		}
		var object map[string]any
		if err := decodeOneJSON([]byte(text), &object); err != nil || object == nil {
			return nil, usageErr("Zeile %d muss genau ein JSON-Objekt sein: %s", line, shorten(text, 80))
		}
		out = append(out, record{data: object, line: line})
	}
	if err := scanner.Err(); err != nil {
		return nil, usageErr("Eingabe nicht lesbar (Zeile %d): %v", line+1, err)
	}
	return out, nil
}

// parseJSONRecords liest ein JSON-Array von Objekten — oder ein Objekt, das
// das Array unter wrapperKey trägt (so sieht der Request-Body selbst aus).
func parseJSONRecords(raw []byte, wrapperKey string) ([]record, error) {
	var list []json.RawMessage
	if err := decodeOneJSON(raw, &list); err != nil {
		var wrapper map[string]json.RawMessage
		if errWrap := decodeOneJSON(raw, &wrapper); errWrap != nil || wrapper[wrapperKey] == nil {
			return nil, usageErr("JSON-Eingabe muss genau ein Array von Objekten sein (oder genau ein {%q: [...]})", wrapperKey)
		}
		// Im Umschlag gilt nur das Array. Ein dry_run oder model aus der
		// Datei würde sonst still übergangen — {"dry_run":true} schrieb echt.
		var extra []string
		for key := range wrapper {
			if key != wrapperKey {
				extra = append(extra, key)
			}
		}
		if len(extra) > 0 {
			sort.Strings(extra)
			return nil, usageErr("Im Umschlag ist nur %q erlaubt, gefunden außerdem: %s — "+
				"Optionen wie dry_run gehen nur über Flags (--dry-run), nie aus der Datei",
				wrapperKey, strings.Join(extra, ", "))
		}
		if err := decodeJSONExact(wrapper[wrapperKey], &list); err != nil {
			return nil, usageErr("%q muss ein Array von Objekten sein", wrapperKey)
		}
	}
	out := make([]record, 0, len(list))
	for i, item := range list {
		var object map[string]any
		if err := decodeJSONExact(item, &object); err != nil || object == nil {
			return nil, usageErr("Eintrag %d (0-basiert) ist kein JSON-Objekt", i)
		}
		out = append(out, record{data: object})
	}
	return out, nil
}

// decodeOneJSON liest genau einen JSON-Wert (Zahlen exakt) und verlangt
// danach das Ende. Ein zweites Objekt in derselben Zeile oder ein zweites
// Array in der Datei fiele sonst still weg.
func decodeOneJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("nach dem JSON-Wert folgen weitere Daten")
	}
	return nil
}

func shorten(text string, max int) string {
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}

// batchError ergänzt einen gescheiterten Stapel um seine Lage in der Datei:
// errors[].index zeigt danach in die Eingabe (batch_index ist der Wert des
// Backends), line nennt die Zeile, dazu committed_batches, batch und
// batch_offset. Die Meldung des Backends bleibt unverändert — sie zählt die
// Einträge innerhalb des Stapels.
func batchError(err error, batch, offset, committed int, records []record) error {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		apiErr = &api.Error{Message: err.Error(), Exit: 1}
	}
	out := *apiErr
	details := map[string]any{}
	for key, value := range apiErr.Details {
		details[key] = value
	}
	if list, ok := details["errors"].([]any); ok {
		shifted := make([]any, 0, len(list))
		for _, item := range list {
			entry, ok := item.(map[string]any)
			if !ok {
				shifted = append(shifted, item)
				continue
			}
			copied := map[string]any{}
			for key, value := range entry {
				copied[key] = value
			}
			if number, ok := entry["index"].(json.Number); ok {
				if idx, err := number.Int64(); err == nil {
					global := int(idx) + offset
					copied["batch_index"] = idx
					copied["index"] = global
					if global >= 0 && global < len(records) && records[global].line > 0 {
						copied["line"] = records[global].line
					}
				}
			}
			shifted = append(shifted, copied)
		}
		details["errors"] = shifted
	}
	details["committed_batches"] = committed
	details["batch"] = batch
	details["batch_offset"] = offset
	out.Details = details
	return &out
}
