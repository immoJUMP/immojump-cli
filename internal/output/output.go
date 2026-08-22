// Package output schreibt Nutzdaten nach stdout und Fehler als eine
// JSON-Zeile nach stderr. Kompakt ist der Default (Kontext-Ökonomie für
// Agenten), --pretty und --fields sind die beiden Schalter.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Options steuert die Darstellung.
type Options struct {
	Pretty bool
	// Fields sind Pfade wie "id" oder "adresse.stadt".
	Fields []string
}

// Render schreibt die Antwort. Nicht-JSON (z. B. Pipeline-Export als YAML)
// geht unverändert durch.
func Render(w io.Writer, body []byte, contentType string, opts Options) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	if !json.Valid(body) {
		return writeRaw(w, body)
	}

	payload := json.RawMessage(body)
	if len(opts.Fields) > 0 {
		projected, err := project(payload, parsePaths(opts.Fields))
		if err != nil {
			return err
		}
		payload = projected
	}

	buf := &bytes.Buffer{}
	if opts.Pretty {
		if err := json.Indent(buf, payload, "", "  "); err != nil {
			return fmt.Errorf("Ausgabe nicht formatierbar: %w", err)
		}
	} else if err := json.Compact(buf, payload); err != nil {
		return fmt.Errorf("Ausgabe nicht kompaktierbar: %w", err)
	}
	buf.WriteByte('\n')
	_, err := w.Write(buf.Bytes())
	return err
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

// errorPayload ist die Fehlerzeile auf stderr. Die Feldreihenfolge ist die
// Deklarationsreihenfolge; status und code entfallen, wenn sie leer sind
// (z. B. bei lokalen Usage-Fehlern).
type errorPayload struct {
	Error   bool   `json:"error"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// WriteError schreibt die Fehlerzeile für stderr.
func WriteError(w io.Writer, status int, message, code string) error {
	encoded, err := Marshal(errorPayload{Error: true, Status: status, Message: message, Code: code})
	if err != nil {
		return err
	}
	if _, err := w.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
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

// project reduziert Objekte bzw. alle Elemente eines Arrays auf die
// angefragten Pfade. Die Ausgabereihenfolge folgt der Reihenfolge in --fields.
func project(raw json.RawMessage, paths [][]string) (json.RawMessage, error) {
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
			projected, err := project(item, paths)
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
		tree := &node{}
		for _, path := range paths {
			value, ok := fields[path[0]]
			if !ok {
				continue
			}
			if len(path) > 1 {
				if value, ok = lookup(value, path[1:]); !ok {
					continue
				}
			}
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
