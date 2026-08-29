#!/bin/sh
# immoJUMP CLI installieren oder aktualisieren.
#
#   curl -fsSL https://raw.githubusercontent.com/immoJUMP/immojump-cli/main/install.sh | sh
#
# Holt das passende Binary vom GitHub-Release, prueft die Checksumme und legt
# es in einem Verzeichnis auf dem PATH ab. Ohne Go, ohne Repo-Klon.
#
# Steuerbar ueber Umgebungsvariablen — dafuer ist das Skript da, damit es sich
# in Dockerfiles und Agent-Container einbauen laesst:
#
#   IMMOJUMP_VERSION=v0.3.0   Feste Version statt "latest" (reproduzierbare Builds)
#   IMMOJUMP_INSTALL_DIR=...  Zielverzeichnis (Standard: /usr/local/bin, sonst ~/.local/bin)
#
# Wer gar kein Skript will, laedt direkt — die URL ist stabil:
#
#   https://github.com/immoJUMP/immojump-cli/releases/latest/download/immojump-linux-amd64

set -eu

REPO="immoJUMP/immojump-cli"
VERSION="${IMMOJUMP_VERSION:-latest}"

fehler() {
	echo "Fehler: $*" >&2
	exit 1
}

hinweis() {
	echo "==> $*"
}

# --- Plattform bestimmen ----------------------------------------------------
betriebssystem="$(uname -s)"
architektur="$(uname -m)"

case "$betriebssystem" in
Linux) os="linux" ;;
Darwin) os="darwin" ;;
*) fehler "Nicht unterstuetztes System: $betriebssystem. Binaries gibt es fuer Linux und macOS." ;;
esac

case "$architektur" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*) fehler "Nicht unterstuetzte Architektur: $architektur." ;;
esac

datei="immojump-${os}-${arch}"

# --- Quelle bestimmen -------------------------------------------------------
if [ "$VERSION" = "latest" ]; then
	basis="https://github.com/${REPO}/releases/latest/download"
else
	basis="https://github.com/${REPO}/releases/download/${VERSION}"
fi

# --- Werkzeuge pruefen ------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
	lade() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
	lade() { wget -qO "$2" "$1"; }
else
	fehler "Weder curl noch wget gefunden."
fi

if command -v sha256sum >/dev/null 2>&1; then
	pruefsumme() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
	pruefsumme() { shasum -a 256 "$1" | awk '{print $1}'; }
else
	pruefsumme() { echo ""; }
fi

# --- Herunterladen ----------------------------------------------------------
arbeitsordner="$(mktemp -d)"
# shellcheck disable=SC2064 # Pfad soll jetzt aufgeloest werden, nicht beim Exit.
trap "rm -rf '$arbeitsordner'" EXIT INT TERM

# Den Tag hinter "latest" ueber die API aufloesen.
#
# Noetig, weil GitHub die Weiterleitung `releases/latest/download/...` kurz
# nach einer Veroeffentlichung noch mit 404 beantwortet, waehrend die URL mit
# ausgeschriebener Version bereits liefert. Genau in dieses Fenster laufen
# Leute, die zufaellig direkt nach einem Release installieren.
#
# Der API-Aufruf passiert deshalb erst im Fehlerfall — im Normalfall kostet
# er nichts und das anonyme Rate-Limit bleibt unangetastet.
neuester_tag() {
	lade "https://api.github.com/repos/${REPO}/releases/latest" \
		"${arbeitsordner}/release.json" 2>/dev/null || return 1
	sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
		"${arbeitsordner}/release.json" | head -1
}

hinweis "Lade ${datei} (${VERSION}) …"

ziel_datei="${arbeitsordner}/${datei}"
umweg_versucht=0
geladen=0

versuch=1
while [ "$versuch" -le 3 ]; do
	if lade "${basis}/${datei}" "$ziel_datei" 2>/dev/null; then
		geladen=1
		break
	fi

	# Erster Fehlschlag mit "latest": auf die ausgeschriebene Version wechseln.
	if [ "$VERSION" = "latest" ] && [ "$umweg_versucht" -eq 0 ]; then
		umweg_versucht=1
		tag="$(neuester_tag || true)"
		if [ -n "${tag:-}" ]; then
			hinweis "\"latest\" antwortet gerade nicht — versuche ${tag} direkt."
			basis="https://github.com/${REPO}/releases/download/${tag}"
			continue
		fi
	fi

	versuch=$((versuch + 1))
	if [ "$versuch" -le 3 ]; then
		hinweis "Fehlgeschlagen, neuer Versuch in 3 Sekunden …"
		sleep 3
	fi
done

if [ "$geladen" -ne 1 ]; then
	echo "Fehler: Download fehlgeschlagen: ${basis}/${datei}" >&2
	echo "" >&2
	echo "Falls gerade eben eine neue Version erschienen ist, braucht GitHub" >&2
	echo "einen Moment, bis die Dateien ueberall abrufbar sind. In einer Minute" >&2
	echo "noch einmal versuchen — oder eine Version festnageln:" >&2
	echo "  curl -fsSL .../install.sh | IMMOJUMP_VERSION=v0.3.0 sh" >&2
	echo "Alle Versionen: https://github.com/${REPO}/releases" >&2
	exit 1
fi

# --- Checksumme pruefen -----------------------------------------------------
# Ohne Pruefung waere das hier ein „curl | sh" auf eine Binaerdatei — die
# Checksumme liegt am selben Release und kostet einen zweiten kleinen Download.
if lade "${basis}/SHA256SUMS.txt" "${arbeitsordner}/SHA256SUMS.txt" 2>/dev/null; then
	erwartet="$(awk -v f="$datei" '$2 == f || $2 == "*"f {print $1}' "${arbeitsordner}/SHA256SUMS.txt")"
	tatsaechlich="$(pruefsumme "${arbeitsordner}/${datei}")"
	if [ -z "$tatsaechlich" ]; then
		hinweis "Warnung: weder sha256sum noch shasum vorhanden — Checksumme nicht geprueft."
	elif [ -z "$erwartet" ]; then
		hinweis "Warnung: keine Checksumme fuer ${datei} im Release gefunden."
	elif [ "$erwartet" != "$tatsaechlich" ]; then
		fehler "Checksumme stimmt nicht (erwartet ${erwartet}, bekommen ${tatsaechlich}). Abbruch."
	else
		hinweis "Checksumme geprueft."
	fi
else
	hinweis "Warnung: SHA256SUMS.txt nicht abrufbar — Checksumme nicht geprueft."
fi

# --- Zielverzeichnis --------------------------------------------------------
if [ -n "${IMMOJUMP_INSTALL_DIR:-}" ]; then
	ziel="$IMMOJUMP_INSTALL_DIR"
elif [ -w /usr/local/bin ] 2>/dev/null; then
	ziel="/usr/local/bin"
else
	ziel="${HOME}/.local/bin"
fi

mkdir -p "$ziel" || fehler "Zielverzeichnis nicht anlegbar: $ziel"

chmod +x "${arbeitsordner}/${datei}"
mv "${arbeitsordner}/${datei}" "${ziel}/immojump" ||
	fehler "Konnte nicht nach ${ziel} schreiben. Setze IMMOJUMP_INSTALL_DIR oder nutze sudo."

hinweis "Installiert: ${ziel}/immojump"

# --- Auf dem PATH? ----------------------------------------------------------
case ":${PATH}:" in
*":${ziel}:"*) ;;
*)
	echo ""
	echo "Hinweis: ${ziel} liegt nicht auf deinem PATH. Ergaenze in deiner Shell-Konfiguration:"
	echo "  export PATH=\"${ziel}:\$PATH\""
	;;
esac

echo ""
"${ziel}/immojump" --version 2>/dev/null || true
echo ""
echo "Naechster Schritt: Token unter Einstellungen -> Schnittstellen-Zugang erzeugen, dann"
echo "  immojump context add ..."
