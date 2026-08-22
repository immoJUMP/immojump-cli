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
}

// Response ist die rohe Antwort — die Interpretation macht internal/output.
type Response struct {
	Status      int
	ContentType string
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
}

// Error ist ein Backend- oder Transportfehler mit stabilem Exit-Code.
type Error struct {
	Status  int
	Message string
	Code    string
	// Exit überschreibt die Ableitung aus Status (z. B. lokale Fehler).
	Exit int
	Err  error
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
func (c *Client) Do(req Request) (*Response, error) {
	if err := config.CheckBaseURL(c.BaseURL, c.Env); err != nil {
		return nil, &Error{Message: err.Error(), Code: "BASE_URL_NOT_ALLOWED", Exit: 3}
	}

	target, err := c.buildURL(req)
	if err != nil {
		return nil, &Error{Message: err.Error(), Code: "BAD_REQUEST_URL", Exit: 2}
	}

	body, contentType, err := buildBody(req)
	if err != nil {
		return nil, &Error{Message: err.Error(), Code: "BAD_REQUEST_BODY", Exit: 2}
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	// Der Timeout hängt am Context, damit er auch dann greift, wenn der
	// HTTP-Client von außen kommt und keinen eigenen mitbringt. Der Body ist
	// vollständig gelesen, bevor Do zurückkehrt — cancel darf hier stehen.
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout())
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, &Error{Message: fmt.Sprintf("Request nicht baubar: %v", err), Exit: 2}
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
		Body:        raw,
	}
	if httpRes.StatusCode >= 400 {
		return res, backendError(res)
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

func buildBody(req Request) (io.Reader, string, error) {
	if req.Multipart != nil {
		buf := &bytes.Buffer{}
		writer := multipart.NewWriter(buf)
		for _, file := range req.Multipart.Files {
			part, err := writer.CreateFormFile(file.Field, file.Filename)
			if err != nil {
				return nil, "", fmt.Errorf("Multipart-Feld %s nicht baubar: %v", file.Field, err)
			}
			if _, err := part.Write(file.Content); err != nil {
				return nil, "", fmt.Errorf("Datei %s nicht schreibbar: %v", file.Filename, err)
			}
		}
		for key, value := range req.Multipart.Fields {
			if err := writer.WriteField(key, value); err != nil {
				return nil, "", fmt.Errorf("Formfeld %s nicht schreibbar: %v", key, err)
			}
		}
		if err := writer.Close(); err != nil {
			return nil, "", fmt.Errorf("Multipart nicht abschließbar: %v", err)
		}
		return buf, writer.FormDataContentType(), nil
	}
	if len(req.Body) == 0 {
		return nil, "", nil
	}
	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	return bytes.NewReader(req.Body), contentType, nil
}

// backendError liest {message, code} aus der Antwort — die Meldung wird
// unverändert durchgereicht (sie kommt aus api_error() im Backend).
func backendError(res *Response) *Error {
	e := &Error{Status: res.Status}
	var payload map[string]any
	if json.Unmarshal(res.Body, &payload) == nil {
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
	if e.Message == "" {
		snippet := strings.TrimSpace(string(res.Body))
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		e.Message = fmt.Sprintf("HTTP %d %s", res.Status, http.StatusText(res.Status))
		if snippet != "" {
			e.Message += ": " + snippet
		}
	}
	return e
}
