# Design: immojump-cli

## Warum ein CLI, wenn es den MCP-Server schon gibt?

`mcp-immojump` lädt beim Verbinden 87 Tool-Schemas in den Kontext des Agenten —
bevor auch nur eine Frage gestellt wurde. Ein CLI kostet **null Kontext bis zum
ersten Aufruf**: Der Agent entdeckt Befehle progressiv (`immojump --help`,
`immojump shares --help`) und bezahlt nur für das, was er wirklich braucht.
Vorbild ist [openclaw/gogcli](https://github.com/openclaw/gogcli): eine
Binary, maschinenlesbarer Output, Schema/Referenz/Hilfe aus demselben
Befehlsbaum generiert.

Das CLI ist — wie der MCP-Server — bewusst **thin**: alle Business-Regeln
liegen im immoJUMP-Backend. Dieses Repo kapselt nur HTTP-Zugriff auf die
bestehenden API-Endpoints. Es hält keine eigenen Daten und dupliziert keine
Business-Logik.

## Sprache & Abhängigkeiten

**Go, ausschließlich Standardbibliothek.** Gründe:

- Eine statische Binary pro Plattform — trivial in Agent-Container
  (OpenClaw-Runtimes) zu legen, kein Python/Node zur Laufzeit nötig.
- Schneller Prozessstart (Agenten rufen das CLI pro Aktion einmal auf).
- `httptest` + Tabellen-Tests machen TDD gegen die HTTP-Schicht billig.
- Null Dependencies = null Supply-Chain-Pflege. Kein cobra: Der deklarative
  Befehlsbaum (siehe unten) erzeugt Hilfe, Referenz und Schema selbst.

## Architektur

```
cmd/immojump/main.go        → Exit-Code-Mapping, sonst nichts
internal/cli/               → Registry (deklarative Command-Specs), Dispatch,
                              Flag-/Body-Builder, help/docs/schema/context/auth
internal/config/            → Contexts (kubectl-Analogie), Env, Allowlist
internal/api/               → HTTP-Client: Bearer + X-Organisation-Id,
                              Fehler-Mapping auf {message}, Multipart-Upload
internal/output/            → JSON-Ausgabe, --pretty, --fields-Projektion
```

### Deklarative Command-Registry

Jeder Befehl ist ein Datensatz, kein Code:

```go
{Resource: "contacts", Verb: "get", Method: "GET",
 Path: "/api/contacts/{id}", Args: []Arg{{Name: "id"}},
 Summary: "Einen Kontakt laden"}
```

Aus dieser einen Tabelle entstehen: Dispatch, `--help` auf jeder Ebene,
`immojump docs` (Markdown-Referenz, eingecheckt als REFERENCE.md) und
`immojump schema` (JSON für Tooling). Neue Endpoints = eine Spec-Zeile plus
eine Test-Zeile.

`{org}` im Pfad wird aus der aufgelösten Organisation gefüllt (Pipelines und
Tags haben die Org im Pfad, alle anderen Routen nur im Header).

### Generische Parameter statt 87 Schemas

Das Backend ist die Quelle der Wahrheit für Feldnamen und Validierung
(Marshmallow lehnt Unbekanntes ab). Das CLI erfindet deshalb keine eigene
Schema-Schicht:

- `-q key=value` (wiederholbar) → Query-Parameter
- `--set pfad.zu.feld=wert` (wiederholbar) → JSON-Body; Werte werden als JSON
  interpretiert (`true`, `3`, `null`, `["a"]`), sonst als String
- `--body '{…}'`, `--body @datei.json`, `--body -` (stdin) → kompletter Body;
  `--set` überlagert danach einzelne Felder
- `--idempotency-key <key>` → wird als `Idempotency-Key`-Header mitgeschickt.
  Vorbereitend: Das Backend wertet den Header heute noch nicht aus; sobald es
  das tut, werden sichere Retries nach Netzwerkabbrüchen möglich, ohne den
  CLI-Contract zu ändern.

Was eine Route an Query-Parametern **wirklich** auswertet, steht deklarativ in
der Spec (`QueryHints []QueryHint{Name, Summary}`) und erscheint daraus in
`--help`, `docs` und `schema` als „Bekannte Query-Parameter". Das ist der
Unterschied zwischen 125.598 und 3.002 Zeichen für dieselbe Immobilienliste
(24 echte Objekte): `-q slim=true` plus `--fields id,name`. Ein Parameter, der
nur im Backend existiert, existiert für einen Agenten nicht.

Die Einträge sind in `modules/routes/` nachgelesen, nicht geraten — `-q limit=3`
lieferte gegen die Produktion alle Objekte, weil `/api/v2/immobilien` `limit`
gar nicht liest. Eine Registry-Invariante hält QueryHints deshalb auf
GET-Befehlen fest, eine zweite prüft, dass kein Beispiel einen Parameter
benutzt, den der Befehl nicht führt.

Wo es für Agenten zählt, gibt es kuratierte Sugar-Flags (z. B.
`shares create --immobilie <id> --password …`), die intern nur Body bauen.
Sie stehen deklarativ in der Spec: `Body []FlagBody` bildet Flag → Body-Pfad
ab, `Query []FlagQuery` Flag → Query-Parameter. `Kind` bestimmt den JSON-Typ
(`string`, `number`, `bool`), `{Null: true}` macht aus einem Schalter ein
`"key": null` (so entfernt `--remove-password` den Passwortschutz). Zwei Flags
auf denselben Body-Schlüssel sind ein Bedienfehler, kein stiller Überschreiber.

Zahlen bleiben exakt: `--body` und `--set` werden mit `UseNumber` gelesen, sonst
macht `encoding/json` aus jeder Zahl ein `float64` — eine 20-stellige ID
verliert Stellen, `225000.00` wird zu `225000`. Beides landet sonst so beim
Kunden.

`--expires-at 2026-09-30` wird zu `2026-09-30T23:59:59` — „gültig bis
einschließlich", dieselbe Semantik wie die Web-App
(`immobilien-ka/src/Services/ShareLinkService.ts`, `resolveExpiresAt`).
Bewusst ohne Zeitzonen-Suffix: Die Web-App rechnet das lokale Tagesende in UTC
um, ein CLI in einem Container kennt die gemeinte Zeitzone des Nutzers aber
nicht. Vollständige ISO-Zeitstempel gehen unverändert durch.

Befehle, die ihren Body selbst bauen (`documents upload`, `pipelines import`,
`tags set`), lehnen `--set`/`--body` mit Exit 2 ab; `auth` und `context`
zusätzlich `-q`. Sie stillschweigend zu ignorieren wäre schlimmer als ein
Bedienfehler — ein Agent hielte sein Feld für angekommen.

`pipelines import` hängt die aufgelöste Organisation immer als
`?organisation_id=…` an: Die Import-Route liest sie ausschließlich aus Payload
oder Query, den Header `X-Organisation-Id` ignoriert sie. Fehlt die
Organisation, endet der Aufruf mit Exit 3, bevor ein Request rausgeht.

## Authentifizierung & Kontexte

Identisch zum MCP-Server: `Authorization: Bearer <API-Token>` (Token aus
*Einstellungen → API-Zugang*) plus `X-Organisation-Id`.

Multi-Org-Arbeit funktioniert wie `kubectl --context`:

```jsonc
// ~/.config/immojump/config.json  (0600)
{
  "current_context": "prod",
  "contexts": {
    "prod": {"base_url": "https://immojump.de",      "organisation_id": "…", "token": "…"},
    "beta": {"base_url": "https://beta.immojump.de", "organisation_id": "…", "token_env": "IMMOJUMP_BETA_TOKEN"}
  }
}
```

- `immojump context list | use <name> | current | delete <name>`
- `immojump auth login --context beta --base-url … --organisation … --token …`
  legt einen Context an (Token wahlweise als `token_env`-Verweis statt
  Klartext) und prüft ihn gegen `GET /api/user/me`.
- Pro Aufruf: `--context <name>` wählt den Context, `--org` / `--base-url`
  überschreiben einzelne Felder.

`auth login` und `auth status` antworten **kompakt**: Context, `base_url`,
`organisation_id`, maskiertes Token, `token_source` (`env:IMMOJUMP_TOKEN`,
`env:<name>` oder `context`), `user` mit `id` und `username` plus die eigene
Rolle in der aktiven Organisation (`organisation_role` aus
`organisation_access`). Das vollständige Nutzerobjekt gibt es über `--full`.

Der Grund ist Kontext-Ökonomie an der teuersten Stelle: `/api/user/me` liefert
Abo-Daten, Login-Zähler und jede Organisation des Nutzers — ~1.400 Zeichen als
Anmeldebestätigung, im Befehl, den jeder Agent als Allererstes ausführt.
`token_source` beantwortet dabei die häufigste Diagnosefrage („warum nimmt der
Aufruf ein anderes Token?") und kommt aus `config.Resolve`, damit sich
Auflösung und Anzeige nicht auseinanderentwickeln.

Auflösungsreihenfolge je Feld: **Flag > Env > Context-Datei.**
Env-Variablen heißen wie beim MCP-Server: `IMMOJUMP_TOKEN`,
`IMMOJUMP_ORGANISATION_ID`, dazu `IMMOJUMP_BASE_URL` und `IMMOJUMP_CONTEXT`.

`--token-env` ist strikt: Es wird genau die genannte Variable gelesen. Ist sie
leer oder nicht gesetzt, endet `auth login` mit Exit 3 und speichert nichts —
ein stiller Rückfall auf `IMMOJUMP_TOKEN` würde einen Context anlegen, der auf
eine leere Variable zeigt und später wortlos ein fremdes Token benutzt.

### Wo die Konfiguration liegt

`IMMOJUMP_CONFIG` > `XDG_CONFIG_HOME` > `HOME`. Fehlt alles drei — in
Agenten-Containern durchaus üblich —, gibt es **keinen** Pfad: Lesen liefert
eine leere Konfiguration, Schreiben (`auth login`, `context use|delete`) endet
mit Exit 3 und dem Hinweis auf `IMMOJUMP_CONFIG`. Der frühere Fallback auf
einen relativen Pfad hätte eine Datei mit Klartext-Token ins
Arbeitsverzeichnis geschrieben — im Zweifel ins Repository des Kunden.

Geschrieben wird atomar (Temp-Datei im Zielverzeichnis mit 0600, dann
`rename`): Ein Abbruch mittendrin lässt die alte Konfiguration heil, statt
eine halbe Datei ohne Contexts zu hinterlassen.

### Base-URL-Allowlist

Token dürfen nur an bekannte immoJUMP-Instanzen gehen (Schutz gegen Tippfehler
und Look-alike-Hosts durch Agenten). Default-Allowlist wie beim MCP-Server:
`https://immojump.de`, `https://beta.immojump.de`, `http://localhost:8081`.
Erweiterbar per `IMMOJUMP_EXTRA_BASE_URLS` (kommagetrennt; das MCP-kompatible
`ALLOWED_BASE_URLS_EXTRA` wird ebenfalls akzeptiert).

## Risk-Level & lokale Policy

Jede Command-Spec trägt ein Risk-Level — sichtbar in `--help`, `docs` und
`schema`, damit Agenten (und Menschen) Konsequenzen einschätzen können:

| Level         | Beispiel                                                 |
| ------------- | -------------------------------------------------------- |
| `read`        | `immobilien list`, `shares list`                          |
| `write`       | `contacts create`, `shares revoke`                        |
| `external`    | `shares create`, `shares update`, `--send-email`          |
| `destructive` | `immobilien delete`, `documents delete`                   |

Zwei bewusste Einstufungen bei den Freigabe-Links:

- **`shares revoke` ist `write`, nicht `destructive`.** Der Widerruf ist der
  sichere Ausweg und muss auch für vorsichtig konfigurierte Agenten erreichbar
  bleiben.
- **`shares update` ist `external`, nicht `write`.** Ein Update kann einen
  abgelaufenen Link durch ein neues `expires_at` wieder aufmachen und per
  `--remove-password` den Passwortschutz entfernen — beides erlaubt der
  Backend-Service (`update_share_link`) bewusst. Damit macht `update` Inhalte
  genauso nach außen sichtbar wie `create`; `--allow read,write` darf das nicht
  durchlassen.

Begrenzung pro Aufruf oder Umgebung: `--readonly` (nur `read`) bzw.
`--allow read,write` (Liste erlaubter Level), Env `IMMOJUMP_ALLOW`. Ein
blockierter Befehl endet mit Exit 6 und `code: "POLICY_BLOCKED"` auf stderr.

**Fail-closed.** Eine gesetzte, aber leere Liste ist ein Konfigurationsfehler,
kein „dann eben alles":

| Angabe                                        | Ergebnis                       |
| --------------------------------------------- | ------------------------------ |
| gar keine (`--allow` fehlt, `IMMOJUMP_ALLOW` leer/ungesetzt) | alles erlaubt |
| `--allow ""`, `--allow ","`, `IMMOJUMP_ALLOW=","` | **Exit 3**, nichts läuft   |
| `--allow read,quatsch`                        | **Exit 3**, nichts läuft       |

Ohne das würde ein leerer Shell-Ausdruck (`--allow "$LEVELS"`) die Policy
lautlos abschalten — genau in dem Moment, in dem sie gebraucht wird.

### Risk des Escape-Hatch

`api <METHOD> <pfad>` trägt **kein festes Level**; in `--help`, `docs` und
`schema` steht dort `dynamic` plus die Regel (`risk_rule`). Aufgelöst wird pro
Aufruf:

1. **Registry-Match.** Methode und Pfad werden gegen die Pfad-Templates der
   Registry geprüft; `{platzhalter}` deckt genau ein Segment ab, `{org}` zählt
   als Segment. Passt eine Spec, gilt deren Risk. Passen mehrere, gewinnt das
   strengere.
2. **Ohne Treffer konservativ nach Methode:** `GET`/`HEAD` = `read`,
   `DELETE` = `destructive`, **alles andere = `external`**.

Der Registry-Match ist kein Komfort, sondern der Bypass-Schutz:
`--allow read,write api POST /api/share-links …` würde sonst einen
Freigabe-Link erzeugen, obwohl `shares create` genau dafür gesperrt ist. Der
Fallback ist bewusst `external` statt `write` — bei einem unkuratierten Pfad
lässt sich nicht erkennen, ob er etwas nach außen sichtbar macht, also wird es
angenommen.

**Ehrlicher Rahmen:** Diese Policy ist Schutz gegen Versehen, keine
Sicherheitsgrenze — ein Agent mit uneingeschränktem Token kann die API auch
direkt aufrufen. Die echte Grenze sind serverseitige Token-Scopes; das ist
Backend-Roadmap (siehe „Abgrenzung/später").

## Output & Exit-Codes

- stdout: Response-JSON, kompakt (eine Zeile). `--pretty` für Menschen.
  `--fields a,b.c` projiziert Objekte bzw. Listenelemente auf wenige Felder —
  Kontext-Ökonomie für Agenten.
- Nicht-JSON-Antworten (z. B. Pipeline-Export als YAML) gehen roh nach stdout.
- stderr: genau drei Zeilenformen, jede eine JSON-Zeile mit einem
  Marker-Feld vorneweg — `{"error":true,…}`, `{"warning":true,…}`,
  `{"trace":true,…}`. **stdout bleibt in jedem Fall reines JSON.**
- Exit-Codes (stabil, Agenten branchen darauf statt Meldungen zu parsen):

| Exit | Bedeutung                                      |
| ---: | ---------------------------------------------- |
|  `0` | Erfolg                                         |
|  `1` | sonstiger API-Fehler, plus lokale Fehler       |
|  `2` | Usage-Fehler (unbekannter Befehl, Flag, Args)  |
|  `3` | lokale Konfiguration/Auth unvollständig        |
|  `4` | 401 — Token fehlt/ungültig                     |
|  `5` | 404 — nicht gefunden                           |
|  `6` | 403 — keine Berechtigung, oder Policy-Block    |
|  `7` | 429 — Rate Limit, später erneut versuchen      |
|  `8` | 5xx/Netzwerkfehler — temporär, Retry möglich   |
|  `9` | 409 — Konflikt (z. B. widerrufener Share-Link) |
| `11` | 400/422 — Validierung fehlgeschlagen           |

**Exit 8 ist ausschließlich HTTP-5xx, Netzwerkabbruch und Timeout.** Er ist
eine Zusage („temporär, Retry möglich"), auf die Agenten ihre Retry-Schleife
bauen. Lokale Fehler — kaputtes stdout, volle Platte, nicht serialisierbare
Ausgabe — enden deshalb mit Exit 1: Ein Retry würde daran nichts ändern.

Der Timeout (`--timeout`, Default 60 s) hängt am API-Client und nicht am
`http.Client`. So gilt er auch, wenn ein HTTP-Client injiziert wird (Tests,
künftige Aufrufer) — sonst wäre `--timeout` eine Zusage mit Löchern.

### Die Fehlerzeile trägt das komplette Payload

`api_error(message, status_code, *, code=None, **extra)` erlaubt **beliebige**
Zusatzfelder, und genau dort steht die Lösung für den Aufrufer: welches Feld
abgelehnt wurde (`errors`), welche Werte erlaubt sind (`valid_values`), wie der
Kontingentstand aussieht (402). Die Fehlerzeile übernimmt deshalb **alle**
Schlüssel der JSON-Antwort, nicht nur `message`/`code`:

```json
{"error":true,"status":400,"message":"Validierungsfehler.","errors":{"type":["Invalid enum value task"]},"valid_values":{"type":["ANRUF","BESICHTIGUNG",…]}}
```

Vorher blieb davon `{"error":true,"status":400,"message":"Validierungsfehler."}`
übrig — der Agent bekam die Diagnose gestellt und die Antwort weggenommen. Eine
eigene Schema-Schicht im CLI wäre die falsche Antwort darauf (und über
`/api/openapi.json` ohnehin nicht zu haben: der Endpoint lehnt Bearer-Tokens
ab). Das Backend weiß es besser und sagt es bereits — es muss nur ankommen.

Regeln:

- Reihenfolge und Bedeutung von `error`, `status`, `message`, `code` bleiben
  unverändert; die Zusatzfelder folgen danach alphabetisch (reproduzierbar).
- Kollidiert ein Backend-Feld mit den CLI-eigenen `error`/`status`, gewinnt das
  CLI und der Backend-Wert steht als `backend_error`/`backend_status` daneben.
  Verloren geht nichts.
- Gelesen wird mit `UseNumber` — ein Kontingentstand darf nicht durch `float64`
  laufen.

**Nicht-JSON-Antworten** bekommen eine eigene, knappe Meldung statt einer
halben HTML-Seite: `HTTP 404 Not Found — die Route existiert auf dieser Instanz
nicht oder erlaubt diese Methode nicht (Antwort war kein JSON)`. Der Rohtext
bleibt als Feld `raw` erhalten, ohne Tags und Umbrüche, gekürzt auf 200 Zeichen.
404 und 405 teilen sich diese Meldung bewusst: In der Produktion antwortet eine
unbekannte Route mit 405, und beides bedeutet für den Aufrufer dasselbe —
nicht weiter Pfade raten.

### `--fields`, das ins Leere zeigt, sagt das

`contacts create … --fields id,first_name` gab stumm `{}` und Exit 0 zurück:
Die Antwort ist verpackt (`{"contact":{…},"success":true}`), die Felder liegen
unter `contact.*`. Für einen Agenten ist ein leeres Objekt kein Signal — er
hält den Aufruf für gescheitert und legt im Zweifel doppelt an.

Trifft kein einziges (oder nur ein Teil der) angeforderten Felder, geht eine
Warnzeile nach stderr, die die fehlenden Pfade und die tatsächlich vorhandenen
Top-Level-Schlüssel nennt (höchstens acht, sonst gekürzt). Bei Listen zählt ein
Feld als vorhanden, sobald ein einziges Element es trägt; eine leere Liste
meldet nichts, dort ist schlicht nichts zu finden. Der Exit-Code bleibt `0` —
es ist kein Fehler.

Bewusst **nicht** implementiert: automatisches Auspacken von Wrapper-Keys. Das
CLI soll nicht raten, welcher Schlüssel gemeint war; es nennt die vorhandenen
und überlässt die Wahl dem Aufrufer.

### Selbstauskunft: `--verbose`, `docs`/`schema` mit Ausschnitt

`--verbose` schreibt Methode und vollständige URL vor dem Request nach stderr
(`{"trace":true,…}`) — ohne Header und ohne Body, der Token taucht dort nie
auf. Ohne diese Zeile lässt sich ein 404 nicht einordnen: falscher Pfad oder
Route fehlt auf dieser Instanz? Genau daran hing zuvor eine Serie geratener
Pfade über den Escape-Hatch.

`docs` nimmt seit demselben Durchgang `[resource [verb]]` wie `schema` — beide
über dieselbe Auswahlfunktion, damit Meldungen und Exit-Codes identisch
bleiben. Der ungescopte Aufruf (37 KB Markdown, 28 KB JSON) weist auf stderr
auf die gezielte Form hin (~2 KB); `immojump docs > REFERENCE.md` bleibt davon
unberührt, weil der Hinweis nicht auf stdout geht.

## Befehlsumfang v1

Spiegel des MCP-Standard-Tiers plus das neue Freigabe-Feature:

- `auth` login/status (kompakt, `--full` für die vollständige Antwort),
  `context` list/use/current/delete
- `contacts` list/get/create/update/set-status/delete/activities/immobilien
- `immobilien` list/search/get/create/update/patch/delete/contacts/duplicate
- `units` list/create/update/delete
- `activities` list/get/for-immobilie/create/update/delete
- `pipelines` list/get/create/update/delete/statuses/add-status/export/import
- `statuses` list/update/delete/swap/aliases/add-alias
- `templates` list/recurring/by-status/get/create/update/delete/batch-move
- `documents` list/upload/rename/delete/analyze/analyze-details/mark-reviewed/analysis-results
- `tags` list/create/update/delete/of/set
- `shares` list/create/update/revoke — **Freigabe-Links** für `immobilie`,
  `dokument`, `bild` (`/api/share-links`), mit Sugar-Flags für Items,
  Passwort, Ablauf, E-Mail-Versand; `update` folgt der Sent-Keys-Semantik des
  Backends (nur explizit gesetzte Flags landen im PATCH-Body,
  `--remove-password` schickt `password: null`)
- `api <METHOD> <pfad>` — Escape-Hatch für alles, was (noch) keinen kuratierten
  Befehl hat; gleiche Auth, gleiche Allowlist, gleiche Policy (das Risk kommt
  aus der Registry, siehe oben)
- `docs [resource [verb]]`, `schema [resource [verb]]`, `version`

Spezialfälle: `documents upload` (Multipart `files[]` gegen
`/api/documents/documents/bulk-upload`), `pipelines import`
(`Content-Type: application/x-yaml` plus `?organisation_id=…`), `tags set`
(Body ist ein rohes JSON-Array von Tag-IDs), `shares create` (items-Liste aus
den Sugar-Flags).

Feldnamen kommen aus dem Backend, nicht aus dem Bauch: Die Beispiele in der
Registry sind gegen `modules/schemas/` und die Routen geprüft — `first_name`
statt `vorname`, `title` statt `titel`, `einheit`/`ist_rent` bei Einheiten,
`color` statt `farbe`, `new_filename` beim Umbenennen von Dokumenten. Ein
Beispiel, das 422 zurückgibt, ist schlimmer als gar keins.

Bewusst NICHT in v1: destruktive Massen-Operationen (`contacts bulk-delete`),
Merge-Flows, Deals/Tickets/Feed/Loans (Profi-/Full-Tier) — über `api …`
erreichbar, kuratiert werden sie bei Bedarf.

## Teststrategie (TDD)

1. **config**: Auflösungskette, Context-Befehle, Allowlist — reine Unit-Tests.
2. **api**: `httptest`-Server prüft Header, Fehler-Mapping, Multipart.
3. **output**: JSON-Formate, `--fields`.
4. **cli**: Tabellen-Tests `argv → erwarteter HTTP-Request` für jede
   Command-Spec (ein Testfall pro Befehl), plus Help-/Exit-Code-Verhalten.
   Ein generischer Durchlauf über die komplette Registry stellt sicher, dass
   jeder Pfad-Platzhalter durch Args oder `{org}` gedeckt ist.

## Release-Fluss

Ein Workflow (`.github/workflows/ci.yml`), drei Stufen in fester Reihenfolge:

1. **`test`** — bei jedem Pull Request und bei jedem Push auf `main`. Führt
   `make ci` aus: `gofmt`-Prüfung, `go vet`, `go test ./...` und den Abgleich
   des eingecheckten `REFERENCE.md` gegen die Registry (`make docs-check`).
   Damit kann die generierte Referenz nicht mehr auseinanderlaufen.
2. **`release-please`** — nur auf `main`, `needs: test`. Pflegt aus den
   konventionellen Commits einen Release-PR (CHANGELOG + Versions-Bump); der
   Merge erzeugt das GitHub-Release.
3. **`release-assets`** — nur wenn dabei ein Release entstanden ist. Checkt
   exakt den Tag aus, baut die Cross-Binaries, schreibt `SHA256SUMS.txt` und
   hängt alles ans Release.

Ein zweiter Testlauf vor dem Asset-Build entfällt bewusst: Derselbe
Workflow-Run hat `test` bereits bestanden, und `release-assets` hängt über
`needs` daran.

Versionierung: `bump-minor-pre-major` ist gesetzt, `bump-patch-for-minor-pre-major`
bewusst **nicht** — sonst würde das erste `feat` in einer 0.x-Reihe zu `v0.0.1`
statt `v0.1.0`.

## Abgrenzung / später

- `docs --skill`: fertiges SKILL.md für Claude-Agenten generieren.
- Images-Upload, Custom Fields, Deals/Tickets als kuratierte Befehle.
