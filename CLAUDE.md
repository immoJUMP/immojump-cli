# CLAUDE.md

Hinweise für Claude Code (claude.ai/code) und andere Agenten, die in diesem
Repository arbeiten.

## Was das hier ist

`immojump-cli` ist ein Kommandozeilen-Client für die immoJUMP-API: eine
statische Go-Binary, gebaut für Agenten-Runtimes und Skripte. Es ist die
CLI-Schwester von `mcp-immojump` — derselbe API-Vertrag, aber progressive
Discovery über `--help` statt 87 Tool-Schemas im Kontext.

Die verbindliche Spezifikation steht in [`doc/DESIGN.md`](doc/DESIGN.md):
Architektur, Contexts, Allowlist, Risk-Level, Exit-Codes, Befehlsumfang,
Teststrategie. Bei Zweifeln gilt dieses Dokument.

## Befehle

```bash
make ci        # das komplette Gate: lint + test + docs-check (wie in der CI)
make build     # Binary nach bin/immojump
make test      # go test ./...
make lint      # gofmt-Prüfung + go vet
make docs      # REFERENCE.md aus der Registry neu erzeugen (eingecheckt!)
make docs-check # erzeugt REFERENCE.md und lehnt Drift ab
make install   # nach $GOBIN
make release-build  # Cross-Builds nach dist/

go test ./internal/cli -run TestCommandRequestTable   # einzelner Test
go test ./internal/cli -run 'TestShares.*' -v         # Muster
```

Vor jedem Commit: **`make ci`** muss grün sein — das deckt `gofmt -l .` (leer),
`go vet ./...`, `go test ./...` und ein frisch erzeugtes, unverändertes
`REFERENCE.md` ab.

Der Release-Fluss läuft in einem Workflow (`.github/workflows/ci.yml`):
`test` → `release-please` (nur auf `main`) → `release-assets` (nur wenn ein
Release entstanden ist). Ein zweiter Testlauf vor dem Asset-Build entfällt
bewusst: Derselbe Run hat `test` schon bestanden.

## Harte Regeln

- **Nur die Go-Standardbibliothek.** Keine externen Dependencies, `go.sum`
  bleibt leer. Kein cobra, kein viper, kein testify.
- **TDD.** Erst der Testfall (rot), dann die Implementierung (grün). Das gilt
  auch für Bugfixes: Zuerst ein Test, der den Fehler zeigt.
- **Alles auf Deutsch**: Hilfetexte, Summaries, Fehlermeldungen, Kommentare.
  Zielgruppe sind Agenten und Entwickler, nicht Endkunden — knapp und präzise.
- **Backend-Meldungen unverändert durchreichen.** Das `message`-Feld aus
  `api_error()` wird nie umformuliert oder übersetzt.
- **stdout ist für Nutzdaten, stderr für alles andere.** Bei Erfolg bleibt
  stderr leer.

## Architektur

```
cmd/immojump/main.go   → hängt die Umgebung ein, os.Exit(cli.Run(...))
internal/cli/          → Registry (Command-Specs), Dispatch, Flag-/Body-Bau,
                         help, docs, schema, auth, context
internal/config/       → Contexts (kubectl-Analogie), Auflösungskette, Allowlist
internal/api/          → HTTP: Bearer + X-Organisation-Id, Multipart,
                         Fehler-Mapping auf Exit-Codes
internal/output/       → stdout-Rendering, --pretty, --fields-Projektion
```

Die Abhängigkeiten zeigen nur nach unten: `cli` → `api` → `config`, plus
`cli` → `output`. `config` und `output` kennen niemanden.

### Die Registry ist die einzige Quelle

`internal/cli/registry.go` beschreibt jeden Befehl als Datensatz:

```go
{Resource: "contacts", Verb: "get", Method: "GET",
 Path: "/api/contacts/{id}", Args: idArg("ID des Kontakts"),
 Risk: RiskRead, Summary: "Einen Kontakt laden",
 Example: "immojump contacts get 42"}
```

Daraus entstehen **alle** vier Ausgaben: Dispatch, `--help` auf jeder Ebene,
`immojump docs` (→ REFERENCE.md) und `immojump schema` (JSON). Wer eine dieser
Ausgaben ändern will, ändert die Registry — nicht den Renderer.

Platzhalter im Pfad werden aus gleichnamigen Argumenten gefüllt; `{org}` kommt
aus der aufgelösten Organisation. Fehlt sie, endet der Aufruf mit Exit 3, bevor
ein Request rausgeht.

## Konvention: neuer Endpoint = Spec-Zeile + Test-Zeile

Ein neuer API-Endpoint braucht genau zwei Ergänzungen:

1. eine `Spec` in `internal/cli/registry.go`,
2. eine Zeile in der Tabelle in `internal/cli/request_table_test.go`
   (argv → erwartete Methode, Pfad, Query, Body).

Danach `make docs` laufen lassen. **Keine Business-Logik im CLI** — Validierung,
Berechnung und Regeln gehören ins immoJUMP-Backend. Wenn ein Befehl hier
"schlau" werden will, ist das ein Hinweis darauf, dass im Backend etwas fehlt.

Ausnahmen sind die wenigen `Special`-Fälle (Multipart-Upload, YAML-Import,
rohes Tag-Array, Freigabe-Sugar). Sie stehen als Konstanten in der Registry und
werden im Dispatcher behandelt — nicht als anonyme Funktionen in der Spec, sonst
lassen sich `docs` und `schema` nicht mehr generieren.

Kuratierte Sugar-Flags gehen so weit wie möglich deklarativ über `Body` bzw.
`Query` in der Spec (Flag → Body-Pfad, Flag → Query-Parameter). Erst wenn das
nicht reicht — Listen bauen, `null` schicken, Werte normalisieren — kommt ein
`Special` dazu.

## Tests

- `internal/config` — Auflösungskette, Allowlist, Datei-Rechte.
- `internal/api` — `httptest`: Header, Fehler-Mapping, Multipart, Allowlist.
- `internal/output` — kompakt vs. `--pretty`, `--fields`-Projektion.
- `internal/cli` — Tabellen-Test argv → Request für jeden Befehl, dazu
  Registry-Invarianten, Policy, Exit-Codes, Hilfe/Docs/Schema, auth/context.

Die komplette Umgebung ist injizierbar (`cli.Options` mit `Stdin`, `Stdout`,
`Stderr`, `Getenv`, `HTTP`). Tests dürfen weder echte Umgebungsvariablen noch
die echte Konfigurationsdatei anfassen — `IMMOJUMP_CONFIG` zeigt im Test immer
auf ein `t.TempDir()`.

## Wenn sich das Backend ändert

Ändern sich Routen, Request-/Response-Schemas oder Feldnamen im
immo-calc-Backend, gehört dieses Repo mit nachgezogen — genauso wie
`mcp-immojump`, `n8n-nodes-immojump` und `immokalkulation-chrome`. Die
Registry-Zeile und die Testzeile sind der einzige Ort, an dem der Vertrag steht.
