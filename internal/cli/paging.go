package cli

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/immoJUMP/immojump-cli/internal/api"
	"github.com/immoJUMP/immojump-cli/internal/output"
)

// maxPageSize ist die Obergrenze der blätterbaren Routen (limit 1–500).
const maxPageSize = 500

// maxPages bricht eine Blätter-Schleife ab, die nie endet (ein Backend, das
// total falsch meldet). 2000 Seiten zu 500 sind eine Million Zeilen.
const maxPages = 2000

// envelope ist der Umschlag der blätterbaren Routen.
type envelope struct {
	Items []json.RawMessage `json:"items"`
	Total *json.Number      `json:"total"`
}

// pageWindow liest --limit (Seitengröße) und --offset (Start) für --all.
func pageWindow(flags *flagValues) (int, int, error) {
	size, start := maxPageSize, 0
	if flags.has("limit") {
		raw := strings.TrimSpace(flags.get("limit"))
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxPageSize {
			return 0, 0, usageErr("--limit erwartet eine ganze Zahl von 1 bis %d, bekommen: %q", maxPageSize, raw)
		}
		size = n
	}
	if flags.has("offset") {
		raw := strings.TrimSpace(flags.get("offset"))
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, usageErr("--offset erwartet eine ganze Zahl ab 0, bekommen: %q", raw)
		}
		start = n
	}
	return size, start, nil
}

// fetchAllPages holt alle Seiten einer blätterbaren Route. Antwortet die Route
// mit einem nackten Array (ein Backend ohne Blättern), ist das bereits alles.
// isArray meldet genau diesen Fall.
func fetchAllPages(client *api.Client, request *api.Request, size, start int) (items []json.RawMessage, isArray bool, err error) {
	items = []json.RawMessage{}
	offset := start
	for page := 0; page < maxPages; page++ {
		query := url.Values{}
		for key, values := range request.Query {
			query[key] = append([]string(nil), values...)
		}
		query.Set("limit", strconv.Itoa(size))
		query.Set("offset", strconv.Itoa(offset))
		pageRequest := *request
		pageRequest.Query = query

		response, err := client.Do(pageRequest)
		if err != nil {
			return nil, false, err
		}
		trimmed := strings.TrimSpace(string(response.Body))
		if page == 0 && strings.HasPrefix(trimmed, "[") {
			var all []json.RawMessage
			if err := decodeJSONExact(response.Body, &all); err != nil {
				return nil, false, &api.Error{Message: "Antwort ist kein gültiges JSON-Array: " + err.Error(), Exit: 1}
			}
			return all, true, nil
		}
		var env envelope
		if err := decodeJSONExact(response.Body, &env); err != nil || env.Items == nil {
			return nil, false, &api.Error{
				Message: "Antwort ohne Umschlag {items, total} — die Route blättert auf dieser Instanz nicht",
				Exit:    1,
			}
		}
		items = append(items, env.Items...)
		offset += len(env.Items)
		if len(env.Items) == 0 || env.Total == nil {
			return items, false, nil
		}
		total, err := env.Total.Int64()
		if err != nil || int64(offset) >= total {
			return items, false, nil
		}
	}
	return nil, false, &api.Error{Message: "Abbruch nach " + strconv.Itoa(maxPages) + " Seiten — total wächst schneller als geblättert wird", Exit: 1}
}

// runAllPages ist --all: alle Seiten holen, als EIN Array ausgeben. Scheitert
// eine Seite, bleibt stdout leer — eine halbe Liste sähe aus wie die ganze.
func (r *runner) runAllPages(client *api.Client, request *api.Request, flags *flagValues) int {
	size, start, err := pageWindow(flags)
	if err != nil {
		return r.fail(err)
	}
	items, _, err := fetchAllPages(client, request, size, start)
	if err != nil {
		return r.fail(err)
	}
	raw, err := output.Marshal(items)
	if err != nil {
		return r.fail(err)
	}
	if err := r.render(raw, "application/json", flags); err != nil {
		return r.fail(err)
	}
	return 0
}
