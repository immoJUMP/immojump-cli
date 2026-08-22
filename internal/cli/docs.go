package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/immoJUMP/immojump-cli/internal/config"
)

// ExitCode dokumentiert einen stabilen Exit-Code (siehe doc/DESIGN.md).
type ExitCode struct {
	Code    int
	Meaning string
}

// ExitCodes ist die einzige Quelle für Referenz, Schema und README.
var ExitCodes = []ExitCode{
	{0, "Erfolg"},
	{1, "sonstiger API-Fehler (nicht unten gemappt)"},
	{2, "Usage-Fehler (unbekannter Befehl, Flag, Args)"},
	{3, "lokale Konfiguration/Auth unvollständig"},
	{4, "401 — Token fehlt oder ist ungültig"},
	{5, "404 — nicht gefunden"},
	{6, "403 — keine Berechtigung, oder lokal durch Policy blockiert"},
	{7, "429 — Rate Limit, später erneut versuchen"},
	{8, "5xx oder Netzwerkfehler — temporär, Retry möglich"},
	{9, "409 — Konflikt (z. B. widerrufener Share-Link)"},
	{11, "400/422 — Validierung fehlgeschlagen"},
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "(keine)"
	}
	return strings.Join(values, ", ")
}

// --- schema ---------------------------------------------------------------

type schemaFlag struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

type schemaArg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Optional    bool   `json:"optional,omitempty"`
}

type schemaCommand struct {
	Resource string `json:"resource"`
	Verb     string `json:"verb"`
	Summary  string `json:"summary"`
	// Risk ist "dynamic", wenn das Level erst pro Aufruf entsteht (api).
	Risk string `json:"risk"`
	// RiskRule erklärt dann, wie es entsteht.
	RiskRule string       `json:"risk_rule,omitempty"`
	Method   string       `json:"method,omitempty"`
	Path     string       `json:"path,omitempty"`
	Endpoint string       `json:"endpoint"`
	Local    bool         `json:"local,omitempty"`
	Raw      bool         `json:"raw,omitempty"`
	Args     []schemaArg  `json:"args"`
	Flags    []schemaFlag `json:"flags"`
	Example  string       `json:"example"`
}

func toSchemaCommand(spec Spec) schemaCommand {
	args := []schemaArg{}
	for _, arg := range spec.Args {
		args = append(args, schemaArg{Name: arg.Name, Description: arg.Desc, Optional: arg.Optional})
	}
	flags := []schemaFlag{}
	for _, flag := range spec.Flags {
		flags = append(flags, schemaFlag{
			Name: flag.Name, Kind: string(flag.Kind), Description: flag.Desc, Required: flag.Required,
		})
	}
	return schemaCommand{
		Resource: spec.Resource,
		Verb:     spec.Verb,
		Summary:  spec.Summary,
		Risk:     spec.RiskLabel(),
		RiskRule: spec.RiskRule(),
		Method:   spec.Method,
		Path:     spec.Path,
		Endpoint: spec.Endpoint(),
		Local:    spec.Local,
		Raw:      spec.Raw,
		Args:     args,
		Flags:    flags,
		Example:  spec.Example,
	}
}

func (r *runner) runSchema(args []string, flags *flagValues) int {
	specs := Registry
	if len(args) > 0 {
		filtered := specsForResource(args[0])
		if len(filtered) == 0 {
			return r.fail(usageErr("Unbekannte Ressource %q.%s", args[0], suggestion(args[0], resourceNames())))
		}
		if len(args) > 1 {
			spec, ok := Lookup(args[0], args[1])
			if !ok {
				return r.fail(usageErr("Unbekannter Befehl %q für %q.%s",
					args[1], args[0], suggestion(args[1], verbNames(filtered))))
			}
			filtered = []Spec{spec}
		}
		specs = filtered
	}

	commands := []schemaCommand{}
	for _, spec := range specs {
		commands = append(commands, toSchemaCommand(spec))
	}
	globals := []schemaFlag{}
	for _, flag := range GlobalFlags {
		globals = append(globals, schemaFlag{Name: flag.Name, Kind: string(flag.Kind), Description: flag.Desc})
	}
	exitCodes := map[string]string{}
	for _, code := range ExitCodes {
		exitCodes[strconv.Itoa(code.Code)] = code.Meaning
	}

	return r.emit(map[string]any{
		"version":      Version,
		"exit_codes":   exitCodes,
		"risk_levels":  []string{string(RiskRead), string(RiskWrite), string(RiskExternal), string(RiskDestructive)},
		"global_flags": globals,
		"commands":     commands,
	}, flags)
}

// --- docs -----------------------------------------------------------------

func (r *runner) runDocs() int {
	out := &strings.Builder{}

	out.WriteString("# immojump — Befehlsreferenz\n\n")
	out.WriteString("Erzeugt mit `make docs` (`immojump docs`) aus der Command-Registry.\n")
	out.WriteString("Nicht von Hand bearbeiten — neue Endpoints entstehen als Spec-Zeile in\n")
	out.WriteString("`internal/cli/registry.go`.\n\n")

	out.WriteString("## Ressourcen\n\n")
	out.WriteString("| Ressource | Beschreibung |\n| --- | --- |\n")
	for _, res := range Resources {
		fmt.Fprintf(out, "| `%s` | %s |\n", res.Name, res.Summary)
	}

	out.WriteString("\n## Globale Flags\n\n")
	out.WriteString("| Flag | Beschreibung |\n| --- | --- |\n")
	for _, flag := range GlobalFlags {
		if flag.Name == "h" {
			continue
		}
		fmt.Fprintf(out, "| `%s` | %s |\n", flagUsage(flag), flag.Desc)
	}

	out.WriteString("\n## Umgebungsvariablen\n\n")
	out.WriteString("| Variable | Wirkung |\n| --- | --- |\n")
	fmt.Fprintf(out, "| `%s` | API-Token |\n", config.EnvToken)
	fmt.Fprintf(out, "| `%s` | Organisations-ID |\n", config.EnvOrg)
	fmt.Fprintf(out, "| `%s` | Instanz (muss auf der Allowlist stehen) |\n", config.EnvBaseURL)
	fmt.Fprintf(out, "| `%s` | Context aus der Konfiguration |\n", config.EnvContext)
	fmt.Fprintf(out, "| `%s` | Pfad der Konfigurationsdatei |\n", config.EnvConfig)
	fmt.Fprintf(out, "| `%s` | zusätzlich erlaubte Instanzen (kommagetrennt) |\n", config.EnvExtraURLs)
	fmt.Fprintf(out, "| `%s` | MCP-kompatibler Alias dazu |\n", config.EnvExtraURLsMCP)
	out.WriteString("| `IMMOJUMP_ALLOW` | erlaubte Risk-Level, wie `--allow` |\n")

	out.WriteString("\n## Risk-Level und Policy\n\n")
	out.WriteString("Jeder Befehl trägt ein Risk-Level:\n\n")
	out.WriteString("| Level | Bedeutung |\n| --- | --- |\n")
	out.WriteString("| `read` | liest nur |\n")
	out.WriteString("| `write` | ändert Daten in immoJUMP |\n")
	out.WriteString("| `external` | macht etwas nach außen sichtbar (Freigabe-Link, E-Mail) |\n")
	out.WriteString("| `destructive` | löscht Daten |\n\n")
	out.WriteString("Begrenzen mit `--readonly` (nur `read`) oder `--allow read,write`;\n")
	out.WriteString("dasselbe geht per `IMMOJUMP_ALLOW`. Ein blockierter Befehl endet mit\n")
	out.WriteString("Exit 6 und `code: \"POLICY_BLOCKED\"` auf stderr.\n\n")
	out.WriteString("Die Policy ist fail-closed: Eine gesetzte, aber leere Liste\n")
	out.WriteString("(`--allow \"\"`, `IMMOJUMP_ALLOW=,`) und ein unbekanntes Level sind\n")
	out.WriteString("Konfigurationsfehler (Exit 3), kein „dann eben alles\". Ohne jede\n")
	out.WriteString("Angabe bleibt alles erlaubt.\n\n")
	fmt.Fprintf(out, "`api <METHOD> <pfad>` trägt kein festes Level: %s\n\n", APIRiskRule)
	out.WriteString("Das ist Schutz gegen Versehen, keine Sicherheitsgrenze: Ein Agent mit\n")
	out.WriteString("uneingeschränktem Token kann die API auch direkt aufrufen.\n")

	out.WriteString("\n## Exit-Codes\n\n")
	out.WriteString("| Exit | Bedeutung |\n| ---: | --- |\n")
	for _, code := range ExitCodes {
		fmt.Fprintf(out, "| `%d` | %s |\n", code.Code, code.Meaning)
	}

	out.WriteString("\n## Befehle\n")
	for _, res := range Resources {
		specs := specsForResource(res.Name)
		if len(specs) == 0 {
			continue
		}
		fmt.Fprintf(out, "\n### %s\n\n%s\n\n", res.Name, res.Summary)
		out.WriteString("| Befehl | Risk | Endpoint |\n| --- | --- | --- |\n")
		for _, spec := range specs {
			fmt.Fprintf(out, "| `%s` | %s | `%s` |\n", spec.Name(), spec.RiskLabel(), spec.Endpoint())
		}
		for _, spec := range specs {
			writeCommandSection(out, spec)
		}
	}

	fmt.Fprint(r.stdout, out.String())
	return 0
}

func writeCommandSection(out *strings.Builder, spec Spec) {
	fmt.Fprintf(out, "\n#### %s\n\n%s\n\n", spec.Name(), spec.Summary)
	fmt.Fprintf(out, "- **Aufruf:** `%s`\n", spec.Usage())
	fmt.Fprintf(out, "- **Endpoint:** `%s`\n", spec.Endpoint())
	fmt.Fprintf(out, "- **Risk:** `%s`\n", spec.RiskLabel())
	if rule := spec.RiskRule(); rule != "" {
		fmt.Fprintf(out, "  - %s\n", rule)
	}
	if len(spec.Args) > 0 {
		out.WriteString("- **Argumente:**\n")
		for _, arg := range spec.Args {
			suffix := ""
			if arg.Optional {
				suffix = " (optional)"
			}
			fmt.Fprintf(out, "  - `%s`%s — %s\n", arg.Name, suffix, arg.Desc)
		}
	}
	if len(spec.Flags) > 0 {
		out.WriteString("- **Flags:**\n")
		for _, flag := range spec.Flags {
			prefix := ""
			if flag.Required {
				prefix = "(Pflicht) "
			}
			fmt.Fprintf(out, "  - `%s` — %s%s\n", flagUsage(flag), prefix, flag.Desc)
		}
	}
	if hint := bodyHint(spec); hint != "" {
		fmt.Fprintf(out, "- **Body:** %s\n", hint)
	}
	if spec.Raw {
		out.WriteString("- **Antwort:** möglicherweise kein JSON — wird roh nach stdout geschrieben.\n")
	}
	fmt.Fprintf(out, "- **Beispiel:** `%s`\n", spec.Example)
}
