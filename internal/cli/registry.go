package cli

import (
	"net/http"
	"strings"
)

// Risk beschreibt die Konsequenz eines Befehls — sichtbar in --help, docs und
// schema, damit Agenten (und Menschen) einschätzen können, was passiert.
type Risk string

const (
	RiskRead        Risk = "read"
	RiskWrite       Risk = "write"
	RiskExternal    Risk = "external"
	RiskDestructive Risk = "destructive"
)

// riskRank ordnet die Level für den Fall, dass mehrere Registry-Einträge auf
// denselben Pfad passen: Dann gewinnt das strengere — eine Policy ist ein
// Geländer, das lieber einmal zu viel greift.
var riskRank = map[Risk]int{RiskRead: 0, RiskWrite: 1, RiskExternal: 2, RiskDestructive: 3}

// FlagKind bestimmt, ob nach dem Flag ein Wert folgt, und wie er im Body
// landet (String, JSON-Zahl oder Boolean).
type FlagKind string

const (
	FlagString FlagKind = "string"
	FlagBool   FlagKind = "bool"
	FlagList   FlagKind = "list"   // wiederholbar
	FlagNumber FlagKind = "number" // landet als JSON-Zahl im Body
)

// Norm markiert Werte, die vor dem Senden normalisiert werden.
const NormDateTime = "datetime"

// Special markiert Befehle, die der Dispatcher gesondert behandelt.
const (
	SpecialAPI             = "api"
	SpecialAuthLogin       = "auth-login"
	SpecialAuthStatus      = "auth-status"
	SpecialContextList     = "context-list"
	SpecialContextCurrent  = "context-current"
	SpecialContextUse      = "context-use"
	SpecialContextDelete   = "context-delete"
	SpecialDocs            = "docs"
	SpecialSchema          = "schema"
	SpecialVersion         = "version"
	SpecialSharesCreate    = "shares-create"
	SpecialTagsSet         = "tags-set"
	SpecialDocumentsUpload = "documents-upload"
	SpecialPipelineImport  = "pipelines-import"
)

// APIRiskRule erklärt, wie der Escape-Hatch sein Risk-Level bekommt. Der Text
// steht in --help, in der Referenz und im Schema — damit sich niemand auf ein
// statisches Level verlässt, das es nicht gibt.
const APIRiskRule = "Risk kommt aus dem passenden Registry-Befehl (Methode + Pfad); " +
	"ohne Treffer konservativ nach Methode: GET/HEAD = read, DELETE = destructive, alles andere = external."

// Arg ist ein positionales Argument.
type Arg struct {
	Name     string
	Desc     string
	Optional bool
}

// Flag ist ein benanntes Argument.
type Flag struct {
	Name     string
	Kind     FlagKind
	Desc     string
	Required bool
	// Norm normalisiert den Wert vor dem Senden (siehe NormDateTime).
	Norm string
	// NonEmpty lehnt einen leeren Wert ab und nennt die gemeinte Alternative.
	NonEmpty string
}

// FlagBody bildet ein Flag auf einen Body-Pfad ab (dotted, z. B. settings.x).
// Null macht aus einem Schalter ein `"key": null` — so löscht das Backend das
// Feld, statt es unverändert zu lassen.
type FlagBody struct {
	Flag string
	Key  string
	Null bool
}

// FlagQuery bildet ein Flag auf einen Query-Parameter ab.
type FlagQuery struct {
	Flag string
	Key  string
}

// QueryHint nennt einen Query-Parameter, den die Route tatsächlich auswertet
// — nachgelesen in modules/routes/, nicht geraten.
//
// Das ist der Unterschied zwischen 125.598 und 3.002 Zeichen für dieselbe
// Immobilienliste: `slim=true` plus `--fields id,name`. Ein Parameter, der
// nur im Backend steht, existiert für einen Agenten nicht — deshalb steht er
// deklarativ in der Spec und erscheint in --help, docs und schema.
//
// Was die Route NICHT liest, gehört hier nicht hin: `-q limit=3` lieferte
// gegen die Produktion alle Objekte, weil die Route limit stillschweigend
// ignoriert.
type QueryHint struct {
	Name    string
	Summary string
}

// Spec ist ein Befehl — ein Datensatz, kein Code. Aus dieser Tabelle
// entstehen Dispatch, Hilfe, REFERENCE.md und das JSON-Schema.
type Spec struct {
	Resource string
	Verb     string
	Summary  string
	Method   string
	Path     string
	Args     []Arg
	Flags    []Flag
	Body     []FlagBody
	Query    []FlagQuery
	// QueryHints sind die Parameter, die diese Route auswertet — sichtbar in
	// Hilfe, Referenz und Schema, benutzbar über -q key=value.
	QueryHints []QueryHint
	Risk       Risk
	Example    string
	Special    string
	// Local: kein HTTP-Aufruf (reine Konfigurationsbefehle, docs/schema/version).
	Local bool
	// Raw: Die Antwort ist möglicherweise kein JSON (z. B. YAML-Export).
	Raw bool
	// EmptyBodyHint: Ein leerer Body ergibt für diesen Befehl keinen Sinn.
	// Der Text sagt, was stattdessen zu setzen ist — statt eines 400 nach
	// einem unnötigen Roundtrip.
	EmptyBodyHint string
}

// Name ist "resource verb" bzw. nur "resource" bei Befehlen ohne Verb.
func (s Spec) Name() string {
	if s.Verb == "" {
		return s.Resource
	}
	return s.Resource + " " + s.Verb
}

// Usage ist die Aufrufzeile für Hilfe und Referenz.
func (s Spec) Usage() string {
	parts := []string{"immojump", s.Resource}
	if s.Verb != "" {
		parts = append(parts, s.Verb)
	}
	for _, arg := range s.Args {
		if arg.Optional {
			parts = append(parts, "["+arg.Name+"]")
		} else {
			parts = append(parts, "<"+arg.Name+">")
		}
	}
	return strings.Join(parts, " ")
}

// Endpoint ist die Kurzform "METHOD /pfad" für Hilfe und Referenz.
func (s Spec) Endpoint() string {
	switch {
	case s.Local:
		return "lokal"
	case s.Special == SpecialAPI:
		return "<METHOD> <pfad>"
	default:
		return s.Method + " " + s.Path
	}
}

// RiskLabel ist das Risk für Anzeige und Schema. Der Escape-Hatch trägt keins:
// sein Level entsteht erst aus Methode und Pfad (siehe APIRiskRule).
func (s Spec) RiskLabel() string {
	if s.Special == SpecialAPI {
		return "dynamic"
	}
	return string(s.Risk)
}

// RiskRule erklärt ein dynamisches Risk — leer bei allen festen Levels.
func (s Spec) RiskRule() string {
	if s.Special == SpecialAPI {
		return APIRiskRule
	}
	return ""
}

// defaultBodyHint gilt für alle Befehle, die den generischen Body-Bau nutzen.
const defaultBodyHint = "`--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar)."

// specialBodyHints erklärt je Sonderfall, woraus der Body entsteht. Hilfe und
// Referenz lesen beide hier — nicht aus einem switch im Renderer.
var specialBodyHints = map[string]string{
	SpecialDocumentsUpload: "Multipart-Upload im Feld `files[]`, dazu `organisation_id` aus der Konfiguration.",
	SpecialPipelineImport:  "YAML aus `--file` oder stdin (Content-Type `application/x-yaml`), Organisation als Query-Parameter.",
	SpecialTagsSet:         "rohes JSON-Array der Tag-IDs, gebaut aus `--tag-ids`.",
}

// bodyHint erklärt, wie der Body gefüllt wird — ohne Präfix, damit Hilfe und
// Referenz ihn jeweils passend einbetten können.
func bodyHint(spec Spec) string {
	if spec.Local || spec.Special == SpecialAPI || !methodExpectsBody(spec.Method) {
		return ""
	}
	if hint, ok := specialBodyHints[spec.Special]; ok {
		return hint
	}
	return defaultBodyHint
}

// matchPathTemplate prüft einen konkreten Pfad gegen ein Registry-Template.
// Ein {platzhalter} deckt genau ein Segment ab — auch {org}.
func matchPathTemplate(template, path string) bool {
	want := strings.Split(strings.Trim(template, "/"), "/")
	got := strings.Split(strings.Trim(path, "/"), "/")
	if len(want) != len(got) {
		return false
	}
	for i, segment := range want {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			if got[i] == "" {
				return false
			}
			continue
		}
		if segment != got[i] {
			return false
		}
	}
	return true
}

// RiskForRequest liefert das Risk-Level, das die Registry für Methode und Pfad
// vorsieht. Genau das macht `api <METHOD> <pfad>` ehrlich: Wer einen
// kuratierten Endpoint über den Escape-Hatch aufruft, bekommt dessen Level und
// kann die Policy nicht umgehen.
func RiskForRequest(method, path string) (Risk, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	var best Risk
	found := false
	for _, spec := range Registry {
		if spec.Local || spec.Special == SpecialAPI || spec.Method == "" {
			continue
		}
		if !strings.EqualFold(spec.Method, method) || !matchPathTemplate(spec.Path, path) {
			continue
		}
		if !found || riskRank[spec.Risk] > riskRank[best] {
			best, found = spec.Risk, true
		}
	}
	return best, found
}

// riskForAPI stuft einen Escape-Hatch-Aufruf ein: erst die Registry, sonst
// konservativ nach Methode (siehe APIRiskRule).
func riskForAPI(method, path string) Risk {
	if risk, ok := RiskForRequest(method, path); ok {
		return risk
	}
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead:
		return RiskRead
	case http.MethodDelete:
		return RiskDestructive
	default:
		// Unkuratiert lässt sich "external" nicht erkennen — also annehmen.
		return RiskExternal
	}
}

// ResourceInfo beschreibt eine Ressource in der Übersicht.
type ResourceInfo struct {
	Name    string
	Summary string
}

// Resources bestimmt die Reihenfolge in --help und REFERENCE.md.
var Resources = []ResourceInfo{
	{"auth", "Anmelden und die aufgelöste Konfiguration prüfen"},
	{"context", "Instanzen und Organisationen verwalten (wie kubectl)"},
	{"contacts", "Kontakte"},
	{"immobilien", "Immobilien"},
	{"units", "Einheiten einer Immobilie"},
	{"activities", "Aktivitäten und Aufgaben"},
	{"pipelines", "Pipelines und ihre Phasen"},
	{"statuses", "Status (Phasen) einzeln bearbeiten"},
	{"templates", "Aktivitäts-Vorlagen"},
	{"documents", "Dokumente hochladen, analysieren, verwalten"},
	{"tags", "Tags und ihre Zuordnung zu Objekten"},
	{"shares", "Freigabe-Links für Immobilien, Dokumente und Bilder"},
	{"email", "Postfach: Nachrichten lesen, sortieren und versenden"},
	{"api", "Beliebigen /api/-Pfad aufrufen (Escape-Hatch)"},
	{"docs", "Markdown-Referenz ausgeben — komplett oder für eine Ressource/einen Befehl"},
	{"schema", "Befehls-Schema als JSON ausgeben — komplett oder als Ausschnitt"},
	{"version", "Version ausgeben"},
}

// GlobalFlags gelten für jeden Befehl — vor oder nach dem Befehl notierbar.
var GlobalFlags = []Flag{
	{Name: "context", Kind: FlagString, Desc: "Context aus der Konfiguration wählen"},
	{Name: "org", Kind: FlagString, Desc: "Organisation überschreiben"},
	{Name: "base-url", Kind: FlagString, Desc: "Instanz überschreiben (muss auf der Allowlist stehen)"},
	{Name: "q", Kind: FlagList, Desc: "Query-Parameter key=value (wiederholbar)"},
	{Name: "set", Kind: FlagList, Desc: "Body-Feld pfad=wert (wiederholbar, Wert als JSON-Literal sonst String)"},
	{Name: "body", Kind: FlagString, Desc: "Kompletter Body: JSON, @datei oder - für stdin"},
	{Name: "fields", Kind: FlagString, Desc: "Ausgabe auf Felder projizieren, z. B. id,adresse.stadt"},
	{Name: "pretty", Kind: FlagBool, Desc: "Ausgabe einrücken (für Menschen)"},
	{Name: "readonly", Kind: FlagBool, Desc: "Nur lesende Befehle zulassen"},
	{Name: "allow", Kind: FlagString, Desc: "Erlaubte Risk-Level, z. B. read,write (Env: IMMOJUMP_ALLOW)"},
	{Name: "idempotency-key", Kind: FlagString, Desc: "Wird als Idempotency-Key-Header mitgeschickt"},
	{Name: "timeout", Kind: FlagString, Desc: "Timeout in Sekunden (Default 60)"},
	{Name: "verbose", Kind: FlagBool, Desc: "Methode und aufgerufene URL vor dem Request auf stderr zeigen"},
	{Name: "version", Kind: FlagBool, Desc: "Version ausgeben"},
	{Name: "help", Kind: FlagBool, Desc: "Hilfe zur jeweiligen Ebene ausgeben"},
	{Name: "h", Kind: FlagBool, Desc: "Kurzform von --help"},
}

// fullUserFlagDesc erklärt --full bei auth login und auth status. Beide
// Befehle antworten sonst kompakt: Das vollständige Nutzerobjekt ist als
// Anmeldebestätigung reine Kontextverschwendung.
const fullUserFlagDesc = "Vollständige Antwort von /api/user/me ausgeben statt id/username plus Rolle"

// idArg ist die häufigste Argument-Definition.
func idArg(desc string) []Arg { return []Arg{{Name: "id", Desc: desc}} }

// Registry ist die vollständige Befehlstabelle.
var Registry = []Spec{
	// --- auth -------------------------------------------------------------
	{
		Resource: "auth", Verb: "login", Risk: RiskRead,
		Summary: "Context anlegen oder aktualisieren und gegen die Instanz prüfen",
		Method:  "GET", Path: "/api/user/me", Special: SpecialAuthLogin,
		Flags: []Flag{
			{Name: "token", Kind: FlagString, Desc: "API-Token (Einstellungen → API-Zugang)"},
			{Name: "token-env", Kind: FlagString, Desc: "Name der Env-Variablen mit dem Token (statt Klartext)"},
			{Name: "organisation", Kind: FlagString, Desc: "Organisations-ID für diesen Context"},
			{Name: "full", Kind: FlagBool, Desc: fullUserFlagDesc},
		},
		Example: "immojump auth login --context prod --base-url https://immojump.de --organisation <org-id> --token <token>",
	},
	{
		Resource: "auth", Verb: "status", Risk: RiskRead,
		Summary: "Aufgelöste Konfiguration zeigen und gegen /api/user/me prüfen",
		Method:  "GET", Path: "/api/user/me", Special: SpecialAuthStatus,
		Flags: []Flag{
			{Name: "full", Kind: FlagBool, Desc: fullUserFlagDesc},
		},
		Example: "immojump auth status",
	},

	// --- context (rein lokal) ---------------------------------------------
	{
		Resource: "context", Verb: "list", Risk: RiskRead, Local: true, Special: SpecialContextList,
		Summary: "Alle konfigurierten Contexts auflisten",
		Example: "immojump context list",
	},
	{
		Resource: "context", Verb: "current", Risk: RiskRead, Local: true, Special: SpecialContextCurrent,
		Summary: "Aktiven Context zeigen",
		Example: "immojump context current",
	},
	{
		Resource: "context", Verb: "use", Risk: RiskRead, Local: true, Special: SpecialContextUse,
		Summary: "Aktiven Context wechseln",
		Args:    []Arg{{Name: "name", Desc: "Name des Contexts"}},
		Example: "immojump context use beta",
	},
	{
		Resource: "context", Verb: "delete", Risk: RiskWrite, Local: true, Special: SpecialContextDelete,
		Summary: "Context aus der lokalen Konfiguration entfernen",
		Args:    []Arg{{Name: "name", Desc: "Name des Contexts"}},
		Example: "immojump context delete beta",
	},

	// --- contacts ---------------------------------------------------------
	{
		Resource: "contacts", Verb: "list", Risk: RiskRead,
		Summary: "Kontakte auflisten", Method: "GET", Path: "/api/contacts",
		QueryHints: []QueryHint{
			{"slim", "true = ohne die Aktivitäten jedes Kontakts (deutlich kleiner und schneller)"},
			{"q", "Freitext über Name, E-Mail, Telefon, Firma, Rolle, Adresse"},
			{"page", "Seite (ab 1); erst damit kommt ein Envelope statt aller Treffer"},
			{"per_page", "Treffer pro Seite (Default 50, max. 200)"},
			{"sort", "last_name, first_name, email, created_at, updated_at, last_activity_at …"},
			{"order", "asc (Default) oder desc"},
			{"status_id", "nur diese Phase; none = Kontakte ohne Status"},
			{"tag_ids", "Tag-IDs, kommagetrennt oder wiederholt"},
			{"tag_match", "all (Default, alle Tags) oder any (mindestens einer)"},
		},
		Example: "immojump contacts list -q slim=true -q per_page=25 --fields items.id,items.first_name,items.last_name",
	},
	{
		Resource: "contacts", Verb: "get", Risk: RiskRead,
		Summary: "Einen Kontakt laden", Method: "GET", Path: "/api/contacts/{id}",
		Args:    idArg("ID des Kontakts"),
		Example: "immojump contacts get 42",
	},
	{
		Resource: "contacts", Verb: "create", Risk: RiskWrite,
		Summary: "Kontakt anlegen", Method: "POST", Path: "/api/contacts",
		Example: "immojump contacts create --set first_name=Ada --set last_name=Lovelace",
	},
	{
		Resource: "contacts", Verb: "update", Risk: RiskWrite,
		Summary: "Kontakt ändern", Method: "PUT", Path: "/api/contacts/{id}",
		Args:    idArg("ID des Kontakts"),
		Example: "immojump contacts update 42 --set email=ada@example.com",
	},
	{
		Resource: "contacts", Verb: "set-status", Risk: RiskWrite,
		Summary: "Kontakt in eine andere Phase schieben", Method: "PUT", Path: "/api/contacts/{id}/status",
		Args:    idArg("ID des Kontakts"),
		Example: "immojump contacts set-status 42 --set status_id=7",
	},
	{
		Resource: "contacts", Verb: "delete", Risk: RiskDestructive,
		Summary: "Kontakt löschen", Method: "DELETE", Path: "/api/contacts/{id}",
		Args:    idArg("ID des Kontakts"),
		Example: "immojump contacts delete 42",
	},
	{
		Resource: "contacts", Verb: "activities", Risk: RiskRead,
		Summary: "Aktivitäten eines Kontakts", Method: "GET", Path: "/api/contacts/{id}/activities",
		Args:    idArg("ID des Kontakts"),
		Example: "immojump contacts activities 42",
	},
	{
		Resource: "contacts", Verb: "immobilien", Risk: RiskRead,
		Summary: "Immobilien eines Kontakts", Method: "GET", Path: "/api/contacts/{id}/immobilien",
		Args:    idArg("ID des Kontakts"),
		Example: "immojump contacts immobilien 42",
	},

	// --- immobilien -------------------------------------------------------
	{
		Resource: "immobilien", Verb: "list", Risk: RiskRead,
		Summary: "Immobilien auflisten", Method: "GET", Path: "/api/v2/immobilien",
		QueryHints: []QueryHint{
			{"slim", "true = reduziertes Feldset; die stärkste Ersparnis überhaupt (wirkt nur ohne page)"},
			{"page", "Seite (ab 1); liefert einen Envelope {items, pagination} — dann ohne slim"},
			{"per_page", "Treffer pro Seite (Default 20)"},
			{"sort", "created_at (Default), name, kaufpreis, wohnflaeche oder preis_pro_qm"},
			{"order", "desc (Default) oder asc"},
		},
		Example: "immojump immobilien list -q slim=true --fields id,name",
	},
	{
		Resource: "immobilien", Verb: "search", Risk: RiskRead,
		Summary: "Immobilien suchen", Method: "GET", Path: "/api/v2/immobilien/search",
		QueryHints: []QueryHint{
			{"search", "Suchbegriff über Name und Adresse (heißt hier search, nicht q)"},
			{"tag_ids", "Tag-IDs, wiederholt angeben (?tag_ids=a&tag_ids=b); ODER-verknüpft"},
			{"status_ids", "Phasen-IDs, wiederholt angeben; ODER-verknüpft"},
			{"page", "Seite (Default 1); die Antwort ist immer {items, pagination}"},
			{"per_page", "Treffer pro Seite (Default 20)"},
			{"sort", "created_at (Default), name, kaufpreis, wohnflaeche oder preis_pro_qm"},
			{"order", "desc (Default) oder asc"},
		},
		Example: "immojump immobilien search -q search=Köln --fields items.id,items.name",
	},
	{
		Resource: "immobilien", Verb: "get", Risk: RiskRead,
		Summary: "Eine Immobilie laden", Method: "GET", Path: "/api/v2/immobilien/{id}",
		Args:    idArg("ID der Immobilie"),
		Example: "immojump immobilien get 5",
	},
	{
		Resource: "immobilien", Verb: "create", Risk: RiskWrite,
		Summary: "Immobilie anlegen", Method: "POST", Path: "/api/v2/immobilien",
		Example: `immojump immobilien create --set name='MFH Köln' --set type=MFH`,
	},
	{
		Resource: "immobilien", Verb: "update", Risk: RiskWrite,
		Summary: "Immobilie vollständig ersetzen", Method: "PUT", Path: "/api/v2/immobilien/{id}",
		Args:    idArg("ID der Immobilie"),
		Example: "immojump immobilien update 5 --body @immobilie.json",
	},
	{
		Resource: "immobilien", Verb: "patch", Risk: RiskWrite,
		Summary: "Einzelne Felder einer Immobilie ändern", Method: "PATCH", Path: "/api/v2/immobilien/{id}",
		Args:    idArg("ID der Immobilie"),
		Example: "immojump immobilien patch 5 --set kaufpreis=239000",
	},
	{
		Resource: "immobilien", Verb: "delete", Risk: RiskDestructive,
		Summary: "Immobilie löschen", Method: "DELETE", Path: "/api/v2/immobilien/{id}",
		Args:    idArg("ID der Immobilie"),
		Example: "immojump immobilien delete 5",
	},
	{
		Resource: "immobilien", Verb: "contacts", Risk: RiskRead,
		Summary: "Kontakte zu einer Immobilie", Method: "GET", Path: "/api/v2/immobilien/{id}/contacts",
		Args:    idArg("ID der Immobilie"),
		Example: "immojump immobilien contacts 5",
	},
	{
		Resource: "immobilien", Verb: "duplicate", Risk: RiskWrite,
		Summary: "Immobilie duplizieren", Method: "POST", Path: "/api/v2/immobilien/{id}/duplicate",
		Args:    idArg("ID der Immobilie"),
		Example: "immojump immobilien duplicate 5",
	},

	// --- units ------------------------------------------------------------
	{
		Resource: "units", Verb: "list", Risk: RiskRead,
		Summary: "Einheiten einer Immobilie auflisten",
		Method:  "GET", Path: "/api/units/immobilie/{immobilie-id}/units",
		Args:    []Arg{{Name: "immobilie-id", Desc: "ID der Immobilie"}},
		Example: "immojump units list 5",
	},
	{
		Resource: "units", Verb: "create", Risk: RiskWrite,
		Summary: "Einheit zu einer Immobilie anlegen",
		Method:  "POST", Path: "/api/units/unit/{immobilie-id}",
		Args:    []Arg{{Name: "immobilie-id", Desc: "ID der Immobilie"}},
		Example: "immojump units create 5 --set einheit='WE 1'",
	},
	{
		Resource: "units", Verb: "update", Risk: RiskWrite,
		Summary: "Einheit ändern", Method: "PUT", Path: "/api/units/unit/{unit-id}",
		Args:    []Arg{{Name: "unit-id", Desc: "ID der Einheit"}},
		Example: "immojump units update 9 --set ist_rent=780",
	},
	{
		Resource: "units", Verb: "delete", Risk: RiskDestructive,
		Summary: "Einheit löschen", Method: "DELETE", Path: "/api/units/unit/{unit-id}",
		Args:    []Arg{{Name: "unit-id", Desc: "ID der Einheit"}},
		Example: "immojump units delete 9",
	},

	// --- activities -------------------------------------------------------
	{
		Resource: "activities", Verb: "list", Risk: RiskRead,
		Summary: "Aktivitäten auflisten", Method: "GET", Path: "/api/activities/activities",
		QueryHints: []QueryHint{
			{"q", "Freitext über Titel, Beschreibung, Typ, Status, Priorität"},
			{"type", "ANRUF, BESICHTIGUNG, BRIEF, E-MAIL, MEETING, NOTIZ, SONSTIGES (mehrere kommagetrennt)"},
			{"status", "Geplant, In Bearbeitung, Abgeschlossen, Abgebrochen (mehrere kommagetrennt)"},
			{"priority", "Hoch, Mittel, Niedrig, NA (mehrere kommagetrennt)"},
			{"immobilie", "nur Aktivitäten dieser Immobilie (der Parameter heißt immobilie)"},
			{"overdue", "true = offene Aktivitäten, deren Fälligkeit vorbei ist"},
			{"due", "today oder week — Fälligkeitsfenster, nur offene Aktivitäten"},
			{"page", "Seite (ab 1); erst damit kommt ein Envelope statt aller Treffer"},
			{"per_page", "Treffer pro Seite (Default 25, max. 200)"},
		},
		Example: "immojump activities list -q overdue=true --fields id,title,scheduled_end",
	},
	{
		Resource: "activities", Verb: "get", Risk: RiskRead,
		Summary: "Eine Aktivität laden", Method: "GET", Path: "/api/activities/activities/{id}",
		Args:    idArg("ID der Aktivität"),
		Example: "immojump activities get 3",
	},
	{
		Resource: "activities", Verb: "for-immobilie", Risk: RiskRead,
		Summary: "Aktivitäten zu einer Immobilie",
		Method:  "GET", Path: "/api/activities/activities/immobilie/{immobilie-id}",
		Args:    []Arg{{Name: "immobilie-id", Desc: "ID der Immobilie"}},
		Example: "immojump activities for-immobilie 5",
	},
	{
		Resource: "activities", Verb: "create", Risk: RiskWrite,
		Summary: "Aktivität anlegen", Method: "POST", Path: "/api/activities/activities",
		Example: "immojump activities create --set title='Makler anrufen' --set type=ANRUF --set immobilien_id=5",
	},
	{
		Resource: "activities", Verb: "update", Risk: RiskWrite,
		Summary: "Aktivität ändern", Method: "PUT", Path: "/api/activities/activities/{id}",
		Args:    idArg("ID der Aktivität"),
		Example: "immojump activities update 3 --set status=Abgeschlossen",
	},
	{
		Resource: "activities", Verb: "delete", Risk: RiskDestructive,
		Summary: "Aktivität löschen", Method: "DELETE", Path: "/api/activities/activities/{id}",
		Args:    idArg("ID der Aktivität"),
		Example: "immojump activities delete 3",
	},

	// --- pipelines --------------------------------------------------------
	{
		Resource: "pipelines", Verb: "list", Risk: RiskRead,
		Summary: "Pipelines der Organisation auflisten",
		Method:  "GET", Path: "/api/pipelines/{org}/pipelines",
		Example: "immojump pipelines list",
	},
	{
		Resource: "pipelines", Verb: "create", Risk: RiskWrite,
		Summary: "Pipeline anlegen", Method: "POST", Path: "/api/pipelines/{org}/pipelines",
		Example: "immojump pipelines create --set name=Ankauf --set entity_type=immobilie",
	},
	{
		Resource: "pipelines", Verb: "get", Risk: RiskRead,
		Summary: "Eine Pipeline laden", Method: "GET", Path: "/api/pipelines/pipelines/{id}",
		Args:    idArg("ID der Pipeline"),
		Example: "immojump pipelines get 2",
	},
	{
		Resource: "pipelines", Verb: "update", Risk: RiskWrite,
		Summary: "Pipeline ändern", Method: "PUT", Path: "/api/pipelines/pipelines/{id}",
		Args:    idArg("ID der Pipeline"),
		Example: "immojump pipelines update 2 --set name='Ankauf 2026'",
	},
	{
		Resource: "pipelines", Verb: "delete", Risk: RiskDestructive,
		Summary: "Pipeline löschen", Method: "DELETE", Path: "/api/pipelines/pipelines/{id}",
		Args:    idArg("ID der Pipeline"),
		Example: "immojump pipelines delete 2",
	},
	{
		Resource: "pipelines", Verb: "statuses", Risk: RiskRead,
		Summary: "Phasen einer Pipeline", Method: "GET", Path: "/api/pipelines/pipelines/{id}/statuses",
		Args:    idArg("ID der Pipeline"),
		Example: "immojump pipelines statuses 2",
	},
	{
		Resource: "pipelines", Verb: "add-status", Risk: RiskWrite,
		Summary: "Phase zu einer Pipeline hinzufügen",
		Method:  "POST", Path: "/api/pipelines/pipelines/{id}/statuses",
		Args:    idArg("ID der Pipeline"),
		Example: "immojump pipelines add-status 2 --set name='Besichtigung'",
	},
	{
		Resource: "pipelines", Verb: "export", Risk: RiskRead, Raw: true,
		Summary: "Pipeline als YAML exportieren", Method: "GET", Path: "/api/pipelines/pipelines/{id}/export",
		Args:    idArg("ID der Pipeline"),
		Example: "immojump pipelines export 2 > ankauf.yaml",
	},
	{
		Resource: "pipelines", Verb: "import", Risk: RiskWrite, Special: SpecialPipelineImport,
		Summary: "Pipeline aus YAML importieren", Method: "POST", Path: "/api/pipelines/pipelines/import",
		Flags: []Flag{
			{Name: "file", Kind: FlagString, Desc: "YAML-Datei; ohne Angabe wird stdin gelesen"},
		},
		Example: "immojump pipelines import --file ankauf.yaml",
	},

	// --- statuses ---------------------------------------------------------
	{
		Resource: "statuses", Verb: "list", Risk: RiskRead,
		Summary: "Alle Phasen auflisten", Method: "GET", Path: "/api/statuses/statuses",
		QueryHints: []QueryHint{
			{"lite", "true = ohne die Aktivitäts-Vorlagen jeder Phase (deutlich kleiner)"},
		},
		Example: "immojump statuses list -q lite=true --fields id,name",
	},
	{
		Resource: "statuses", Verb: "update", Risk: RiskWrite,
		Summary: "Phase ändern", Method: "PUT", Path: "/api/statuses/statuses/{id}",
		Args:    idArg("ID der Phase"),
		Example: "immojump statuses update 4 --set name='Geprüft'",
	},
	{
		Resource: "statuses", Verb: "delete", Risk: RiskDestructive,
		Summary: "Phase löschen", Method: "DELETE", Path: "/api/statuses/statuses/{id}",
		Args:    idArg("ID der Phase"),
		Example: "immojump statuses delete 4",
	},
	{
		// Die Route tauscht nicht selbst: Sie schreibt die beiden übergebenen
		// order-Werte zu. Ohne sie antwortet das Backend mit 400.
		Resource: "statuses", Verb: "swap", Risk: RiskWrite,
		Summary: "Reihenfolge zweier Phasen tauschen",
		Method:  "PUT", Path: "/api/statuses/statuses/swap/{current-id}/{target-id}",
		Args: []Arg{
			{Name: "current-id", Desc: "ID der Phase, die verschoben wird"},
			{Name: "target-id", Desc: "ID der Phase, mit der getauscht wird"},
		},
		Flags: []Flag{
			{Name: "current-order", Kind: FlagNumber, Required: true,
				Desc: "Neue Position der ersten Phase (Ganzzahl)"},
			{Name: "target-order", Kind: FlagNumber, Required: true,
				Desc: "Neue Position der zweiten Phase (Ganzzahl)"},
		},
		Body: []FlagBody{
			{Flag: "current-order", Key: "current_status_order"},
			{Flag: "target-order", Key: "target_status_order"},
		},
		Example: "immojump statuses swap 4 5 --current-order 1 --target-order 2",
	},
	{
		Resource: "statuses", Verb: "aliases", Risk: RiskRead,
		Summary: "E-Mail-Aliase einer Phase",
		Method:  "GET", Path: "/api/statuses/statuses/{status-id}/inbound-aliases",
		Args:    []Arg{{Name: "status-id", Desc: "ID der Phase"}},
		Example: "immojump statuses aliases 4",
	},
	{
		Resource: "statuses", Verb: "add-alias", Risk: RiskWrite,
		Summary: "E-Mail-Alias zu einer Phase hinzufügen",
		Method:  "POST", Path: "/api/statuses/statuses/{status-id}/inbound-aliases",
		Args:    []Arg{{Name: "status-id", Desc: "ID der Phase"}},
		Example: "immojump statuses add-alias 4 --set alias=ankauf",
	},

	// --- templates --------------------------------------------------------
	{
		Resource: "templates", Verb: "list", Risk: RiskRead,
		Summary: "Aktivitäts-Vorlagen auflisten",
		Method:  "GET", Path: "/api/activity-templates/activity_templates",
		Example: "immojump templates list",
	},
	{
		Resource: "templates", Verb: "recurring", Risk: RiskRead,
		Summary: "Wiederkehrende Vorlagen auflisten",
		Method:  "GET", Path: "/api/activity-templates/activity_templates/recurring",
		Example: "immojump templates recurring",
	},
	{
		Resource: "templates", Verb: "by-status", Risk: RiskRead,
		Summary: "Vorlagen einer Phase",
		Method:  "GET", Path: "/api/activity-templates/activity_templates/status/{status-id}",
		Args:    []Arg{{Name: "status-id", Desc: "ID der Phase"}},
		Example: "immojump templates by-status 4",
	},
	{
		Resource: "templates", Verb: "get", Risk: RiskRead,
		Summary: "Eine Vorlage laden",
		Method:  "GET", Path: "/api/activity-templates/activity_templates/{id}",
		Args:    idArg("ID der Vorlage"),
		Example: "immojump templates get 8",
	},
	{
		Resource: "templates", Verb: "create", Risk: RiskWrite,
		Summary: "Vorlage anlegen",
		Method:  "POST", Path: "/api/activity-templates/activity_templates",
		Example: "immojump templates create --set title='Exposé prüfen' --set type=SONSTIGES --set activity_status=Geplant --set priority=Mittel --set status_id=4",
	},
	{
		Resource: "templates", Verb: "update", Risk: RiskWrite,
		Summary: "Vorlage ändern",
		Method:  "PUT", Path: "/api/activity-templates/activity_templates/{id}",
		Args:    idArg("ID der Vorlage"),
		Example: "immojump templates update 8 --set title='Exposé geprüft'",
	},
	{
		Resource: "templates", Verb: "delete", Risk: RiskDestructive,
		Summary: "Vorlage löschen",
		Method:  "DELETE", Path: "/api/activity-templates/activity_templates/{id}",
		Args:    idArg("ID der Vorlage"),
		Example: "immojump templates delete 8",
	},
	{
		Resource: "templates", Verb: "batch-move", Risk: RiskWrite,
		Summary: "Vorlagen einer Phase gesammelt in eine andere Phase verschieben",
		Method:  "POST", Path: "/api/activity-templates/activity_templates/status/batch_move",
		Example: "immojump templates batch-move --set from_status_id=4 --set to_status_id=5",
	},

	// --- documents --------------------------------------------------------
	{
		Resource: "documents", Verb: "list", Risk: RiskRead,
		Summary: "Dokumente auflisten", Method: "GET", Path: "/api/documents/documents",
		QueryHints: []QueryHint{
			{"immobilien_id", "Pflicht — die Route listet immer die Dokumente genau einer Immobilie"},
		},
		Example: "immojump documents list -q immobilien_id=5 --fields id,dateiname",
	},
	{
		Resource: "documents", Verb: "upload", Risk: RiskWrite, Special: SpecialDocumentsUpload,
		Summary: "Dokument hochladen (Multipart)",
		Method:  "POST", Path: "/api/documents/documents/bulk-upload",
		Args: []Arg{{Name: "datei", Desc: "Pfad zur Datei"}},
		Flags: []Flag{
			{Name: "immobilie-id", Kind: FlagString, Desc: "Dokument dieser Immobilie zuordnen"},
			{Name: "allow-duplicate", Kind: FlagBool, Desc: "Upload auch bei erkanntem Duplikat erlauben"},
		},
		Example: "immojump documents upload expose.pdf --immobilie-id 5",
	},
	{
		Resource: "documents", Verb: "rename", Risk: RiskWrite,
		Summary: "Dokument umbenennen", Method: "PUT", Path: "/api/documents/documents/{id}/rename",
		Args: idArg("ID des Dokuments"),
		Flags: []Flag{
			{Name: "name", Kind: FlagString, Required: true, Desc: "Neuer Dateiname"},
		},
		// Das Backend liest new_filename (document_routes.py), nicht name.
		Body:    []FlagBody{{Flag: "name", Key: "new_filename"}},
		Example: "immojump documents rename 11 --name 'Exposé Köln.pdf'",
	},
	{
		Resource: "documents", Verb: "delete", Risk: RiskDestructive,
		Summary: "Dokument löschen", Method: "DELETE", Path: "/api/documents/documents/{id}",
		Args:    idArg("ID des Dokuments"),
		Example: "immojump documents delete 11",
	},
	{
		// external, nicht write: Veroeffentlichen legt die Datei unbefristet
		// und ohne Anmeldung offen ins Netz (ACL public-read, feste URL). Das
		// ist die haertere Aktion als ein Freigabe-Link — der laeuft ab, kann
		// ein Passwort tragen und ist widerrufbar.
		Resource: "documents", Verb: "publish", Risk: RiskExternal,
		Summary: "HTML-Dokument als oeffentliche Seite veroeffentlichen (dauerhaft ohne Anmeldung erreichbar)",
		Method:  "POST", Path: "/api/documents/documents/{id}/publish",
		Args:    idArg("ID des HTML-Dokuments"),
		Example: "immojump documents publish 11",
	},
	{
		Resource: "documents", Verb: "unpublish", Risk: RiskExternal,
		Summary: "Veroeffentlichung eines HTML-Dokuments aufheben",
		Method:  "POST", Path: "/api/documents/documents/{id}/unpublish",
		Args:    idArg("ID des HTML-Dokuments"),
		Example: "immojump documents unpublish 11",
	},
	{
		Resource: "documents", Verb: "analyze", Risk: RiskWrite,
		Summary: "KI-Analyse eines Dokuments starten",
		Method:  "POST", Path: "/api/documents/documents/{id}/analyze",
		Args:    idArg("ID des Dokuments"),
		Example: "immojump documents analyze 11",
	},
	{
		Resource: "documents", Verb: "analyze-details", Risk: RiskWrite,
		Summary: "Detail-Analyse eines Dokuments starten",
		Method:  "POST", Path: "/api/documents/documents/{id}/analyze/details",
		Args:    idArg("ID des Dokuments"),
		Example: "immojump documents analyze-details 11",
	},
	{
		Resource: "documents", Verb: "mark-reviewed", Risk: RiskWrite,
		Summary: "Analyse als geprüft markieren",
		Method:  "POST", Path: "/api/documents/documents/{id}/mark-reviewed",
		Args:    idArg("ID des Dokuments"),
		Example: "immojump documents mark-reviewed 11",
	},
	{
		Resource: "documents", Verb: "analysis-results", Risk: RiskRead,
		Summary: "Analyse-Ergebnisse abrufen", Method: "GET", Path: "/api/documents/analysis-results",
		QueryHints: []QueryHint{
			{"immobilien_id", "nur Ergebnisse zu dieser Immobilie"},
			{"document_id", "nur Ergebnisse zu diesem Dokument"},
			{"limit", "Anzahl der Ergebnisse (Default 50)"},
		},
		Example: "immojump documents analysis-results -q immobilien_id=5 -q limit=5",
	},

	// --- tags -------------------------------------------------------------
	{
		Resource: "tags", Verb: "list", Risk: RiskRead,
		Summary: "Tags der Organisation auflisten", Method: "GET", Path: "/api/{org}/tags",
		QueryHints: []QueryHint{
			{"for", "nur Tags dieser Objektart, z. B. contact oder immobilie"},
		},
		Example: "immojump tags list -q for=contact",
	},
	{
		Resource: "tags", Verb: "create", Risk: RiskWrite,
		Summary: "Tag anlegen", Method: "POST", Path: "/api/{org}/tags",
		Example: "immojump tags create --set name=Wichtig --set color='#ff0000'",
	},
	{
		Resource: "tags", Verb: "update", Risk: RiskWrite,
		Summary: "Tag ändern", Method: "PUT", Path: "/api/{org}/tags/{tag-id}",
		Args:    []Arg{{Name: "tag-id", Desc: "ID des Tags"}},
		Example: "immojump tags update 3 --set name='Sehr wichtig'",
	},
	{
		Resource: "tags", Verb: "delete", Risk: RiskDestructive,
		Summary: "Tag löschen", Method: "DELETE", Path: "/api/tags/{tag-id}",
		Args:    []Arg{{Name: "tag-id", Desc: "ID des Tags"}},
		Example: "immojump tags delete 3",
	},
	{
		Resource: "tags", Verb: "of", Risk: RiskRead,
		Summary: "Tags eines Objekts", Method: "GET", Path: "/api/tags/{entity-type}/{entity-id}",
		Args: []Arg{
			{Name: "entity-type", Desc: "Objektart, z. B. contact oder immobilie"},
			{Name: "entity-id", Desc: "ID des Objekts"},
		},
		Example: "immojump tags of contact 42",
	},
	{
		Resource: "tags", Verb: "set", Risk: RiskWrite, Special: SpecialTagsSet,
		Summary: "Tags eines Objekts ersetzen (Body ist ein JSON-Array von IDs)",
		Method:  "PUT", Path: "/api/tags/{entity-type}/{entity-id}",
		Args: []Arg{
			{Name: "entity-type", Desc: "Objektart, z. B. contact oder immobilie"},
			{Name: "entity-id", Desc: "ID des Objekts"},
		},
		Flags: []Flag{
			{Name: "tag-ids", Kind: FlagList, Required: true, Desc: "Tag-IDs, kommagetrennt; leer entfernt alle Tags"},
		},
		Example: "immojump tags set contact 42 --tag-ids 3,7",
	},

	// --- shares (Freigabe-Links) ------------------------------------------
	{
		Resource: "shares", Verb: "list", Risk: RiskRead,
		Summary: "Freigabe-Links auflisten", Method: "GET", Path: "/api/share-links",
		Flags: []Flag{
			{Name: "entity-type", Kind: FlagString, Desc: "Nach Objektart filtern: immobilie, dokument oder bild"},
			{Name: "entity-id", Kind: FlagString, Desc: "Nach Objekt-ID filtern"},
		},
		Query:   []FlagQuery{{Flag: "entity-type", Key: "entity_type"}, {Flag: "entity-id", Key: "entity_id"}},
		Example: "immojump shares list --entity-type immobilie --entity-id 5",
	},
	{
		Resource: "shares", Verb: "create", Risk: RiskExternal, Special: SpecialSharesCreate,
		EmptyBodyHint: "Mindestens ein Objekt freigeben: --immobilie <id>, --dokument <id> oder --bild <id>",
		Summary:       "Freigabe-Link erzeugen (Inhalte werden nach außen sichtbar)",
		Method:        "POST", Path: "/api/share-links",
		Flags: []Flag{
			{Name: "immobilie", Kind: FlagList, Desc: "Immobilie freigeben (wiederholbar)"},
			{Name: "dokument", Kind: FlagList, Desc: "Dokument freigeben (wiederholbar)"},
			{Name: "bild", Kind: FlagList, Desc: "Bild freigeben (wiederholbar)"},
			{Name: "title", Kind: FlagString, Desc: "Titel der Freigabe"},
			{Name: "note", Kind: FlagString, Desc: "Nachricht an den Empfänger"},
			{Name: "expires-at", Kind: FlagString, Norm: NormDateTime,
				Desc: "Ablauf als YYYY-MM-DD (= Tagesende) oder vollständiger ISO-Zeitstempel"},
			{Name: "password", Kind: FlagString, NonEmpty: "ohne Passwort das Flag weglassen",
				Desc: "Passwortschutz setzen (mindestens 4 Zeichen)"},
			{Name: "recipient-email", Kind: FlagString, Desc: "E-Mail-Adresse des Empfängers"},
			{Name: "send-email", Kind: FlagBool, Desc: "Link direkt per E-Mail verschicken"},
			{Name: "include-password-in-email", Kind: FlagBool, Desc: "Passwort mit in die E-Mail schreiben (nur auf Wunsch)"},
			{Name: "show-key-facts", Kind: FlagBool, Desc: "Eckdaten der Immobilie mit anzeigen"},
		},
		Body: []FlagBody{
			{Flag: "title", Key: "title"},
			{Flag: "note", Key: "note"},
			{Flag: "expires-at", Key: "expires_at"},
			{Flag: "password", Key: "password"},
			{Flag: "recipient-email", Key: "recipient_email"},
			{Flag: "send-email", Key: "send_email"},
			{Flag: "include-password-in-email", Key: "include_password_in_email"},
			{Flag: "show-key-facts", Key: "settings.show_key_facts"},
		},
		Example: "immojump shares create --immobilie 5 --dokument 11 --title 'Finanzierung' --password bank2026 --expires-at 2026-09-30",
	},
	{
		// external, nicht write: Ein Update kann einen abgelaufenen Link wieder
		// aufmachen (neues expires_at) und per --remove-password den
		// Passwortschutz entfernen. Beides erlaubt der Backend-Service bewusst
		// — beides macht Inhalte erneut nach außen sichtbar, genau wie create.
		Resource: "shares", Verb: "update", Risk: RiskExternal,
		Summary: "Freigabe-Link ändern (nur gesetzte Flags werden geschickt)",
		Method:  "PATCH", Path: "/api/share-links/{id}",
		Args: idArg("ID des Freigabe-Links"),
		Flags: []Flag{
			{Name: "title", Kind: FlagString, Desc: "Titel ändern"},
			{Name: "note", Kind: FlagString, Desc: "Nachricht ändern (leerer String löscht sie)"},
			{Name: "expires-at", Kind: FlagString, Norm: NormDateTime,
				Desc: "Ablauf als YYYY-MM-DD (= Tagesende) oder vollständiger ISO-Zeitstempel"},
			{Name: "password", Kind: FlagString, NonEmpty: "Passwortschutz entfernen: --remove-password",
				Desc: "Neues Passwort setzen (mindestens 4 Zeichen)"},
			{Name: "remove-password", Kind: FlagBool, Desc: "Passwortschutz entfernen (schickt password: null)"},
		},
		Body: []FlagBody{
			{Flag: "title", Key: "title"},
			{Flag: "note", Key: "note"},
			{Flag: "expires-at", Key: "expires_at"},
			{Flag: "password", Key: "password"},
			{Flag: "remove-password", Key: "password", Null: true},
		},
		EmptyBodyHint: "Nichts zu ändern. Setze mindestens eines von --title, --note, --expires-at, --password oder --remove-password",
		Example:       "immojump shares update 7 --expires-at 2026-12-31",
	},
	{
		Resource: "shares", Verb: "revoke", Risk: RiskWrite,
		Summary: "Freigabe-Link widerrufen (der sichere Ausweg, deshalb nur write)",
		Method:  "DELETE", Path: "/api/share-links/{id}",
		Args:    idArg("ID des Freigabe-Links"),
		Example: "immojump shares revoke 7",
	},

	// --- Escape-Hatch und Meta -------------------------------------------
	// --- email ------------------------------------------------------------
	// Die Organisation reist hier ausschliesslich im Header X-Organisation-Id
	// (email_message_routes._resolve_org_id) — deshalb steht in keinem dieser
	// Pfade ein {org}, anders als bei pipelines und tags.
	//
	// Konten anlegen/aendern/loeschen fehlt bewusst: Das setzt SMTP- und
	// IMAP-Passwoerter, und als Flag landen die in der Shell-History und im
	// Transkript jedes Agenten. Dafuer bleiben das Frontend und der
	// Escape-Hatch zustaendig.
	{
		Resource: "email", Verb: "list", Risk: RiskRead,
		Summary: "Nachrichten im Postfach auflisten", Method: "GET", Path: "/api/email-messages",
		QueryHints: []QueryHint{
			{"account_id", "nur dieses Postfach (IDs liefert `email accounts`)"},
			{"folder", "Ordner, Default INBOX; virtuell auch SENT, STARRED, ARCHIVE, TRASH, DRAFTS"},
			{"is_read", "true = nur gelesene, false = nur ungelesene"},
			{"is_starred", "true = nur markierte"},
			{"q", "Freitext über Betreff, Absender und Text"},
			{"page", "Seite (ab 1)"},
			{"per_page", "Treffer pro Seite (Default 50, max. 200)"},
		},
		Example: "immojump email list -q is_read=false --fields items.id,items.subject,items.from_email",
	},
	{
		Resource: "email", Verb: "get", Risk: RiskRead,
		// Der Nebeneffekt gehoert in die Summary, nicht in einen Kommentar:
		// Die Route markiert die Nachricht beim Oeffnen als gelesen. Ein
		// Agent, der stumm 50 Mails auf gelesen setzt, ist eine Beschwerde.
		Summary: "Eine Nachricht mit vollem Text laden — markiert sie dabei als gelesen",
		Method:  "GET", Path: "/api/email-messages/{id}",
		Args:    idArg("ID der Nachricht"),
		Example: "immojump email get 3f2a…",
	},
	{
		Resource: "email", Verb: "thread", Risk: RiskRead,
		Summary: "Einen Thread mit allen Nachrichten laden",
		Method:  "GET", Path: "/api/email-messages/threads/{thread-id}",
		Args:    []Arg{{Name: "thread-id", Desc: "ID des Threads"}},
		Example: "immojump email thread 9c11…",
	},
	{
		Resource: "email", Verb: "search", Risk: RiskRead,
		Summary: "Nachrichten über alle Ordner durchsuchen",
		Method:  "GET", Path: "/api/email-messages/search",
		QueryHints: []QueryHint{
			{"q", "Suchbegriff; ohne ihn antwortet die Route mit einer leeren Liste"},
			{"page", "Seite (ab 1)"},
			{"per_page", "Treffer pro Seite (Default 50, max. 200)"},
		},
		Example: "immojump email search -q q=Notartermin --fields items.id,items.subject",
	},
	{
		Resource: "email", Verb: "folders", Risk: RiskRead,
		Summary: "Ordner des Postfachs auflisten",
		Method:  "GET", Path: "/api/email-messages/folders",
		QueryHints: []QueryHint{
			{"account_id", "nur die Ordner dieses Postfachs"},
		},
		Example: "immojump email folders",
	},
	{
		Resource: "email", Verb: "for-immobilie", Risk: RiskRead,
		Summary: "Nachrichten aller Kontakte, die an einer Immobilie hängen",
		Method:  "GET", Path: "/api/email-messages/immobilie/{immobilie-id}",
		Args: []Arg{{Name: "immobilie-id", Desc: "ID der Immobilie"}},
		QueryHints: []QueryHint{
			{"page", "Seite (ab 1)"},
			{"per_page", "Treffer pro Seite (Default 50, max. 200)"},
		},
		Example: "immojump email for-immobilie 5",
	},
	{
		Resource: "email", Verb: "for-contact", Risk: RiskRead,
		Summary: "Nachrichten eines Kontakts",
		Method:  "GET", Path: "/api/email-messages/contact/{contact-id}",
		Args: []Arg{{Name: "contact-id", Desc: "ID des Kontakts"}},
		QueryHints: []QueryHint{
			{"page", "Seite (ab 1)"},
			{"per_page", "Treffer pro Seite (Default 20, max. 100)"},
		},
		Example: "immojump email for-contact 42",
	},
	{
		Resource: "email", Verb: "outbox", Risk: RiskRead,
		Summary: "Warteschlange der noch nicht zum IMAP-Server gespiegelten Änderungen",
		Method:  "GET", Path: "/api/email-messages/outbox",
		QueryHints: []QueryHint{
			{"status", "PENDING, IN_PROGRESS, COMPLETED oder FAILED (exakt so geschrieben)"},
			{"limit", "Anzahl Einträge (Default 50, max. 500)"},
		},
		Example: "immojump email outbox -q status=FAILED",
	},
	{
		Resource: "email", Verb: "outbox-stats", Risk: RiskRead,
		Summary: "Zählstand der Warteschlange (offen, fehlgeschlagen, erledigt)",
		Method:  "GET", Path: "/api/email-messages/outbox/stats",
		Example: "immojump email outbox-stats",
	},
	{
		Resource: "email", Verb: "accounts", Risk: RiskRead,
		Summary: "Postfächer der Organisation auflisten — liefert die account-id für `email send`",
		Method:  "GET", Path: "/api/org/email-accounts",
		Example: "immojump email accounts --fields items.id,items.email",
	},
	{
		Resource: "email", Verb: "signatures", Risk: RiskRead,
		Summary: "Signaturen der Organisation — liefert die ID für `email send --signature-id`",
		Method:  "GET", Path: "/api/org/email-signatures",
		Example: "immojump email signatures --fields id,name",
	},
	{
		Resource: "email", Verb: "mark-read", Risk: RiskWrite,
		Summary: "Nachrichten als gelesen markieren (--is-read=false setzt sie zurück)",
		Method:  "POST", Path: "/api/email-messages/mark-read",
		Flags: []Flag{
			{Name: "message-ids", Kind: FlagList, Required: true,
				Desc: "IDs der Nachrichten, kommagetrennt oder wiederholt"},
			{Name: "is-read", Kind: FlagBool, Desc: "`--is-read=false` setzt wieder auf ungelesen (Default true)"},
		},
		Body: []FlagBody{
			{Flag: "message-ids", Key: "message_ids"},
			{Flag: "is-read", Key: "is_read"},
		},
		Example: "immojump email mark-read --message-ids 3f2a…,9c11…",
	},
	{
		Resource: "email", Verb: "mark-starred", Risk: RiskWrite,
		Summary: "Nachrichten markieren (--is-starred=false nimmt die Markierung weg)",
		Method:  "POST", Path: "/api/email-messages/mark-starred",
		Flags: []Flag{
			{Name: "message-ids", Kind: FlagList, Required: true,
				Desc: "IDs der Nachrichten, kommagetrennt oder wiederholt"},
			{Name: "is-starred", Kind: FlagBool, Desc: "`--is-starred=false` entfernt die Markierung (Default true)"},
		},
		Body: []FlagBody{
			{Flag: "message-ids", Key: "message_ids"},
			{Flag: "is-starred", Key: "is_starred"},
		},
		Example: "immojump email mark-starred --message-ids 3f2a…",
	},
	{
		Resource: "email", Verb: "archive", Risk: RiskWrite,
		Summary: "Nachrichten archivieren",
		Method:  "POST", Path: "/api/email-messages/archive",
		Flags: []Flag{
			{Name: "message-ids", Kind: FlagList, Required: true,
				Desc: "IDs der Nachrichten, kommagetrennt oder wiederholt"},
		},
		Body:    []FlagBody{{Flag: "message-ids", Key: "message_ids"}},
		Example: "immojump email archive --message-ids 3f2a…",
	},
	{
		// Bewusst write, nicht destructive: Die Route setzt nur is_deleted
		// (email_message_service.trash_messages). Zurueck geht es mit
		// `email move --folder INBOX`. Festgehalten in TestEmailTrashIsReversible.
		Resource: "email", Verb: "trash", Risk: RiskWrite,
		Summary: "Nachrichten in den Papierkorb legen (umkehrbar über `email move`)",
		Method:  "POST", Path: "/api/email-messages/trash",
		Flags: []Flag{
			{Name: "message-ids", Kind: FlagList, Required: true,
				Desc: "IDs der Nachrichten, kommagetrennt oder wiederholt"},
		},
		Body:    []FlagBody{{Flag: "message-ids", Key: "message_ids"}},
		Example: "immojump email trash --message-ids 3f2a…",
	},
	{
		Resource: "email", Verb: "move", Risk: RiskWrite,
		Summary: "Nachrichten in einen anderen Ordner verschieben",
		Method:  "POST", Path: "/api/email-messages/move",
		Flags: []Flag{
			{Name: "message-ids", Kind: FlagList, Required: true,
				Desc: "IDs der Nachrichten, kommagetrennt oder wiederholt"},
			{Name: "folder", Kind: FlagString, Required: true,
				Desc: "Zielordner, Default INBOX; SENT/STARRED/ARCHIVE/TRASH/DRAFTS sind virtuell und bleiben lokal"},
		},
		Body: []FlagBody{
			{Flag: "message-ids", Key: "message_ids"},
			{Flag: "folder", Key: "folder"},
		},
		Example: "immojump email move --message-ids 3f2a… --folder Notar",
	},
	{
		Resource: "email", Verb: "sync", Risk: RiskWrite,
		Summary: "IMAP-Abgleich anstoßen (Backend-Limit: 10 Aufrufe pro Stunde)",
		Method:  "POST", Path: "/api/email-messages/sync",
		Flags: []Flag{
			{Name: "account-id", Kind: FlagString, Desc: "nur dieses Postfach; ohne Angabe alle der Organisation"},
		},
		Body:    []FlagBody{{Flag: "account-id", Key: "account_id"}},
		Example: "immojump email sync",
	},
	{
		Resource: "email", Verb: "outbox-retry", Risk: RiskWrite,
		Summary: "Fehlgeschlagene Einträge der Warteschlange erneut versuchen",
		Method:  "POST", Path: "/api/email-messages/outbox/retry",
		Flags: []Flag{
			{Name: "entry-ids", Kind: FlagList,
				Desc: "IDs aus `email outbox`; ohne Angabe alle fehlgeschlagenen"},
		},
		Body:    []FlagBody{{Flag: "entry-ids", Key: "entry_ids"}},
		Example: "immojump email outbox-retry",
	},
	{
		Resource: "email", Verb: "folder-create", Risk: RiskWrite,
		Summary: "Ordner anlegen",
		Method:  "POST", Path: "/api/email-messages/folders",
		Flags: []Flag{
			{Name: "name", Kind: FlagString, Required: true,
				NonEmpty: "einen Ordnernamen angeben",
				Desc:     "Name des Ordners; ohne / \\ < > \" und nicht mit Punkt beginnend oder endend"},
		},
		Body:    []FlagBody{{Flag: "name", Key: "name"}},
		Example: "immojump email folder-create --name Notar",
	},
	{
		Resource: "email", Verb: "folder-rename", Risk: RiskWrite,
		Summary: "Ordner umbenennen",
		Method:  "POST", Path: "/api/email-messages/folders/rename",
		Flags: []Flag{
			{Name: "old-name", Kind: FlagString, Required: true,
				NonEmpty: "den bisherigen Ordnernamen angeben", Desc: "bisheriger Name"},
			{Name: "new-name", Kind: FlagString, Required: true,
				NonEmpty: "den neuen Ordnernamen angeben", Desc: "neuer Name"},
		},
		Body: []FlagBody{
			{Flag: "old-name", Key: "old_name"},
			{Flag: "new-name", Key: "new_name"},
		},
		Example: "immojump email folder-rename --old-name Notar --new-name Notartermine",
	},
	{
		Resource: "email", Verb: "folder-delete", Risk: RiskDestructive,
		Summary: "Ordner löschen",
		Method:  "POST", Path: "/api/email-messages/folders/delete",
		Flags: []Flag{
			{Name: "name", Kind: FlagString, Required: true,
				NonEmpty: "einen Ordnernamen angeben", Desc: "Name des Ordners"},
		},
		Body:    []FlagBody{{Flag: "name", Key: "name"}},
		Example: "immojump email folder-delete --name Notar",
	},
	{
		// external, nicht write: Eine verschickte Mail holt niemand zurueck.
		// Das Backend zieht dieselbe Grenze (rbac.can(org, 'create', 'email')
		// zusaetzlich zu is_scoped, seit a463f22ac).
		Resource: "email", Verb: "send", Risk: RiskExternal,
		Summary: "E-Mail über ein Postfach der Organisation versenden",
		Method:  "POST", Path: "/api/org/email-accounts/{account-id}/send",
		Args: []Arg{{Name: "account-id", Desc: "ID des Postfachs (aus `email accounts`)"}},
		Flags: []Flag{
			{Name: "to", Kind: FlagList, Required: true,
				NonEmpty: "mindestens eine Empfängeradresse angeben",
				Desc:     "Empfänger, kommagetrennt oder wiederholt"},
			{Name: "cc", Kind: FlagList, Desc: "Kopie, kommagetrennt oder wiederholt"},
			{Name: "bcc", Kind: FlagList, Desc: "Blindkopie, kommagetrennt oder wiederholt"},
			{Name: "subject", Kind: FlagString, Desc: "Betreff"},
			{Name: "html", Kind: FlagString, Desc: "Inhalt als HTML"},
			{Name: "signature-id", Kind: FlagString, Desc: "Signatur anhängen (IDs aus `email signatures`)"},
		},
		Body: []FlagBody{
			{Flag: "to", Key: "to"},
			{Flag: "cc", Key: "cc"},
			{Flag: "bcc", Key: "bcc"},
			{Flag: "subject", Key: "subject"},
			{Flag: "html", Key: "html"},
			{Flag: "signature-id", Key: "signature_id"},
		},
		Example: "immojump email send 7b1c… --to kunde@example.com --subject \"Exposé\" --html \"<p>Anbei.</p>\"",
	},

	{
		// Kein statisches Risk: Es entsteht pro Aufruf aus Methode und Pfad
		// (siehe APIRiskRule und RiskForRequest).
		Resource: "api", Special: SpecialAPI,
		Summary: "Beliebigen /api/-Pfad aufrufen (Escape-Hatch); das Risk-Level entsteht pro Aufruf",
		Args: []Arg{
			{Name: "method", Desc: "HTTP-Methode, z. B. GET oder POST"},
			{Name: "pfad", Desc: "Pfad ab /api/, z. B. /api/deals"},
		},
		// status_ids ist der Parameter, den die Deals-Route wirklich liest
		// (modules/routes/deal_routes.py) — ein Beispiel, das stillschweigend
		// nichts filtert, wäre schlimmer als gar keins.
		Example: "immojump api GET /api/deals -q status_ids=7",
	},
	{
		Resource: "docs", Risk: RiskRead, Local: true, Special: SpecialDocs,
		Summary: "Markdown-Referenz nach stdout schreiben — komplett oder als Ausschnitt",
		Args: []Arg{
			{Name: "resource", Desc: "Nur diese Ressource ausgeben", Optional: true},
			{Name: "verb", Desc: "Nur diesen Befehl ausgeben", Optional: true},
		},
		Example: "immojump docs shares create",
	},
	{
		Resource: "schema", Risk: RiskRead, Local: true, Special: SpecialSchema,
		Summary: "Befehls-Schema als JSON (Risk, Args, Flags, Exit-Codes)",
		Args: []Arg{
			{Name: "resource", Desc: "Nur diese Ressource ausgeben", Optional: true},
			{Name: "verb", Desc: "Nur diesen Befehl ausgeben", Optional: true},
		},
		Example: "immojump schema shares create",
	},
	{
		Resource: "version", Risk: RiskRead, Local: true, Special: SpecialVersion,
		Summary: "Version des CLI ausgeben",
		Example: "immojump version",
	},
}

// Lookup findet einen Befehl. Befehle ohne Verb (api, docs, schema, version)
// werden mit leerem verb gesucht.
func Lookup(resource, verb string) (Spec, bool) {
	for _, spec := range Registry {
		if spec.Resource == resource && spec.Verb == verb {
			return spec, true
		}
	}
	return Spec{}, false
}

// specsForResource liefert alle Befehle einer Ressource in Registry-Reihenfolge.
func specsForResource(resource string) []Spec {
	var out []Spec
	for _, spec := range Registry {
		if spec.Resource == resource {
			out = append(out, spec)
		}
	}
	return out
}

// pathPlaceholders liest alle {…} aus einem Pfad.
func pathPlaceholders(path string) []string {
	var out []string
	rest := path
	for {
		start := strings.Index(rest, "{")
		if start < 0 {
			return out
		}
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			return out
		}
		out = append(out, rest[start+1:start+end])
		rest = rest[start+end+1:]
	}
}
