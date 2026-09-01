package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestRegistryInvariants ist der generische Durchlauf über die komplette
// Registry: Er fängt neue Spec-Zeilen ab, die Hilfe, Doku oder Dispatch
// kaputt machen würden.
func TestRegistryInvariants(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range Registry {
		name := spec.Resource + " " + spec.Verb

		if spec.Resource == "" {
			t.Errorf("%q: Resource fehlt", name)
		}
		if seen[name] {
			t.Errorf("%q ist doppelt in der Registry", name)
		}
		seen[name] = true

		if strings.TrimSpace(spec.Summary) == "" {
			t.Errorf("%q: Summary fehlt (wird in --help, docs und schema gezeigt)", name)
		}
		if strings.TrimSpace(spec.Example) == "" {
			t.Errorf("%q: Example fehlt", name)
		}
		if spec.Special == SpecialAPI {
			// Der Escape-Hatch hat kein festes Risk — es entsteht erst aus
			// Methode und Pfad. Ein statisches Level wäre eine Lüge.
			if spec.Risk != "" {
				t.Errorf("%q: der Escape-Hatch darf kein statisches Risk tragen, hat %q", name, spec.Risk)
			}
			if spec.RiskLabel() != "dynamic" {
				t.Errorf("%q: RiskLabel dynamic erwartet, got %q", name, spec.RiskLabel())
			}
			if strings.TrimSpace(spec.RiskRule()) == "" {
				t.Errorf("%q: RiskRule muss die Auflösungsregel erklären", name)
			}
		} else {
			switch spec.Risk {
			case RiskRead, RiskWrite, RiskExternal, RiskDestructive:
			default:
				t.Errorf("%q: unbekanntes Risk-Level %q", name, spec.Risk)
			}
			if spec.RiskLabel() != string(spec.Risk) {
				t.Errorf("%q: RiskLabel soll das Risk spiegeln, got %q", name, spec.RiskLabel())
			}
		}

		argNames := map[string]bool{}
		for _, arg := range spec.Args {
			if arg.Name == "" {
				t.Errorf("%q: Arg ohne Name", name)
			}
			if strings.TrimSpace(arg.Desc) == "" {
				t.Errorf("%q: Arg %q ohne Beschreibung", name, arg.Name)
			}
			argNames[arg.Name] = true
		}

		optionalSeen := false
		for _, arg := range spec.Args {
			if arg.Optional {
				optionalSeen = true
				continue
			}
			if optionalSeen {
				t.Errorf("%q: Pflicht-Arg %q steht hinter einem optionalen Arg", name, arg.Name)
			}
		}

		if spec.Local {
			if spec.Method != "" || spec.Path != "" {
				t.Errorf("%q: lokaler Befehl darf keine Methode/Pfad haben", name)
			}
			continue
		}

		if spec.Special == SpecialAPI {
			// Der Escape-Hatch bekommt Methode und Pfad aus den Argumenten.
			if len(spec.Args) != 2 {
				t.Errorf("%q: erwartet genau zwei Argumente (Methode, Pfad)", name)
			}
			continue
		}

		if spec.Method == "" {
			t.Errorf("%q: Methode fehlt", name)
		}
		if !strings.HasPrefix(spec.Path, "/api/") {
			t.Errorf("%q: Pfad %q muss mit /api/ beginnen", name, spec.Path)
		}
		for _, placeholder := range pathPlaceholders(spec.Path) {
			if placeholder == "org" {
				continue
			}
			if !argNames[placeholder] {
				t.Errorf("%q: Platzhalter {%s} ist weder durch ein Arg noch durch {org} gedeckt",
					name, placeholder)
			}
		}
		for _, flag := range spec.Flags {
			if strings.TrimSpace(flag.Desc) == "" {
				t.Errorf("%q: Flag --%s ohne Beschreibung", name, flag.Name)
			}
			switch flag.Kind {
			case FlagString, FlagBool, FlagList, FlagNumber:
			default:
				t.Errorf("%q: Flag --%s mit unbekanntem Typ %q", name, flag.Name, flag.Kind)
			}
		}
	}
}

// TestQueryHintsOnlyOnReadingCommands: Query-Parameter beschreiben, wie eine
// Abfrage eingegrenzt wird — auf einem POST wäre das eine Falschaussage.
func TestQueryHintsOnlyOnReadingCommands(t *testing.T) {
	for _, spec := range Registry {
		if len(spec.QueryHints) == 0 {
			continue
		}
		if spec.Local || spec.Special == SpecialAPI || !strings.EqualFold(spec.Method, "GET") {
			t.Errorf("%q: QueryHints gehören nur an GET-Befehle (Methode %q, Local %v)",
				spec.Name(), spec.Method, spec.Local)
		}
		seen := map[string]bool{}
		for _, hint := range spec.QueryHints {
			if strings.TrimSpace(hint.Name) == "" {
				t.Errorf("%q: QueryHint ohne Name", spec.Name())
			}
			if strings.TrimSpace(hint.Summary) == "" {
				t.Errorf("%q: QueryHint %q ohne Beschreibung", spec.Name(), hint.Name)
			}
			if seen[hint.Name] {
				t.Errorf("%q: QueryHint %q ist doppelt", spec.Name(), hint.Name)
			}
			seen[hint.Name] = true
		}
	}
}

// TestListCommandsAdvertiseTheirQueryParameters: `slim=true` schrumpft die
// Immobilienliste um Faktor 6, `per_page` begrenzt sie — beides fand ein
// Agent bisher nirgends. Was das Backend auswertet, gehört in die Registry.
func TestListCommandsAdvertiseTheirQueryParameters(t *testing.T) {
	want := map[string][]string{
		"immobilien list":   {"slim", "page", "per_page"},
		"immobilien search": {"search", "page", "per_page"},
		"contacts list":     {"slim", "q", "page", "per_page"},
		"activities list":   {"q", "type", "status", "page", "per_page"},
		"documents list":    {"immobilien_id"},
		"statuses list":     {"lite"},
		"tags list":         {"for"},
	}
	for name, params := range want {
		parts := strings.SplitN(name, " ", 2)
		spec, ok := Lookup(parts[0], parts[1])
		if !ok {
			t.Fatalf("%q fehlt in der Registry", name)
		}
		declared := map[string]bool{}
		for _, hint := range spec.QueryHints {
			declared[hint.Name] = true
		}
		for _, param := range params {
			if !declared[param] {
				t.Errorf("%q: Query-Parameter %q ist im Backend belegt, fehlt aber in QueryHints", name, param)
			}
		}
	}
}

// TestNoExampleUsesUnsupportedParameters: `-q limit=3` lieferte gegen die
// Produktion alle Objekte — die Route kennt limit nicht und ignoriert es
// stillschweigend. Ein Beispiel, das nichts tut, ist schlimmer als keins.
func TestNoExampleUsesUnsupportedParameters(t *testing.T) {
	for _, spec := range Registry {
		if !strings.Contains(spec.Example, "-q ") {
			continue
		}
		if spec.Special == SpecialAPI {
			// Der Escape-Hatch ruft beliebige Pfade auf — welche Parameter
			// die auswerten, weiß die Registry naturgemäß nicht.
			continue
		}
		declared := map[string]bool{}
		for _, hint := range spec.QueryHints {
			declared[hint.Name] = true
		}
		for _, part := range strings.Split(spec.Example, "-q ")[1:] {
			key, _, found := strings.Cut(strings.Fields(part)[0], "=")
			if !found {
				continue
			}
			if !declared[key] {
				t.Errorf("%q: Beispiel nutzt -q %s=…, das der Befehl nicht als QueryHint führt",
					spec.Name(), key)
			}
		}
	}
}

// TestQueryHintsAreVisibleEverywhere: Ein Parameter, der nur im Code steht,
// existiert für einen Agenten nicht.
func TestQueryHintsAreVisibleEverywhere(t *testing.T) {
	h := newHarness(t)

	_, help, _ := h.run("immobilien", "list", "--help")
	for _, want := range []string{"Bekannte Query-Parameter", "slim", "per_page"} {
		if !strings.Contains(help, want) {
			t.Errorf("--help soll %q zeigen:\n%s", want, help)
		}
	}

	_, docs, _ := h.run("docs", "immobilien", "list")
	if !strings.Contains(docs, "slim") {
		t.Errorf("die Referenz soll slim nennen:\n%s", docs)
	}

	_, raw, _ := h.run("schema", "immobilien", "list")
	var doc struct {
		Commands []struct {
			QueryHints []struct {
				Name    string `json:"name"`
				Summary string `json:"summary"`
			} `json:"query_hints"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("JSON erwartet: %v", err)
	}
	if len(doc.Commands) != 1 || len(doc.Commands[0].QueryHints) == 0 {
		t.Fatalf("query_hints im Schema erwartet, got %s", raw)
	}
	found := false
	for _, hint := range doc.Commands[0].QueryHints {
		if hint.Name == "slim" && hint.Summary != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("slim mit Beschreibung erwartet, got %s", raw)
	}
}

// TestFlagKindsAreConsistent: Der Argument-Parser entscheidet anhand des
// Flag-Namens, ob ein Wert folgt — derselbe Name darf deshalb nicht in einem
// Befehl bool und in einem anderen string sein.
func TestFlagKindsAreConsistent(t *testing.T) {
	kinds := map[string]FlagKind{}
	for _, spec := range Registry {
		for _, flag := range spec.Flags {
			if existing, ok := kinds[flag.Name]; ok && existing != flag.Kind {
				t.Errorf("Flag --%s ist mal %q und mal %q", flag.Name, existing, flag.Kind)
			}
			kinds[flag.Name] = flag.Kind
		}
	}
	for _, flag := range GlobalFlags {
		if existing, ok := kinds[flag.Name]; ok && existing != flag.Kind {
			t.Errorf("Befehls-Flag --%s kollidiert mit dem globalen Flag gleichen Namens", flag.Name)
		}
	}
}

// TestEveryResourceIsDocumented stellt sicher, dass jede Ressource in der
// Übersicht (immojump --help) auftaucht.
func TestEveryResourceIsDocumented(t *testing.T) {
	documented := map[string]bool{}
	for _, res := range Resources {
		if strings.TrimSpace(res.Summary) == "" {
			t.Errorf("Ressource %q ohne Summary", res.Name)
		}
		documented[res.Name] = true
	}
	for _, spec := range Registry {
		if !documented[spec.Resource] {
			t.Errorf("Ressource %q fehlt in Resources (taucht nicht in --help auf)", spec.Resource)
		}
	}
	for _, res := range Resources {
		found := false
		for _, spec := range Registry {
			if spec.Resource == res.Name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Ressource %q hat keinen einzigen Befehl", res.Name)
		}
	}
}

// TestRegistryCoversDesignScope hält den in DESIGN.md zugesagten Befehlsumfang fest.
func TestRegistryCoversDesignScope(t *testing.T) {
	want := map[string][]string{
		"auth":       {"login", "status"},
		"context":    {"list", "current", "use", "delete"},
		"contacts":   {"list", "get", "create", "update", "set-status", "delete", "activities", "immobilien"},
		"immobilien": {"list", "search", "get", "create", "update", "patch", "delete", "contacts", "duplicate"},
		"units":      {"list", "create", "update", "delete"},
		"activities": {"list", "get", "for-immobilie", "create", "update", "delete"},
		"pipelines":  {"list", "create", "get", "update", "delete", "statuses", "add-status", "export", "import"},
		"statuses":   {"list", "update", "delete", "swap", "aliases", "add-alias"},
		"templates":  {"list", "recurring", "by-status", "get", "create", "update", "delete", "batch-move"},
		"documents":  {"list", "upload", "rename", "delete", "analyze", "analyze-details", "mark-reviewed", "analysis-results"},
		"tags":       {"list", "create", "update", "delete", "of", "set"},
		"shares":     {"list", "create", "update", "revoke"},
		"feed": {"list", "by-context", "comments", "channels", "attachments", "mentions",
			"post", "comment", "comment-object", "react", "seen", "edit", "comment-edit",
			"comment-delete", "channel-create", "channel-rename", "channel-delete"},
		"notifications": {"list", "read-all"},
		"email": {"list", "get", "thread", "search", "folders", "for-immobilie", "for-contact",
			"outbox", "outbox-stats", "accounts", "signatures",
			"mark-read", "mark-starred", "archive", "trash", "move", "sync", "outbox-retry",
			"folder-create", "folder-rename", "folder-delete", "send"},
		"api":     {""},
		"docs":    {""},
		"schema":  {""},
		"version": {""},
	}
	for resource, verbs := range want {
		for _, verb := range verbs {
			if _, ok := Lookup(resource, verb); !ok {
				t.Errorf("Befehl %q %q fehlt in der Registry", resource, verb)
			}
		}
	}
}

// TestSharesRevokeIsNotDestructive hält die bewusste Design-Entscheidung fest:
// Der Widerruf muss auch für vorsichtig konfigurierte Agenten erreichbar sein.
func TestSharesRevokeIsNotDestructive(t *testing.T) {
	spec, ok := Lookup("shares", "revoke")
	if !ok {
		t.Fatal("shares revoke fehlt")
	}
	if spec.Risk != RiskWrite {
		t.Errorf("shares revoke soll write sein (sicherer Ausweg), ist %q", spec.Risk)
	}
	create, _ := Lookup("shares", "create")
	if create.Risk != RiskExternal {
		t.Errorf("shares create soll external sein (Link nach außen), ist %q", create.Risk)
	}
	// update kann abgelaufene Links reaktivieren und Passwörter entfernen —
	// beides wirkt nach außen, genau wie das Erzeugen.
	update, _ := Lookup("shares", "update")
	if update.Risk != RiskExternal {
		t.Errorf("shares update soll external sein (reaktiviert/entsperrt Links), ist %q", update.Risk)
	}
}

// TestBodyAndQueryFlagsAreDeclared: Ein Tippfehler in Body[].Flag oder
// Query[].Flag hat bisher nichts gemeldet — das Flag wurde einfach ignoriert.
func TestBodyAndQueryFlagsAreDeclared(t *testing.T) {
	for _, spec := range Registry {
		declared := map[string]bool{}
		for _, flag := range spec.Flags {
			declared[flag.Name] = true
		}
		for _, mapping := range spec.Body {
			if !declared[mapping.Flag] {
				t.Errorf("%q: Body-Mapping zeigt auf --%s, das der Befehl nicht kennt",
					spec.Name(), mapping.Flag)
			}
			if strings.TrimSpace(mapping.Key) == "" {
				t.Errorf("%q: Body-Mapping für --%s ohne Key", spec.Name(), mapping.Flag)
			}
		}
		for _, mapping := range spec.Query {
			if !declared[mapping.Flag] {
				t.Errorf("%q: Query-Mapping zeigt auf --%s, das der Befehl nicht kennt",
					spec.Name(), mapping.Flag)
			}
			if strings.TrimSpace(mapping.Key) == "" {
				t.Errorf("%q: Query-Mapping für --%s ohne Key", spec.Name(), mapping.Flag)
			}
		}
	}
}

// TestNullMappingsNeedBoolFlags: {Null: true} schickt `null` — das ergibt nur
// als Schalter Sinn.
func TestNullMappingsNeedBoolFlags(t *testing.T) {
	for _, spec := range Registry {
		for _, mapping := range spec.Body {
			if !mapping.Null {
				continue
			}
			flag, ok := findFlag(spec, mapping.Flag)
			if !ok {
				continue // von TestBodyAndQueryFlagsAreDeclared abgedeckt
			}
			if flag.Kind != FlagBool {
				t.Errorf("%q: --%s schickt null, muss deshalb bool sein, ist %q",
					spec.Name(), mapping.Flag, flag.Kind)
			}
		}
	}
}

// TestEverySpecialHasABodyHint: Wer --set/--body ablehnt, muss in Hilfe und
// Referenz erklären, woraus der Body sonst entsteht. Der generische Hinweis
// wäre dort schlicht falsch.
func TestEverySpecialHasABodyHint(t *testing.T) {
	for _, spec := range Registry {
		if spec.Special == "" || spec.Local || spec.Special == SpecialAPI {
			continue
		}
		if !methodExpectsBody(spec.Method) {
			continue
		}
		hint := bodyHint(spec)
		if strings.TrimSpace(hint) == "" {
			t.Errorf("%q: Special %q ohne Body-Hinweis in Hilfe und Referenz",
				spec.Name(), spec.Special)
			continue
		}
		if rejectsBodyFlags(spec) && hint == defaultBodyHint {
			t.Errorf("%q: lehnt --set/--body ab, zeigt aber den generischen Body-Hinweis",
				spec.Name())
		}
	}
}

// TestUnsupportedGlobalsOnlyNameGlobalFlags: Die Ablehnliste zeigt auf globale
// Flags — ein Tippfehler dort würde stumm nichts ablehnen.
func TestUnsupportedGlobalsOnlyNameGlobalFlags(t *testing.T) {
	global := map[string]bool{}
	for _, flag := range GlobalFlags {
		global[flag.Name] = true
	}
	specials := map[string]bool{}
	for _, spec := range Registry {
		if spec.Special != "" {
			specials[spec.Special] = true
		}
	}
	for special, names := range unsupportedGlobals {
		if !specials[special] {
			t.Errorf("unsupportedGlobals nennt %q — diesen Special gibt es nicht mehr", special)
		}
		for _, name := range names {
			if !global[name] {
				t.Errorf("unsupportedGlobals[%q] nennt --%s, das kein globales Flag ist", special, name)
			}
		}
	}
}

// TestSharesUpdateHasNoSpecial: Die Sent-Keys-Semantik trägt inzwischen die
// deklarative Body-Tabelle — ein Sonderfall im Dispatcher wäre Ballast.
func TestSharesUpdateHasNoSpecial(t *testing.T) {
	spec, _ := Lookup("shares", "update")
	if spec.Special != "" {
		t.Errorf("shares update soll ohne Special auskommen, hat %q", spec.Special)
	}
}

// TestAPISchemaReportsDynamicRisk: schema ist die Quelle für Tooling — dort
// darf der Escape-Hatch nicht als "read" auftauchen.
func TestAPISchemaReportsDynamicRisk(t *testing.T) {
	h := newHarness(t)
	code, stdout, stderr := h.run("schema", "api")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	var doc struct {
		Commands []struct {
			Risk     string `json:"risk"`
			RiskRule string `json:"risk_rule"`
		} `json:"commands"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("JSON erwartet: %v", err)
	}
	if len(doc.Commands) != 1 {
		t.Fatalf("genau einen api-Befehl erwartet, got %d", len(doc.Commands))
	}
	if doc.Commands[0].Risk != "dynamic" {
		t.Errorf(`risk "dynamic" erwartet, got %q`, doc.Commands[0].Risk)
	}
	if doc.Commands[0].RiskRule == "" {
		t.Error("risk_rule mit der Auflösungsregel erwartet")
	}
}

func TestAPIHelpAndDocsShowDynamicRisk(t *testing.T) {
	h := newHarness(t)
	_, help, _ := h.run("api", "--help")
	if !strings.Contains(help, "dynamic") {
		t.Errorf("api --help soll das dynamische Risk zeigen:\n%s", help)
	}
	if strings.Contains(help, "Risk:     read") {
		t.Errorf("api --help darf kein statisches read behaupten:\n%s", help)
	}

	_, docs, _ := h.run("docs")
	if !strings.Contains(docs, "dynamic") {
		t.Error("REFERENCE.md soll das dynamische Risk des Escape-Hatch nennen")
	}
}
