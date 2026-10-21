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

// localFile ist eine Datei, die hochgeladen werden soll. root/rel öffnen sie
// beim Senden innerhalb des Datenraums (os.Root), path ist der Name für
// Bericht und Meldungen.
type localFile struct {
	path string
	size int64
	root *os.Root
	rel  string
}

// uploadRoots hält je Ordner genau ein os.Root offen — für ein
// Ordner-Argument der Ordner selbst, für eine einzelne Datei ihr Ordner.
type uploadRoots struct {
	byDir map[string]*os.Root
}

func (u *uploadRoots) open(dir string) (*os.Root, error) {
	if u.byDir == nil {
		u.byDir = map[string]*os.Root{}
	}
	key := filepath.Clean(dir)
	if root, ok := u.byDir[key]; ok {
		return root, nil
	}
	root, err := os.OpenRoot(key)
	if err != nil {
		return nil, usageErr("Ordner %s nicht lesbar: %v", dir, err)
	}
	u.byDir[key] = root
	return root, nil
}

func (u *uploadRoots) close() {
	for _, root := range u.byDir {
		_ = root.Close()
	}
}

func hiddenName(name string) bool {
	return strings.HasPrefix(name, ".") || skippedNames[strings.ToLower(name)]
}

// collectFiles löst Dateien und Ordner auf. Ein Ordner liefert seine Dateien
// (mit recursive auch die der Unterordner); versteckte Dateien und
// Systemdateien bleiben weg — außer, sie sind ausdrücklich genannt.
func collectFiles(args []string, recursive bool, roots *uploadRoots) ([]localFile, error) {
	var out []localFile
	seen := map[string]bool{}
	add := func(path string, size int64, root *os.Root, rel string) {
		key := filepath.Clean(path)
		if abs, err := filepath.Abs(path); err == nil {
			key = abs
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, localFile{path: path, size: size, root: root, rel: rel})
	}
	for _, arg := range args {
		info, err := os.Lstat(arg)
		if err != nil {
			return nil, usageErr("%s nicht lesbar: %v", arg, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Symlinks werden nie hochgeladen: Ihr Ziel kann außerhalb des
			// Datenraums liegen und sich bis zum Senden ändern.
			return nil, usageErr("%s ist ein symbolischer Link — bitte das Ziel selbst angeben", arg)
		}
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				return nil, usageErr("%s ist keine normale Datei", arg)
			}
			root, err := roots.open(filepath.Dir(arg))
			if err != nil {
				return nil, err
			}
			add(arg, info.Size(), root, filepath.Base(arg))
			continue
		}
		root, err := roots.open(arg)
		if err != nil {
			return nil, err
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
			rel, err := filepath.Rel(arg, path)
			if err != nil {
				return err
			}
			add(path, info.Size(), root, rel)
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

// sourcePath findet die Datei des Stapels, deren gesendeter Name (Basename)
// genau source ist. Im Stapel ist er eindeutig (batchFiles).
func sourcePath(batch []localFile, source string) string {
	for _, file := range batch {
		if filepath.Base(file.path) == source {
			return file.path
		}
	}
	return ""
}

// uploadNameKey ist der Name, unter dem das Backend eine Datei meldet —
// konservativ normalisiert: secure_filename, ohne Endung (das Backend wandelt
// z. B. .docx in .pdf), ohne Groß-/Kleinschreibung.
func uploadNameKey(path string) string {
	return stem(secureFilename(filepath.Base(path)))
}

// batchFiles teilt nach Anzahl und Größe; eine Datei über der Größengrenze
// geht allein. Im selben Stapel ist jeder gesendete Name (Basename, auch
// ohne Groß-/Kleinschreibung) eindeutig — darüber ordnet sourceFile zu.
// Zusätzlich bleibt der uploadNameKey eindeutig: Ohne sourceFile (ältere
// Instanz) ist der gespeicherte Name die einzige Spur. Die zweite Datei rückt
// in einen späteren Stapel, die Reihenfolge bleibt sonst erhalten.
func batchFiles(files []localFile, maxFiles int, maxBytes int64) [][]localFile {
	var batches [][]localFile
	pending := files
	for len(pending) > 0 {
		var batch, later []localFile
		var size int64
		names := map[string]bool{}
		for i, file := range pending {
			if len(batch) >= maxFiles {
				later = append(later, pending[i:]...)
				break
			}
			if len(batch) > 0 && size+file.size > maxBytes {
				later = append(later, pending[i:]...)
				break
			}
			sent := "name:" + strings.ToLower(filepath.Base(file.path))
			key := "secure:" + uploadNameKey(file.path)
			if names[sent] || names[key] {
				later = append(later, file)
				continue
			}
			names[sent], names[key] = true, true
			batch = append(batch, file)
			size += file.size
		}
		batches = append(batches, batch)
		pending = later
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
	if apiErr.Code == "LOCAL_FILE" || apiErr.Code == "LOCAL_FILE_CHANGED" {
		return true
	}
	if apiErr.Exit != 0 {
		return false
	}
	return apiErr.Status == 0 || apiErr.Status >= 500 || apiErr.Status == 413
}

// dropLocalFile erkennt den Fehler „diese eine Datei ist vor dem Senden
// gescheitert" (LOCAL_FILE mit Pfad) und liefert sie samt Rest des Stapels.
func dropLocalFile(err error, batch []localFile) (*localFile, []localFile) {
	var apiErr *api.Error
	if err == nil || !errors.As(err, &apiErr) || apiErr.Code != "LOCAL_FILE" {
		return nil, nil
	}
	path, _ := apiErr.Details["file"].(string)
	for j, file := range batch {
		if file.path == path {
			rest := append(append([]localFile{}, batch[:j]...), batch[j+1:]...)
			return &batch[j], rest
		}
	}
	return nil, nil
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
	roots := &uploadRoots{}
	defer roots.close()
	files, err := collectFiles(args, flags.bool("recursive"), roots)
	if err != nil {
		return r.fail(err)
	}

	// Genau eine ausdrücklich genannte Datei ohne --report ist der bisherige
	// Aufruf: ein Request, Ausgabe das Array des Backends unverändert —
	// bestehende Agenten lesen dort --fields id bzw. .[0].id.
	legacy := len(args) == 1 && len(files) == 1 && files[0].path == args[0] && !flags.bool("report")

	fields := map[string]string{"organisation_id": resolved.Org}
	if !legacy {
		fields["report"] = "true"
	}
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

	if legacy {
		file := files[0]
		res, err := client.Do(api.Request{
			Method: "POST", Path: "/api/documents/documents/bulk-upload", Query: query,
			IdempotencyKey: flags.get("idempotency-key"),
			Multipart: &api.Multipart{Fields: fields, Files: []api.FilePart{{
				Field: "files[]", Filename: filepath.Base(file.path), Path: file.path, ContentType: contentTypeFor(file.path),
				Root: file.root, Name: file.rel,
			}}},
		})
		if err != nil {
			return r.fail(err)
		}
		if err := r.render(res.Body, res.ContentType, flags); err != nil {
			return r.fail(err)
		}
		return 0
	}

	heuristicWarned := false
	report := uploadReport{Uploaded: []uploadedDoc{}, Duplicates: []map[string]any{}, Failed: []failedFile{}, Empty: []string{}}
	batches := batchFiles(files, maxFiles, int64(maxMB*1024*1024))
	failBatch := func(batch []localFile, message string) {
		for _, file := range batch {
			report.Failed = append(report.Failed, failedFile{File: file.path, Message: message})
		}
	}
	for i := 0; i < len(batches); i++ {
		batch := batches[i]
		if len(batch) == 0 {
			continue
		}
		parts := make([]api.FilePart, 0, len(batch))
		for _, file := range batch {
			parts = append(parts, api.FilePart{
				Field: "files[]", Filename: filepath.Base(file.path), Path: file.path, ContentType: contentTypeFor(file.path),
				Root: file.root, Name: file.rel,
			})
		}
		// Uploads sind nicht idempotent (allow_duplicate, Archive) — der
		// Client wiederholt sie deshalb nie.
		res, err := client.Do(api.Request{
			Method: "POST", Path: "/api/documents/documents/bulk-upload", Query: query,
			IdempotencyKey: flags.get("idempotency-key"),
			Multipart:      &api.Multipart{Files: parts, Fields: fields},
		})
		if dropped, rest := dropLocalFile(err, batch); dropped != nil {
			// Die Datei scheiterte vor dem Senden (kein Request ging raus):
			// aussortieren, den Rest des Stapels erneut versuchen.
			report.Failed = append(report.Failed, failedFile{File: dropped.path, Message: err.Error()})
			batches[i] = rest
			i--
			continue
		}
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
		// Zuordnung über sourceFile: der Name, wie er gesendet wurde. Nur ohne
		// sourceFile (ältere Instanz) bleibt die Namens-Heuristik — mit einem
		// Hinweis, denn Normalisierung (Şahin → Sahin) und Archivmitglieder
		// machen sie unsicher.
		locate := func(entry map[string]any, name string) string {
			if source, ok := entry["sourceFile"].(string); ok && source != "" {
				if path := sourcePath(batch, source); path != "" {
					return path
				}
			}
			if !heuristicWarned {
				heuristicWarned = true
				_ = output.WriteWarning(r.stderr,
					"Upload-Bericht ohne sourceFile (ältere Instanz): Zuordnung zu lokalen Dateien über den gespeicherten Namen — "+
						"bei Umlauten/Sonderzeichen und Archiven unsicher.", nil)
			}
			return matcher.match(name)
		}
		for _, doc := range parsed.Documents {
			path := locate(doc, stringID(doc["dateiname"]))
			report.Uploaded = append(report.Uploaded, uploadedDoc{ID: doc["id"], Dateiname: doc["dateiname"], File: path})
		}
		for _, dup := range parsed.Duplicates {
			entry := map[string]any{}
			for key, value := range dup {
				entry[key] = value
			}
			if path := locate(dup, stringID(dup["fileName"])); path != "" {
				entry["file"] = path
			}
			report.Duplicates = append(report.Duplicates, entry)
		}
		for _, failed := range parsed.Failed {
			name := stringID(failed["fileName"])
			path := locate(failed, name)
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
