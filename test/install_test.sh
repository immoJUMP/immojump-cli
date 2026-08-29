#!/bin/sh
# Verhaltenstest fuer install.sh.
#
# Geprueft wird, was der Nutzer merkt: Installiert es? Bricht es sauber ab?
# Bleibt der Exit-Code aussagekraeftig? Der Test laeuft gegen die echten
# GitHub-Releases — Attrappen wuerden genau das nicht pruefen, was hier
# schiefgehen kann (URLs, Weiterleitungen, Asset-Namen).
#
# Aufruf: sh test/install_test.sh

set -eu

skript="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)/install.sh"
fehlgeschlagen=0

bestanden() { echo "  ok   $*"; }
durchgefallen() {
	echo "  FAIL $*" >&2
	fehlgeschlagen=1
}

# --- 1. Normalfall ----------------------------------------------------------
echo "install.sh installiert die aktuelle Version"
ordner="$(mktemp -d)"
if IMMOJUMP_INSTALL_DIR="$ordner" sh "$skript" >"${ordner}/log" 2>&1; then
	bestanden "Exit 0"
else
	durchgefallen "Exit ungleich 0 — Log:"
	cat "${ordner}/log" >&2
fi

if [ -x "${ordner}/immojump" ]; then
	bestanden "Binary liegt da und ist ausfuehrbar"
else
	durchgefallen "Binary fehlt oder ist nicht ausfuehrbar"
fi

if "${ordner}/immojump" --version >/dev/null 2>&1; then
	bestanden "laeuft: $("${ordner}/immojump" --version)"
else
	durchgefallen "Binary laeuft nicht"
fi

if grep -q "Checksumme geprueft" "${ordner}/log"; then
	bestanden "Checksumme wurde geprueft"
else
	durchgefallen "Checksumme wurde NICHT geprueft — Log:"
	cat "${ordner}/log" >&2
fi
rm -rf "$ordner"

# --- 2. Feste Version -------------------------------------------------------
# Der Weg, den Dockerfiles nehmen: reproduzierbar, ohne "latest".
echo "install.sh nagelt eine Version fest"
ordner="$(mktemp -d)"
if IMMOJUMP_VERSION=v0.3.0 IMMOJUMP_INSTALL_DIR="$ordner" sh "$skript" >"${ordner}/log" 2>&1; then
	ausgabe="$("${ordner}/immojump" --version 2>/dev/null || echo '?')"
	if [ "$ausgabe" = "v0.3.0" ]; then
		bestanden "IMMOJUMP_VERSION wird eingehalten (${ausgabe})"
	else
		durchgefallen "v0.3.0 erwartet, bekommen ${ausgabe}"
	fi
else
	durchgefallen "Exit ungleich 0 bei fester Version"
	cat "${ordner}/log" >&2
fi
rm -rf "$ordner"

# --- 3. Sauberer Abbruch ----------------------------------------------------
# Ein Fehlschlag muss sich als Fehlschlag anfuehlen: Exit ungleich 0 und ein
# Hinweis, wo es weitergeht. Frueher endete das Skript hier wortkarg.
echo "install.sh bricht bei unbekannter Version sauber ab"
ordner="$(mktemp -d)"
if IMMOJUMP_VERSION=v99.99.99 IMMOJUMP_INSTALL_DIR="$ordner" sh "$skript" >"${ordner}/log" 2>&1; then
	durchgefallen "Exit 0 obwohl es die Version nicht gibt"
else
	bestanden "Exit ungleich 0"
fi

if grep -q "releases" "${ordner}/log"; then
	bestanden "nennt den Weg zu den verfuegbaren Versionen"
else
	durchgefallen "Fehlermeldung ohne Hinweis — Log:"
	cat "${ordner}/log" >&2
fi

if [ -e "${ordner}/immojump" ]; then
	durchgefallen "halbfertige Datei zurueckgelassen"
else
	bestanden "nichts Halbfertiges hinterlassen"
fi
rm -rf "$ordner"

# --- Ergebnis ---------------------------------------------------------------
if [ "$fehlgeschlagen" -ne 0 ]; then
	echo "install.sh: Tests fehlgeschlagen" >&2
	exit 1
fi
echo "install.sh: alle Tests bestanden"
