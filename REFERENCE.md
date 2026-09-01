# immojump — Befehlsreferenz

Erzeugt mit `make docs` (`immojump docs`) aus der Command-Registry.
Nicht von Hand bearbeiten — neue Endpoints entstehen als Spec-Zeile in
`internal/cli/registry.go`.

## Ressourcen

| Ressource | Beschreibung |
| --- | --- |
| `auth` | Anmelden und die aufgelöste Konfiguration prüfen |
| `context` | Instanzen und Organisationen verwalten (wie kubectl) |
| `contacts` | Kontakte |
| `immobilien` | Immobilien |
| `units` | Einheiten einer Immobilie |
| `activities` | Aktivitäten und Aufgaben |
| `pipelines` | Pipelines und ihre Phasen |
| `statuses` | Status (Phasen) einzeln bearbeiten |
| `templates` | Aktivitäts-Vorlagen |
| `documents` | Dokumente hochladen, analysieren, verwalten |
| `tags` | Tags und ihre Zuordnung zu Objekten |
| `shares` | Freigabe-Links für Immobilien, Dokumente und Bilder |
| `email` | Postfach: Nachrichten lesen, sortieren und versenden |
| `api` | Beliebigen /api/-Pfad aufrufen (Escape-Hatch) |
| `docs` | Markdown-Referenz ausgeben — komplett oder für eine Ressource/einen Befehl |
| `schema` | Befehls-Schema als JSON ausgeben — komplett oder als Ausschnitt |
| `version` | Version ausgeben |

## Globale Flags

| Flag | Beschreibung |
| --- | --- |
| `--context <wert>` | Context aus der Konfiguration wählen |
| `--org <wert>` | Organisation überschreiben |
| `--base-url <wert>` | Instanz überschreiben (muss auf der Allowlist stehen) |
| `-q <wert>` | Query-Parameter key=value (wiederholbar) |
| `--set <wert>` | Body-Feld pfad=wert (wiederholbar, Wert als JSON-Literal sonst String) |
| `--body <wert>` | Kompletter Body: JSON, @datei oder - für stdin |
| `--fields <wert>` | Ausgabe auf Felder projizieren, z. B. id,adresse.stadt |
| `--pretty` | Ausgabe einrücken (für Menschen) |
| `--readonly` | Nur lesende Befehle zulassen |
| `--allow <wert>` | Erlaubte Risk-Level, z. B. read,write (Env: IMMOJUMP_ALLOW) |
| `--idempotency-key <wert>` | Wird als Idempotency-Key-Header mitgeschickt |
| `--timeout <wert>` | Timeout in Sekunden (Default 60) |
| `--verbose` | Methode und aufgerufene URL vor dem Request auf stderr zeigen |
| `--version` | Version ausgeben |
| `--help` | Hilfe zur jeweiligen Ebene ausgeben |

## Umgebungsvariablen

| Variable | Wirkung |
| --- | --- |
| `IMMOJUMP_TOKEN` | API-Token |
| `IMMOJUMP_ORGANISATION_ID` | Organisations-ID |
| `IMMOJUMP_BASE_URL` | Instanz (muss auf der Allowlist stehen) |
| `IMMOJUMP_CONTEXT` | Context aus der Konfiguration |
| `IMMOJUMP_CONFIG` | Pfad der Konfigurationsdatei |
| `IMMOJUMP_EXTRA_BASE_URLS` | zusätzlich erlaubte Instanzen (kommagetrennt) |
| `ALLOWED_BASE_URLS_EXTRA` | MCP-kompatibler Alias dazu |
| `IMMOJUMP_ALLOW` | erlaubte Risk-Level, wie `--allow` |

## Risk-Level und Policy

Jeder Befehl trägt ein Risk-Level:

| Level | Bedeutung |
| --- | --- |
| `read` | liest nur |
| `write` | ändert Daten in immoJUMP |
| `external` | macht etwas nach außen sichtbar (Freigabe-Link, E-Mail) |
| `destructive` | löscht Daten |

Begrenzen mit `--readonly` (nur `read`) oder `--allow read,write`;
dasselbe geht per `IMMOJUMP_ALLOW`. Ein blockierter Befehl endet mit
Exit 6 und `code: "POLICY_BLOCKED"` auf stderr.

Die Policy ist fail-closed: Eine gesetzte, aber leere Liste
(`--allow ""`, `IMMOJUMP_ALLOW=,`) und ein unbekanntes Level sind
Konfigurationsfehler (Exit 3), kein „dann eben alles". Ohne jede
Angabe bleibt alles erlaubt.

`api <METHOD> <pfad>` trägt kein festes Level: Risk kommt aus dem passenden Registry-Befehl (Methode + Pfad); ohne Treffer konservativ nach Methode: GET/HEAD = read, DELETE = destructive, alles andere = external.

Das ist Schutz gegen Versehen, keine Sicherheitsgrenze: Ein Agent mit
uneingeschränktem Token kann die API auch direkt aufrufen.

## Exit-Codes

| Exit | Bedeutung |
| ---: | --- |
| `0` | Erfolg |
| `1` | sonstiger API-Fehler (nicht unten gemappt) |
| `2` | Usage-Fehler (unbekannter Befehl, Flag, Args) |
| `3` | lokale Konfiguration/Auth unvollständig |
| `4` | 401 — Token fehlt oder ist ungültig |
| `5` | 404 — nicht gefunden |
| `6` | 403 — keine Berechtigung, oder lokal durch Policy blockiert |
| `7` | 429 — Rate Limit, später erneut versuchen |
| `8` | 5xx oder Netzwerkfehler — temporär, Retry möglich |
| `9` | 409 — Konflikt (z. B. widerrufener Share-Link) |
| `11` | 400/422 — Validierung fehlgeschlagen |

## Befehle

### auth

Anmelden und die aufgelöste Konfiguration prüfen

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `auth login` | read | `GET /api/user/me` |
| `auth status` | read | `GET /api/user/me` |

#### auth login

Context anlegen oder aktualisieren und gegen die Instanz prüfen

- **Aufruf:** `immojump auth login`
- **Endpoint:** `GET /api/user/me`
- **Risk:** `read`
- **Flags:**
  - `--token <wert>` — API-Token (Einstellungen → API-Zugang)
  - `--token-env <wert>` — Name der Env-Variablen mit dem Token (statt Klartext)
  - `--organisation <wert>` — Organisations-ID für diesen Context
  - `--full` — Vollständige Antwort von /api/user/me ausgeben statt id/username plus Rolle
- **Beispiel:** `immojump auth login --context prod --base-url https://immojump.de --organisation <org-id> --token <token>`

#### auth status

Aufgelöste Konfiguration zeigen und gegen /api/user/me prüfen

- **Aufruf:** `immojump auth status`
- **Endpoint:** `GET /api/user/me`
- **Risk:** `read`
- **Flags:**
  - `--full` — Vollständige Antwort von /api/user/me ausgeben statt id/username plus Rolle
- **Beispiel:** `immojump auth status`

### context

Instanzen und Organisationen verwalten (wie kubectl)

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `context list` | read | `lokal` |
| `context current` | read | `lokal` |
| `context use` | read | `lokal` |
| `context delete` | write | `lokal` |

#### context list

Alle konfigurierten Contexts auflisten

- **Aufruf:** `immojump context list`
- **Endpoint:** `lokal`
- **Risk:** `read`
- **Beispiel:** `immojump context list`

#### context current

Aktiven Context zeigen

- **Aufruf:** `immojump context current`
- **Endpoint:** `lokal`
- **Risk:** `read`
- **Beispiel:** `immojump context current`

#### context use

Aktiven Context wechseln

- **Aufruf:** `immojump context use <name>`
- **Endpoint:** `lokal`
- **Risk:** `read`
- **Argumente:**
  - `name` — Name des Contexts
- **Beispiel:** `immojump context use beta`

#### context delete

Context aus der lokalen Konfiguration entfernen

- **Aufruf:** `immojump context delete <name>`
- **Endpoint:** `lokal`
- **Risk:** `write`
- **Argumente:**
  - `name` — Name des Contexts
- **Beispiel:** `immojump context delete beta`

### contacts

Kontakte

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `contacts list` | read | `GET /api/contacts` |
| `contacts get` | read | `GET /api/contacts/{id}` |
| `contacts create` | write | `POST /api/contacts` |
| `contacts update` | write | `PUT /api/contacts/{id}` |
| `contacts set-status` | write | `PUT /api/contacts/{id}/status` |
| `contacts delete` | destructive | `DELETE /api/contacts/{id}` |
| `contacts activities` | read | `GET /api/contacts/{id}/activities` |
| `contacts immobilien` | read | `GET /api/contacts/{id}/immobilien` |

#### contacts list

Kontakte auflisten

- **Aufruf:** `immojump contacts list`
- **Endpoint:** `GET /api/contacts`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `slim` — true = ohne die Aktivitäten jedes Kontakts (deutlich kleiner und schneller)
  - `q` — Freitext über Name, E-Mail, Telefon, Firma, Rolle, Adresse
  - `page` — Seite (ab 1); erst damit kommt ein Envelope statt aller Treffer
  - `per_page` — Treffer pro Seite (Default 50, max. 200)
  - `sort` — last_name, first_name, email, created_at, updated_at, last_activity_at …
  - `order` — asc (Default) oder desc
  - `status_id` — nur diese Phase; none = Kontakte ohne Status
  - `tag_ids` — Tag-IDs, kommagetrennt oder wiederholt
  - `tag_match` — all (Default, alle Tags) oder any (mindestens einer)
- **Beispiel:** `immojump contacts list -q slim=true -q per_page=25 --fields items.id,items.first_name,items.last_name`

#### contacts get

Einen Kontakt laden

- **Aufruf:** `immojump contacts get <id>`
- **Endpoint:** `GET /api/contacts/{id}`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID des Kontakts
- **Beispiel:** `immojump contacts get 42`

#### contacts create

Kontakt anlegen

- **Aufruf:** `immojump contacts create`
- **Endpoint:** `POST /api/contacts`
- **Risk:** `write`
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump contacts create --set first_name=Ada --set last_name=Lovelace`

#### contacts update

Kontakt ändern

- **Aufruf:** `immojump contacts update <id>`
- **Endpoint:** `PUT /api/contacts/{id}`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID des Kontakts
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump contacts update 42 --set email=ada@example.com`

#### contacts set-status

Kontakt in eine andere Phase schieben

- **Aufruf:** `immojump contacts set-status <id>`
- **Endpoint:** `PUT /api/contacts/{id}/status`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID des Kontakts
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump contacts set-status 42 --set status_id=7`

#### contacts delete

Kontakt löschen

- **Aufruf:** `immojump contacts delete <id>`
- **Endpoint:** `DELETE /api/contacts/{id}`
- **Risk:** `destructive`
- **Argumente:**
  - `id` — ID des Kontakts
- **Beispiel:** `immojump contacts delete 42`

#### contacts activities

Aktivitäten eines Kontakts

- **Aufruf:** `immojump contacts activities <id>`
- **Endpoint:** `GET /api/contacts/{id}/activities`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID des Kontakts
- **Beispiel:** `immojump contacts activities 42`

#### contacts immobilien

Immobilien eines Kontakts

- **Aufruf:** `immojump contacts immobilien <id>`
- **Endpoint:** `GET /api/contacts/{id}/immobilien`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID des Kontakts
- **Beispiel:** `immojump contacts immobilien 42`

### immobilien

Immobilien

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `immobilien list` | read | `GET /api/v2/immobilien` |
| `immobilien search` | read | `GET /api/v2/immobilien/search` |
| `immobilien get` | read | `GET /api/v2/immobilien/{id}` |
| `immobilien create` | write | `POST /api/v2/immobilien` |
| `immobilien update` | write | `PUT /api/v2/immobilien/{id}` |
| `immobilien patch` | write | `PATCH /api/v2/immobilien/{id}` |
| `immobilien delete` | destructive | `DELETE /api/v2/immobilien/{id}` |
| `immobilien contacts` | read | `GET /api/v2/immobilien/{id}/contacts` |
| `immobilien duplicate` | write | `POST /api/v2/immobilien/{id}/duplicate` |

#### immobilien list

Immobilien auflisten

- **Aufruf:** `immojump immobilien list`
- **Endpoint:** `GET /api/v2/immobilien`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `slim` — true = reduziertes Feldset; die stärkste Ersparnis überhaupt (wirkt nur ohne page)
  - `page` — Seite (ab 1); liefert einen Envelope {items, pagination} — dann ohne slim
  - `per_page` — Treffer pro Seite (Default 20)
  - `sort` — created_at (Default), name, kaufpreis, wohnflaeche oder preis_pro_qm
  - `order` — desc (Default) oder asc
- **Beispiel:** `immojump immobilien list -q slim=true --fields id,name`

#### immobilien search

Immobilien suchen

- **Aufruf:** `immojump immobilien search`
- **Endpoint:** `GET /api/v2/immobilien/search`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `search` — Suchbegriff über Name und Adresse (heißt hier search, nicht q)
  - `tag_ids` — Tag-IDs, wiederholt angeben (?tag_ids=a&tag_ids=b); ODER-verknüpft
  - `status_ids` — Phasen-IDs, wiederholt angeben; ODER-verknüpft
  - `page` — Seite (Default 1); die Antwort ist immer {items, pagination}
  - `per_page` — Treffer pro Seite (Default 20)
  - `sort` — created_at (Default), name, kaufpreis, wohnflaeche oder preis_pro_qm
  - `order` — desc (Default) oder asc
- **Beispiel:** `immojump immobilien search -q search=Köln --fields items.id,items.name`

#### immobilien get

Eine Immobilie laden

- **Aufruf:** `immojump immobilien get <id>`
- **Endpoint:** `GET /api/v2/immobilien/{id}`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID der Immobilie
- **Beispiel:** `immojump immobilien get 5`

#### immobilien create

Immobilie anlegen

- **Aufruf:** `immojump immobilien create`
- **Endpoint:** `POST /api/v2/immobilien`
- **Risk:** `write`
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump immobilien create --set name='MFH Köln' --set type=MFH`

#### immobilien update

Immobilie vollständig ersetzen

- **Aufruf:** `immojump immobilien update <id>`
- **Endpoint:** `PUT /api/v2/immobilien/{id}`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID der Immobilie
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump immobilien update 5 --body @immobilie.json`

#### immobilien patch

Einzelne Felder einer Immobilie ändern

- **Aufruf:** `immojump immobilien patch <id>`
- **Endpoint:** `PATCH /api/v2/immobilien/{id}`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID der Immobilie
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump immobilien patch 5 --set kaufpreis=239000`

#### immobilien delete

Immobilie löschen

- **Aufruf:** `immojump immobilien delete <id>`
- **Endpoint:** `DELETE /api/v2/immobilien/{id}`
- **Risk:** `destructive`
- **Argumente:**
  - `id` — ID der Immobilie
- **Beispiel:** `immojump immobilien delete 5`

#### immobilien contacts

Kontakte zu einer Immobilie

- **Aufruf:** `immojump immobilien contacts <id>`
- **Endpoint:** `GET /api/v2/immobilien/{id}/contacts`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID der Immobilie
- **Beispiel:** `immojump immobilien contacts 5`

#### immobilien duplicate

Immobilie duplizieren

- **Aufruf:** `immojump immobilien duplicate <id>`
- **Endpoint:** `POST /api/v2/immobilien/{id}/duplicate`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID der Immobilie
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump immobilien duplicate 5`

### units

Einheiten einer Immobilie

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `units list` | read | `GET /api/units/immobilie/{immobilie-id}/units` |
| `units create` | write | `POST /api/units/unit/{immobilie-id}` |
| `units update` | write | `PUT /api/units/unit/{unit-id}` |
| `units delete` | destructive | `DELETE /api/units/unit/{unit-id}` |

#### units list

Einheiten einer Immobilie auflisten

- **Aufruf:** `immojump units list <immobilie-id>`
- **Endpoint:** `GET /api/units/immobilie/{immobilie-id}/units`
- **Risk:** `read`
- **Argumente:**
  - `immobilie-id` — ID der Immobilie
- **Beispiel:** `immojump units list 5`

#### units create

Einheit zu einer Immobilie anlegen

- **Aufruf:** `immojump units create <immobilie-id>`
- **Endpoint:** `POST /api/units/unit/{immobilie-id}`
- **Risk:** `write`
- **Argumente:**
  - `immobilie-id` — ID der Immobilie
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump units create 5 --set einheit='WE 1'`

#### units update

Einheit ändern

- **Aufruf:** `immojump units update <unit-id>`
- **Endpoint:** `PUT /api/units/unit/{unit-id}`
- **Risk:** `write`
- **Argumente:**
  - `unit-id` — ID der Einheit
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump units update 9 --set ist_rent=780`

#### units delete

Einheit löschen

- **Aufruf:** `immojump units delete <unit-id>`
- **Endpoint:** `DELETE /api/units/unit/{unit-id}`
- **Risk:** `destructive`
- **Argumente:**
  - `unit-id` — ID der Einheit
- **Beispiel:** `immojump units delete 9`

### activities

Aktivitäten und Aufgaben

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `activities list` | read | `GET /api/activities/activities` |
| `activities get` | read | `GET /api/activities/activities/{id}` |
| `activities for-immobilie` | read | `GET /api/activities/activities/immobilie/{immobilie-id}` |
| `activities create` | write | `POST /api/activities/activities` |
| `activities update` | write | `PUT /api/activities/activities/{id}` |
| `activities delete` | destructive | `DELETE /api/activities/activities/{id}` |

#### activities list

Aktivitäten auflisten

- **Aufruf:** `immojump activities list`
- **Endpoint:** `GET /api/activities/activities`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `q` — Freitext über Titel, Beschreibung, Typ, Status, Priorität
  - `type` — ANRUF, BESICHTIGUNG, BRIEF, E-MAIL, MEETING, NOTIZ, SONSTIGES (mehrere kommagetrennt)
  - `status` — Geplant, In Bearbeitung, Abgeschlossen, Abgebrochen (mehrere kommagetrennt)
  - `priority` — Hoch, Mittel, Niedrig, NA (mehrere kommagetrennt)
  - `immobilie` — nur Aktivitäten dieser Immobilie (der Parameter heißt immobilie)
  - `overdue` — true = offene Aktivitäten, deren Fälligkeit vorbei ist
  - `due` — today oder week — Fälligkeitsfenster, nur offene Aktivitäten
  - `page` — Seite (ab 1); erst damit kommt ein Envelope statt aller Treffer
  - `per_page` — Treffer pro Seite (Default 25, max. 200)
- **Beispiel:** `immojump activities list -q overdue=true --fields id,title,scheduled_end`

#### activities get

Eine Aktivität laden

- **Aufruf:** `immojump activities get <id>`
- **Endpoint:** `GET /api/activities/activities/{id}`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID der Aktivität
- **Beispiel:** `immojump activities get 3`

#### activities for-immobilie

Aktivitäten zu einer Immobilie

- **Aufruf:** `immojump activities for-immobilie <immobilie-id>`
- **Endpoint:** `GET /api/activities/activities/immobilie/{immobilie-id}`
- **Risk:** `read`
- **Argumente:**
  - `immobilie-id` — ID der Immobilie
- **Beispiel:** `immojump activities for-immobilie 5`

#### activities create

Aktivität anlegen

- **Aufruf:** `immojump activities create`
- **Endpoint:** `POST /api/activities/activities`
- **Risk:** `write`
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump activities create --set title='Makler anrufen' --set type=ANRUF --set immobilien_id=5`

#### activities update

Aktivität ändern

- **Aufruf:** `immojump activities update <id>`
- **Endpoint:** `PUT /api/activities/activities/{id}`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID der Aktivität
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump activities update 3 --set status=Abgeschlossen`

#### activities delete

Aktivität löschen

- **Aufruf:** `immojump activities delete <id>`
- **Endpoint:** `DELETE /api/activities/activities/{id}`
- **Risk:** `destructive`
- **Argumente:**
  - `id` — ID der Aktivität
- **Beispiel:** `immojump activities delete 3`

### pipelines

Pipelines und ihre Phasen

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `pipelines list` | read | `GET /api/pipelines/{org}/pipelines` |
| `pipelines create` | write | `POST /api/pipelines/{org}/pipelines` |
| `pipelines get` | read | `GET /api/pipelines/pipelines/{id}` |
| `pipelines update` | write | `PUT /api/pipelines/pipelines/{id}` |
| `pipelines delete` | destructive | `DELETE /api/pipelines/pipelines/{id}` |
| `pipelines statuses` | read | `GET /api/pipelines/pipelines/{id}/statuses` |
| `pipelines add-status` | write | `POST /api/pipelines/pipelines/{id}/statuses` |
| `pipelines export` | read | `GET /api/pipelines/pipelines/{id}/export` |
| `pipelines import` | write | `POST /api/pipelines/pipelines/import` |

#### pipelines list

Pipelines der Organisation auflisten

- **Aufruf:** `immojump pipelines list`
- **Endpoint:** `GET /api/pipelines/{org}/pipelines`
- **Risk:** `read`
- **Beispiel:** `immojump pipelines list`

#### pipelines create

Pipeline anlegen

- **Aufruf:** `immojump pipelines create`
- **Endpoint:** `POST /api/pipelines/{org}/pipelines`
- **Risk:** `write`
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump pipelines create --set name=Ankauf --set entity_type=immobilie`

#### pipelines get

Eine Pipeline laden

- **Aufruf:** `immojump pipelines get <id>`
- **Endpoint:** `GET /api/pipelines/pipelines/{id}`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID der Pipeline
- **Beispiel:** `immojump pipelines get 2`

#### pipelines update

Pipeline ändern

- **Aufruf:** `immojump pipelines update <id>`
- **Endpoint:** `PUT /api/pipelines/pipelines/{id}`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID der Pipeline
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump pipelines update 2 --set name='Ankauf 2026'`

#### pipelines delete

Pipeline löschen

- **Aufruf:** `immojump pipelines delete <id>`
- **Endpoint:** `DELETE /api/pipelines/pipelines/{id}`
- **Risk:** `destructive`
- **Argumente:**
  - `id` — ID der Pipeline
- **Beispiel:** `immojump pipelines delete 2`

#### pipelines statuses

Phasen einer Pipeline

- **Aufruf:** `immojump pipelines statuses <id>`
- **Endpoint:** `GET /api/pipelines/pipelines/{id}/statuses`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID der Pipeline
- **Beispiel:** `immojump pipelines statuses 2`

#### pipelines add-status

Phase zu einer Pipeline hinzufügen

- **Aufruf:** `immojump pipelines add-status <id>`
- **Endpoint:** `POST /api/pipelines/pipelines/{id}/statuses`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID der Pipeline
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump pipelines add-status 2 --set name='Besichtigung'`

#### pipelines export

Pipeline als YAML exportieren

- **Aufruf:** `immojump pipelines export <id>`
- **Endpoint:** `GET /api/pipelines/pipelines/{id}/export`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID der Pipeline
- **Antwort:** möglicherweise kein JSON — wird roh nach stdout geschrieben.
- **Beispiel:** `immojump pipelines export 2 > ankauf.yaml`

#### pipelines import

Pipeline aus YAML importieren

- **Aufruf:** `immojump pipelines import`
- **Endpoint:** `POST /api/pipelines/pipelines/import`
- **Risk:** `write`
- **Flags:**
  - `--file <wert>` — YAML-Datei; ohne Angabe wird stdin gelesen
- **Body:** YAML aus `--file` oder stdin (Content-Type `application/x-yaml`), Organisation als Query-Parameter.
- **Beispiel:** `immojump pipelines import --file ankauf.yaml`

### statuses

Status (Phasen) einzeln bearbeiten

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `statuses list` | read | `GET /api/statuses/statuses` |
| `statuses update` | write | `PUT /api/statuses/statuses/{id}` |
| `statuses delete` | destructive | `DELETE /api/statuses/statuses/{id}` |
| `statuses swap` | write | `PUT /api/statuses/statuses/swap/{current-id}/{target-id}` |
| `statuses aliases` | read | `GET /api/statuses/statuses/{status-id}/inbound-aliases` |
| `statuses add-alias` | write | `POST /api/statuses/statuses/{status-id}/inbound-aliases` |

#### statuses list

Alle Phasen auflisten

- **Aufruf:** `immojump statuses list`
- **Endpoint:** `GET /api/statuses/statuses`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `lite` — true = ohne die Aktivitäts-Vorlagen jeder Phase (deutlich kleiner)
- **Beispiel:** `immojump statuses list -q lite=true --fields id,name`

#### statuses update

Phase ändern

- **Aufruf:** `immojump statuses update <id>`
- **Endpoint:** `PUT /api/statuses/statuses/{id}`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID der Phase
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump statuses update 4 --set name='Geprüft'`

#### statuses delete

Phase löschen

- **Aufruf:** `immojump statuses delete <id>`
- **Endpoint:** `DELETE /api/statuses/statuses/{id}`
- **Risk:** `destructive`
- **Argumente:**
  - `id` — ID der Phase
- **Beispiel:** `immojump statuses delete 4`

#### statuses swap

Reihenfolge zweier Phasen tauschen

- **Aufruf:** `immojump statuses swap <current-id> <target-id>`
- **Endpoint:** `PUT /api/statuses/statuses/swap/{current-id}/{target-id}`
- **Risk:** `write`
- **Argumente:**
  - `current-id` — ID der Phase, die verschoben wird
  - `target-id` — ID der Phase, mit der getauscht wird
- **Flags:**
  - `--current-order <wert>` — (Pflicht) Neue Position der ersten Phase (Ganzzahl)
  - `--target-order <wert>` — (Pflicht) Neue Position der zweiten Phase (Ganzzahl)
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump statuses swap 4 5 --current-order 1 --target-order 2`

#### statuses aliases

E-Mail-Aliase einer Phase

- **Aufruf:** `immojump statuses aliases <status-id>`
- **Endpoint:** `GET /api/statuses/statuses/{status-id}/inbound-aliases`
- **Risk:** `read`
- **Argumente:**
  - `status-id` — ID der Phase
- **Beispiel:** `immojump statuses aliases 4`

#### statuses add-alias

E-Mail-Alias zu einer Phase hinzufügen

- **Aufruf:** `immojump statuses add-alias <status-id>`
- **Endpoint:** `POST /api/statuses/statuses/{status-id}/inbound-aliases`
- **Risk:** `write`
- **Argumente:**
  - `status-id` — ID der Phase
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump statuses add-alias 4 --set alias=ankauf`

### templates

Aktivitäts-Vorlagen

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `templates list` | read | `GET /api/activity-templates/activity_templates` |
| `templates recurring` | read | `GET /api/activity-templates/activity_templates/recurring` |
| `templates by-status` | read | `GET /api/activity-templates/activity_templates/status/{status-id}` |
| `templates get` | read | `GET /api/activity-templates/activity_templates/{id}` |
| `templates create` | write | `POST /api/activity-templates/activity_templates` |
| `templates update` | write | `PUT /api/activity-templates/activity_templates/{id}` |
| `templates delete` | destructive | `DELETE /api/activity-templates/activity_templates/{id}` |
| `templates batch-move` | write | `POST /api/activity-templates/activity_templates/status/batch_move` |

#### templates list

Aktivitäts-Vorlagen auflisten

- **Aufruf:** `immojump templates list`
- **Endpoint:** `GET /api/activity-templates/activity_templates`
- **Risk:** `read`
- **Beispiel:** `immojump templates list`

#### templates recurring

Wiederkehrende Vorlagen auflisten

- **Aufruf:** `immojump templates recurring`
- **Endpoint:** `GET /api/activity-templates/activity_templates/recurring`
- **Risk:** `read`
- **Beispiel:** `immojump templates recurring`

#### templates by-status

Vorlagen einer Phase

- **Aufruf:** `immojump templates by-status <status-id>`
- **Endpoint:** `GET /api/activity-templates/activity_templates/status/{status-id}`
- **Risk:** `read`
- **Argumente:**
  - `status-id` — ID der Phase
- **Beispiel:** `immojump templates by-status 4`

#### templates get

Eine Vorlage laden

- **Aufruf:** `immojump templates get <id>`
- **Endpoint:** `GET /api/activity-templates/activity_templates/{id}`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID der Vorlage
- **Beispiel:** `immojump templates get 8`

#### templates create

Vorlage anlegen

- **Aufruf:** `immojump templates create`
- **Endpoint:** `POST /api/activity-templates/activity_templates`
- **Risk:** `write`
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump templates create --set title='Exposé prüfen' --set type=SONSTIGES --set activity_status=Geplant --set priority=Mittel --set status_id=4`

#### templates update

Vorlage ändern

- **Aufruf:** `immojump templates update <id>`
- **Endpoint:** `PUT /api/activity-templates/activity_templates/{id}`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID der Vorlage
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump templates update 8 --set title='Exposé geprüft'`

#### templates delete

Vorlage löschen

- **Aufruf:** `immojump templates delete <id>`
- **Endpoint:** `DELETE /api/activity-templates/activity_templates/{id}`
- **Risk:** `destructive`
- **Argumente:**
  - `id` — ID der Vorlage
- **Beispiel:** `immojump templates delete 8`

#### templates batch-move

Vorlagen einer Phase gesammelt in eine andere Phase verschieben

- **Aufruf:** `immojump templates batch-move`
- **Endpoint:** `POST /api/activity-templates/activity_templates/status/batch_move`
- **Risk:** `write`
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump templates batch-move --set from_status_id=4 --set to_status_id=5`

### documents

Dokumente hochladen, analysieren, verwalten

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `documents list` | read | `GET /api/documents/documents` |
| `documents upload` | write | `POST /api/documents/documents/bulk-upload` |
| `documents rename` | write | `PUT /api/documents/documents/{id}/rename` |
| `documents delete` | destructive | `DELETE /api/documents/documents/{id}` |
| `documents publish` | external | `POST /api/documents/documents/{id}/publish` |
| `documents unpublish` | external | `POST /api/documents/documents/{id}/unpublish` |
| `documents analyze` | write | `POST /api/documents/documents/{id}/analyze` |
| `documents analyze-details` | write | `POST /api/documents/documents/{id}/analyze/details` |
| `documents mark-reviewed` | write | `POST /api/documents/documents/{id}/mark-reviewed` |
| `documents analysis-results` | read | `GET /api/documents/analysis-results` |

#### documents list

Dokumente auflisten

- **Aufruf:** `immojump documents list`
- **Endpoint:** `GET /api/documents/documents`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `immobilien_id` — Pflicht — die Route listet immer die Dokumente genau einer Immobilie
- **Beispiel:** `immojump documents list -q immobilien_id=5 --fields id,dateiname`

#### documents upload

Dokument hochladen (Multipart)

- **Aufruf:** `immojump documents upload <datei>`
- **Endpoint:** `POST /api/documents/documents/bulk-upload`
- **Risk:** `write`
- **Argumente:**
  - `datei` — Pfad zur Datei
- **Flags:**
  - `--immobilie-id <wert>` — Dokument dieser Immobilie zuordnen
  - `--allow-duplicate` — Upload auch bei erkanntem Duplikat erlauben
- **Body:** Multipart-Upload im Feld `files[]`, dazu `organisation_id` aus der Konfiguration.
- **Beispiel:** `immojump documents upload expose.pdf --immobilie-id 5`

#### documents rename

Dokument umbenennen

- **Aufruf:** `immojump documents rename <id>`
- **Endpoint:** `PUT /api/documents/documents/{id}/rename`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID des Dokuments
- **Flags:**
  - `--name <wert>` — (Pflicht) Neuer Dateiname
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump documents rename 11 --name 'Exposé Köln.pdf'`

#### documents delete

Dokument löschen

- **Aufruf:** `immojump documents delete <id>`
- **Endpoint:** `DELETE /api/documents/documents/{id}`
- **Risk:** `destructive`
- **Argumente:**
  - `id` — ID des Dokuments
- **Beispiel:** `immojump documents delete 11`

#### documents publish

HTML-Dokument als oeffentliche Seite veroeffentlichen (dauerhaft ohne Anmeldung erreichbar)

- **Aufruf:** `immojump documents publish <id>`
- **Endpoint:** `POST /api/documents/documents/{id}/publish`
- **Risk:** `external`
- **Argumente:**
  - `id` — ID des HTML-Dokuments
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump documents publish 11`

#### documents unpublish

Veroeffentlichung eines HTML-Dokuments aufheben

- **Aufruf:** `immojump documents unpublish <id>`
- **Endpoint:** `POST /api/documents/documents/{id}/unpublish`
- **Risk:** `external`
- **Argumente:**
  - `id` — ID des HTML-Dokuments
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump documents unpublish 11`

#### documents analyze

KI-Analyse eines Dokuments starten

- **Aufruf:** `immojump documents analyze <id>`
- **Endpoint:** `POST /api/documents/documents/{id}/analyze`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID des Dokuments
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump documents analyze 11`

#### documents analyze-details

Detail-Analyse eines Dokuments starten

- **Aufruf:** `immojump documents analyze-details <id>`
- **Endpoint:** `POST /api/documents/documents/{id}/analyze/details`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID des Dokuments
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump documents analyze-details 11`

#### documents mark-reviewed

Analyse als geprüft markieren

- **Aufruf:** `immojump documents mark-reviewed <id>`
- **Endpoint:** `POST /api/documents/documents/{id}/mark-reviewed`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID des Dokuments
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump documents mark-reviewed 11`

#### documents analysis-results

Analyse-Ergebnisse abrufen

- **Aufruf:** `immojump documents analysis-results`
- **Endpoint:** `GET /api/documents/analysis-results`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `immobilien_id` — nur Ergebnisse zu dieser Immobilie
  - `document_id` — nur Ergebnisse zu diesem Dokument
  - `limit` — Anzahl der Ergebnisse (Default 50)
- **Beispiel:** `immojump documents analysis-results -q immobilien_id=5 -q limit=5`

### tags

Tags und ihre Zuordnung zu Objekten

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `tags list` | read | `GET /api/{org}/tags` |
| `tags create` | write | `POST /api/{org}/tags` |
| `tags update` | write | `PUT /api/{org}/tags/{tag-id}` |
| `tags delete` | destructive | `DELETE /api/tags/{tag-id}` |
| `tags of` | read | `GET /api/tags/{entity-type}/{entity-id}` |
| `tags set` | write | `PUT /api/tags/{entity-type}/{entity-id}` |

#### tags list

Tags der Organisation auflisten

- **Aufruf:** `immojump tags list`
- **Endpoint:** `GET /api/{org}/tags`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `for` — nur Tags dieser Objektart, z. B. contact oder immobilie
- **Beispiel:** `immojump tags list -q for=contact`

#### tags create

Tag anlegen

- **Aufruf:** `immojump tags create`
- **Endpoint:** `POST /api/{org}/tags`
- **Risk:** `write`
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump tags create --set name=Wichtig --set color='#ff0000'`

#### tags update

Tag ändern

- **Aufruf:** `immojump tags update <tag-id>`
- **Endpoint:** `PUT /api/{org}/tags/{tag-id}`
- **Risk:** `write`
- **Argumente:**
  - `tag-id` — ID des Tags
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump tags update 3 --set name='Sehr wichtig'`

#### tags delete

Tag löschen

- **Aufruf:** `immojump tags delete <tag-id>`
- **Endpoint:** `DELETE /api/tags/{tag-id}`
- **Risk:** `destructive`
- **Argumente:**
  - `tag-id` — ID des Tags
- **Beispiel:** `immojump tags delete 3`

#### tags of

Tags eines Objekts

- **Aufruf:** `immojump tags of <entity-type> <entity-id>`
- **Endpoint:** `GET /api/tags/{entity-type}/{entity-id}`
- **Risk:** `read`
- **Argumente:**
  - `entity-type` — Objektart, z. B. contact oder immobilie
  - `entity-id` — ID des Objekts
- **Beispiel:** `immojump tags of contact 42`

#### tags set

Tags eines Objekts ersetzen (Body ist ein JSON-Array von IDs)

- **Aufruf:** `immojump tags set <entity-type> <entity-id>`
- **Endpoint:** `PUT /api/tags/{entity-type}/{entity-id}`
- **Risk:** `write`
- **Argumente:**
  - `entity-type` — Objektart, z. B. contact oder immobilie
  - `entity-id` — ID des Objekts
- **Flags:**
  - `--tag-ids <wert>` — (Pflicht) Tag-IDs, kommagetrennt; leer entfernt alle Tags
- **Body:** rohes JSON-Array der Tag-IDs, gebaut aus `--tag-ids`.
- **Beispiel:** `immojump tags set contact 42 --tag-ids 3,7`

### shares

Freigabe-Links für Immobilien, Dokumente und Bilder

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `shares list` | read | `GET /api/share-links` |
| `shares create` | external | `POST /api/share-links` |
| `shares update` | external | `PATCH /api/share-links/{id}` |
| `shares revoke` | write | `DELETE /api/share-links/{id}` |

#### shares list

Freigabe-Links auflisten

- **Aufruf:** `immojump shares list`
- **Endpoint:** `GET /api/share-links`
- **Risk:** `read`
- **Flags:**
  - `--entity-type <wert>` — Nach Objektart filtern: immobilie, dokument oder bild
  - `--entity-id <wert>` — Nach Objekt-ID filtern
- **Beispiel:** `immojump shares list --entity-type immobilie --entity-id 5`

#### shares create

Freigabe-Link erzeugen (Inhalte werden nach außen sichtbar)

- **Aufruf:** `immojump shares create`
- **Endpoint:** `POST /api/share-links`
- **Risk:** `external`
- **Flags:**
  - `--immobilie <wert>` — Immobilie freigeben (wiederholbar)
  - `--dokument <wert>` — Dokument freigeben (wiederholbar)
  - `--bild <wert>` — Bild freigeben (wiederholbar)
  - `--title <wert>` — Titel der Freigabe
  - `--note <wert>` — Nachricht an den Empfänger
  - `--expires-at <wert>` — Ablauf als YYYY-MM-DD (= Tagesende) oder vollständiger ISO-Zeitstempel
  - `--password <wert>` — Passwortschutz setzen (mindestens 4 Zeichen)
  - `--recipient-email <wert>` — E-Mail-Adresse des Empfängers
  - `--send-email` — Link direkt per E-Mail verschicken
  - `--include-password-in-email` — Passwort mit in die E-Mail schreiben (nur auf Wunsch)
  - `--show-key-facts` — Eckdaten der Immobilie mit anzeigen
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump shares create --immobilie 5 --dokument 11 --title 'Finanzierung' --password bank2026 --expires-at 2026-09-30`

#### shares update

Freigabe-Link ändern (nur gesetzte Flags werden geschickt)

- **Aufruf:** `immojump shares update <id>`
- **Endpoint:** `PATCH /api/share-links/{id}`
- **Risk:** `external`
- **Argumente:**
  - `id` — ID des Freigabe-Links
- **Flags:**
  - `--title <wert>` — Titel ändern
  - `--note <wert>` — Nachricht ändern (leerer String löscht sie)
  - `--expires-at <wert>` — Ablauf als YYYY-MM-DD (= Tagesende) oder vollständiger ISO-Zeitstempel
  - `--password <wert>` — Neues Passwort setzen (mindestens 4 Zeichen)
  - `--remove-password` — Passwortschutz entfernen (schickt password: null)
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump shares update 7 --expires-at 2026-12-31`

#### shares revoke

Freigabe-Link widerrufen (der sichere Ausweg, deshalb nur write)

- **Aufruf:** `immojump shares revoke <id>`
- **Endpoint:** `DELETE /api/share-links/{id}`
- **Risk:** `write`
- **Argumente:**
  - `id` — ID des Freigabe-Links
- **Beispiel:** `immojump shares revoke 7`

### email

Postfach: Nachrichten lesen, sortieren und versenden

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `email list` | read | `GET /api/email-messages` |
| `email get` | read | `GET /api/email-messages/{id}` |
| `email thread` | read | `GET /api/email-messages/threads/{thread-id}` |
| `email search` | read | `GET /api/email-messages/search` |
| `email folders` | read | `GET /api/email-messages/folders` |
| `email for-immobilie` | read | `GET /api/email-messages/immobilie/{immobilie-id}` |
| `email for-contact` | read | `GET /api/email-messages/contact/{contact-id}` |
| `email outbox` | read | `GET /api/email-messages/outbox` |
| `email outbox-stats` | read | `GET /api/email-messages/outbox/stats` |
| `email accounts` | read | `GET /api/org/email-accounts` |
| `email signatures` | read | `GET /api/org/email-signatures` |
| `email mark-read` | write | `POST /api/email-messages/mark-read` |
| `email mark-starred` | write | `POST /api/email-messages/mark-starred` |
| `email archive` | write | `POST /api/email-messages/archive` |
| `email trash` | write | `POST /api/email-messages/trash` |
| `email move` | write | `POST /api/email-messages/move` |
| `email sync` | write | `POST /api/email-messages/sync` |
| `email outbox-retry` | write | `POST /api/email-messages/outbox/retry` |
| `email folder-create` | write | `POST /api/email-messages/folders` |
| `email folder-rename` | write | `POST /api/email-messages/folders/rename` |
| `email folder-delete` | destructive | `POST /api/email-messages/folders/delete` |
| `email send` | external | `POST /api/org/email-accounts/{account-id}/send` |

#### email list

Nachrichten im Postfach auflisten

- **Aufruf:** `immojump email list`
- **Endpoint:** `GET /api/email-messages`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `account_id` — nur dieses Postfach (IDs liefert `email accounts`)
  - `folder` — Ordner, Default INBOX; virtuell auch SENT, STARRED, ARCHIVE, TRASH, DRAFTS
  - `is_read` — true = nur gelesene, false = nur ungelesene
  - `is_starred` — true = nur markierte
  - `q` — Freitext über Betreff, Absender und Text
  - `page` — Seite (ab 1)
  - `per_page` — Treffer pro Seite (Default 50, max. 200)
- **Beispiel:** `immojump email list -q is_read=false --fields items.id,items.subject,items.from_email`

#### email get

Eine Nachricht mit vollem Text laden — markiert sie dabei als gelesen

- **Aufruf:** `immojump email get <id>`
- **Endpoint:** `GET /api/email-messages/{id}`
- **Risk:** `read`
- **Argumente:**
  - `id` — ID der Nachricht
- **Beispiel:** `immojump email get 3f2a…`

#### email thread

Einen Thread mit allen Nachrichten laden

- **Aufruf:** `immojump email thread <thread-id>`
- **Endpoint:** `GET /api/email-messages/threads/{thread-id}`
- **Risk:** `read`
- **Argumente:**
  - `thread-id` — ID des Threads
- **Beispiel:** `immojump email thread 9c11…`

#### email search

Nachrichten über alle Ordner durchsuchen

- **Aufruf:** `immojump email search`
- **Endpoint:** `GET /api/email-messages/search`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `q` — Suchbegriff; ohne ihn antwortet die Route mit einer leeren Liste
  - `page` — Seite (ab 1)
  - `per_page` — Treffer pro Seite (Default 50, max. 200)
- **Beispiel:** `immojump email search -q q=Notartermin --fields items.id,items.subject`

#### email folders

Ordner des Postfachs auflisten

- **Aufruf:** `immojump email folders`
- **Endpoint:** `GET /api/email-messages/folders`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `account_id` — nur die Ordner dieses Postfachs
- **Beispiel:** `immojump email folders`

#### email for-immobilie

Nachrichten aller Kontakte, die an einer Immobilie hängen

- **Aufruf:** `immojump email for-immobilie <immobilie-id>`
- **Endpoint:** `GET /api/email-messages/immobilie/{immobilie-id}`
- **Risk:** `read`
- **Argumente:**
  - `immobilie-id` — ID der Immobilie
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `page` — Seite (ab 1)
  - `per_page` — Treffer pro Seite (Default 50, max. 200)
- **Beispiel:** `immojump email for-immobilie 5`

#### email for-contact

Nachrichten eines Kontakts

- **Aufruf:** `immojump email for-contact <contact-id>`
- **Endpoint:** `GET /api/email-messages/contact/{contact-id}`
- **Risk:** `read`
- **Argumente:**
  - `contact-id` — ID des Kontakts
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `page` — Seite (ab 1)
  - `per_page` — Treffer pro Seite (Default 20, max. 100)
- **Beispiel:** `immojump email for-contact 42`

#### email outbox

Warteschlange der noch nicht zum IMAP-Server gespiegelten Änderungen

- **Aufruf:** `immojump email outbox`
- **Endpoint:** `GET /api/email-messages/outbox`
- **Risk:** `read`
- **Bekannte Query-Parameter** (per `-q key=value`):
  - `status` — PENDING, IN_PROGRESS, COMPLETED oder FAILED (exakt so geschrieben)
  - `limit` — Anzahl Einträge (Default 50, max. 500)
- **Beispiel:** `immojump email outbox -q status=FAILED`

#### email outbox-stats

Zählstand der Warteschlange (offen, fehlgeschlagen, erledigt)

- **Aufruf:** `immojump email outbox-stats`
- **Endpoint:** `GET /api/email-messages/outbox/stats`
- **Risk:** `read`
- **Beispiel:** `immojump email outbox-stats`

#### email accounts

Postfächer der Organisation auflisten — liefert die account-id für `email send`

- **Aufruf:** `immojump email accounts`
- **Endpoint:** `GET /api/org/email-accounts`
- **Risk:** `read`
- **Beispiel:** `immojump email accounts --fields items.id,items.email`

#### email signatures

Signaturen der Organisation — liefert die ID für `email send --signature-id`

- **Aufruf:** `immojump email signatures`
- **Endpoint:** `GET /api/org/email-signatures`
- **Risk:** `read`
- **Beispiel:** `immojump email signatures --fields id,name`

#### email mark-read

Nachrichten als gelesen markieren (--is-read=false setzt sie zurück)

- **Aufruf:** `immojump email mark-read`
- **Endpoint:** `POST /api/email-messages/mark-read`
- **Risk:** `write`
- **Flags:**
  - `--message-ids <wert>` — (Pflicht) IDs der Nachrichten, kommagetrennt oder wiederholt
  - `--is-read` — `--is-read=false` setzt wieder auf ungelesen (Default true)
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email mark-read --message-ids 3f2a…,9c11…`

#### email mark-starred

Nachrichten markieren (--is-starred=false nimmt die Markierung weg)

- **Aufruf:** `immojump email mark-starred`
- **Endpoint:** `POST /api/email-messages/mark-starred`
- **Risk:** `write`
- **Flags:**
  - `--message-ids <wert>` — (Pflicht) IDs der Nachrichten, kommagetrennt oder wiederholt
  - `--is-starred` — `--is-starred=false` entfernt die Markierung (Default true)
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email mark-starred --message-ids 3f2a…`

#### email archive

Nachrichten archivieren

- **Aufruf:** `immojump email archive`
- **Endpoint:** `POST /api/email-messages/archive`
- **Risk:** `write`
- **Flags:**
  - `--message-ids <wert>` — (Pflicht) IDs der Nachrichten, kommagetrennt oder wiederholt
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email archive --message-ids 3f2a…`

#### email trash

Nachrichten in den Papierkorb legen (umkehrbar über `email move`)

- **Aufruf:** `immojump email trash`
- **Endpoint:** `POST /api/email-messages/trash`
- **Risk:** `write`
- **Flags:**
  - `--message-ids <wert>` — (Pflicht) IDs der Nachrichten, kommagetrennt oder wiederholt
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email trash --message-ids 3f2a…`

#### email move

Nachrichten in einen anderen Ordner verschieben

- **Aufruf:** `immojump email move`
- **Endpoint:** `POST /api/email-messages/move`
- **Risk:** `write`
- **Flags:**
  - `--message-ids <wert>` — (Pflicht) IDs der Nachrichten, kommagetrennt oder wiederholt
  - `--folder <wert>` — (Pflicht) Zielordner, Default INBOX; SENT/STARRED/ARCHIVE/TRASH/DRAFTS sind virtuell und bleiben lokal
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email move --message-ids 3f2a… --folder Notar`

#### email sync

IMAP-Abgleich anstoßen (Backend-Limit: 10 Aufrufe pro Stunde)

- **Aufruf:** `immojump email sync`
- **Endpoint:** `POST /api/email-messages/sync`
- **Risk:** `write`
- **Flags:**
  - `--account-id <wert>` — nur dieses Postfach; ohne Angabe alle der Organisation
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email sync`

#### email outbox-retry

Fehlgeschlagene Einträge der Warteschlange erneut versuchen

- **Aufruf:** `immojump email outbox-retry`
- **Endpoint:** `POST /api/email-messages/outbox/retry`
- **Risk:** `write`
- **Flags:**
  - `--entry-ids <wert>` — IDs aus `email outbox`; ohne Angabe alle fehlgeschlagenen
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email outbox-retry`

#### email folder-create

Ordner anlegen

- **Aufruf:** `immojump email folder-create`
- **Endpoint:** `POST /api/email-messages/folders`
- **Risk:** `write`
- **Flags:**
  - `--name <wert>` — (Pflicht) Name des Ordners; ohne / \ < > " und nicht mit Punkt beginnend oder endend
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email folder-create --name Notar`

#### email folder-rename

Ordner umbenennen

- **Aufruf:** `immojump email folder-rename`
- **Endpoint:** `POST /api/email-messages/folders/rename`
- **Risk:** `write`
- **Flags:**
  - `--old-name <wert>` — (Pflicht) bisheriger Name
  - `--new-name <wert>` — (Pflicht) neuer Name
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email folder-rename --old-name Notar --new-name Notartermine`

#### email folder-delete

Ordner löschen

- **Aufruf:** `immojump email folder-delete`
- **Endpoint:** `POST /api/email-messages/folders/delete`
- **Risk:** `destructive`
- **Flags:**
  - `--name <wert>` — (Pflicht) Name des Ordners
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email folder-delete --name Notar`

#### email send

E-Mail über ein Postfach der Organisation versenden

- **Aufruf:** `immojump email send <account-id>`
- **Endpoint:** `POST /api/org/email-accounts/{account-id}/send`
- **Risk:** `external`
- **Argumente:**
  - `account-id` — ID des Postfachs (aus `email accounts`)
- **Flags:**
  - `--to <wert>` — (Pflicht) Empfänger, kommagetrennt oder wiederholt
  - `--cc <wert>` — Kopie, kommagetrennt oder wiederholt
  - `--bcc <wert>` — Blindkopie, kommagetrennt oder wiederholt
  - `--subject <wert>` — Betreff
  - `--html <wert>` — Inhalt als HTML
  - `--signature-id <wert>` — Signatur anhängen (IDs aus `email signatures`)
- **Body:** `--body '<json>'`, `--body @datei` oder `--body -` (stdin), dazu `--set pfad=wert` (wiederholbar).
- **Beispiel:** `immojump email send 7b1c… --to kunde@example.com --subject "Exposé" --html "<p>Anbei.</p>"`

### api

Beliebigen /api/-Pfad aufrufen (Escape-Hatch)

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `api` | dynamic | `<METHOD> <pfad>` |

#### api

Beliebigen /api/-Pfad aufrufen (Escape-Hatch); das Risk-Level entsteht pro Aufruf

- **Aufruf:** `immojump api <method> <pfad>`
- **Endpoint:** `<METHOD> <pfad>`
- **Risk:** `dynamic`
  - Risk kommt aus dem passenden Registry-Befehl (Methode + Pfad); ohne Treffer konservativ nach Methode: GET/HEAD = read, DELETE = destructive, alles andere = external.
- **Argumente:**
  - `method` — HTTP-Methode, z. B. GET oder POST
  - `pfad` — Pfad ab /api/, z. B. /api/deals
- **Beispiel:** `immojump api GET /api/deals -q status_ids=7`

### docs

Markdown-Referenz ausgeben — komplett oder für eine Ressource/einen Befehl

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `docs` | read | `lokal` |

#### docs

Markdown-Referenz nach stdout schreiben — komplett oder als Ausschnitt

- **Aufruf:** `immojump docs [resource] [verb]`
- **Endpoint:** `lokal`
- **Risk:** `read`
- **Argumente:**
  - `resource` (optional) — Nur diese Ressource ausgeben
  - `verb` (optional) — Nur diesen Befehl ausgeben
- **Beispiel:** `immojump docs shares create`

### schema

Befehls-Schema als JSON ausgeben — komplett oder als Ausschnitt

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `schema` | read | `lokal` |

#### schema

Befehls-Schema als JSON (Risk, Args, Flags, Exit-Codes)

- **Aufruf:** `immojump schema [resource] [verb]`
- **Endpoint:** `lokal`
- **Risk:** `read`
- **Argumente:**
  - `resource` (optional) — Nur diese Ressource ausgeben
  - `verb` (optional) — Nur diesen Befehl ausgeben
- **Beispiel:** `immojump schema shares create`

### version

Version ausgeben

| Befehl | Risk | Endpoint |
| --- | --- | --- |
| `version` | read | `lokal` |

#### version

Version des CLI ausgeben

- **Aufruf:** `immojump version`
- **Endpoint:** `lokal`
- **Risk:** `read`
- **Beispiel:** `immojump version`
