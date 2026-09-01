// Package output schreibt Nutzdaten nach stdout und Fehler als eine
// JSON-Zeile nach stderr. Kompakt ist der Default (Kontext-Ökonomie für
// Agenten), --pretty und --fields sind die beiden Schalter.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Options steuert die Darstellung.
type Options struct {
	Pretty bool
	// Fields sind Pfade wie "id" oder "adresse.stadt".
	Fields []string
	// Table rendert für Menschen statt für Agenten. Bewusst ein Schalter und
	// keine TTY-Erkennung: Agenten-Runtimes laufen oft in einem PTY und
	// bekämen sonst unangekündigt Tabellen statt JSON.
	Table bool
}

// Report sagt, was --fields in der Antwort gefunden hat. Ohne ihn bekäme ein
// Agent bei einer verpackten Antwort (`{"contact":{…}}`) nur ein `{}` zu sehen
// und hielte den Aufruf für gescheitert — im Zweifel legt er doppelt an.
type Report struct {
	// Requested sind alle angefragten Pfade in der Reihenfolge von --fields.
	Requested []string
	// Missing sind die Pfade, die in keinem untersuchten Objekt vorkamen.
	Missing []string
	// Keys sind die tatsächlich vorhandenen Top-Level-Schlüssel (gekürzt).
	Keys []string
	// KeysTruncated sagt, dass es mehr Schlüssel gibt als Keys nennt.
	KeysTruncated bool
}

// maxReportedKeys begrenzt die Schlüsselliste im Hinweis — sie soll führen,
// nicht die halbe Antwort wiederholen.
const maxReportedKeys = 8

// Render schreibt die Antwort. Nicht-JSON (z. B. Pipeline-Export als YAML)
// geht unverändert durch. Der Report ist nur bei --fields gefüllt.
func Render(w io.Writer, body []byte, contentType string, opts Options) (Report, error) {
	report := Report{}
	if len(bytes.TrimSpace(body)) == 0 {
		return report, nil
	}
	if !json.Valid(body) {
		return report, writeRaw(w, body)
	}

	payload := json.RawMessage(body)
	if len(opts.Fields) > 0 {
		collector := newFieldCollector(parsePaths(opts.Fields))
		projected, err := project(payload, collector.paths, collector)
		if err != nil {
			return report, err
		}
		payload = projected
		report = collector.report()
	}

	// Tabelle nach der Projektion: --fields grenzt ein, --table stellt dar.
	if opts.Table {
		done, err := renderTable(w, payload)
		if err != nil {
			return report, err
		}
		if done {
			return report, nil
		}
	}

	buf := &bytes.Buffer{}
	if opts.Pretty {
		if err := json.Indent(buf, payload, "", "  "); err != nil {
			return report, fmt.Errorf("Ausgabe nicht formatierbar: %w", err)
		}
	} else if err := json.Compact(buf, payload); err != nil {
		return report, fmt.Errorf("Ausgabe nicht kompaktierbar: %w", err)
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return report, err
}

func writeRaw(w io.Writer, body []byte) error {
	if _, err := w.Write(body); err != nil {
		return err
	}
	if !bytes.HasSuffix(body, []byte("\n")) {
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
	}
	return nil
}

// Marshal serialisiert ohne HTML-Escaping. Meldungen wie
// `--context <name>` sollen lesbar bleiben und nicht als <name>
// im Terminal landen.
func Marshal(value any) ([]byte, error) {
	buf := &bytes.Buffer{}
	encoder := json.NewEncoder(buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Compact entfernt Whitespace aus JSON. Anders als ein stiller Fallback auf
// die Eingabe meldet es kaputtes JSON — der Aufrufer soll das merken.
func Compact(raw []byte) ([]byte, error) {
	buf := &bytes.Buffer{}
	if err := json.Compact(buf, raw); err != nil {
		return nil, fmt.Errorf("JSON nicht kompaktierbar: %w", err)
	}
	return buf.Bytes(), nil
}

// field ist ein Schlüssel-Wert-Paar in fester Reihenfolge — anders als eine
// Map, die encoding/json alphabetisch sortiert.
type field struct {
	key   string
	value any
}

// writeLine schreibt genau eine JSON-Zeile in der übergebenen Reihenfolge.
func writeLine(w io.Writer, fields []field) error {
	buf := &bytes.Buffer{}
	buf.WriteByte('{')
	for i, f := range fields {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := Marshal(f.key)
		if err != nil {
			return err
		}
		value, err := Marshal(f.value)
		if err != nil {
			return err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteString("}\n")
	_, err := w.Write(buf.Bytes())
	return err
}

// WriteError schreibt die Fehlerzeile für stderr: erst die festen Felder
// (error, status, message, code — Bedeutung und Position unverändert), danach
// jedes weitere Feld der Backend-Antwort.
//
// Das ist der Kern: `api_error()` erlaubt beliebige Zusatzfelder, und genau
// darin steht die Lösung für den Aufrufer — welches Feld falsch war
// (`errors`), welche Werte erlaubt sind (`valid_values`), wie der
// Kontingentstand aussieht (402). Eine Fehlerzeile, die nur `message`
// durchreicht, wirft die Selbstkorrektur weg.
func WriteError(w io.Writer, status int, message, code string, details map[string]any) error {
	fields := []field{{"error", true}}
	if status != 0 {
		fields = append(fields, field{"status", status})
	}
	fields = append(fields, field{"message", message})
	if code != "" {
		fields = append(fields, field{"code", code})
	}
	return writeLine(w, append(fields, extraFields(details, status, message, code)...))
}

// WriteWarning schreibt eine Hinweiszeile nach stderr. Sie trägt bewusst
// `warning` statt `error`: Der Aufruf ist gelungen (Exit 0), es gibt nur etwas
// zu wissen.
func WriteWarning(w io.Writer, message string, details map[string]any) error {
	fields := []field{{"warning", true}, {"message", message}}
	return writeLine(w, append(fields, extraFields(details, 0, "", "")...))
}

// WriteTrace schreibt die Zeile für --verbose: Methode und vollständige URL,
// bevor der Request rausgeht. Bewusst kein Header und kein Body — dort steht
// der Token.
func WriteTrace(w io.Writer, method, url string) error {
	return writeLine(w, []field{{"trace", true}, {"method", method}, {"url", url}})
}

// extraFields hängt die übrigen Felder der Backend-Antwort an — sortiert,
// damit die Zeile reproduzierbar bleibt.
//
// Bei den CLI-eigenen Schlüsseln `error` und `status` gewinnt das CLI; der
// Backend-Wert wandert nach `backend_error`/`backend_status`, statt verloren
// zu gehen. Was in `message`/`code` schon steht, wird nicht wiederholt.
func extraFields(details map[string]any, status int, message, code string) []field {
	if len(details) == 0 {
		return nil
	}
	out := make([]field, 0, len(details))
	for _, key := range sortedKeys(details) {
		value := details[key]
		switch key {
		case "message":
			if text, ok := value.(string); ok && text == message {
				continue
			}
		case "code":
			if text, ok := value.(string); ok && text == code {
				continue
			}
		case "status":
			if number, ok := value.(json.Number); ok && number.String() == strconv.Itoa(status) {
				continue
			}
		case "error":
		default:
			out = append(out, field{key, value})
			continue
		}
		out = append(out, field{freeKey("backend_"+key, details), value})
	}
	return out
}

// freeKey sucht einen Namen, den die Antwort nicht selbst schon belegt.
func freeKey(name string, details map[string]any) string {
	for {
		if _, taken := details[name]; !taken {
			return name
		}
		name = "backend_" + name
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// parsePaths zerlegt "a,b.c" in [][]string{{"a"},{"b","c"}}.
func parsePaths(fields []string) [][]string {
	paths := make([][]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		parts := strings.Split(field, ".")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		paths = append(paths, parts)
	}
	return paths
}

// fieldCollector protokolliert während der Projektion, welche Pfade wirklich
// vorkamen und welche Schlüssel die Antwort stattdessen hat.
type fieldCollector struct {
	paths [][]string
	hit   []bool
	keys  map[string]bool
	// objects zählt die untersuchten Objekte. Ohne ein einziges (leere Liste,
	// Skalar) gibt es nichts zu melden — dort ist nichts zu finden.
	objects int
}

func newFieldCollector(paths [][]string) *fieldCollector {
	return &fieldCollector{paths: paths, hit: make([]bool, len(paths)), keys: map[string]bool{}}
}

func (c *fieldCollector) seeObject(fields map[string]json.RawMessage) {
	c.objects++
	for key := range fields {
		c.keys[key] = true
	}
}

func (c *fieldCollector) markHit(index int) { c.hit[index] = true }

func (c *fieldCollector) report() Report {
	report := Report{}
	if c.objects == 0 {
		return report
	}
	for i, path := range c.paths {
		joined := strings.Join(path, ".")
		report.Requested = append(report.Requested, joined)
		if !c.hit[i] {
			report.Missing = append(report.Missing, joined)
		}
	}
	keys := make([]string, 0, len(c.keys))
	for key := range c.keys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > maxReportedKeys {
		keys, report.KeysTruncated = keys[:maxReportedKeys], true
	}
	report.Keys = keys
	return report
}

// project reduziert Objekte bzw. alle Elemente eines Arrays auf die
// angefragten Pfade. Die Ausgabereihenfolge folgt der Reihenfolge in --fields.
func project(raw json.RawMessage, paths [][]string, collector *fieldCollector) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return raw, nil
	}
	switch trimmed[0] {
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, fmt.Errorf("Array nicht lesbar: %w", err)
		}
		buf := &bytes.Buffer{}
		buf.WriteByte('[')
		for i, item := range items {
			if i > 0 {
				buf.WriteByte(',')
			}
			projected, err := project(item, paths, collector)
			if err != nil {
				return nil, err
			}
			buf.Write(projected)
		}
		buf.WriteByte(']')
		return json.RawMessage(buf.Bytes()), nil
	case '{':
		// Genau ein Unmarshal pro Objekt (also pro Listenelement): Alle
		// Top-Level-Felder kommen aus dieser einen Karte, nur für
		// verschachtelte Pfade wird weiter abgestiegen.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &fields); err != nil {
			return nil, fmt.Errorf("Objekt nicht lesbar: %w", err)
		}
		collector.seeObject(fields)

		// Pfade, die in eine Liste zeigen ("items.id"), werden elementweise
		// projiziert — sonst bräche lookup() am Array ab und --fields wäre
		// ausgerechnet bei paginierten Antworten wirkungslos.
		lists := listProjections(fields, paths)

		tree := &node{}
		done := map[string]bool{}
		for i, path := range paths {
			value, ok := fields[path[0]]
			if !ok {
				continue
			}
			if sub, isSub := lists[path[0]]; isSub && len(path) > 1 {
				// Reihenfolge folgt dem ersten Pfad dieser Liste in --fields.
				if sub.hit[indexOf(sub.origin, i)] {
					collector.markHit(i)
				}
				if !done[path[0]] {
					done[path[0]] = true
					tree.insert(path[:1], sub.value)
				}
				continue
			}
			if len(path) > 1 {
				if value, ok = lookup(value, path[1:]); !ok {
					continue
				}
			}
			collector.markHit(i)
			tree.insert(path, value)
		}
		return tree.marshalObject()
	default:
		// Skalare lassen sich nicht projizieren — unverändert durchreichen.
		return raw, nil
	}
}

// lookup folgt einem Pfad durch verschachtelte Objekte.
func lookup(raw json.RawMessage, path []string) (json.RawMessage, bool) {
	current := raw
	for _, key := range path {
		trimmed := bytes.TrimSpace(current)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			return nil, false
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(trimmed, &obj); err != nil {
			return nil, false
		}
		value, ok := obj[key]
		if !ok {
			return nil, false
		}
		current = value
	}
	return current, true
}

// isList sagt, ob ein Rohwert ein JSON-Array ist.
func isList(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '['
}

// listProjection ist das Ergebnis für einen Schlüssel, dessen Wert eine Liste
// ist: das projizierte Array plus die Treffer je Restpfad.
type listProjection struct {
	value  json.RawMessage
	hit    []bool
	origin []int // Index des Restpfads im ursprünglichen paths-Slice
}

// listProjections projiziert jeden Listen-Schlüssel einmal mit ALLEN seinen
// Restpfaden. Nur so entsteht [{id,name},…] statt {id:[…],name:[…]} — die
// Elemente müssen zusammenbleiben.
//
// Eine leere Liste zählt als Treffer: `items.id` ist ein gültiger Pfad, auch
// wenn gerade nichts drin steht. Sonst warnte ein leeres Postfach so, als
// hätte sich der Aufrufer vertippt.
func listProjections(fields map[string]json.RawMessage, paths [][]string) map[string]*listProjection {
	rest := map[string][][]string{}
	origin := map[string][]int{}
	for i, path := range paths {
		if len(path) < 2 {
			continue
		}
		value, ok := fields[path[0]]
		if !ok || !isList(value) {
			continue
		}
		rest[path[0]] = append(rest[path[0]], path[1:])
		origin[path[0]] = append(origin[path[0]], i)
	}
	if len(rest) == 0 {
		return nil
	}
	out := make(map[string]*listProjection, len(rest))
	for key, subPaths := range rest {
		subCollector := newFieldCollector(subPaths)
		projected, err := project(fields[key], subPaths, subCollector)
		if err != nil {
			continue
		}
		hit := subCollector.hit
		if bytes.Equal(bytes.TrimSpace(fields[key]), []byte("[]")) {
			hit = make([]bool, len(subPaths))
			for i := range hit {
				hit[i] = true
			}
		}
		out[key] = &listProjection{value: projected, hit: hit, origin: origin[key]}
	}
	return out
}

func indexOf(values []int, want int) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return 0
}

// node ist ein Ausgabebaum, der die Einfügereihenfolge behält.
type node struct {
	keys     []string
	children map[string]*node
	value    json.RawMessage
	isLeaf   bool
}

func (n *node) child(key string) *node {
	if n.children == nil {
		n.children = map[string]*node{}
	}
	if existing, ok := n.children[key]; ok {
		return existing
	}
	created := &node{}
	n.children[key] = created
	n.keys = append(n.keys, key)
	return created
}

// insert hängt einen Wert an einen Pfad. Überschneiden sich zwei Pfade
// (`--fields adresse,adresse.stadt`), gewinnt immer der breitere — unabhängig
// davon, in welcher Reihenfolge sie angegeben wurden.
func (n *node) insert(path []string, value json.RawMessage) {
	current := n
	for i, key := range path {
		next := current.child(key)
		if i == len(path)-1 {
			// Der breitere Pfad ersetzt bereits gesammelte Teilfelder.
			next.isLeaf = true
			next.value = value
			next.keys = nil
			next.children = nil
			return
		}
		if next.isLeaf {
			// Der breitere Pfad steht schon da — der schmalere ist enthalten.
			return
		}
		current = next
	}
}

func (n *node) marshalObject() (json.RawMessage, error) {
	buf := &bytes.Buffer{}
	buf.WriteByte('{')
	for i, key := range n.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		encodedKey, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf.Write(encodedKey)
		buf.WriteByte(':')
		child := n.children[key]
		if child.isLeaf {
			compact := &bytes.Buffer{}
			if err := json.Compact(compact, child.value); err != nil {
				return nil, err
			}
			buf.Write(compact.Bytes())
			continue
		}
		nested, err := child.marshalObject()
		if err != nil {
			return nil, err
		}
		buf.Write(nested)
	}
	buf.WriteByte('}')
	return json.RawMessage(buf.Bytes()), nil
}
