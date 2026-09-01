package output

// Tabellenausgabe für Menschen. Der Default bleibt NDJSON: --table ist ein
// bewusster Schalter, keine TTY-Erkennung. Agenten-Runtimes (Claude Code,
// Codex, n8n) starten Prozesse häufig in einem PTY — eine Autoerkennung
// würde ihnen unangekündigt Tabellen statt JSON liefern, und dieser Fehler
// wäre lokal in der Shell nicht reproduzierbar.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// renderTable schreibt eine Antwort als Tabelle. Der zweite Rückgabewert sagt,
// ob das gelungen ist — sonst bleibt der Aufrufer bei JSON.
func renderTable(w io.Writer, raw json.RawMessage) (bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false, nil
	}

	switch trimmed[0] {
	case '[':
		return true, writeRows(w, trimmed, nil)
	case '{':
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &fields); err != nil {
			return false, nil
		}
		// Envelope ({items: […], total, page}): die Liste ist der Inhalt,
		// der Rest die Fusszeile. Ohne das Auspacken müsste ein Mensch erst
		// --fields items.… tippen, um überhaupt etwas zu sehen.
		if key, list, ok := envelopeList(fields); ok {
			footer := map[string]json.RawMessage{}
			for name, value := range fields {
				if name != key {
					footer[name] = value
				}
			}
			return true, writeRows(w, list, footer)
		}
		return true, writePairs(w, fields)
	default:
		// Skalar: unverändert ausgeben.
		_, err := fmt.Fprintln(w, strings.Trim(string(trimmed), `"`))
		return true, err
	}
}

// envelopeList findet die eine Liste in einem Envelope. Mehrere Listen wären
// mehrdeutig — dann bleibt es bei der Feld/Wert-Ansicht.
func envelopeList(fields map[string]json.RawMessage) (string, json.RawMessage, bool) {
	var key string
	found := 0
	for name, value := range fields {
		if isList(value) {
			key, found = name, found+1
		}
	}
	if found != 1 {
		return "", nil, false
	}
	// "items" ist der Regelfall; jede andere einzelne Liste zählt genauso,
	// solange sie die einzige ist.
	return key, fields[key], true
}

// writeRows tabelliert eine Liste von Objekten.
func writeRows(w io.Writer, raw json.RawMessage, footer map[string]json.RawMessage) error {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return err
	}
	if len(items) == 0 {
		if _, err := fmt.Fprintln(w, "(keine Einträge)"); err != nil {
			return err
		}
		return writeFooter(w, footer)
	}

	// Spaltenreihenfolge folgt dem ersten Objekt; später auftauchende Felder
	// werden hinten angehängt, damit nichts verschwindet.
	var columns []string
	seen := map[string]bool{}
	rows := make([]map[string]string, 0, len(items))
	for _, item := range items {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(item, &obj); err != nil {
			// Liste von Skalaren: eine Spalte.
			rows = append(rows, map[string]string{"wert": cell(item)})
			if !seen["wert"] {
				seen["wert"] = true
				columns = append(columns, "wert")
			}
			continue
		}
		for _, key := range orderedKeys(item, obj) {
			if !seen[key] {
				seen[key] = true
				columns = append(columns, key)
			}
		}
		row := make(map[string]string, len(obj))
		for key, value := range obj {
			row[key] = cell(value)
		}
		rows = append(rows, row)
	}

	widths := make([]int, len(columns))
	for i, col := range columns {
		widths[i] = utf8.RuneCountInString(col)
	}
	for _, row := range rows {
		for i, col := range columns {
			if n := utf8.RuneCountInString(row[col]); n > widths[i] {
				widths[i] = n
			}
		}
	}

	if err := writeRow(w, columns, widths, func(i int) string { return columns[i] }); err != nil {
		return err
	}
	rules := make([]string, len(columns))
	for i := range columns {
		rules[i] = strings.Repeat("─", widths[i])
	}
	if err := writeRow(w, columns, widths, func(i int) string { return rules[i] }); err != nil {
		return err
	}
	for _, row := range rows {
		if err := writeRow(w, columns, widths, func(i int) string { return row[columns[i]] }); err != nil {
			return err
		}
	}
	return writeFooter(w, footer)
}

// orderedKeys hält die Feldreihenfolge der Antwort ein, statt sie zu
// sortieren — das Backend stellt Wichtiges nach vorn.
func orderedKeys(raw json.RawMessage, obj map[string]json.RawMessage) []string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil { // öffnende Klammer
		return sortedRawKeys(obj)
	}
	keys := make([]string, 0, len(obj))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return sortedRawKeys(obj)
		}
		key, ok := token.(string)
		if !ok {
			return sortedRawKeys(obj)
		}
		keys = append(keys, key)
		var skip json.RawMessage
		if err := decoder.Decode(&skip); err != nil {
			return sortedRawKeys(obj)
		}
	}
	return keys
}

func sortedRawKeys(obj map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeRow(w io.Writer, columns []string, widths []int, value func(int) string) error {
	parts := make([]string, len(columns))
	for i := range columns {
		text := value(i)
		pad := widths[i] - utf8.RuneCountInString(text)
		if pad < 0 {
			pad = 0
		}
		if i == len(columns)-1 {
			parts[i] = text // letzte Spalte nicht auffüllen
			continue
		}
		parts[i] = text + strings.Repeat(" ", pad)
	}
	_, err := fmt.Fprintln(w, strings.Join(parts, "  "))
	return err
}

// writePairs zeigt ein einzelnes Objekt untereinander — bei 40 Feldern ist
// eine Zeile mit 40 Spalten unlesbar.
func writePairs(w io.Writer, fields map[string]json.RawMessage) error {
	keys := sortedRawKeys(fields)
	width := 0
	for _, key := range keys {
		if n := utf8.RuneCountInString(key); n > width {
			width = n
		}
	}
	for _, key := range keys {
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(key))
		if _, err := fmt.Fprintf(w, "%s%s  %s\n", key, pad, cell(fields[key])); err != nil {
			return err
		}
	}
	return nil
}

func writeFooter(w io.Writer, footer map[string]json.RawMessage) error {
	if len(footer) == 0 {
		return nil
	}
	parts := make([]string, 0, len(footer))
	for _, key := range sortedRawKeys(footer) {
		parts = append(parts, key+"="+cell(footer[key]))
	}
	_, err := fmt.Fprintf(w, "\n%s\n", strings.Join(parts, "  "))
	return err
}

// cell macht aus einem Rohwert eine Zellendarstellung: Strings ohne
// Anführungszeichen, alles Verschachtelte kompakt — ausgeklappt würde es die
// Tabelle sprengen.
func cell(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	switch trimmed[0] {
	case '"':
		var text string
		if err := json.Unmarshal(trimmed, &text); err == nil {
			return strings.ReplaceAll(text, "\n", " ")
		}
	case 'n':
		if string(trimmed) == "null" {
			return "—"
		}
	}
	compact, err := Compact(trimmed)
	if err != nil {
		return string(trimmed)
	}
	return string(compact)
}
