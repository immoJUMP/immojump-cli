package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/immoJUMP/immojump-cli/internal/api"
	"github.com/immoJUMP/immojump-cli/internal/config"
	"github.com/immoJUMP/immojump-cli/internal/output"
)

// uploadTimeout ist der Default je Upload-Stapel: Das Backend wandelt um und
// macht OCR, bevor es antwortet — 60 s reichen für 25 MB Scans nicht.
const uploadTimeout = 300 * time.Second

// Stapel-Defaults für documents upload.
const (
	defaultBatchFiles = 10
	defaultBatchMB    = 25.0
)

// skippedNames sind Systemdateien, die in keinem Datenraum etwas verloren
// haben. Versteckte Dateien und Ordner (Punkt vorne) fallen ohnehin raus.
var skippedNames = map[string]bool{"thumbs.db": true, "desktop.ini": true}

// localFile ist eine Datei, die hochgeladen werden soll.
type localFile struct {
	path string
	size int64
}

func hiddenName(name string) bool {
	return strings.HasPrefix(name, ".") || skippedNames[strings.ToLower(name)]
}

// collectFiles löst Dateien und Ordner auf. Ein Ordner liefert seine Dateien
// (mit recursive auch die der Unterordner); versteckte Dateien und
// Systemdateien bleiben weg — außer, sie sind ausdrücklich genannt.
func collectFiles(args []string, recursive bool) ([]localFile, error) {
	var out []localFile
	seen := map[string]bool{}
	add := func(path string, size int64) {
		key := filepath.Clean(path)
		if abs, err := filepath.Abs(path); err == nil {
			key = abs
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, localFile{path: path, size: size})
	}
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, usageErr("%s nicht lesbar: %v", arg, err)
		}
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				return nil, usageErr("%s ist keine normale Datei", arg)
			}
			add(arg, info.Size())
			continue
		}
		err = filepath.WalkDir(arg, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == arg {
				return nil
			}
			if entry.IsDir() {
				if !recursive || hiddenName(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if hiddenName(entry.Name()) || !entry.Type().IsRegular() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			add(path, info.Size())
			return nil
		})
		if err != nil {
			return nil, usageErr("Ordner %s nicht lesbar: %v", arg, err)
		}
	}
	if len(out) == 0 {
		return nil, usageErr("Keine Dateien gefunden (versteckte Dateien zählen nicht; Unterordner nur mit --recursive)")
	}
	return out, nil
}

// batchFiles teilt nach Anzahl und Größe. Eine Datei über der Größengrenze
// geht allein.
func batchFiles(files []localFile, maxFiles int, maxBytes int64) [][]localFile {
	var batches [][]localFile
	var current []localFile
	var size int64
	for _, file := range files {
		if len(current) > 0 && (len(current) >= maxFiles || size+file.size > maxBytes) {
			batches = append(batches, current)
			current, size = nil, 0
		}
		current = append(current, file)
		size += file.size
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// contentTypeFor bestimmt den Content-Type aus der Endung.
func contentTypeFor(path string) string {
	if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(path))); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// transliteration bildet, was NFKD + ASCII in werkzeug.secure_filename
// übrig lässt, für die häufigen Zeichen nach.
var transliteration = map[rune]string{
	'ä': "a", 'ö': "o", 'ü': "u", 'Ä': "A", 'Ö': "O", 'Ü': "U",
	'á': "a", 'à': "a", 'â': "a", 'ã': "a", 'å': "a", 'Á': "A", 'À': "A", 'Â': "A",
	'é': "e", 'è': "e", 'ê': "e", 'ë': "e", 'É': "E", 'È': "E", 'Ê': "E",
	'í': "i", 'ì': "i", 'î': "i", 'ï': "i", 'Í': "I",
	'ó': "o", 'ò': "o", 'ô': "o", 'õ': "o", 'Ó': "O", 'Ô': "O",
	'ú': "u", 'ù': "u", 'û': "u", 'Ú': "U",
	'ç': "c", 'Ç': "C", 'ñ': "n", 'Ñ': "N",
}

// secureFilename bildet werkzeug.utils.secure_filename nach: Unter diesem
// Namen meldet das Backend Duplikate, leere und fehlgeschlagene Dateien.
func secureFilename(name string) string {
	var ascii strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '\\':
			ascii.WriteRune(' ')
		case r < 128:
			ascii.WriteRune(r)
		default:
			ascii.WriteString(transliteration[r])
		}
	}
	joined := strings.Join(strings.Fields(ascii.String()), "_")
	var clean strings.Builder
	for _, r := range joined {
		if r == '_' || r == '.' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			clean.WriteRune(r)
		}
	}
	return strings.Trim(clean.String(), "._")
}

func stem(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
}

// fileMatcher ordnet Namen aus der Antwort den lokalen Dateien eines Stapels
// zu: erst exakt, dann über secure_filename, dann über den Namen ohne Endung
// (das Backend wandelt z. B. .docx in .pdf). Jede Datei wird höchstens
// einmal vergeben.
type fileMatcher struct {
	files []localFile
	used  []bool
}

func newFileMatcher(files []localFile) *fileMatcher {
	return &fileMatcher{files: files, used: make([]bool, len(files))}
}

func (m *fileMatcher) match(name string) string {
	if name == "" {
		return ""
	}
	tests := []func(base string) bool{
		func(base string) bool { return base == name },
		func(base string) bool { return secureFilename(base) == name },
		func(base string) bool { return stem(secureFilename(base)) == stem(name) },
	}
	for _, test := range tests {
		for i, file := range m.files {
			if !m.used[i] && test(filepath.Base(file.path)) {
				m.used[i] = true
				return file.path
			}
		}
	}
	return ""
}

// uploadReport ist die Ausgabe von documents upload.
type uploadReport struct {
	Uploaded   []uploadedDoc    `json:"uploaded"`
	Duplicates []map[string]any `json:"duplicates"`
	Failed     []failedFile     `json:"failed"`
	Empty      []string         `json:"empty"`
}

type uploadedDoc struct {
	ID        any    `json:"id"`
	Dateiname any    `json:"dateiname"`
	File      string `json:"file,omitempty"`
}

type failedFile struct {
	File    string `json:"file"`
	Message string `json:"message"`
}

// backendReport ist die Antwort mit report=true.
type backendReport struct {
	Documents  []map[string]any `json:"documents"`
	Duplicates []map[string]any `json:"duplicates"`
	Failed     []map[string]any `json:"failed"`
	Empty      []string         `json:"empty"`
}

// parseUploadResponse versteht beide Formen: den Bericht (report=true) und
// das Array einer älteren Instanz mit den Listen in X-Upload-*-Headern.
func parseUploadResponse(res *api.Response) (backendReport, error) {
	var report backendReport
	trimmed := strings.TrimSpace(string(res.Body))
	if strings.HasPrefix(trimmed, "[") {
		if err := decodeJSONExact(res.Body, &report.Documents); err != nil {
			return report, err
		}
		header := func(name string, target any) {
			if raw := res.Header.Get(name); raw != "" {
				_ = decodeJSONExact([]byte(raw), target)
			}
		}
		header("X-Upload-Duplicate-Files", &report.Duplicates)
		header("X-Upload-Failed-Files", &report.Failed)
		header("X-Upload-Empty-Files", &report.Empty)
		return report, nil
	}
	if trimmed == "" {
		return report, nil
	}
	return report, decodeJSONExact(res.Body, &report)
}

// continuesAfter: Nach 5xx, Netzwerkfehler, 413 oder einer unlesbaren Datei
// kann der nächste Stapel gelingen. Jeder andere Fehler (400 wegen unit_id,
// 401, 403, 404, 429 …) träfe jeden weiteren Stapel genauso.
func continuesAfter(err error) bool {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.Code == "LOCAL_FILE" {
		return true
	}
	if apiErr.Exit != 0 {
		return false
	}
	return apiErr.Status == 0 || apiErr.Status >= 500 || apiErr.Status == 413
}

// abortedUpload ergänzt den Abbruch im ersten von mehreren Stapeln um die
// Lage: Der Rest der Dateien wurde nie gesendet. Die Meldung des Backends
// bleibt unverändert.
func abortedUpload(err error, batches int, files, first []localFile) error {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	out := *apiErr
	details := map[string]any{}
	for key, value := range apiErr.Details {
		details[key] = value
	}
	details["batch"] = 1
	details["batches"] = batches
	details["files_total"] = len(files)
	details["files_not_sent"] = len(files) - len(first)
	out.Details = details
	return &out
}

func (r *runner) runDocumentsUpload(args []string, flags *flagValues, resolved config.Resolved) int {
	if resolved.Org == "" {
		return r.fail(configErr("Für den Upload wird eine Organisation gebraucht. Setze IMMOJUMP_ORGANISATION_ID oder --org."))
	}
	immobilie := strings.TrimSpace(flags.get("immobilie-id"))
	unit := strings.TrimSpace(flags.get("unit-id"))
	if unit != "" && immobilie == "" {
		return r.fail(usageErr("--unit-id braucht --immobilie-id: Die Einheit muss zu dieser Immobilie gehören"))
	}
	maxFiles := defaultBatchFiles
	if flags.has("batch-files") {
		raw := strings.TrimSpace(flags.get("batch-files"))
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			return r.fail(usageErr("--batch-files erwartet eine ganze Zahl ab 1, bekommen: %q", raw))
		}
		maxFiles = n
	}
	maxMB := defaultBatchMB
	if flags.has("batch-mb") {
		raw := strings.TrimSpace(flags.get("batch-mb"))
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || n <= 0 {
			return r.fail(usageErr("--batch-mb erwartet eine positive Zahl, bekommen: %q", raw))
		}
		maxMB = n
	}
	files, err := collectFiles(args, flags.bool("recursive"))
	if err != nil {
		return r.fail(err)
	}

	fields := map[string]string{"organisation_id": resolved.Org, "report": "true"}
	if immobilie != "" {
		fields["immobilien_id"] = immobilie
	}
	if unit != "" {
		fields["unit_id"] = unit
	}
	if category := strings.TrimSpace(flags.get("category")); category != "" {
		fields["category"] = category
	}
	if flags.bool("allow-duplicate") {
		fields["allow_duplicate_upload"] = "true"
	}

	client, err := r.newClientWithTimeout(resolved, flags, uploadTimeout)
	if err != nil {
		return r.fail(err)
	}
	query, err := buildQuery(Spec{}, flags)
	if err != nil {
		return r.fail(err)
	}

	report := uploadReport{Uploaded: []uploadedDoc{}, Duplicates: []map[string]any{}, Failed: []failedFile{}, Empty: []string{}}
	batches := batchFiles(files, maxFiles, int64(maxMB*1024*1024))
	failBatch := func(batch []localFile, message string) {
		for _, file := range batch {
			report.Failed = append(report.Failed, failedFile{File: file.path, Message: message})
		}
	}
	for i, batch := range batches {
		parts := make([]api.FilePart, 0, len(batch))
		for _, file := range batch {
			parts = append(parts, api.FilePart{
				Field: "files[]", Filename: filepath.Base(file.path), Path: file.path, ContentType: contentTypeFor(file.path),
			})
		}
		// Uploads sind nicht idempotent (allow_duplicate, Archive) — der
		// Client wiederholt sie deshalb nie.
		res, err := client.Do(api.Request{
			Method: "POST", Path: "/api/documents/documents/bulk-upload", Query: query,
			IdempotencyKey: flags.get("idempotency-key"),
			Multipart:      &api.Multipart{Files: parts, Fields: fields},
		})
		if err != nil {
			if len(batches) == 1 {
				// Ein Stapel verhält sich wie ein einzelner Request: Fehler
				// des Backends mit seinem Exit-Code, stdout leer.
				return r.fail(err)
			}
			if !continuesAfter(err) {
				if i == 0 {
					return r.fail(abortedUpload(err, len(batches), files, batches[0]))
				}
				for _, rest := range batches[i:] {
					failBatch(rest, err.Error())
				}
				break
			}
			failBatch(batch, err.Error())
			continue
		}
		parsed, err := parseUploadResponse(res)
		if err != nil {
			failBatch(batch, "Antwort des Backends nicht lesbar: "+err.Error())
			continue
		}
		matcher := newFileMatcher(batch)
		for _, doc := range parsed.Documents {
			name := stringID(doc["dateiname"])
			report.Uploaded = append(report.Uploaded, uploadedDoc{ID: doc["id"], Dateiname: doc["dateiname"], File: matcher.match(name)})
		}
		for _, dup := range parsed.Duplicates {
			entry := map[string]any{}
			for key, value := range dup {
				entry[key] = value
			}
			if path := matcher.match(stringID(dup["fileName"])); path != "" {
				entry["file"] = path
			}
			report.Duplicates = append(report.Duplicates, entry)
		}
		for _, failed := range parsed.Failed {
			name := stringID(failed["fileName"])
			path := matcher.match(name)
			if path == "" {
				path = name
			}
			report.Failed = append(report.Failed, failedFile{File: path, Message: stringID(failed["message"])})
		}
		for _, name := range parsed.Empty {
			path := matcher.match(name)
			if path == "" {
				path = name
			}
			report.Empty = append(report.Empty, path)
		}
	}

	raw, err := output.Marshal(report)
	if err != nil {
		return r.fail(err)
	}
	if err := r.render(raw, "application/json", flags); err != nil {
		return r.fail(err)
	}
	if len(report.Failed) > 0 {
		return r.fail(&api.Error{
			Message: fmt.Sprintf("%d von %d Dateien nicht hochgeladen — Details unter failed auf stdout.", len(report.Failed), len(files)),
			Code:    "UPLOAD_INCOMPLETE",
			Exit:    1,
		})
	}
	return 0
}
