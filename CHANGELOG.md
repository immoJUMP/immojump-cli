# Changelog

## [0.8.0](https://github.com/immoJUMP/immojump-cli/compare/v0.7.0...v0.8.0) (2026-10-22)


### ⚠ BREAKING CHANGES

* **immobilien:** `immobilien update` entfällt. Den Status setzt `immobilien set-status <id> --status-id <id>` (bzw. `--remove-status`), Felder ändert `immobilien patch <id>`.

### Features

* **api:** lesende Aufrufe bei 429/502/503/504 mit Backoff wiederholen ([c9d836f](https://github.com/immoJUMP/immojump-cli/commit/c9d836fba1a7a641269ecf9b3069053633f48f20))
* **documents:** analyze nimmt mehrere IDs; analyze-all stößt alle passenden Dokumente an ([9b681d9](https://github.com/immoJUMP/immojump-cli/commit/9b681d92d0cdac6164f8e7f343343d304f1f2740))
* **documents:** list mit Filtern und --all; set-unit ordnet einer Einheit zu ([4d080c2](https://github.com/immoJUMP/immojump-cli/commit/4d080c2f793932b66e720b73906b826df62b5c96))
* **documents:** upload nimmt Dateien und Ordner, streamt in Stapeln und berichtet je Datei ([da9b6f2](https://github.com/immoJUMP/immojump-cli/commit/da9b6f2fcd155bafefb9f40452fd377b1ccaddd7))
* **fields:** Definitionen benutzerdefinierter Felder auflisten und anlegen ([3f6b2d6](https://github.com/immoJUMP/immojump-cli/commit/3f6b2d64e76f9a34a54be6c5434262d6814c3ed6))
* **immobilien:** --type nennt die erlaubten Objekttypen ([211b79e](https://github.com/immoJUMP/immojump-cli/commit/211b79e54719e1cc22dabea66b866a120eb6ba04))
* **immobilien:** get --exclude lässt Einheiten und Dokumente weg ([b035f68](https://github.com/immoJUMP/immojump-cli/commit/b035f6820712a64f396efbd19553346da02cc3d4))
* **theo-intelligence:** expose property assessment and investor feedback ([71174b4](https://github.com/immoJUMP/immojump-cli/commit/71174b497f2bce0c1bb244567ce577f7f45a07fa))
* **units:** eigene Feldwerte vieler Einheiten lesen (fields-get) und setzen (fields-set) ([7c48c6b](https://github.com/immoJUMP/immojump-cli/commit/7c48c6b44677579b4ccb2ac659326258e54532b5))
* **units:** Feld-Flags für create/update und Hinweis auf „Einheit 1“ ([75a99d5](https://github.com/immoJUMP/immojump-cli/commit/75a99d598561fa1966e7a28581c085fd7456d853))
* **units:** import lädt eine Mieterliste in Stapeln (NDJSON, JSON, CSV) ([c37a865](https://github.com/immoJUMP/immojump-cli/commit/c37a86596602528b7f434d987f5e4ea47dfe7fa5))
* **units:** Listen filtern, blättern und mit --all komplett holen; list-org ([b30d4e0](https://github.com/immoJUMP/immojump-cli/commit/b30d4e0407a164fa5c36a6c8ddf5a2e47db7a979))


### Bug Fixes

* **api:** Timeout beim Lesen des Bodys ist ein Timeout, kein Netzwerkfehler ([3e48d62](https://github.com/immoJUMP/immojump-cli/commit/3e48d6215eda9cf3d140159c04ac04c93c77af83))
* **api:** Timeouts bei Schreibaufrufen nie wiederholen; --timeout gilt je Versuch ([4bd3c12](https://github.com/immoJUMP/immojump-cli/commit/4bd3c1245f58f9de2e5085b8cccf78ed0845e9b3))
* **cli:** leere IDs und Argumente vor jedem Request ablehnen ([9f856aa](https://github.com/immoJUMP/immojump-cli/commit/9f856aa89177263a30d630a87ff24a06f9e5b06c))
* **cli:** unbekannte Werte für Schalter ablehnen statt still false ([9adbdfc](https://github.com/immoJUMP/immojump-cli/commit/9adbdfcd69c015252c0c088911069e3e87651ca3))
* **documents:** --max-file-mb gilt für die Größe beim Senden, nicht nur bei der Aufzählung ([dae8407](https://github.com/immoJUMP/immojump-cli/commit/dae8407609cd09c66a25873ac31843bf83d5f337))
* **documents:** --unit-id bei Duplikaten meldet UNIT_ID_NOT_APPLIED statt Exit 0 ([f1ca7e1](https://github.com/immoJUMP/immojump-cli/commit/f1ca7e11a9fae7cad4b7ed8e0b799b690a61e61d))
* **documents:** Abbruch eines späteren Upload-Stapels behält seinen Exit-Code ([6841f16](https://github.com/immoJUMP/immojump-cli/commit/6841f1679b99990a86c4dd3d384e090407e4cee4))
* **documents:** Abbruch im ersten Upload-Stapel gibt trotzdem den Bericht aus ([c587ae2](https://github.com/immoJUMP/immojump-cli/commit/c587ae277b8af501ce7ace8d6e80c54d7885e4f0))
* **documents:** Abbruch im ersten Upload-Stapel nennt die nie gesendeten Dateien ([7468e8a](https://github.com/immoJUMP/immojump-cli/commit/7468e8a4e20df2442e412f6aae804dd76cf2ec56))
* **documents:** Analysen nur nach 429 wiederholen, nie nach 5xx oder Netzwerkfehler ([2279007](https://github.com/immoJUMP/immojump-cli/commit/2279007f6881022a45d6b87da57230d0e0a32cd8))
* **documents:** Bericht auch bei einem einzigen gescheiterten Stapel im Bericht-Modus ([081eb02](https://github.com/immoJUMP/immojump-cli/commit/081eb024d7814e4c6e79b9e6c369f376dbbb6ecf))
* **documents:** CLI beurteilt Archive nicht mehr selbst ([cd81a93](https://github.com/immoJUMP/immojump-cli/commit/cd81a93389af32e7ce51d9758dbcc2538f0fa2ba))
* **documents:** Dateien über 250 MB lokal ablehnen statt senden ([8e8efc0](https://github.com/immoJUMP/immojump-cli/commit/8e8efc0e1d7a075485c76fed793266b470966066))
* **documents:** einzelne Datei behält die bisherige Ausgabe ([012d854](https://github.com/immoJUMP/immojump-cli/commit/012d8541f25de3db193d33787c5f9b6217cdce2b))
* **documents:** einzelnes Archiv meldet fehlgeschlagene und leere Mitglieder ([b87af00](https://github.com/immoJUMP/immojump-cli/commit/b87af0032f5be84f163c65fa336f6a3ebd9c1582))
* **documents:** fehlgeschlagene Upload-Stapel nach 5xx/Timeout als may_exist markieren ([f05dfaf](https://github.com/immoJUMP/immojump-cli/commit/f05dfaf11e989acce953a0b5dd85cdd6dc8de09f))
* **documents:** gleichnamige Dateien nie im selben Upload-Stapel ([ef19769](https://github.com/immoJUMP/immojump-cli/commit/ef197690896ceddffdbe1fc6ece1b695ba297374))
* **documents:** Idempotency-Key je Request ableiten statt für jeden Stapel wiederholen ([e4633f3](https://github.com/immoJUMP/immojump-cli/commit/e4633f3a63b52b18495aa02a8b7b3db70140a647))
* **documents:** ignoriertes --unit-id einer älteren Instanz erkennen ([c3b27a4](https://github.com/immoJUMP/immojump-cli/commit/c3b27a4f0b3559dcf678144fcfaf53525a6158c7))
* **documents:** jedes Archiv geht in einem eigenen Upload-Stapel ([9e7c8ef](https://github.com/immoJUMP/immojump-cli/commit/9e7c8ef59654f90d4127ce4d09b75474828c682f))
* **documents:** leere Dateien machen den Upload unvollständig (Exit 1) ([d9b9ad9](https://github.com/immoJUMP/immojump-cli/commit/d9b9ad96c1528d81742b1e9839f49d60a29fbac2))
* **documents:** leere Einzeldatei lokal ablehnen (Exit 1) statt 400 vom Backend ([af95e0e](https://github.com/immoJUMP/immojump-cli/commit/af95e0e44fa3a16bb9ccf0cccf436cbe2a296bcb))
* **documents:** leeres Archiv-Mitglied nicht der gleichnamigen losen Datei zuordnen ([f2984a2](https://github.com/immoJUMP/immojump-cli/commit/f2984a245e8609b46ba3f544be8c6c282b04be0b))
* **documents:** mehrdeutiges ZIP-Ende prüft das Backend, nicht das CLI ([009dd8d](https://github.com/immoJUMP/immojump-cli/commit/009dd8d664c647f6d2ddd5759f1e95e627b71cc7))
* **documents:** unlesbare 2xx-Antwort eines Upload-Stapels als may_exist markieren ([a2079b5](https://github.com/immoJUMP/immojump-cli/commit/a2079b55d6c5066b85445d8ecfc5fa2fc369d74a))
* **documents:** Upload blockiert nicht an einer untergeschobenen Named Pipe ([d5b223e](https://github.com/immoJUMP/immojump-cli/commit/d5b223e2b28f75a4dcb32519868cbc10a4305b90))
* **documents:** Upload folgt keinem untergeschobenen Symlink (TOCTOU) ([0764dbe](https://github.com/immoJUMP/immojump-cli/commit/0764dbeebbe1e2cceea928552ebdc1f71869169c))
* **documents:** Upload öffnet Dateien über os.Root, kein getauschter Elternordner führt hinaus ([5755ab3](https://github.com/immoJUMP/immojump-cli/commit/5755ab3ad146d67ae6b31ff710c7b0290c07bc27))
* **documents:** Upload-Bericht ordnet über sourceFile zu, nicht über den gespeicherten Namen ([921f1b3](https://github.com/immoJUMP/immojump-cli/commit/921f1b3bd9faa8d78b357d348eb5403149c34ddc))
* **documents:** ZIP-Vorabcheck öffnet wie der Upload und liest begrenzt ([cbeefa0](https://github.com/immoJUMP/immojump-cli/commit/cbeefa0adbe70551bfd96abf1887390f6a207b69))
* **documents:** ZIP-Vorabcheck wieder über archive/zip, Speicher über die Dateigröße begrenzt ([a711b57](https://github.com/immoJUMP/immojump-cli/commit/a711b5788a02fa595b6e8d9760de9d0633b13c96))
* **documents:** ZIP-Vorabcheck zählt die Einträge selbst, mit festem Speicher ([331eb00](https://github.com/immoJUMP/immojump-cli/commit/331eb0063cdfc4957426e177586a804a169a253c))
* **immobilien:** set-status statt eines Vollupdates, das keines war ([8974ecc](https://github.com/immoJUMP/immojump-cli/commit/8974ecca37a6356280bfea2c5d5ecbe5005b868e))
* **templates:** batch-move-Beispiel folgt dem Backend-Vertrag, Vorlagen-IDs als UUID ([3fa7b35](https://github.com/immoJUMP/immojump-cli/commit/3fa7b3548c39318b044fff5d24bceacd506bfd8e))
* **units:** auch negativer deutscher Tausender in ;-CSV ohne Schalter ist mehrdeutig ([482ab88](https://github.com/immoJUMP/immojump-cli/commit/482ab881654d2c2031b04f72a7a3fd7419c52e38))
* **units:** CSV-Zahlen streng lesen statt still umdeuten ([ac80baa](https://github.com/immoJUMP/immojump-cli/commit/ac80baa21097f0cfd51a2dbe4b05d2dcf63b413e))
* **units:** doppelte id in der Eingabe vor dem ersten Stapel ablehnen ([2134a7c](https://github.com/immoJUMP/immojump-cli/commit/2134a7c918dd3dc4002692942c1b72cf35fe8bb3))
* **units:** fields-get liest Feldwerte per POST …/values/batch/read ([8b2a7cc](https://github.com/immoJUMP/immojump-cli/commit/8b2a7ccdff42102904d6a7677f007a35d8ac95b1))
* **units:** fields-set lehnt dasselbe Feld derselben Einheit doppelt ab ([25d0337](https://github.com/immoJUMP/immojump-cli/commit/25d0337ce6feadc1b529cfe1b12c6c5786f3cef7))
* **units:** fields-set löst Felder immer über die Definitionen auf, verglichen wie im Backend ([e96fdd0](https://github.com/immoJUMP/immojump-cli/commit/e96fdd0c52180db831816ae3926fa7d47e2e1d9f))
* **units:** führende Nullen in CSV-Zahlen normalisieren, Umwandlung nie still ignorieren ([f836f7a](https://github.com/immoJUMP/immojump-cli/commit/f836f7ac5afd0ff09b3224e59779c0488aa03945))
* **units:** genau ein JSON-Wert je NDJSON-Zeile bzw. je JSON-Datei ([7a25a12](https://github.com/immoJUMP/immojump-cli/commit/7a25a1206b41bab158a1fdce3a1b4a6e7b5c08cd))
* **units:** gescheiterter Import zeigt die gespeicherten Stapel; unsichere Wiederholung markiert ([2548dcc](https://github.com/immoJUMP/immojump-cli/commit/2548dccee0bd94461066352006a296f49ddefa81))
* **units:** import löst id und extern_id gegen die Einheiten des Hauses auf ([a3f3354](https://github.com/immoJUMP/immojump-cli/commit/a3f3354888c83851737b445f762b312a3e794d26))
* **units:** JSON-Eingabe verträgt ein UTF-8-BOM ([c57cef6](https://github.com/immoJUMP/immojump-cli/commit/c57cef65e4bad993864ece4ff6ddad77cf7d2763))
* **units:** Teilsumme eines gescheiterten Imports behält complete trotz --fields ([bca2382](https://github.com/immoJUMP/immojump-cli/commit/bca2382c10e791b061a109be07a168fe6fe3d091))
* **units:** Umschlag der JSON-Eingabe nimmt nur sein Array, kein dry_run aus der Datei ([d62ba74](https://github.com/immoJUMP/immojump-cli/commit/d62ba74d70bd3e0a027bb4e3f73baaf5e3e28737))

## [0.7.0](https://github.com/immoJUMP/immojump-cli/compare/v0.6.0...v0.7.0) (2026-09-01)


### Features

* **feed:** Organisations-Feed und Benachrichtigungen ([2bbf10c](https://github.com/immoJUMP/immojump-cli/commit/2bbf10c9eef32a834f52396a987089655abb3599))

## [0.6.0](https://github.com/immoJUMP/immojump-cli/compare/v0.5.0...v0.6.0) (2026-09-01)


### Features

* **cli:** Browser-Login und Tabellenausgabe für Menschen ([6eb79ca](https://github.com/immoJUMP/immojump-cli/commit/6eb79ca5c13a166dc2b5d78d5dca5f17bacf986e))


### Bug Fixes

* **output:** --fields projiziert auch in Listen ([bfd7dbe](https://github.com/immoJUMP/immojump-cli/commit/bfd7dbe362c6ec0e7b3795b2d9d4a9778c968fd7))

## [0.5.0](https://github.com/immoJUMP/immojump-cli/compare/v0.4.1...v0.5.0) (2026-09-01)


### Features

* **email:** Postfach lesen, sortieren und versenden ([ae37e2b](https://github.com/immoJUMP/immojump-cli/commit/ae37e2b07790bf4112abc5b03e559e034981c176))

## [0.4.1](https://github.com/immoJUMP/immojump-cli/compare/v0.4.0...v0.4.1) (2026-08-29)


### Bug Fixes

* **install:** Download ueberlebt das Fenster nach einer Veroeffentlichung ([32cf2aa](https://github.com/immoJUMP/immojump-cli/commit/32cf2aa199c98cb9807a886780c65d20e7187251))

## [0.4.0](https://github.com/immoJUMP/immojump-cli/compare/v0.3.0...v0.4.0) (2026-08-29)


### Features

* **install:** Installationsskript und Direkt-Download dokumentieren ([b0356df](https://github.com/immoJUMP/immojump-cli/commit/b0356dfc25ed3cf21c7cecac31718b410c4d371a))

## [0.3.0](https://github.com/immoJUMP/immojump-cli/compare/v0.2.0...v0.3.0) (2026-08-29)


### Features

* **documents:** publish und unpublish aufnehmen ([a577823](https://github.com/immoJUMP/immojump-cli/commit/a5778238b8ef21d52427d132d332b235fbd5e96a))

## [0.2.0](https://github.com/immoJUMP/immojump-cli/compare/v0.1.0...v0.2.0) (2026-08-22)


### Features

* Fehlerdetails durchreichen, Kontext sparen, Aufrufe nachvollziehen ([c7e1b78](https://github.com/immoJUMP/immojump-cli/commit/c7e1b784c5acc579f82f0604cd848fa0174a9b92))


### Bug Fixes

* Version nach `go install` melden statt "dev" ([13ae0b9](https://github.com/immoJUMP/immojump-cli/commit/13ae0b99e0150c537ae09a59c2450b43c4e4ea9f))

## 0.1.0 (2026-08-22)


### Features

* immojump-CLI für Coding-Agenten (Go, Single Binary) ([e670871](https://github.com/immoJUMP/immojump-cli/commit/e6708718ec9775e1bdfae00d666b1994781f8648))
