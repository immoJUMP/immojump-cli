package cli

import (
	"strings"
	"testing"
)

// TestListFlagsBecomeJSONArrays hält den Vertrag von FlagList im Body fest:
// wiederholbar UND kommagetrennt, wie bei --tag-ids.
//
// Vorher war das still falsch: flagBodyValue griff über flags.get() nur den
// LETZTEN Wert ab. Eine Mail an drei Empfänger wäre an einen gegangen, ein
// Stapel-Befehl hätte genau eine Nachricht angefasst — ohne Fehlermeldung.
func TestListFlagsBecomeJSONArrays(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "wiederholt",
			args: []string{"email", "mark-read", "--message-ids", "a", "--message-ids", "b"},
			want: `{"message_ids":["a","b"]}`,
		},
		{
			name: "kommagetrennt",
			args: []string{"email", "mark-read", "--message-ids", "a,b"},
			want: `{"message_ids":["a","b"]}`,
		},
		{
			name: "gemischt, mit Leerzeichen",
			args: []string{"email", "mark-read", "--message-ids", "a, b", "--message-ids", "c"},
			want: `{"message_ids":["a","b","c"]}`,
		},
		{
			name: "ein einzelner Wert bleibt ein Array",
			args: []string{"email", "mark-read", "--message-ids", "a"},
			want: `{"message_ids":["a"]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(tc.args...)
			if code != 0 {
				t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
			}
			if h.last == nil {
				t.Fatal("kein Request abgesetzt")
			}
			if !sameJSON(t, h.last.Body, tc.want) {
				t.Errorf("Body = %s, want %s", h.last.Body, tc.want)
			}
		})
	}
}

// TestEmailSendBuildsRecipientArrays: to/cc/bcc sind im Backend Listen
// (modules/routes/email_account_routes.py: data.get('to') or []). Ein String
// statt einer Liste würde dort zeichenweise iteriert.
func TestEmailSendBuildsRecipientArrays(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run(
		"email", "send", "acc-1",
		"--to", "kunde@example.com",
		"--cc", "chef@example.com,buero@example.com",
		"--bcc", "archiv@example.com",
		"--subject", "Exposé Musterstraße 1",
		"--html", "<p>Anbei das Exposé.</p>",
	)
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last == nil {
		t.Fatal("kein Request abgesetzt")
	}
	if h.last.Method != "POST" || h.last.Path != "/api/org/email-accounts/acc-1/send" {
		t.Errorf("Endpoint = %s %s", h.last.Method, h.last.Path)
	}
	want := `{"to":["kunde@example.com"],` +
		`"cc":["chef@example.com","buero@example.com"],` +
		`"bcc":["archiv@example.com"],` +
		`"subject":"Exposé Musterstraße 1",` +
		`"html":"<p>Anbei das Exposé.</p>"}`
	if !sameJSON(t, h.last.Body, want) {
		t.Errorf("Body = %s, want %s", h.last.Body, want)
	}
}

// TestEmailSendIsExternal: Eine verschickte Mail holt niemand zurück. Das
// Level muss external sein — sonst kommt ein Agent mit `--allow read,write`
// daran vorbei, und genau so eine Rolle hat das Backend im August
// zurückgewiesen (a463f22ac, rbac.can(org, 'create', 'email')).
func TestEmailSendIsExternal(t *testing.T) {
	spec, ok := Lookup("email", "send")
	if !ok {
		t.Fatal("email send fehlt in der Registry")
	}
	if spec.Risk != RiskExternal {
		t.Errorf("Risk = %q, want %q", spec.Risk, RiskExternal)
	}

	h := newHarness(t)
	code, _, stderr := h.run("--allow", "read,write", "email", "send", "acc-1",
		"--to", "kunde@example.com", "--subject", "Test")
	if code != 6 {
		t.Fatalf("Exit 6 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("blockierter Versand darf keinen Request absetzen")
	}
}

// TestEmailTrashIsReversible hält eine bewusste Entscheidung fest: /trash
// setzt nur is_deleted=True (email_message_service.trash_messages) — die
// Nachricht ist über `email move --folder INBOX` zurückholbar. Als
// destructive eingestuft wäre das Level eine Lüge und der Befehl für
// vorsichtig konfigurierte Agenten unnötig gesperrt.
//
// Der Gegentest steht direkt darunter: Ordner löschen IST destruktiv.
func TestEmailTrashIsReversible(t *testing.T) {
	spec, ok := Lookup("email", "trash")
	if !ok {
		t.Fatal("email trash fehlt in der Registry")
	}
	if spec.Risk != RiskWrite {
		t.Errorf("Risk = %q, want %q — /trash flippt ein Flag, es löscht nicht", spec.Risk, RiskWrite)
	}
	if !strings.Contains(strings.ToLower(spec.Summary), "papierkorb") {
		t.Errorf("Summary muss sagen, dass es der Papierkorb ist: %q", spec.Summary)
	}
}

func TestEmailFolderDeleteIsDestructive(t *testing.T) {
	spec, ok := Lookup("email", "folder-delete")
	if !ok {
		t.Fatal("email folder-delete fehlt in der Registry")
	}
	if spec.Risk != RiskDestructive {
		t.Errorf("Risk = %q, want %q", spec.Risk, RiskDestructive)
	}
}

// TestEmailGetAnnouncesItsSideEffect: GET /api/email-messages/<id> markiert
// die Nachricht beim Öffnen als gelesen. Das ist eine Zustandsänderung in
// einem Befehl mit Risk read — vertretbar nur, solange sie in --help, docs
// und schema steht. Ein Agent, der stumm 50 Mails als gelesen markiert, ist
// eine Kundenbeschwerde.
func TestEmailGetAnnouncesItsSideEffect(t *testing.T) {
	spec, ok := Lookup("email", "get")
	if !ok {
		t.Fatal("email get fehlt in der Registry")
	}
	if spec.Risk != RiskRead {
		t.Errorf("Risk = %q, want %q", spec.Risk, RiskRead)
	}
	if !strings.Contains(strings.ToLower(spec.Summary), "gelesen") {
		t.Errorf("Summary muss den Nebeneffekt nennen (markiert als gelesen): %q", spec.Summary)
	}

	// Und er muss beim Aufrufer wirklich ankommen, nicht nur in der Struktur.
	h := newHarness(t)
	_, help, _ := h.run("email", "get", "--help")
	if !strings.Contains(strings.ToLower(help), "gelesen") {
		t.Errorf("--help soll den Nebeneffekt zeigen:\n%s", help)
	}
}

// TestEmailReadCommandsSurviveReadonly: Das Postfach zu lesen ist der
// Hauptzweck; ein --readonly-Agent muss das können.
func TestEmailReadCommandsSurviveReadonly(t *testing.T) {
	for _, args := range [][]string{
		{"--readonly", "email", "list"},
		{"--readonly", "email", "get", "msg-1"},
		{"--readonly", "email", "search", "-q", "q=Notar"},
		{"--readonly", "email", "accounts"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(args...)
			if code != 0 {
				t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
			}
			if h.last == nil {
				t.Error("Lesebefehl muss durchgehen")
			}
		})
	}
}

// TestEmailWriteCommandsBlockedByReadonly ist die Gegenprobe.
func TestEmailWriteCommandsBlockedByReadonly(t *testing.T) {
	for _, args := range [][]string{
		{"--readonly", "email", "mark-read", "--message-ids", "a"},
		{"--readonly", "email", "trash", "--message-ids", "a"},
		{"--readonly", "email", "send", "acc-1", "--to", "x@example.com"},
		{"--readonly", "email", "folder-delete", "--name", "Alt"},
		{"--readonly", "email", "sync"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(args...)
			if code != 6 {
				t.Fatalf("Exit 6 erwartet, got %d (%s)", code, stderr)
			}
			if h.last != nil {
				t.Error("blockierter Befehl darf keinen Request absetzen")
			}
			if line := errorLine(t, stderr); line["code"] != "POLICY_BLOCKED" {
				t.Errorf("code POLICY_BLOCKED erwartet, got %#v", line["code"])
			}
		})
	}
}

// TestEmailAccountManagementStaysOut: Konten anlegen/ändern heißt SMTP- und
// IMAP-Passwörter setzen. Als Flag landen die in der Shell-History und im
// Transkript jedes Agenten — deshalb bleibt das bewusst dem Frontend (und
// im Notfall dem Escape-Hatch) überlassen. Lesen der Konten reicht, um die
// account-id für `email send` zu finden.
func TestEmailAccountManagementStaysOut(t *testing.T) {
	for _, verb := range []string{"account-create", "account-update", "account-delete"} {
		if _, ok := Lookup("email", verb); ok {
			t.Errorf("email %s soll es nicht geben — Zugangsdaten gehören nicht in argv", verb)
		}
	}
	if _, ok := Lookup("email", "accounts"); !ok {
		t.Error("email accounts (lesen) wird für die account-id von `email send` gebraucht")
	}
}

// TestEmailSendNeedsRecipients: Ein Versand ohne Empfänger ist immer ein
// Bedienfehler — den fängt das CLI ab, statt ihn als 200 mit null Wirkung
// durchzureichen (das Backend nimmt `to: []` klaglos an).
func TestEmailSendNeedsRecipients(t *testing.T) {
	cases := map[string][]string{
		"--to fehlt ganz": {"email", "send", "acc-1", "--subject", "Ohne Empfänger"},
		"--to ist leer":   {"email", "send", "acc-1", "--to", "", "--subject", "Ohne Empfänger"},
		"--to nur Kommas": {"email", "send", "acc-1", "--to", " , ", "--subject", "Ohne Empfänger"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(args...)
			if code != 2 {
				t.Fatalf("Exit 2 (Bedienfehler) erwartet, got %d (%s)", code, stderr)
			}
			if h.last != nil {
				t.Error("ohne Empfänger darf kein Request rausgehen")
			}
		})
	}
}

// TestEnvelopeExamplesProjectIntoItems: Diese Routen antworten mit einem
// Envelope ({items, total, …}). Ein Beispiel wie `--fields id,subject` trifft
// dort NICHTS — das CLI gibt `{}` aus und warnt auf stderr.
//
// Gemessen gegen Produktion am 01.09.2026: `immojump immobilien search
// --fields id,name` und `contacts list -q per_page=25 --fields id,…` lieferten
// beide "fields_missing". Das Beispiel war seit v0.1 falsch, und ein Beispiel,
// das nichts tut, ist schlimmer als keins (dieselbe Regel wie bei -q).
//
// `immobilien list` steht bewusst NICHT in der Liste: ohne page/per_page
// antwortet es flach, und genau so steht es im Beispiel.
func TestEnvelopeExamplesProjectIntoItems(t *testing.T) {
	envelope := []string{
		"contacts list",
		"immobilien search",
		"email list",
		"email search",
		"email accounts",
	}
	for _, name := range envelope {
		parts := strings.SplitN(name, " ", 2)
		spec, ok := Lookup(parts[0], parts[1])
		if !ok {
			t.Fatalf("%q fehlt in der Registry", name)
		}
		_, fields, found := strings.Cut(spec.Example, "--fields ")
		if !found {
			continue // kein --fields-Beispiel, nichts zu prüfen
		}
		for _, field := range strings.Split(strings.Fields(fields)[0], ",") {
			if !strings.HasPrefix(field, "items.") {
				t.Errorf("%q: Beispiel projiziert auf %q, die Antwort ist aber ein Envelope — items.%s gemeint?",
					name, field, field)
			}
		}
	}
}

// TestOutboxStatusHintNamesRealValues: EmailImapOutbox.status wird exakt
// verglichen (list_entries: `.filter(status == status)`), und OutboxStatus
// ist GROSSGESCHRIEBEN. Ein Hinweis auf "pending, failed oder done" wäre
// gleich dreifach falsch: falsche Schreibweise, und "done" gibt es nicht.
func TestOutboxStatusHintNamesRealValues(t *testing.T) {
	spec, ok := Lookup("email", "outbox")
	if !ok {
		t.Fatal("email outbox fehlt in der Registry")
	}
	var hint string
	for _, h := range spec.QueryHints {
		if h.Name == "status" {
			hint = h.Summary
		}
	}
	if hint == "" {
		t.Fatal("QueryHint status fehlt")
	}
	for _, want := range []string{"PENDING", "IN_PROGRESS", "COMPLETED", "FAILED"} {
		if !strings.Contains(hint, want) {
			t.Errorf("status-Hinweis nennt %q nicht: %q", want, hint)
		}
	}
	if strings.Contains(hint, "done") {
		t.Errorf("status-Hinweis nennt den erfundenen Wert \"done\": %q", hint)
	}
	// Das Beispiel muss denselben echten Wert benutzen — kleingeschrieben
	// filtert es gegen Produktion stillschweigend alles weg.
	if strings.Contains(spec.Example, "status=") && !strings.Contains(spec.Example, "status=FAILED") {
		t.Errorf("Beispiel nutzt einen anderen status-Wert als das Backend kennt: %q", spec.Example)
	}
}
