# immojump-cli

Ein Kommandozeilen-Client für die [immoJUMP](https://immojump.de)-API — eine
statische Go-Binary ohne Laufzeit-Abhängigkeiten, gebaut für Agenten (OpenClaw,
Claude Code, Codex) und für Menschen, die schnell etwas nachsehen wollen.

Das CLI ist bewusst **thin**: Alle Business-Regeln liegen im immoJUMP-Backend.
Dieses Repo kapselt nur den HTTP-Zugriff auf bestehende Endpoints, hält keine
eigenen Daten und dupliziert keine Logik.

## Warum ein CLI, wenn es den MCP-Server schon gibt?

`mcp-immojump` lädt beim Verbinden 87 Tool-Schemas in den Kontext des Agenten —
bevor überhaupt eine Frage gestellt wurde. Ein CLI kostet **null Kontext bis zum
ersten Aufruf**: Der Agent entdeckt Befehle progressiv und bezahlt nur das, was
er wirklich braucht.

```bash
immojump --help                 # 16 Ressourcen, eine Bildschirmseite
immojump shares --help          # die vier Befehle dieser Ressource
immojump shares create --help   # Argumente, Flags, Risk-Level, Beispiel
immojump schema shares create   # dasselbe als JSON für Tooling
immojump docs shares create     # dasselbe als Markdown-Ausschnitt
```

`schema` und `docs` nehmen beide `[ressource [befehl]]`. Ohne Angabe kommt der
komplette Dump (28 KB bzw. 38 KB), mit Angabe der Ausschnitt (~2 KB) — deshalb
weisen beide auf stderr auf die gezielte Form hin.

Beide Wege bleiben gültig: Der MCP-Server ist die richtige Wahl, wenn ein Agent
dauerhaft mit immoJUMP arbeitet und Tool-Calls braucht. Das CLI ist die richtige
Wahl in Shell-Umgebungen, Skripten, CI und überall dort, wo Kontext teuer ist.

## Installation

Ein Befehl, kein Go nötig:

```bash
curl -fsSL https://raw.githubusercontent.com/immoJUMP/immojump-cli/main/install.sh | sh
```

Das Skript erkennt System und Architektur, lädt das passende Binary vom
GitHub-Release, **prüft die Checksumme** und legt es nach `/usr/local/bin`
(bzw. `~/.local/bin`, wenn dort kein Schreibrecht besteht). Derselbe Befehl
aktualisiert eine vorhandene Installation.

Zwei Schalter, damit es sich in Dockerfiles und Agent-Container einbauen lässt:

```bash
# Feste Version statt "latest" — für reproduzierbare Builds
curl -fsSL .../install.sh | IMMOJUMP_VERSION=v0.3.0 sh

# Eigenes Zielverzeichnis, ohne sudo
curl -fsSL .../install.sh | IMMOJUMP_INSTALL_DIR=$HOME/bin sh
```

### Ohne Skript: direkter Download

Die Release-URLs sind stabil und brauchen keine Anmeldung — praktisch für
`Dockerfile`, `Makefile` oder ein Ansible-Playbook:

```
https://github.com/immoJUMP/immojump-cli/releases/latest/download/immojump-linux-amd64
https://github.com/immoJUMP/immojump-cli/releases/latest/download/immojump-linux-arm64
https://github.com/immoJUMP/immojump-cli/releases/latest/download/immojump-darwin-arm64
https://github.com/immoJUMP/immojump-cli/releases/latest/download/immojump-darwin-amd64
```

Für eine feste Version `latest/download` durch `download/v0.3.0` ersetzen. Die
Checksummen liegen als `SHA256SUMS.txt` am selben Release.

```dockerfile
ARG IMMOJUMP_VERSION=v0.3.0
RUN curl -fsSL -o /usr/local/bin/immojump \
      "https://github.com/immoJUMP/immojump-cli/releases/download/${IMMOJUMP_VERSION}/immojump-linux-amd64" \
 && chmod +x /usr/local/bin/immojump
```

### Aus dem Quellcode

```bash
go install github.com/immoJUMP/immojump-cli/cmd/immojump@latest
```

Oder im Klon `make install` (nach `$GOBIN` bzw. `$GOPATH/bin`).
`make release-build` baut alle vier Binaries nach `dist/`.

## Schnellstart

Token unter **Einstellungen → API-Zugang** erzeugen, dann einen Context anlegen.
Contexts funktionieren wie bei `kubectl`: eine Instanz plus eine Organisation
plus ein Token, gespeichert in `~/.config/immojump/config.json` (Modus 0600).

```bash
immojump auth login \
  --context prod \
  --base-url https://immojump.de \
  --organisation <organisations-id> \
  --token <api-token>

immojump auth status            # aufgelöste Konfiguration, Token maskiert
immojump auth status --full     # dazu die komplette Antwort von /api/user/me
immojump context list           # alle Contexts
immojump context use beta       # Instanz/Organisation wechseln
```

`auth login` und `auth status` antworten kompakt: Context, Instanz,
Organisation, maskiertes Token, Herkunft des Tokens, `user` mit `id` und
`username` sowie die eigene Rolle in der aktiven Organisation. Das vollständige
Nutzerobjekt (Abo-Daten, Login-Zähler, alle Organisationen) gibt es auf Wunsch
mit `--full`.

`auth login` prüft die Angaben gegen `GET /api/user/me` und speichert erst nach
erfolgreicher Prüfung. Wer das Token nicht im Klartext ablegen will, verweist
stattdessen auf eine Umgebungsvariable:

```bash
immojump auth login --context beta --base-url https://beta.immojump.de \
  --organisation <org-id> --token-env IMMOJUMP_BETA_TOKEN
```

Ohne Context geht es auch rein über die Umgebung — identisch zum MCP-Server:

```bash
export IMMOJUMP_TOKEN=…
export IMMOJUMP_ORGANISATION_ID=…
export IMMOJUMP_BASE_URL=https://immojump.de   # optional
```

Auflösungsreihenfolge je Feld: **Flag > Umgebungsvariable > Context-Datei.**

## Bedienung

Jeder Befehl folgt demselben Muster: `immojump <ressource> <befehl> [args] [flags]`.
Globale Flags dürfen vor oder nach dem Befehl stehen.

```bash
# Lesen, auf wenige Felder projiziert — spart Kontext im Agenten
immojump immobilien list -q slim=true --fields id,name
immojump contacts get 42 --pretty

# Query-Parameter — welche eine Route auswertet, steht in ihrer Hilfe
immojump immobilien search -q search=Köln -q per_page=10

# Schreiben: einzelne Felder …
immojump contacts create --set first_name=Ada --set last_name=Lovelace

# … oder ein kompletter Body (JSON, @datei oder - für stdin)
immojump immobilien create --body @neubau.json
echo '{"name":"MFH Köln"}' | immojump immobilien create --body -

# Escape-Hatch für alles ohne kuratierten Befehl
immojump api GET /api/deals -q status_ids=7
```

`--set` interpretiert den Wert als JSON-Literal, sonst als String:
`--set aktiv=true` wird ein Boolean, `--set kaufpreis=225000` eine Zahl,
`--set name=MFH` ein String. Wer eine Zahl als String braucht, schreibt
`--set plz='"50667"'`. Zahlen bleiben dabei exakt — auch 20-stellige IDs und
`225000.00`.

Feldnamen kommen vom Backend (Marshmallow lehnt Unbekanntes ab). Welche es
gibt, zeigt am schnellsten ein `immojump <ressource> get <id> --pretty`; die
Beispiele in `--help` und in [`REFERENCE.md`](REFERENCE.md) sind gegen die
Backend-Schemas geprüft.

Nutzdaten gehen nach **stdout**, Fehler als eine JSON-Zeile nach **stderr**.
Antworten, die kein JSON sind (z. B. `pipelines export`), werden roh
durchgereicht.

## Kontext sparen

Zwei Schalter entscheiden darüber, ob eine Antwort ein Absatz oder ein halber
Kontext ist. Gemessen an 24 echten Immobilien in der Produktion:

| Aufruf                                                  | Zeichen |
| ------------------------------------------------------- | ------: |
| `immojump immobilien list`                              | 125.598 |
| `immojump immobilien list -q slim=true`                 |  19.604 |
| `immojump immobilien list -q slim=true --fields id,name` |   3.002 |

Faktor 42 — für dieselbe Frage.

- **`-q slim=true`** lässt das Backend ein reduziertes Feldset dumpen. Es gibt
  ihn bei `immobilien list` und `contacts list`; `statuses list` heißt das
  Gegenstück `-q lite=true`.
- **`--fields id,name`** projiziert danach auf die Felder, die wirklich
  gebraucht werden — auch verschachtelt (`--fields id,adresse.stadt`).

Welche Query-Parameter eine Route **tatsächlich** auswertet, steht unter
„Bekannte Query-Parameter" in `immojump <ressource> <befehl> --help`, in
[`REFERENCE.md`](REFERENCE.md) und im Schema (`query_hints`). Das ist keine
Kosmetik: `-q limit=3` etwa wird von `immobilien list` stillschweigend ignoriert
— begrenzt wird dort mit `-q page=1 -q per_page=3`.

Zwei Fallstricke, die das Backend nicht meldet:

- `slim` wirkt bei `immobilien list` **nur ohne** `page`. Sobald paginiert wird,
  kommt wieder das volle Feldset.
- Der Suchparameter heißt nicht überall gleich: `immobilien search` liest
  `search`, `contacts list` und `activities list` lesen `q`.

Zeigt `--fields` ins Leere, sagt das CLI das auf stderr — mit den
Top-Level-Schlüsseln, die es stattdessen gibt. Verpackte Antworten
(`{"contact":{…},"success":true}`) brauchen `--fields contact.id`:

```console
$ immojump contacts create --set first_name=Ada --fields id,first_name
{}
{"warning":true,"message":"--fields hat nichts getroffen: … Vorhandene Top-Level-Schlüssel: contact, success. …","fields_missing":["id","first_name"],"top_level_keys":["contact","success"]}
```

Der Exit-Code bleibt dabei `0` — es ist ein Hinweis, kein Fehler.

## Für Agenten

Der übliche Ablauf ist Entdecken → Prüfen → Ausführen:

```bash
immojump --help                       # welche Ressourcen gibt es?
immojump documents --help             # welche Befehle hat documents?
immojump documents upload --help      # welche Argumente, welches Risk?
immojump documents upload expose.pdf --immobilie-id 5
```

`immojump schema` liefert dieselbe Information maschinenlesbar — inklusive
Risk-Level, Argumenten, Flags und der Exit-Code-Tabelle.

### Vollständiges Beispiel: Unterlagen für die Bank freigeben

Eine Immobilie plus zwei Dokumente in einem passwortgeschützten Link
zusammenfassen, der Ende September abläuft, und ihn direkt verschicken:

```bash
# 1. Die Immobilie und ihre Dokumente finden
# search antwortet als Envelope {items, pagination} — die Treffer stehen
# unter items, nicht auf oberster Ebene.
IMMO=$(immojump immobilien search -q search="Hauptstraße 12" \
       | python3 -c 'import json,sys; print(json.load(sys.stdin)["items"][0]["id"])')

immojump documents list -q immobilien_id=$IMMO --fields id,dateiname

# 2. Freigabe-Link erzeugen
immojump shares create \
  --immobilie "$IMMO" \
  --dokument 118 \
  --dokument 119 \
  --title "Finanzierungsunterlagen Hauptstraße 12" \
  --note "Anbei die Unterlagen zur Finanzierungsanfrage." \
  --password bank2026 \
  --expires-at 2026-09-30 \
  --show-key-facts \
  --recipient-email finanzierung@beispielbank.de \
  --send-email \
  --fields id,url,status,expires_at

# 3. Später prüfen, wer den Link geöffnet hat
immojump shares list --entity-type immobilie --entity-id "$IMMO" \
  --fields id,title,access_count,last_accessed_at,status

# 4. Ablauf verlängern (nur gesetzte Flags werden geschickt)
immojump shares update 7 --expires-at 2026-12-31

# 5. Passwortschutz entfernen oder Link widerrufen
immojump shares update 7 --remove-password
immojump shares revoke 7
```

`--expires-at 2026-09-30` wird zu `2026-09-30T23:59:59` normalisiert —
„gültig bis einschließlich", dieselbe Semantik wie in der Web-App.
Vollständige ISO-Zeitstempel gehen unverändert durch.

`shares update` folgt der Sent-Keys-Semantik des Backends: Nur die Flags, die
tatsächlich gesetzt wurden, landen im PATCH-Body. `--note ""` schickt einen
leeren String (löscht die Nachricht), `--remove-password` schickt
`"password": null`.

## Sicherheit

**Base-URL-Allowlist.** Tokens gehen nur an bekannte immoJUMP-Instanzen —
`https://immojump.de`, `https://beta.immojump.de`, `http://localhost:8081`. Das
schützt gegen Tippfehler und Look-alike-Hosts, die ein Agent sich ausdenkt.
Eigene Instanzen (White-Label) ergänzt man per
`IMMOJUMP_EXTRA_BASE_URLS=https://kunde.de` (der MCP-kompatible Alias
`ALLOWED_BASE_URLS_EXTRA` wird ebenfalls akzeptiert).

**Risk-Level.** Jeder Befehl trägt eines von vier Levels, sichtbar in `--help`,
`docs` und `schema`:

| Level         | Beispiel                                            |
| ------------- | --------------------------------------------------- |
| `read`        | `immobilien list`, `shares list`                    |
| `write`       | `contacts create`, `shares revoke`                  |
| `external`    | `shares create`, `shares update` — wirkt nach außen |
| `destructive` | `immobilien delete`, `documents delete`             |

Begrenzen lässt sich das pro Aufruf oder pro Umgebung:

```bash
immojump --readonly immobilien list             # nur read
immojump --allow read,write contacts create …   # Liste erlaubter Level
export IMMOJUMP_ALLOW=read,write                # dasselbe für die ganze Session
```

Ein blockierter Befehl endet mit Exit 6 und `code: "POLICY_BLOCKED"` auf stderr —
und setzt keinen Request ab.

**Fail-closed:** Eine gesetzte, aber leere Liste (`--allow ""`, `--allow ","`)
und ein unbekanntes Level sind Konfigurationsfehler (Exit 3), kein „dann eben
alles". Ohne jede Angabe bleibt alles erlaubt. Sonst würde ein leerer
Shell-Ausdruck wie `--allow "$LEVELS"` die Policy lautlos abschalten.

`shares revoke` ist bewusst `write` und nicht `destructive`: Der Widerruf ist
der sichere Ausweg und muss auch für vorsichtig konfigurierte Agenten
erreichbar bleiben.

`shares update` ist dagegen `external` und nicht `write`: Es kann einen
abgelaufenen Link durch ein neues `expires_at` wieder aufmachen und per
`--remove-password` den Passwortschutz entfernen — beides macht Inhalte
genauso nach außen sichtbar wie das Erzeugen.

Auch der Escape-Hatch hält sich daran. `api <METHOD> <pfad>` trägt kein festes
Level: Methode und Pfad werden gegen die Registry gematcht, ein Treffer bringt
dessen Risk mit. Ohne Treffer gilt konservativ GET/HEAD = `read`,
DELETE = `destructive`, alles andere = `external`. So kommt
`--allow read,write api POST /api/share-links …` nicht an der Sperre für
`shares create` vorbei.

**Ehrlicher Rahmen:** Diese Policy ist Schutz gegen Versehen, keine
Sicherheitsgrenze. Ein Agent mit uneingeschränktem Token kann die API auch
direkt aufrufen. Die echte Grenze sind serverseitige Token-Scopes — das ist
Backend-Roadmap, nicht Aufgabe dieses CLI.

## Exit-Codes

Agenten sollen auf Exit-Codes branchen statt Meldungen zu parsen.

| Exit | Bedeutung                                        |
| ---: | ------------------------------------------------ |
|  `0` | Erfolg                                           |
|  `1` | sonstiger API-Fehler, plus lokale Fehler         |
|  `2` | Usage-Fehler (unbekannter Befehl, Flag, Args)    |
|  `3` | lokale Konfiguration/Auth unvollständig          |
|  `4` | 401 — Token fehlt oder ist ungültig              |
|  `5` | 404 — nicht gefunden                             |
|  `6` | 403 — keine Berechtigung, oder Policy-Block      |
|  `7` | 429 — Rate Limit, später erneut versuchen        |
|  `8` | 5xx oder Netzwerkfehler — temporär, Retry möglich |
|  `9` | 409 — Konflikt (z. B. widerrufener Share-Link)   |
| `11` | 400/422 — Validierung fehlgeschlagen             |

Exit 8 ist eine Zusage: 5xx, Netzwerkabbruch, Timeout — ein Retry kann helfen.
Lokale Fehler (kaputtes stdout, volle Platte) enden mit Exit 1, weil ein Retry
daran nichts ändert.

Die Fehlerzeile auf stderr beginnt immer gleich; `message` kommt unverändert
vom Backend:

```json
{"error":true,"status":403,"message":"Kein Zugriff auf diese Organisation","code":"ORG_FORBIDDEN"}
```

**Danach folgt jedes weitere Feld, das die Antwort mitbringt** — und genau darin
steht meistens die Lösung. Ein falscher Enum-Wert kostet so keinen zweiten
Rateversuch:

```console
$ immojump activities create --set title=Test --set type=task
{"error":true,"status":400,"message":"Validierungsfehler.","errors":{"type":["Invalid enum value task"]},"valid_values":{"type":["ANRUF","BESICHTIGUNG","BRIEF","E-MAIL","MEETING","NOTIZ","SONSTIGES"]}}
```

Dasselbe gilt für `errors: {"contact_id": ["Unknown field."]}` bei einem
falschen Feldnamen und für den Kontingentstand einer 402-Antwort. Die vier
CLI-eigenen Schlüssel (`error`, `status`, `message`, `code`) behalten ihre
Bedeutung; kollidiert ein Backend-Feld mit `error` oder `status`, steht es
daneben als `backend_error` bzw. `backend_status` — verloren geht nichts.

Antworten, die kein JSON sind, werden nicht mehr als HTML-Wüste durchgereicht:

```json
{"error":true,"status":404,"message":"HTTP 404 Not Found — die Route existiert auf dieser Instanz nicht oder erlaubt diese Methode nicht (Antwort war kein JSON)","raw":"404 Not Found Not Found The requested URL was not found on the server."}
```

Ob das CLI den falschen Pfad gebaut hat oder die Route auf der Instanz fehlt,
klärt `--verbose`. Es schreibt Methode und vollständige URL vor dem Request
nach stderr — ohne Token, der steht im Header:

```console
$ immojump immobilien list -q slim=true --verbose
{"trace":true,"method":"GET","url":"https://immojump.de/api/v2/immobilien?slim=true"}
```

## Weiterlesen

- [`REFERENCE.md`](REFERENCE.md) — vollständige Befehlsreferenz, erzeugt mit
  `make docs`.
- [`doc/DESIGN.md`](doc/DESIGN.md) — warum das CLI so gebaut ist, wie es gebaut
  ist: Architektur, Abgrenzung zum MCP-Server, Teststrategie.

## Entwicklung

Go 1.24+, **ausschließlich Standardbibliothek** — keine externen Dependencies,
`go.sum` bleibt leer. Kein cobra: Der deklarative Befehlsbaum in
`internal/cli/registry.go` erzeugt Dispatch, Hilfe, Referenz und Schema selbst.

```bash
make ci        # das komplette Gate: lint + test + docs-check (wie in der CI)
make build     # Binary nach bin/immojump
make test      # go test ./...
make lint      # gofmt-Prüfung + go vet
make docs      # REFERENCE.md neu erzeugen
```

Releases laufen über einen Workflow (`.github/workflows/ci.yml`):
`test` → `release-please` (nur auf `main`) → `release-assets`. Der letzte Job
baut die Cross-Binaries, schreibt `SHA256SUMS.txt` und hängt beides ans
GitHub-Release.

Gearbeitet wird test-getrieben: Erst der Testfall, dann die Implementierung.
Ein neuer Endpoint ist eine Spec-Zeile in der Registry plus eine Zeile in der
Tabelle in `internal/cli/request_table_test.go`. Ein generischer Durchlauf über
die komplette Registry stellt sicher, dass jeder Pfad-Platzhalter durch
Argumente oder `{org}` gedeckt ist, dass Zusammenfassungen und Risk-Level
gesetzt sind und dass Flag-Namen konsistent bleiben.

## Lizenz

Siehe [`LICENSE`](LICENSE).
