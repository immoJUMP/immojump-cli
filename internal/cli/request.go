package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/immoJUMP/immojump-cli/internal/api"
	"github.com/immoJUMP/immojump-cli/internal/config"
	"github.com/immoJUMP/immojump-cli/internal/output"
)

// shareEntityTypes spiegelt SHARE_ENTITY_TYPES im Backend.
var shareEntityTypes = []struct{ Flag, Type string }{
	{"immobilie", "immobilie"},
	{"dokument", "dokument"},
	{"bild", "bild"},
}

// unsupportedGlobals nennt je Sonderfall die globalen Flags, die dieser Befehl
// nicht verarbeiten kann. Sie stillschweigend zu ignorieren wäre schlimmer als
// ein Bedienfehler: Ein Agent hielte sein Feld für angekommen.
var unsupportedGlobals = map[string][]string{
	SpecialDocumentsUpload: {"set", "body"},
	SpecialPipelineImport:  {"set", "body"},
	SpecialTagsSet:         {"set", "body"},
	SpecialAuthLogin:       {"q", "set", "body"},
	SpecialAuthStatus:      {"q", "set", "body"},
	SpecialContextList:     {"q", "set", "body"},
	SpecialContextCurrent:  {"q", "set", "body"},
	SpecialContextUse:      {"q", "set", "body"},
	SpecialContextDelete:   {"q", "set", "body"},
}

// rejectsBodyFlags sagt, ob ein Befehl seinen Body komplett selbst baut.
func rejectsBodyFlags(spec Spec) bool {
	for _, name := range unsupportedGlobals[spec.Special] {
		if name == "body" {
			return true
		}
	}
	return false
}

// checkGlobalFlagSupport lehnt globale Flags ab, die der Befehl nicht umsetzen
// kann — mit einem Hinweis darauf, wie der Body sonst entsteht.
func checkGlobalFlagSupport(spec Spec, flags *flagValues) error {
	unsupported := unsupportedGlobals[spec.Special]
	if len(unsupported) == 0 {
		return nil
	}
	var used []string
	for _, name := range unsupported {
		if flags.has(name) {
			used = append(used, flagLabel(Flag{Name: name}))
		}
	}
	if len(used) == 0 {
		return nil
	}
	var labels []string
	builds := "Body"
	for _, name := range unsupported {
		labels = append(labels, flagLabel(Flag{Name: name}))
		if name == "q" {
			// Wer auch -q ablehnt, baut den kompletten Request selbst.
			builds = "Request"
		}
	}
	return usageErr("%s werden von %q nicht unterstützt; dieser Befehl baut seinen %s selbst (gesetzt: %s)",
		strings.Join(labels, "/"), spec.Name(), builds, strings.Join(used, ", "))
}

// buildRequest übersetzt eine Command-Spec plus Argumente in einen API-Aufruf.
func (r *runner) buildRequest(spec Spec, args []string, flags *flagValues, resolved config.Resolved) (*api.Request, error) {
	method := spec.Method
	path := spec.Path

	if spec.Special == SpecialAPI {
		method = strings.ToUpper(strings.TrimSpace(args[0]))
		normalized, err := normalizeAPIPath(args[1])
		if err != nil {
			return nil, err
		}
		path = normalized
	} else {
		built, err := buildPath(spec, args, resolved.Org)
		if err != nil {
			return nil, err
		}
		path = built
	}

	query, err := buildQuery(spec, flags)
	if err != nil {
		return nil, err
	}

	request := &api.Request{
		Method:         method,
		Path:           path,
		Query:          query,
		IdempotencyKey: flags.get("idempotency-key"),
	}

	switch spec.Special {
	case SpecialDocumentsUpload:
		if resolved.Org == "" {
			return nil, configErr(
				"Für den Upload wird eine Organisation gebraucht. Setze IMMOJUMP_ORGANISATION_ID oder --org.")
		}
		multipart, err := buildUpload(args[0], flags, resolved.Org)
		if err != nil {
			return nil, err
		}
		request.Multipart = multipart
		return request, nil

	case SpecialPipelineImport:
		// Die Import-Route liest die Organisation ausschließlich aus dem
		// Payload oder dem Query-String — den Header X-Organisation-Id
		// ignoriert sie. Ohne den Parameter antwortet sie mit 400.
		if resolved.Org == "" {
			return nil, configErr(
				"%q braucht eine Organisation. Setze IMMOJUMP_ORGANISATION_ID, --org oder lege einen Context an.",
				spec.Name())
		}
		body, err := r.readYAML(flags)
		if err != nil {
			return nil, err
		}
		if request.Query == nil {
			request.Query = url.Values{}
		}
		request.Query.Set("organisation_id", resolved.Org)
		request.Body = body
		request.ContentType = "application/x-yaml"
		return request, nil

	case SpecialTagsSet:
		body, err := buildTagIDs(flags)
		if err != nil {
			return nil, err
		}
		request.Body = body
		request.ContentType = "application/json"
		return request, nil
	}

	body, err := r.buildJSONBody(spec, method, flags)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Body = body
		request.ContentType = "application/json"
	}
	return request, nil
}

// buildPath füllt {arg}- und {org}-Platzhalter.
func buildPath(spec Spec, args []string, org string) (string, error) {
	path := spec.Path
	for i, arg := range spec.Args {
		if i >= len(args) {
			break
		}
		path = strings.ReplaceAll(path, "{"+arg.Name+"}", url.PathEscape(args[i]))
	}
	if strings.Contains(path, "{org}") {
		if org == "" {
			return "", configErr(
				"%q braucht eine Organisation im Pfad. Setze IMMOJUMP_ORGANISATION_ID, --org oder lege einen Context an.",
				spec.Name())
		}
		path = strings.ReplaceAll(path, "{org}", url.PathEscape(org))
	}
	return path, nil
}

func normalizeAPIPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !strings.HasPrefix(path, "/api/") {
		return "", usageErr(
			"Der Escape-Hatch ruft nur /api/-Pfade auf, %q beginnt nicht mit /api/", raw)
	}
	return path, nil
}

func buildQuery(spec Spec, flags *flagValues) (url.Values, error) {
	query := url.Values{}
	for _, pair := range flags.all("q") {
		key, value, err := splitPair(pair, "-q")
		if err != nil {
			return nil, err
		}
		query.Add(key, value)
	}
	for _, mapping := range spec.Query {
		if flags.has(mapping.Flag) {
			query.Set(mapping.Key, flags.get(mapping.Flag))
		}
	}
	if len(query) == 0 {
		return nil, nil
	}
	return query, nil
}

func splitPair(raw, flag string) (string, string, error) {
	idx := strings.Index(raw, "=")
	if idx <= 0 {
		return "", "", usageErr("%s erwartet schluessel=wert, bekommen: %q", flag, raw)
	}
	return raw[:idx], raw[idx+1:], nil
}

// overlay ist ein einzelnes Body-Feld, das gesetzt werden soll.
type overlay struct {
	path  []string
	value any
}

// bodyOverlays sammelt --set und die kuratierten Sugar-Flags. Sugar überlagert
// bewusst --body und --set.
func bodyOverlays(spec Spec, flags *flagValues) ([]overlay, error) {
	var overlays []overlay

	for _, raw := range flags.all("set") {
		key, value, err := splitPair(raw, "--set")
		if err != nil {
			return nil, err
		}
		overlays = append(overlays, overlay{path: strings.Split(key, "."), value: parseSetValue(value)})
	}

	if err := checkBodyFlagConflicts(spec, flags); err != nil {
		return nil, err
	}

	for _, mapping := range spec.Body {
		if !flags.has(mapping.Flag) {
			continue
		}
		flagSpec, ok := findFlag(spec, mapping.Flag)
		if !ok {
			continue
		}
		if mapping.Null {
			// Ein Schalter, der ein Feld leert (`"password": null`).
			if !flags.bool(mapping.Flag) {
				continue
			}
			overlays = append(overlays, overlay{path: strings.Split(mapping.Key, "."), value: nil})
			continue
		}
		value, err := flagBodyValue(flagSpec, flags)
		if err != nil {
			return nil, err
		}
		overlays = append(overlays, overlay{path: strings.Split(mapping.Key, "."), value: value})
	}

	if spec.Special == SpecialSharesCreate {
		if items := shareItems(flags); len(items) > 0 {
			overlays = append(overlays, overlay{path: []string{"items"}, value: items})
		}
	}
	return overlays, nil
}

// flagBodyValue macht aus einem Flag den Wert, der im JSON-Body landet.
func flagBodyValue(flag Flag, flags *flagValues) (any, error) {
	if flag.Kind == FlagBool {
		return flags.bool(flag.Name), nil
	}
	raw := flags.get(flag.Name)
	if flag.NonEmpty != "" && strings.TrimSpace(raw) == "" {
		return nil, usageErr("--%s darf nicht leer sein — %s", flag.Name, flag.NonEmpty)
	}
	if flag.Kind == FlagNumber {
		number, ok := parseNumber(raw)
		if !ok {
			return nil, usageErr("--%s erwartet eine Zahl, bekommen: %q", flag.Name, raw)
		}
		return number, nil
	}
	if flag.Norm == NormDateTime {
		raw = normalizeDateTime(raw)
	}
	return raw, nil
}

// checkBodyFlagConflicts fängt zwei Flags ab, die auf denselben Body-Schlüssel
// zeigen (--password gegen --remove-password). Ohne die Prüfung gewänne
// schlicht das spätere — je nach Reihenfolge in der Registry.
func checkBodyFlagConflicts(spec Spec, flags *flagValues) error {
	seen := map[string]string{}
	for _, mapping := range spec.Body {
		if !flags.has(mapping.Flag) {
			continue
		}
		// Ein ausgeschalteter Schalter schreibt nichts und kollidiert nicht.
		if mapping.Null && !flags.bool(mapping.Flag) {
			continue
		}
		if other, ok := seen[mapping.Key]; ok {
			return usageErr("--%s und --%s schließen sich aus", other, mapping.Flag)
		}
		seen[mapping.Key] = mapping.Flag
	}
	return nil
}

func findFlag(spec Spec, name string) (Flag, bool) {
	for _, flag := range spec.Flags {
		if flag.Name == name {
			return flag, true
		}
	}
	return Flag{}, false
}

// shareItems baut die items-Liste aus --immobilie/--dokument/--bild.
func shareItems(flags *flagValues) []any {
	var items []any
	for _, kind := range shareEntityTypes {
		for _, id := range flags.all(kind.Flag) {
			items = append(items, map[string]any{"entity_type": kind.Type, "entity_id": id})
		}
	}
	return items
}

// buildJSONBody setzt --body, --set und Sugar-Flags zusammen.
func (r *runner) buildJSONBody(spec Spec, method string, flags *flagValues) ([]byte, error) {
	overlays, err := bodyOverlays(spec, flags)
	if err != nil {
		return nil, err
	}

	var base []byte
	if flags.has("body") {
		raw, err := r.readBodySource(flags.get("body"))
		if err != nil {
			return nil, err
		}
		if !json.Valid(raw) {
			return nil, usageErr("--body ist kein gültiges JSON")
		}
		base = raw
	}

	needsBody := base != nil || len(overlays) > 0 || methodExpectsBody(method)
	if !needsBody {
		return nil, nil
	}

	var body []byte
	if len(overlays) == 0 && base != nil {
		// Unverändert durchreichen — auch Arrays und die Feldreihenfolge.
		compact, err := output.Compact(base)
		if err != nil {
			return nil, usageErr("--body ist kein gültiges JSON: %v", err)
		}
		body = compact
	} else {
		object := map[string]any{}
		if base != nil {
			if err := decodeJSONExact(base, &object); err != nil {
				return nil, usageErr(
					"--body muss ein JSON-Objekt sein, wenn zusätzlich --set oder Sugar-Flags gesetzt sind")
			}
		}
		for _, item := range overlays {
			setPath(object, item.path, item.value)
		}
		marshalled, err := output.Marshal(object)
		if err != nil {
			return nil, usageErr("Body nicht serialisierbar: %v", err)
		}
		body = marshalled
	}

	if err := checkBody(spec, body); err != nil {
		return nil, err
	}
	return body, nil
}

// checkBody fängt die Fälle ab, die sonst als 400 vom Backend zurückkämen —
// dann aber nach einem unnötigen Roundtrip.
func checkBody(spec Spec, body []byte) error {
	if spec.EmptyBodyHint == "" {
		return nil
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		return usageErr("--body muss für %q ein JSON-Objekt sein", spec.Name())
	}
	if spec.Special == SpecialSharesCreate {
		// Ein Titel allein ist kein Inhalt: Ohne items gibt es nichts freizugeben.
		if items, _ := probe["items"].([]any); len(items) == 0 {
			return usageErr("%s", spec.EmptyBodyHint)
		}
		return nil
	}
	if len(probe) == 0 {
		return usageErr("%s", spec.EmptyBodyHint)
	}
	return nil
}

// decodeJSONExact liest JSON ohne Zahlen zu verfälschen: Ohne UseNumber macht
// encoding/json aus jeder Zahl ein float64 — große IDs verlieren Stellen,
// 225000.00 wird zu 225000. Beim Re-Marshal landet das beim Kunden.
func decodeJSONExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func methodExpectsBody(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch:
		return true
	default:
		return false
	}
}

// setPath schreibt einen Wert an einen gepunkteten Pfad.
func setPath(object map[string]any, path []string, value any) {
	current := object
	for i, key := range path {
		if i == len(path)-1 {
			current[key] = value
			return
		}
		next, ok := current[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[key] = next
		}
		current = next
	}
}

// parseSetValue interpretiert den Wert als JSON-Literal, sonst als String.
// Zahlen bleiben exakt (json.Number), damit --set nichts verfälscht.
func parseSetValue(raw string) any {
	parsed, ok := decodeSingleJSONValue(raw)
	if !ok {
		return raw
	}
	return parsed
}

// parseNumber akzeptiert genau eine JSON-Zahl.
func parseNumber(raw string) (json.Number, bool) {
	parsed, ok := decodeSingleJSONValue(strings.TrimSpace(raw))
	if !ok {
		return "", false
	}
	number, isNumber := parsed.(json.Number)
	return number, isNumber
}

// decodeSingleJSONValue liest genau ein JSON-Literal. Mehr als eines ("1 2")
// gilt als kein JSON — das ist dann ein String.
func decodeSingleJSONValue(raw string) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		return nil, false
	}
	if decoder.More() {
		return nil, false
	}
	return parsed, true
}

// normalizeDateTime macht aus "2026-09-30" das Ende dieses Tages — dieselbe
// Semantik ("gültig bis einschließlich") wie die Web-App in
// immobilien-ka/src/Services/ShareLinkService.ts. Ohne Zeitzonen-Suffix:
// Was die Zeitzone des Aufrufers ist, weiß nur das Backend. Vollständige
// Zeitstempel gehen unverändert durch (das Backend validiert).
func normalizeDateTime(raw string) string {
	value := strings.TrimSpace(raw)
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return raw
	}
	for i, char := range value {
		if i == 4 || i == 7 {
			continue
		}
		if char < '0' || char > '9' {
			return raw
		}
	}
	return value + "T23:59:59"
}

// readFileOrErr liest eine vom Nutzer benannte Datei — der Fehler ist immer
// ein Bedienfehler (falscher Pfad), nie ein API-Problem.
func readFileOrErr(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, usageErr("Datei %s nicht lesbar: %v", path, err)
	}
	return raw, nil
}

// readBodySource löst --body auf: JSON, @datei oder - für stdin.
func (r *runner) readBodySource(source string) ([]byte, error) {
	switch {
	case source == "-":
		raw, err := io.ReadAll(r.stdin)
		if err != nil {
			return nil, usageErr("stdin nicht lesbar: %v", err)
		}
		return raw, nil
	case strings.HasPrefix(source, "@"):
		return readFileOrErr(source[1:])
	default:
		return []byte(source), nil
	}
}

// readYAML liefert den Body für pipelines import.
func (r *runner) readYAML(flags *flagValues) ([]byte, error) {
	if path := strings.TrimSpace(flags.get("file")); path != "" {
		return readFileOrErr(path)
	}
	raw, err := io.ReadAll(r.stdin)
	if err != nil {
		return nil, usageErr("stdin nicht lesbar: %v", err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, usageErr("Kein YAML übergeben. Nutze --file <pfad> oder leite die Datei über stdin ein.")
	}
	return raw, nil
}

// buildTagIDs baut das rohe JSON-Array, das /api/tags/{type}/{id} erwartet.
func buildTagIDs(flags *flagValues) ([]byte, error) {
	ids := []string{}
	for _, raw := range flags.all("tag-ids") {
		ids = append(ids, config.SplitCSV(raw)...)
	}
	body, err := output.Marshal(ids)
	if err != nil {
		return nil, usageErr("Tag-IDs nicht serialisierbar: %v", err)
	}
	return body, nil
}

// buildUpload baut den Multipart-Body für documents upload.
func buildUpload(path string, flags *flagValues, org string) (*api.Multipart, error) {
	content, err := readFileOrErr(path)
	if err != nil {
		return nil, err
	}
	fields := map[string]string{"organisation_id": org}
	if immobilie := strings.TrimSpace(flags.get("immobilie-id")); immobilie != "" {
		fields["immobilien_id"] = immobilie
	}
	if flags.bool("allow-duplicate") {
		fields["allow_duplicate_upload"] = "true"
	}
	return &api.Multipart{
		Files:  []api.FilePart{{Field: "files[]", Filename: filepath.Base(path), Content: content}},
		Fields: fields,
	}, nil
}
