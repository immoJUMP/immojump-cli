package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// flagLabel schreibt einzeichige Flags mit einem Strich (-q), alle anderen
// mit zweien.
func flagLabel(flag Flag) string {
	if len(flag.Name) == 1 {
		return "-" + flag.Name
	}
	return "--" + flag.Name
}

// flagUsage ist die Anzeigeform inklusive Platzhalter für den Wert.
func flagUsage(flag Flag) string {
	label := flagLabel(flag)
	if flag.Kind == FlagBool {
		return label
	}
	return label + " <wert>"
}

// newTabwriter liefert die einheitliche Spaltenformatierung aller Hilfetexte.
func newTabwriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func (r *runner) printRootHelp() {
	fmt.Fprintln(r.stdout, "immojump — CLI für die immoJUMP-API")
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, "Aufruf:")
	fmt.Fprintln(r.stdout, "  immojump <ressource> <befehl> [argumente] [flags]")
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, "Ressourcen:")
	w := newTabwriter(r.stdout)
	for _, res := range Resources {
		fmt.Fprintf(w, "  %s\t%s\n", res.Name, res.Summary)
	}
	_ = w.Flush()
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, "Globale Flags:")
	w = newTabwriter(r.stdout)
	for _, flag := range GlobalFlags {
		if flag.Name == "h" {
			continue
		}
		fmt.Fprintf(w, "  %s\t%s\n", flagUsage(flag), flag.Desc)
	}
	_ = w.Flush()
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, "Umgebung:")
	fmt.Fprintln(r.stdout, "  IMMOJUMP_TOKEN, IMMOJUMP_ORGANISATION_ID, IMMOJUMP_BASE_URL,")
	fmt.Fprintln(r.stdout, "  IMMOJUMP_CONTEXT, IMMOJUMP_CONFIG, IMMOJUMP_ALLOW, IMMOJUMP_EXTRA_BASE_URLS")
	fmt.Fprintln(r.stdout)
	fmt.Fprintln(r.stdout, "Weiter:")
	w = newTabwriter(r.stdout)
	fmt.Fprintln(w, "  immojump <ressource> --help\tBefehle einer Ressource")
	fmt.Fprintln(w, "  immojump <ressource> <befehl> --help\tArgumente, Flags, Risk, Beispiel")
	fmt.Fprintln(w, "  immojump schema\tkomplettes Schema als JSON")
	fmt.Fprintln(w, "  immojump docs\tMarkdown-Referenz")
	_ = w.Flush()
}

func (r *runner) printResourceHelp(resource string, specs []Spec) {
	summary := ""
	for _, res := range Resources {
		if res.Name == resource {
			summary = res.Summary
			break
		}
	}
	fmt.Fprintf(r.stdout, "immojump %s — %s\n\n", resource, summary)
	fmt.Fprintln(r.stdout, "Befehle:")
	w := newTabwriter(r.stdout)
	for _, spec := range specs {
		fmt.Fprintf(w, "  %s\t%s\t%s\n", spec.Verb, spec.RiskLabel(), spec.Summary)
	}
	_ = w.Flush()
	fmt.Fprintf(r.stdout, "\nDetails: immojump %s <befehl> --help\n", resource)
}

func (r *runner) printCommandHelp(spec Spec) {
	fmt.Fprintf(r.stdout, "immojump %s — %s\n\n", spec.Name(), spec.Summary)
	fmt.Fprintf(r.stdout, "Aufruf:   %s\n", spec.Usage())
	fmt.Fprintf(r.stdout, "Endpoint: %s\n", spec.Endpoint())
	fmt.Fprintf(r.stdout, "Risk:     %s\n", spec.RiskLabel())
	if rule := spec.RiskRule(); rule != "" {
		fmt.Fprintf(r.stdout, "          %s\n", rule)
	}

	if len(spec.Args) > 0 {
		fmt.Fprintln(r.stdout, "\nArgumente:")
		w := newTabwriter(r.stdout)
		for _, arg := range spec.Args {
			label := arg.Name
			if arg.Optional {
				label += " (optional)"
			}
			fmt.Fprintf(w, "  %s\t%s\n", label, arg.Desc)
		}
		_ = w.Flush()
	}

	if len(spec.Flags) > 0 {
		fmt.Fprintln(r.stdout, "\nFlags:")
		w := newTabwriter(r.stdout)
		for _, flag := range spec.Flags {
			desc := flag.Desc
			if flag.Required {
				desc = "(Pflicht) " + desc
			}
			fmt.Fprintf(w, "  %s\t%s\n", flagUsage(flag), desc)
		}
		_ = w.Flush()
	}

	if len(spec.QueryHints) > 0 {
		fmt.Fprintln(r.stdout, "\nBekannte Query-Parameter (per -q key=value):")
		w := newTabwriter(r.stdout)
		for _, hint := range spec.QueryHints {
			fmt.Fprintf(w, "  %s\t%s\n", hint.Name, hint.Summary)
		}
		_ = w.Flush()
	}

	if hint := bodyHint(spec); hint != "" {
		// Die Backticks sind für REFERENCE.md gedacht, im Terminal stören sie.
		fmt.Fprintf(r.stdout, "\nBody: %s\n", strings.ReplaceAll(hint, "`", ""))
	}

	fmt.Fprintf(r.stdout, "\nBeispiel:\n  %s\n", spec.Example)
	fmt.Fprintln(r.stdout, "\nGlobale Flags: immojump --help")
}
