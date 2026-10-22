// Package api kapselt den HTTP-Zugriff auf die immoJUMP-API: Bearer-Token,
// X-Organisation-Id, Multipart-Upload und die Abbildung von Backend-Fehlern
// auf stabile Exit-Codes.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/immoJUMP/immojump-cli/internal/config"
)

// DefaultTimeout gilt, wenn kein --timeout gesetzt ist.
const DefaultTimeout = 60 * time.Second

// UserAgent identifiziert das CLI gegenüber dem Backend.
var UserAgent = "immojump-cli"

// FilePart ist eine Datei in einem Multipart-Upload. Mit Path wird die Datei
// beim Senden von der Platte gestreamt (nie ganz im Speicher); Content ist
// die Alternative für kleine Inhalte aus dem Speicher.
type FilePart struct {
	Field       string
	Filename    string
	Content     []byte
	Path        string
	ContentType string
	// Root und Name: Die Datei wird über os.Root geöffnet (Name relativ zu
	// Root) — kein Symlink und kein „..“ führt aus dem Datenraum heraus,
	// auch nicht über einen nachträglich getauschten Elternordner. Path
	// bleibt der Name für Meldungen.
	Root *os.Root
	Name string
}

// Multipart beschreibt einen Upload aus Dateien plus Formfeldern.
type Multipart struct {
	Files  []FilePart
	Fields map[string]string
}

// Request ist ein einzelner API-Aufruf.
type Request struct {
	Method         string
	Path           string
	Query          url.Values
	Body           []byte
	ContentType    string
	IdempotencyKey string
	Multipart      *Multipart
	// Idempotent erlaubt Wiederholungen auch für schreibende Methoden. Nur
	// setzen, wenn ein doppelt angekommener Aufruf nichts doppelt anlegt
	// (units import gleicht über extern_id/id ab). GET und HEAD gelten immer
	// als idempotent.
	Idempotent bool
	// RetryRateLimit erlaubt bei einem Schreibaufruf genau eine Sorte
	// Wiederholung: nach 429. Das Rate Limit antwortet, bevor die Route läuft
	// — dort ist sicher nichts passiert. Nach 5xx oder Netzwerkfehler kann
	// der Aufruf angekommen sein; dann keine Wiederholung.
	RetryRateLimit bool
}

// Response ist die rohe Antwort — die Interpretation macht internal/output.
type Response struct {
	Status      int
	ContentType string
	Header      http.Header
	Body        []byte
	// AmbiguousRetry: Ein früherer Versuch dieses Aufrufs scheiterte so, dass
	// er angekommen sein kann (5xx, Netzwerk) — die Antwort beschreibt dann
	// womöglich die Wiederholung, nicht den ersten Versuch.
	AmbiguousRetry bool
}

// Client spricht mit genau einer immoJUMP-Instanz.
type Client struct {
	BaseURL string
	Org     string
	Token   string
	HTTP    *http.Client
	// Timeout begrenzt jeden einzelnen Aufruf. Er hängt bewusst am Client und
	// nicht am http.Client: --timeout ist eine Zusage des CLI, die auch für
	// injizierte HTTP-Clients gelten muss.
	Timeout time.Duration
	// Env versorgt die Allowlist-Prüfung (injizierbar für Tests).
	Env func(string) string
	// Trace bekommt Methode und vollständige URL, bevor der Request rausgeht
	// (--verbose). Ohne diese Zeile lässt sich ein 404 nicht einordnen:
	// falscher Pfad oder Route fehlt auf dieser Instanz? Der Token steht im
	// Header und taucht hier nie auf.
	Trace func(method, url string)
	// Retries ist die Zahl der Wiederholungen nach dem ersten Versuch — nur
	// für idempotente Aufrufe (siehe Request.Idempotent) und nur bei 429,
	// 502, 503, 504 oder Netzwerkfehlern. 0 heißt: genau ein Versuch.
	Retries int
	// Sleep wartet zwischen zwei Versuchen (injizierbar für Tests).
	Sleep func(time.Duration)
	// OnRetry meldet jede Wiederholung, bevor gewartet wird: attempt ist der
	// kommende Versuch (ab 2), attempts die Höchstzahl.
	OnRetry func(attempt, attempts int, wait time.Duration, err *Error)
}

// MaxRetryWait deckelt die Pause zwischen zwei Versuchen — auch ein
// Retry-After von einer Stunde blockiert einen Agenten nicht länger.
const MaxRetryWait = 30 * time.Second

// retryBase ist die erste Pause; jede weitere verdoppelt sie.
const retryBase = time.Second

// Error ist ein Backend- oder Transportfehler mit stabilem Exit-Code.
type Error struct {
	Status  int
	Message string
	Code    string
	// Details ist das komplette Fehler-Payload des Backends. api_error()
	// erlaubt beliebige Zusatzfelder (`errors`, `valid_values`, der
	// Kontingentstand bei 402) — genau darin steht, wie der Aufruf zu
	// korrigieren ist. Zahlen bleiben json.Number, damit nichts verfälscht
	// wird.
	Details map[string]any
	// Exit überschreibt die Ableitung aus Status (z. B. lokale Fehler).
	Exit int
	Err  error
	// RetryAfter ist der Retry-After-Header der Antwort (0 ohne Header).
	RetryAfter time.Duration
}

// Retryable sagt, ob ein erneuter Versuch sinnvoll ist: Rate Limit,
// Gateway-/Verfügbarkeitsfehler und Netzwerkabbrüche. 500 gehört bewusst
// nicht dazu — ein Fehler im Backend wird durch Wiederholen nicht besser.
func (e *Error) Retryable() bool {
	if e.Exit != 0 {
		return false
	}
	switch e.Status {
	case 0:
		return e.Code == "NETWORK" || e.Code == "TIMEOUT"
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Err }

// ExitCode bildet den HTTP-Status auf den in DESIGN.md dokumentierten
// Exit-Code ab. Agenten branchen darauf statt Meldungen zu parsen.
func (e *Error) ExitCode() int {
	if e.Exit != 0 {
		return e.Exit
	}
	switch {
	case e.Status == 400, e.Status == 422:
		return 11
	case e.Status == 401:
		return 4
	case e.Status == 403:
		return 6
	case e.Status == 404:
		return 5
	case e.Status == 409:
		return 9
	case e.Status == 429:
		return 7
	case e.Status >= 500:
		return 8
	case e.Status == 0:
		return 8 // Netzwerkfehler: temporär, Retry möglich
	default:
		return 1
	}
}

// ExitCodeFor liefert den Exit-Code zu einem beliebigen Fehler.
func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.ExitCode()
	}
	return 1
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: c.timeout()}
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return DefaultTimeout
}

// Do führt den Aufruf aus. Antworten ab Status 400 werden zu *Error.
//
// Idempotente Aufrufe (GET/HEAD oder Request.Idempotent) wiederholt Do bis zu
// c.Retries-mal bei 429/502/503/504 und Netzwerkfehlern, Aufrufe mit
// RetryRateLimit nur bei 429 — mit exponentiellem
// Backoff ab 1 s, Retry-After hat Vorrang, gedeckelt auf MaxRetryWait.
// Schreibende Aufrufe ohne Idempotent laufen genau einmal.
func (c *Client) Do(req Request) (*Response, error) {
	if err := config.CheckBaseURL(c.BaseURL, c.Env); err != nil {
		return nil, &Error{Message: err.Error(), Code: "BASE_URL_NOT_ALLOWED", Exit: 3}
	}

	target, err := c.buildURL(req)
	if err != nil {
		return nil, &Error{Message: err.Error(), Code: "BAD_REQUEST_URL", Exit: 2}
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}

	idempotent := req.Idempotent || method == http.MethodGet || method == http.MethodHead
	attempts := 1
	if c.Retries > 0 && (idempotent || req.RetryRateLimit) {
		attempts += c.Retries
	}
	ambiguous := false
	for attempt := 1; ; attempt++ {
		res, err := c.doOnce(req, method, target)
		if res != nil {
			res.AmbiguousRetry = ambiguous
		}
		var apiErr *Error
		if err == nil || attempt >= attempts || !errors.As(err, &apiErr) || !apiErr.Retryable() {
			return res, err
		}
		if !idempotent && apiErr.Status != http.StatusTooManyRequests {
			return res, err
		}
		if apiErr.Code == "TIMEOUT" && method != http.MethodGet && method != http.MethodHead {
			// Ein Timeout sagt nicht, ob der Schreibaufruf angekommen ist —
			// und eine Wiederholung hebelte --timeout aus.
			return res, err
		}
		if apiErr.Status != http.StatusTooManyRequests {
			// Nur 429 beweist, dass nichts passiert ist.
			ambiguous = true
		}
		wait := retryWait(attempt, apiErr.RetryAfter)
		if c.OnRetry != nil {
			c.OnRetry(attempt+1, attempts, wait, apiErr)
		}
		c.sleep(wait)
	}
}

func (c *Client) sleep(d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(d)
		return
	}
	time.Sleep(d)
}

// retryWait ist die Pause vor dem nächsten Versuch: Retry-After, sonst
// 1 s, 2 s, 4 s … — beides gedeckelt.
func retryWait(attempt int, retryAfter time.Duration) time.Duration {
	wait := retryAfter
	if wait <= 0 {
		wait = retryBase << (attempt - 1)
	}
	if wait > MaxRetryWait || wait < 0 {
		wait = MaxRetryWait
	}
	return wait
}

// parseRetryAfter liest Sekunden oder ein HTTP-Datum.
func parseRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(raw); err == nil {
		if d := when.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// doOnce ist genau ein HTTP-Versuch. Der Body wird je Versuch neu gebaut —
// ein gelesener Reader ließe sich nicht ein zweites Mal senden.
func (c *Client) doOnce(req Request, method, target string) (*Response, error) {
	// Der Timeout beginnt vor dem Bau des Bodys: Auch das Öffnen und Prüfen
	// der Upload-Dateien zählt zu --timeout.
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	body, contentType, length, err := buildBody(req)
	if err != nil {
		var localErr *Error
		if errors.As(err, &localErr) {
			return nil, localErr
		}
		return nil, &Error{Message: err.Error(), Code: "BAD_REQUEST_BODY", Exit: 2}
	}
	stream, _ := body.(*streamBody)
	if stream != nil {
		defer func() { _ = stream.finish() }()
	}

	if c.Trace != nil {
		c.Trace(method, target)
	}
	// Der Timeout hängt am Context, damit er auch dann greift, wenn der
	// HTTP-Client von außen kommt und keinen eigenen mitbringt. Der Body ist
	// vollständig gelesen, bevor doOnce zurückkehrt — cancel steht oben.
	httpReq, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, &Error{Message: fmt.Sprintf("Request nicht baubar: %v", err), Exit: 2}
	}
	if length >= 0 && body != nil {
		httpReq.ContentLength = length
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", UserAgent)
	if c.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if c.Org != "" {
		httpReq.Header.Set("X-Organisation-Id", c.Org)
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	if req.IdempotencyKey != "" {
		httpReq.Header.Set("Idempotency-Key", req.IdempotencyKey)
	}

	httpRes, err := c.httpClient().Do(httpReq)
	if stream != nil {
		// Ein Lesefehler an einer Datei zählt, auch wenn der Server schon
		// geantwortet hat: Dann kam ein unvollständiger Body an.
		if streamErr := stream.finish(); streamErr != nil {
			if httpRes != nil {
				_ = httpRes.Body.Close()
			}
			return nil, streamErr
		}
	}
	if err != nil {
		var localErr *Error
		if errors.As(err, &localErr) {
			// Ein Fehler an einer lokalen Datei ist kein Netzwerkproblem —
			// ein Retry änderte daran nichts.
			return nil, localErr
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &Error{
				Message: fmt.Sprintf("Keine Antwort von %s innerhalb von %s (--timeout, je Versuch)", c.BaseURL, c.timeout()),
				Code:    "TIMEOUT",
				Err:     err,
			}
		}
		return nil, &Error{
			Status:  0,
			Message: fmt.Sprintf("Verbindung zu %s fehlgeschlagen: %v", c.BaseURL, err),
			Code:    "NETWORK",
			Err:     err,
		}
	}
	defer func() { _ = httpRes.Body.Close() }()

	raw, err := io.ReadAll(httpRes.Body)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			// Header kamen, der Body nicht: genauso ein Timeout — ein
			// Schreibaufruf wird danach nicht wiederholt.
			return nil, &Error{
				Message: fmt.Sprintf("Antwort von %s nicht vollständig innerhalb von %s (--timeout, je Versuch)", c.BaseURL, c.timeout()),
				Code:    "TIMEOUT",
				Err:     err,
			}
		}
		return nil, &Error{Message: fmt.Sprintf("Antwort nicht lesbar: %v", err), Code: "NETWORK", Err: err}
	}

	res := &Response{
		Status:      httpRes.StatusCode,
		ContentType: httpRes.Header.Get("Content-Type"),
		Header:      httpRes.Header,
		Body:        raw,
	}
	if httpRes.StatusCode >= 400 {
		apiErr := backendError(res)
		apiErr.RetryAfter = parseRetryAfter(httpRes.Header.Get("Retry-After"), time.Now())
		return res, apiErr
	}
	return res, nil
}

func (c *Client) buildURL(req Request) (string, error) {
	base := config.NormalizeBaseURL(c.BaseURL)
	path := req.Path
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	parsed, err := url.Parse(base + path)
	if err != nil {
		return "", fmt.Errorf("Pfad %q ergibt keine gültige URL: %v", req.Path, err)
	}
	if len(req.Query) > 0 {
		existing := parsed.Query()
		for key, values := range req.Query {
			for _, v := range values {
				existing.Add(key, v)
			}
		}
		parsed.RawQuery = existing.Encode()
	}
	return parsed.String(), nil
}

func buildBody(req Request) (io.Reader, string, int64, error) {
	if req.Multipart != nil {
		return multipartBody(req.Multipart)
	}
	if len(req.Body) == 0 {
		return nil, "", -1, nil
	}
	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	return bytes.NewReader(req.Body), contentType, int64(len(req.Body)), nil
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// partHeader ist der Kopf eines Datei-Teils — mit Content-Type je Datei
// (CreateFormFile setzte immer application/octet-stream).
func partHeader(file FilePart) textproto.MIMEHeader {
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
		quoteEscaper.Replace(file.Field), quoteEscaper.Replace(file.Filename)))
	contentType := file.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)
	return header
}

// Testhaken für das Zeitfenster zwischen Prüfung und Öffnen bzw. nach dem
// Fstat — nur in Tests gesetzt.
var (
	hookBeforeOpen func(path string)
	hookAfterStat  func(path string)
)

// localFileErr ist ein Fehler an einer lokalen Datei, bevor der Request
// rausgeht: Der Aufrufer kann die Datei aussortieren und den Rest senden.
func localFileErr(path, format string, a ...any) *Error {
	return &Error{
		Message: fmt.Sprintf(format, a...),
		Code:    "LOCAL_FILE",
		Exit:    1,
		Details: map[string]any{"file": path},
	}
}

// openUploadFile öffnet eine Datei für den Upload, ohne einem Symlink zu
// folgen, der nach der Aufzählung untergeschoben wurde: Lstat muss eine
// normale Datei zeigen, und das Fstat des geöffneten Deskriptors muss
// dieselbe Datei sein. Die Länge kommt aus genau diesem Fstat.
func openUploadFile(file FilePart) (*os.File, int64, error) {
	path := file.Path
	lstat := os.Lstat
	// O_NONBLOCK: Wird die Datei vor dem Öffnen durch eine Named Pipe
	// ersetzt, blockierte open(2) sonst ohne Schreiber für immer.
	open := func(name string) (*os.File, error) { return os.OpenFile(name, os.O_RDONLY|nonblockFlag, 0) }
	name := path
	if file.Root != nil {
		lstat, name = file.Root.Lstat, file.Name
		open = func(name string) (*os.File, error) { return file.Root.OpenFile(name, os.O_RDONLY|nonblockFlag, 0) }
	}
	before, err := lstat(name)
	if err != nil {
		return nil, 0, localFileErr(path, "Datei %s nicht lesbar oder außerhalb des Datenraums (Symlink?): %v — nicht hochgeladen", path, err)
	}
	if !before.Mode().IsRegular() {
		return nil, 0, localFileErr(path, "%s ist keine normale Datei (Symlink oder Sonderdatei) — nicht hochgeladen", path)
	}
	if hookBeforeOpen != nil {
		hookBeforeOpen(path)
	}
	f, err := open(name)
	if err != nil {
		return nil, 0, localFileErr(path, "Datei %s nicht lesbar oder außerhalb des Datenraums (Symlink?): %v — nicht hochgeladen", path, err)
	}
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		_ = f.Close()
		return nil, 0, localFileErr(path, "%s wurde zwischen Prüfung und Öffnen ausgetauscht — nicht hochgeladen", path)
	}
	if err := clearNonblock(f); err != nil {
		_ = f.Close()
		return nil, 0, localFileErr(path, "Datei %s nicht lesbar: %v", path, err)
	}
	if hookAfterStat != nil {
		hookAfterStat(path)
	}
	return f, opened.Size(), nil
}

// streamBody ist der gestreamte Multipart-Body. err hält einen Lesefehler an
// einer lokalen Datei fest — auch dann, wenn der Server schon geantwortet
// hat, bevor er den Body ganz gelesen hatte.
type streamBody struct {
	*io.PipeReader
	done chan struct{}
	err  error
}

// finish schließt den Body, wartet auf den Schreiber und meldet dessen
// Datei-Fehler.
func (b *streamBody) finish() error {
	_ = b.PipeReader.Close()
	<-b.done
	return b.err
}

// writeMultipart schreibt Formfelder (sortiert) und Dateien; copyFile füllt
// den Inhalt einer Datei. Ein copyFile, der nichts schreibt, ergibt genau den
// Rahmen — so entsteht die Content-Length vorab.
func writeMultipart(w io.Writer, boundary string, m *Multipart, copyFile func(io.Writer, int, FilePart) error) error {
	writer := multipart.NewWriter(w)
	if err := writer.SetBoundary(boundary); err != nil {
		return err
	}
	keys := make([]string, 0, len(m.Fields))
	for key := range m.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := writer.WriteField(key, m.Fields[key]); err != nil {
			return fmt.Errorf("Formfeld %s nicht schreibbar: %v", key, err)
		}
	}
	for i, file := range m.Files {
		part, err := writer.CreatePart(partHeader(file))
		if err != nil {
			return fmt.Errorf("Multipart-Feld %s nicht baubar: %v", file.Field, err)
		}
		if err := copyFile(part, i, file); err != nil {
			return err
		}
	}
	return writer.Close()
}

// countingWriter zählt nur Bytes.
type countingWriter struct{ n int64 }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// multipartBody streamt den Upload über eine Pipe: Jede Datei wird erst beim
// Senden von der Platte gelesen, nie komplett in den Speicher. Alle Dateien
// sind vorher geöffnet und geprüft (openUploadFile) — schlägt das fehl, geht
// kein Request raus. Die Länge ist vorab bekannt (Rahmen + Fstat-Größen) —
// kein chunked Transfer, den manche Proxys ablehnen.
func multipartBody(m *Multipart) (io.Reader, string, int64, error) {
	boundary := multipart.NewWriter(io.Discard).Boundary()
	opened := make([]*os.File, len(m.Files))
	sizes := make([]int64, len(m.Files))
	closeAll := func() {
		for _, f := range opened {
			if f != nil {
				_ = f.Close()
			}
		}
	}
	var total int64
	for i, file := range m.Files {
		if file.Path == "" && file.Root == nil {
			sizes[i] = int64(len(file.Content))
		} else {
			f, size, err := openUploadFile(file)
			if err != nil {
				closeAll()
				return nil, "", 0, err
			}
			opened[i], sizes[i] = f, size
		}
		total += sizes[i]
	}
	frame := &countingWriter{}
	if err := writeMultipart(frame, boundary, m, func(io.Writer, int, FilePart) error { return nil }); err != nil {
		closeAll()
		return nil, "", 0, err
	}
	contentType := "multipart/form-data; boundary=" + boundary

	reader, writer := io.Pipe()
	body := &streamBody{PipeReader: reader, done: make(chan struct{})}
	go func() {
		defer close(body.done)
		defer closeAll()
		err := writeMultipart(writer, boundary, m, func(w io.Writer, i int, file FilePart) error {
			if opened[i] == nil {
				_, err := w.Write(file.Content)
				return err
			}
			if _, err := io.CopyN(w, opened[i], sizes[i]); err != nil {
				var pipeErr *Error
				if errors.As(err, &pipeErr) || errors.Is(err, io.ErrClosedPipe) {
					return err
				}
				body.err = &Error{Message: fmt.Sprintf("Datei %s nicht lesbar: %v", file.Path, err), Code: "LOCAL_FILE_CHANGED", Exit: 1,
					Details: map[string]any{"file": file.Path}}
				return body.err
			}
			// Mehr als beim Fstat? Dann hat sich die Datei beim Senden
			// geändert — abschneiden wäre ein stiller Datenfehler.
			var probe [1]byte
			if n, _ := opened[i].Read(probe[:]); n > 0 {
				body.err = &Error{Message: fmt.Sprintf("Datei %s hat sich beim Senden geändert — nicht hochgeladen", file.Path),
					Code: "LOCAL_FILE_CHANGED", Exit: 1, Details: map[string]any{"file": file.Path}}
				return body.err
			}
			return nil
		})
		_ = writer.CloseWithError(err)
	}()
	return body, contentType, frame.n + total, nil
}

// maxRawSnippet begrenzt den Rohtext einer Nicht-JSON-Antwort.
const maxRawSnippet = 200

// backendError baut den Fehler aus der Antwort. Die Meldung wird unverändert
// durchgereicht (sie kommt aus api_error() im Backend), und das komplette
// Payload wandert nach Details — dort steht für den Aufrufer die Lösung.
func backendError(res *Response) *Error {
	e := &Error{Status: res.Status}
	payload, isJSON := decodeErrorPayload(res.Body)
	if isJSON {
		e.Details = payload
		for _, key := range []string{"message", "error", "msg", "description"} {
			if v, ok := payload[key].(string); ok && v != "" {
				e.Message = v
				break
			}
		}
		if v, ok := payload["code"].(string); ok {
			e.Code = v
		}
	}
	if e.Message != "" {
		return e
	}

	e.Message = statusMessage(res.Status, isJSON)
	if !isJSON {
		// Der Rohtext bleibt erreichbar, aber als eigenes Feld und gekürzt —
		// eine HTML-Seite als Fehlermeldung ist für niemanden lesbar.
		if raw := condense(res.Body); raw != "" {
			e.Details = map[string]any{"raw": raw}
		}
	}
	return e
}

// decodeErrorPayload liest das Fehler-Objekt, ohne Zahlen zu verfälschen.
// Alles, was kein JSON-Objekt ist (HTML-Seite, Array, Skalar), gilt als
// "kein JSON".
func decodeErrorPayload(body []byte) (map[string]any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return nil, false
	}
	return payload, true
}

// statusMessage ist die Meldung, wenn das Backend selbst keine geliefert hat.
func statusMessage(status int, isJSON bool) string {
	base := fmt.Sprintf("HTTP %d %s", status, http.StatusText(status))
	if isJSON {
		return base + " (Antwort ohne message-Feld)"
	}
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		// Genau der Fall, der sonst zu geratenen Pfaden führt: Nicht das CLI
		// baut Unsinn, die Route gibt es auf dieser Instanz nicht.
		return base + " — die Route existiert auf dieser Instanz nicht oder erlaubt diese Methode nicht (Antwort war kein JSON)"
	default:
		return base + " — die Antwort war kein JSON"
	}
}

// condense macht aus einer HTML-Fehlerseite eine kurze Zeile: Tags raus,
// Whitespace zusammen, nach maxRawSnippet Zeichen Schluss.
func condense(body []byte) string {
	out := &strings.Builder{}
	inTag := false
	pendingSpace := false
	for _, r := range string(body) {
		switch {
		case r == '<':
			inTag = true
			pendingSpace = out.Len() > 0
			continue
		case r == '>':
			inTag = false
			continue
		case inTag:
			continue
		case r == ' ', r == '\t', r == '\n', r == '\r':
			pendingSpace = out.Len() > 0
			continue
		}
		if pendingSpace {
			out.WriteByte(' ')
			pendingSpace = false
		}
		out.WriteRune(r)
	}
	text := []rune(out.String())
	if len(text) > maxRawSnippet {
		return string(text[:maxRawSnippet]) + "…"
	}
	return string(text)
}
