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
```

Beide Wege bleiben gültig: Der MCP-Server ist die richtige Wahl, wenn ein Agent
dauerhaft mit immoJUMP arbeitet und Tool-Calls braucht. Das CLI ist die richtige
Wahl in Shell-Umgebungen, Skripten, CI und überall dort, wo Kontext teuer ist.

## Installation

Solange das Repo privat ist:

```bash
git clone git@github.com:immoJUMP/immojump-cli.git
cd immojump-cli
make install          # nach $GOBIN bzw. $GOPATH/bin
```

Oder direkt über `go install` — dafür muss Go wissen, dass es den Modulproxy
umgehen soll:

```bash
export GOPRIVATE=github.com/immoJUMP/*
go install github.com/immoJUMP/immojump-cli/cmd/immojump@latest
```

Für Agent-Container (Linux) baut `make release-build` fertige Binaries nach
`dist/` — linux/amd64, linux/arm64, darwin/arm64, darwin/amd64.

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
immojump context list           # alle Contexts
immojump context use beta       # Instanz/Organisation wechseln
```

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
immojump immobilien list --fields id,name,type
immojump contacts get 42 --pretty

# Query-Parameter
immojump immobilien search -q q=Köln -q limit=10

# Schreiben: einzelne Felder …
immojump contacts create --set first_name=Ada --set last_name=Lovelace

# … oder ein kompletter Body (JSON, @datei oder - für stdin)
immojump immobilien create --body @neubau.json
echo '{"name":"MFH Köln"}' | immojump immobilien create --body -

# Escape-Hatch für alles ohne kuratierten Befehl
immojump api GET /api/deals -q status=offen
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
IMMO=$(immojump immobilien search -q q="Hauptstraße 12" --fields id \
       | python3 -c 'import json,sys; print(json.load(sys.stdin)[0]["id"])')

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

Die Fehlerzeile auf stderr sieht immer gleich aus; `message` kommt unverändert
vom Backend:

```json
{"error":true,"status":403,"message":"Kein Zugriff auf diese Organisation","code":"ORG_FORBIDDEN"}
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
