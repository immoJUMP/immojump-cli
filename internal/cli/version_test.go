package cli

import (
	"runtime/debug"
	"testing"
)

// TestResolveVersionPrefersLdflags: Setzt der Build die Version per -ldflags
// (so baut das Makefile), gewinnt dieser Wert immer.
func TestResolveVersionPrefersLdflags(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}}
	if got := resolveVersion("v0.1.0", info, true); got != "v0.1.0" {
		t.Errorf("ldflags-Version erwartet, got %q", got)
	}
}

// TestResolveVersionFromBuildInfo: Ohne ldflags — der Fall bei
// `go install github.com/immoJUMP/immojump-cli/cmd/immojump@v0.1.0` — muss die
// Modulversion aus den Build-Infos kommen. Vorher meldete das CLI dort "dev",
// womit weder ein Fehlerbericht noch ein Agent die Version feststellen konnte.
func TestResolveVersionFromBuildInfo(t *testing.T) {
	info := &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}
	if got := resolveVersion(defaultVersion, info, true); got != "v0.1.0" {
		t.Errorf("Modulversion aus BuildInfo erwartet, got %q", got)
	}
}

// TestResolveVersionIgnoresDevelBuildInfo: `go run` und Builds aus dem
// Arbeitsbaum melden "(devel)" — das ist keine brauchbare Version.
func TestResolveVersionIgnoresDevelBuildInfo(t *testing.T) {
	for _, v := range []string{"(devel)", ""} {
		info := &debug.BuildInfo{Main: debug.Module{Version: v}}
		if got := resolveVersion(defaultVersion, info, true); got != defaultVersion {
			t.Errorf("Fallback auf %q erwartet für BuildInfo %q, got %q", defaultVersion, v, got)
		}
	}
}

// TestResolveVersionWithoutBuildInfo: Sind gar keine Build-Infos lesbar,
// bleibt es beim Vorgabewert statt zu paniken.
func TestResolveVersionWithoutBuildInfo(t *testing.T) {
	if got := resolveVersion(defaultVersion, nil, false); got != defaultVersion {
		t.Errorf("%q erwartet, got %q", defaultVersion, got)
	}
}
