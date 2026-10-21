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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/immoJUMP/immojump-cli/internal/config"
)

// DefaultTimeout gilt, wenn kein --timeout gesetzt ist.
const DefaultTimeout = 60 * time.Second

// UserAgent identifiziert das CLI gegenüber dem Backend.
var UserAgent = "immojump-cli"

// FilePart ist eine Datei in einem Multipart-Upload.
type FilePart struct {
	Field    string
	Filename string
	Content  []byte
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
}

// Response ist die rohe Antwort — die Interpretation macht internal/output.
type Response struct {
	Status      int
	ContentType string
	Header      http.Header
	Body        []byte
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
		return e.Code == "NETWORK"
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
// c.Retries-mal bei 429/502/503/504 und Netzwerkfehlern — mit exponentiellem
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

	attempts := 1
	if c.Retries > 0 && (req.Idempotent || method == http.MethodGet || method == http.MethodHead) {
		attempts += c.Retries
	}
	for attempt := 1; ; attempt++ {
		res, err := c.doOnce(req, method, target)
		var apiErr *Error
		if err == nil || attempt >= attempts || !errors.As(err, &apiErr) || !apiErr.Retryable() {
			return res, err
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
	body, contentType, length, err := buildBody(req)
	if err != nil {
		return nil, &Error{Message: err.Error(), Code: "BAD_REQUEST_BODY", Exit: 2}
	}
	if closer, ok := body.(io.Closer); ok {
		defer func() { _ = closer.Close() }()
	}

	if c.Trace != nil {
		c.Trace(method, target)
	}
	// Der Timeout hängt am Context, damit er auch dann greift, wenn der
	// HTTP-Client von außen kommt und keinen eigenen mitbringt. Der Body ist
	// vollständig gelesen, bevor doOnce zurückkehrt — cancel darf hier stehen.
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
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
	if err != nil {
		var localErr *Error
		if errors.As(err, &localErr) {
			// Ein Fehler beim Lesen einer lokalen Datei für den Upload ist
			// kein Netzwerkproblem — ein Retry änderte daran nichts.
			return nil, localErr
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
		buf := &bytes.Buffer{}
		writer := multipart.NewWriter(buf)
		for _, file := range req.Multipart.Files {
			part, err := writer.CreateFormFile(file.Field, file.Filename)
			if err != nil {
				return nil, "", 0, fmt.Errorf("Multipart-Feld %s nicht baubar: %v", file.Field, err)
			}
			if _, err := part.Write(file.Content); err != nil {
				return nil, "", 0, fmt.Errorf("Datei %s nicht schreibbar: %v", file.Filename, err)
			}
		}
		for key, value := range req.Multipart.Fields {
			if err := writer.WriteField(key, value); err != nil {
				return nil, "", 0, fmt.Errorf("Formfeld %s nicht schreibbar: %v", key, err)
			}
		}
		if err := writer.Close(); err != nil {
			return nil, "", 0, fmt.Errorf("Multipart nicht abschließbar: %v", err)
		}
		return buf, writer.FormDataContentType(), int64(buf.Len()), nil
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
