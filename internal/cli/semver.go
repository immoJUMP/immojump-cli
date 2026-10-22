package cli

import (
	"strconv"
	"strings"
)

// parseVersion liest "v1.2.3" bzw. "1.2.3" in drei Zahlen. Ein Anhang aus
// `git describe` ("-3-gabc1234-dirty") zählt nicht mit: Der Build steht dann
// zwischen diesem Tag und dem nächsten und gilt als dieser Tag.
func parseVersion(raw string) ([3]int, bool) {
	var out [3]int
	v := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// versionOlder meldet, ob a älter als b ist. ok ist false, wenn eine der
// beiden Versionen nicht lesbar ist (etwa "dev") — dann gibt es keine Aussage.
func versionOlder(a, b string) (older, ok bool) {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if !okA || !okB {
		return false, false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] < vb[i], true
		}
	}
	return false, true
}
