// Package cli enthält die deklarative Command-Registry und den Dispatcher.
// Aus derselben Tabelle entstehen Hilfe, REFERENCE.md und das JSON-Schema.
package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/immoJUMP/immojump-cli/internal/api"
	"github.com/immoJUMP/immojump-cli/internal/config"
	"github.com/immoJUMP/immojump-cli/internal/output"
)

// Version wird beim Build per -ldflags gesetzt.
var Version = "dev"

// Options injiziert die komplette Umgebung — damit laufen Tests parallel und
// ohne echte Env-Variablen oder Konfigurationsdateien.
type Options struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	HTTP   *http.Client
}

type runner struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	getenv func(string) string
	http   *http.Client
}

// Run führt einen CLI-Aufruf aus und liefert den Exit-Code.
func Run(args []string, opts Options) int {
	r := &runner{
		stdin:  opts.Stdin,
		stdout: opts.Stdout,
		stderr: opts.Stderr,
		getenv: opts.Getenv,
		http:   opts.HTTP,
	}
	if r.stdin == nil {
		r.stdin = strings.NewReader("")
	}
	if r.stdout == nil {
		r.stdout = io.Discard
	}
	if r.stderr == nil {
		r.stderr = io.Discard
	}
	if r.getenv == nil {
		r.getenv = os.Getenv
	}
	return r.dispatch(args)
}

// usageErr ist ein Bedienfehler (Exit 2).
func usageErr(format string, a ...any) error {
	return &api.Error{Message: fmt.Sprintf(format, a...), Code: "USAGE", Exit: 2}
}

// configErr ist eine unvollständige lokale Konfiguration (Exit 3).
func configErr(format string, a ...any) error {
	return &api.Error{Message: fmt.Sprintf(format, a...), Code: "CONFIG", Exit: 3}
}

// fail schreibt die Fehlerzeile nach stderr und liefert den Exit-Code.
//
// Fehler ohne eigenen Exit-Code sind lokal (kaputtes stdout, nicht
// serialisierbare Ausgabe) und bekommen Exit 1. Exit 8 wäre eine falsche
// Zusage: Er verspricht "temporär, Retry möglich".
func (r *runner) fail(err error) int {
	var apiErr *api.Error
	if !errors.As(err, &apiErr) {
		apiErr = &api.Error{Message: err.Error(), Exit: 1}
	}
	_ = output.WriteError(r.stderr, apiErr.Status, apiErr.Message, apiErr.Code)
	return apiErr.ExitCode()
}

func (r *runner) dispatch(args []string) int {
	positionals, flags, err := parseArgs(args)
	if err != nil {
		return r.fail(err)
	}

	if flags.bool("version") {
		return r.printVersion()
	}

	wantHelp := flags.bool("help") || flags.bool("h")
	if len(positionals) > 0 && positionals[0] == "help" {
		wantHelp = true
		positionals = positionals[1:]
	}
	if len(positionals) == 0 {
		r.printRootHelp()
		return 0
	}

	resource := positionals[0]
	specs := specsForResource(resource)
	if len(specs) == 0 {
		return r.fail(usageErr("Unbekannte Ressource %q.%s Übersicht: immojump --help",
			resource, suggestion(resource, resourceNames())))
	}

	var spec Spec
	var rest []string
	if len(specs) == 1 && specs[0].Verb == "" {
		spec = specs[0]
		rest = positionals[1:]
	} else {
		if len(positionals) < 2 {
			if wantHelp {
				r.printResourceHelp(resource, specs)
				return 0
			}
			return r.fail(usageErr("%q braucht einen Befehl. Verfügbar: %s",
				resource, strings.Join(verbNames(specs), ", ")))
		}
		verb := positionals[1]
		found, ok := Lookup(resource, verb)
		if !ok {
			return r.fail(usageErr("Unbekannter Befehl %q für %q.%s Übersicht: immojump %s --help",
				verb, resource, suggestion(verb, verbNames(specs)), resource))
		}
		spec = found
		rest = positionals[2:]
	}

	if wantHelp {
		r.printCommandHelp(spec)
		return 0
	}

	if err := validateFlags(spec, flags); err != nil {
		return r.fail(err)
	}
	if err := validateArgs(spec, rest); err != nil {
		return r.fail(err)
	}
	if err := checkGlobalFlagSupport(spec, flags); err != nil {
		return r.fail(err)
	}

	risk := spec.Risk
	if spec.Special == SpecialAPI {
		risk = riskForAPI(rest[0], matchablePath(rest[1]))
	}
	allowed, err := resolveAllowedRisks(flags, r.getenv)
	if err != nil {
		return r.fail(err)
	}
	if !allowed[risk] {
		return r.fail(&api.Error{
			Message: fmt.Sprintf("%q ist %s und durch die lokale Policy gesperrt. Erlaubt: %s.",
				spec.Name(), risk, joinRisks(allowed)),
			Code: "POLICY_BLOCKED",
			Exit: 6,
		})
	}

	if spec.Local {
		return r.runLocal(spec, rest, flags)
	}
	return r.runHTTP(spec, rest, flags)
}

// --- Argument-Parser ------------------------------------------------------

// flagKinds kennt jedes Flag des Programms. Der Parser braucht das, um zu
// entscheiden, ob nach einem Flag ein Wert folgt — noch bevor feststeht,
// welcher Befehl gemeint ist.
var flagKinds = buildFlagKinds()

func buildFlagKinds() map[string]FlagKind {
	kinds := map[string]FlagKind{}
	for _, flag := range GlobalFlags {
		kinds[flag.Name] = flag.Kind
	}
	for _, spec := range Registry {
		for _, flag := range spec.Flags {
			kinds[flag.Name] = flag.Kind
		}
	}
	return kinds
}

type flagValues struct {
	order []string
	vals  map[string][]string
}

func newFlagValues() *flagValues {
	return &flagValues{vals: map[string][]string{}}
}

func (f *flagValues) add(name, value string) {
	if _, ok := f.vals[name]; !ok {
		f.order = append(f.order, name)
	}
	f.vals[name] = append(f.vals[name], value)
}

func (f *flagValues) has(name string) bool {
	_, ok := f.vals[name]
	return ok
}

// get liefert den zuletzt gesetzten Wert.
func (f *flagValues) get(name string) string {
	values := f.vals[name]
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func (f *flagValues) all(name string) []string { return f.vals[name] }

func (f *flagValues) bool(name string) bool {
	if !f.has(name) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(f.get(name))) {
	case "", "true", "1", "ja", "yes":
		return true
	default:
		return false
	}
}

// parseArgs trennt positionale Argumente von Flags. Globale Flags dürfen
// vor oder nach dem Befehl stehen.
func parseArgs(args []string) ([]string, *flagValues, error) {
	positionals := []string{}
	flags := newFlagValues()

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if len(arg) < 2 || !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}

		name := strings.TrimLeft(arg, "-")
		value := ""
		hasValue := false
		if idx := strings.Index(name, "="); idx >= 0 {
			value, name, hasValue = name[idx+1:], name[:idx], true
		}
		if name == "" {
			return nil, nil, usageErr("Ungültiges Flag %q", arg)
		}
		kind, known := flagKinds[name]
		if !known {
			return nil, nil, usageErr("Unbekanntes Flag %q.%s",
				arg, suggestion(name, knownFlagNames()))
		}
		if kind != FlagBool && !hasValue {
			if i+1 >= len(args) {
				return nil, nil, usageErr("Flag --%s erwartet einen Wert", name)
			}
			value = args[i+1]
			i++
		}
		flags.add(name, value)
	}
	return positionals, flags, nil
}

func validateFlags(spec Spec, flags *flagValues) error {
	allowed := map[string]bool{}
	for _, flag := range GlobalFlags {
		allowed[flag.Name] = true
	}
	for _, flag := range spec.Flags {
		allowed[flag.Name] = true
	}
	for _, name := range flags.order {
		if !allowed[name] {
			return usageErr("Flag --%s gilt nicht für %q. Hilfe: immojump %s --help",
				name, spec.Name(), spec.Name())
		}
	}
	for _, flag := range spec.Flags {
		if flag.Required && !flags.has(flag.Name) {
			return usageErr("%q braucht --%s. Aufruf: %s", spec.Name(), flag.Name, spec.Usage())
		}
	}
	return nil
}

func validateArgs(spec Spec, args []string) error {
	required := 0
	for _, arg := range spec.Args {
		if !arg.Optional {
			required++
		}
	}
	if len(args) < required {
		return usageErr("%q erwartet %d Argument(e). Aufruf: %s", spec.Name(), required, spec.Usage())
	}
	if len(args) > len(spec.Args) {
		return usageErr("%q erwartet höchstens %d Argument(e), bekommen: %d. Aufruf: %s",
			spec.Name(), len(spec.Args), len(args), spec.Usage())
	}
	return nil
}

// --- Policy ---------------------------------------------------------------

// matchablePath macht aus dem Escape-Hatch-Argument einen Pfad, der sich gegen
// die Registry-Templates prüfen lässt.
func matchablePath(raw string) string {
	path := strings.TrimSpace(raw)
	if idx := strings.IndexAny(path, "?#"); idx >= 0 {
		path = path[:idx]
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func allRisks() map[Risk]bool {
	return map[Risk]bool{RiskRead: true, RiskWrite: true, RiskExternal: true, RiskDestructive: true}
}

// resolveAllowedRisks bestimmt die lokale Policy. Fail-closed: Eine gesetzte,
// aber leere Liste (--allow "" oder IMMOJUMP_ALLOW=",") ist ein
// Konfigurationsfehler — nicht "dann eben alles erlauben". Gar keine Angabe
// bleibt "alles erlaubt".
func resolveAllowedRisks(flags *flagValues, getenv func(string) string) (map[Risk]bool, error) {
	if flags.bool("readonly") {
		return map[Risk]bool{RiskRead: true}, nil
	}

	raw, source := "", ""
	switch {
	case flags.has("allow"):
		raw, source = flags.get("allow"), "--allow"
	default:
		// Nur eine komplett leere Umgebungsvariable zählt als "nicht gesetzt"
		// — von einer fehlenden lässt sie sich nicht unterscheiden. Alles
		// andere (auch " " oder ",") ist eine Angabe und wird geprüft.
		if value := getenv("IMMOJUMP_ALLOW"); value != "" {
			raw, source = value, "IMMOJUMP_ALLOW"
		}
	}
	if source == "" {
		return allRisks(), nil
	}

	allowed := map[Risk]bool{}
	for _, part := range config.SplitCSV(raw) {
		level := Risk(part)
		switch level {
		case RiskRead, RiskWrite, RiskExternal, RiskDestructive:
			allowed[level] = true
		default:
			return nil, configErr(
				"Unbekanntes Risk-Level %q in %s. Erlaubt: read, write, external, destructive", part, source)
		}
	}
	if len(allowed) == 0 {
		return nil, configErr(
			"%s ist leer — mindestens ein Level aus read,write,external,destructive angeben, z. B. --allow read,write",
			source)
	}
	return allowed, nil
}

func joinRisks(allowed map[Risk]bool) string {
	var out []string
	for _, level := range []Risk{RiskRead, RiskWrite, RiskExternal, RiskDestructive} {
		if allowed[level] {
			out = append(out, string(level))
		}
	}
	if len(out) == 0 {
		return "nichts"
	}
	return strings.Join(out, ", ")
}

// --- Ausführung -----------------------------------------------------------

func (r *runner) outputOptions(flags *flagValues) output.Options {
	return output.Options{
		Pretty: flags.bool("pretty"),
		Fields: config.SplitCSV(flags.get("fields")),
	}
}

// printVersion ist die eine Stelle, an der die Version rausgeht — `version`
// und `--version` sollen sich nie auseinanderentwickeln.
func (r *runner) printVersion() int {
	fmt.Fprintln(r.stdout, Version)
	return 0
}

// loadConfigFile liest die Konfigurationsdatei und liefert ihren Pfad mit —
// auth und context brauchen beides, run nur das Ergebnis der Auflösung.
func (r *runner) loadConfigFile() (string, *config.File, error) {
	path := config.Path(r.getenv)
	file, err := config.Load(path)
	if err != nil {
		return "", nil, configErr("%s", err.Error())
	}
	return path, file, nil
}

func (r *runner) resolveConfig(flags *flagValues) (config.Resolved, error) {
	_, file, err := r.loadConfigFile()
	if err != nil {
		return config.Resolved{}, err
	}
	resolved, err := config.Resolve(file, config.Overrides{
		Context: flags.get("context"),
		BaseURL: flags.get("base-url"),
		Org:     flags.get("org"),
	}, r.getenv)
	if err != nil {
		return config.Resolved{}, configErr("%s", err.Error())
	}
	return resolved, nil
}

func (r *runner) newClient(resolved config.Resolved, flags *flagValues) (*api.Client, error) {
	timeout := api.DefaultTimeout
	if raw := strings.TrimSpace(flags.get("timeout")); raw != "" {
		seconds, err := strconv.ParseFloat(raw, 64)
		if err != nil || seconds <= 0 {
			return nil, usageErr("--timeout erwartet Sekunden als positive Zahl, bekommen: %q", raw)
		}
		timeout = time.Duration(seconds * float64(time.Second))
	}
	httpClient := r.http
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	return &api.Client{
		BaseURL: resolved.BaseURL,
		Org:     resolved.Org,
		Token:   resolved.Token,
		HTTP:    httpClient,
		// Auch ein injizierter Client muss den Timeout einhalten.
		Timeout: timeout,
		Env:     r.getenv,
	}, nil
}

func (r *runner) runHTTP(spec Spec, args []string, flags *flagValues) int {
	// auth login legt Contexts erst an — es darf deshalb nicht über die
	// strenge Context-Auflösung laufen.
	if spec.Special == SpecialAuthLogin {
		return r.runAuthLogin(spec, flags)
	}

	resolved, err := r.resolveConfig(flags)
	if err != nil {
		return r.fail(err)
	}
	if spec.Special == SpecialAuthStatus {
		return r.runAuthStatus(spec, resolved, flags)
	}

	if resolved.Token == "" {
		return r.fail(configErr(
			"Kein API-Token gefunden. Setze IMMOJUMP_TOKEN oder lege einen Context an: immojump auth login --context <name> --token <token>"))
	}

	request, err := r.buildRequest(spec, args, flags, resolved)
	if err != nil {
		return r.fail(err)
	}

	client, err := r.newClient(resolved, flags)
	if err != nil {
		return r.fail(err)
	}
	response, err := client.Do(*request)
	if err != nil {
		return r.fail(err)
	}
	if err := output.Render(r.stdout, response.Body, response.ContentType, r.outputOptions(flags)); err != nil {
		return r.fail(err)
	}
	return 0
}

func (r *runner) runLocal(spec Spec, args []string, flags *flagValues) int {
	switch spec.Special {
	case SpecialVersion:
		return r.printVersion()
	case SpecialDocs:
		return r.runDocs()
	case SpecialSchema:
		return r.runSchema(args, flags)
	case SpecialContextList, SpecialContextCurrent, SpecialContextUse, SpecialContextDelete:
		return r.runContext(spec, args, flags)
	default:
		return r.fail(usageErr("Befehl %q ist nicht implementiert", spec.Name()))
	}
}

// --- Vorschläge -----------------------------------------------------------

func resourceNames() []string {
	out := make([]string, 0, len(Resources))
	for _, res := range Resources {
		out = append(out, res.Name)
	}
	return out
}

func verbNames(specs []Spec) []string {
	var out []string
	for _, spec := range specs {
		if spec.Verb != "" {
			out = append(out, spec.Verb)
		}
	}
	return out
}

func knownFlagNames() []string {
	out := make([]string, 0, len(flagKinds))
	for name := range flagKinds {
		out = append(out, name)
	}
	return out
}

// suggestion liefert " Meintest du …?" für nahe Treffer.
func suggestion(input string, candidates []string) string {
	var hits []string
	for _, candidate := range candidates {
		if editDistance(input, candidate) <= 2 || strings.HasPrefix(candidate, input) {
			hits = append(hits, candidate)
		}
	}
	if len(hits) == 0 {
		return ""
	}
	if len(hits) > 3 {
		hits = hits[:3]
	}
	return " Meintest du " + strings.Join(hits, ", ") + "?"
}

func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	curr := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		curr[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			curr[j] = minOf(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		copy(prev, curr)
	}
	return prev[len(br)]
}

func minOf(values ...int) int {
	best := values[0]
	for _, v := range values[1:] {
		if v < best {
			best = v
		}
	}
	return best
}
