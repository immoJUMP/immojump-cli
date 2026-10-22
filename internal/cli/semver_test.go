package cli

import "testing"

// TestVersionOlder: Der Vergleich arbeitet numerisch je Stelle — als String
// wäre "v0.10.0" kleiner als "v0.4.0", und ein Nutzer mit 0.4 bekäme nie den
// Hinweis auf 0.10.
func TestVersionOlder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.4.0", "v0.10.0", true},
		{"0.4.0", "v0.10.0", true},
		{"v0.10.0", "v0.4.0", false},
		{"v0.10.0", "v0.10.0", false},
		{"v0.10.0", "0.10.0", false},
		{"v0.9.9", "v1.0.0", true},
		{"v1.2.3", "v1.2.4", true},
		// git describe zwischen zwei Tags: gilt als der Tag davor.
		{"v0.10.0-3-gabc1234", "v0.10.0", false},
		{"v0.9.0-3-gabc1234-dirty", "v0.10.0", true},
	}
	for _, c := range cases {
		older, ok := versionOlder(c.a, c.b)
		if !ok {
			t.Errorf("versionOlder(%q, %q): nicht vergleichbar", c.a, c.b)
			continue
		}
		if older != c.want {
			t.Errorf("versionOlder(%q, %q) = %v, erwartet %v", c.a, c.b, older, c.want)
		}
	}
}

// TestVersionOlderRejectsUnparsable: Ein Entwicklungs-Build ("dev") oder ein
// Commit-Hash ist nicht vergleichbar. Dann gibt es keinen Hinweis — lieber
// schweigen als einen falschen geben.
func TestVersionOlderRejectsUnparsable(t *testing.T) {
	for _, pair := range [][2]string{
		{"dev", "v0.10.0"},
		{"abc1234", "v0.10.0"},
		{"v0.10.0", ""},
		{"v0.10", "v0.10.0"},
	} {
		if _, ok := versionOlder(pair[0], pair[1]); ok {
			t.Errorf("versionOlder(%q, %q) sollte nicht vergleichbar sein", pair[0], pair[1])
		}
	}
}
