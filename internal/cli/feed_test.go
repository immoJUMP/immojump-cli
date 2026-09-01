package cli

import (
	"strings"
	"testing"
)

// TestFeedPostIsExternal: Ein `@nickname` in Titel oder Nachricht erzeugt eine
// Benachrichtigung UND eine E-Mail an echte Kollegen
// (modules/services/mention_service.py: send_email=True). Damit ist ein
// Feed-Beitrag genauso wenig zurückholbar wie eine versendete Mail — ein Agent
// mit `--allow read,write` darf da nicht drankommen.
func TestFeedPostIsExternal(t *testing.T) {
	for _, verb := range []string{"post", "comment", "comment-object"} {
		spec, ok := Lookup("feed", verb)
		if !ok {
			t.Fatalf("feed %s fehlt in der Registry", verb)
		}
		if spec.Risk != RiskExternal {
			t.Errorf("feed %s: Risk = %q, want %q — Erwähnungen verschicken E-Mails",
				verb, spec.Risk, RiskExternal)
		}
	}
}

// Die Gegenprobe: Was niemanden benachrichtigt, bleibt write.
func TestFeedHousekeepingIsWrite(t *testing.T) {
	for _, verb := range []string{"seen", "react", "edit", "channel-create", "channel-rename"} {
		spec, ok := Lookup("feed", verb)
		if !ok {
			t.Fatalf("feed %s fehlt", verb)
		}
		if spec.Risk != RiskWrite {
			t.Errorf("feed %s: Risk = %q, want %q", verb, spec.Risk, RiskWrite)
		}
	}
	for _, verb := range []string{"comment-delete", "channel-delete"} {
		spec, ok := Lookup("feed", verb)
		if !ok {
			t.Fatalf("feed %s fehlt", verb)
		}
		if spec.Risk != RiskDestructive {
			t.Errorf("feed %s: Risk = %q, want %q", verb, spec.Risk, RiskDestructive)
		}
	}
}

// TestFeedPostExplainsMentions: Wer den Befehl in --help liest, muss sehen,
// dass @nickname Menschen anschreibt. Ohne den Hinweis wirkt `feed post` wie
// ein Logbuch-Eintrag.
func TestFeedPostExplainsMentions(t *testing.T) {
	spec, _ := Lookup("feed", "post")
	if !strings.Contains(spec.Summary, "@") {
		t.Errorf("Summary muss die @nickname-Wirkung nennen: %q", spec.Summary)
	}
	h := newHarness(t)
	_, help, _ := h.run("feed", "post", "--help")
	if !strings.Contains(help, "@") {
		t.Errorf("--help soll Erwähnungen erklären:\n%s", help)
	}
}

// TestFeedMentionsIsBotOnly: GET /api/bots/me/mentions weist Menschen mit 403
// ab. Das gehört in die Beschreibung, sonst sucht jemand den Fehler bei sich.
func TestFeedMentionsIsBotOnly(t *testing.T) {
	spec, ok := Lookup("feed", "mentions")
	if !ok {
		t.Fatal("feed mentions fehlt")
	}
	if spec.Risk != RiskRead {
		t.Errorf("Risk = %q, want %q", spec.Risk, RiskRead)
	}
	if !strings.Contains(strings.ToLower(spec.Summary), "bot") {
		t.Errorf("Summary muss sagen, dass es nur mit Bot-Token geht: %q", spec.Summary)
	}
	declared := map[string]bool{}
	for _, hint := range spec.QueryHints {
		declared[hint.Name] = true
	}
	if !declared["since"] {
		t.Error("QueryHint since fehlt — ohne ihn wiederholt sich jede Erwähnung")
	}
}

// TestNotificationsIgnoreOrgHeader hält eine echte Falle fest: Die Route liest
// current_user.current_organisation_id und ignoriert X-Organisation-Id. Ein
// `--org` wirkt hier also NICHT, und das muss dranstehen.
func TestNotificationsIgnoreOrgHeader(t *testing.T) {
	spec, ok := Lookup("notifications", "list")
	if !ok {
		t.Fatal("notifications list fehlt")
	}
	low := strings.ToLower(spec.Summary)
	if !strings.Contains(low, "profil") && !strings.Contains(low, "--org") {
		t.Errorf("Summary muss sagen, dass --org hier nicht greift: %q", spec.Summary)
	}
}

// Lesen muss unter --readonly gehen, Schreiben nicht.
func TestFeedPolicyBoundaries(t *testing.T) {
	allowed := [][]string{
		{"--readonly", "feed", "list"},
		{"--readonly", "feed", "channels"},
		{"--readonly", "notifications", "list"},
	}
	for _, args := range allowed {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)
			if code, _, stderr := h.run(args...); code != 0 {
				t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
			}
		})
	}
	blocked := [][]string{
		{"--readonly", "feed", "post", "--message", "hi"},
		{"--allow", "read,write", "feed", "post", "--message", "hi"},
		{"--allow", "read,write", "feed", "comment", "e1", "--message", "hi"},
		{"--readonly", "feed", "channel-delete", "c1"},
	}
	for _, args := range blocked {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(args...)
			if code != 6 {
				t.Fatalf("Exit 6 erwartet, got %d (%s)", code, stderr)
			}
			if h.last != nil {
				t.Error("blockierter Befehl darf keinen Request absetzen")
			}
		})
	}
}

// Ein Beitrag ohne Text ist immer ein Bedienfehler.
func TestFeedPostNeedsMessage(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("feed", "post", "--title", "Nur ein Titel")
	if code != 2 {
		t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("ohne Nachricht darf kein Request rausgehen")
	}
}
