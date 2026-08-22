package cli

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommandRequestTable prüft für JEDEN Befehl der Registry, welchen
// HTTP-Request das CLI daraus baut. Das ist der Vertrag zum Backend.
func TestCommandRequestTable(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		method    string
		path      string
		query     url.Values
		body      string
		wantCT    string
		wantNoOrg bool
	}{
		// --- auth ---------------------------------------------------------
		{name: "auth status", args: []string{"auth", "status"}, method: "GET", path: "/api/user/me"},

		// --- contacts -----------------------------------------------------
		{name: "contacts list", args: []string{"contacts", "list"}, method: "GET", path: "/api/contacts"},
		{name: "contacts list mit Query", args: []string{"contacts", "list", "-q", "limit=10", "-q", "offset=20"},
			method: "GET", path: "/api/contacts", query: url.Values{"limit": {"10"}, "offset": {"20"}}},
		{name: "contacts get", args: []string{"contacts", "get", "42"}, method: "GET", path: "/api/contacts/42"},
		{name: "contacts create", args: []string{"contacts", "create", "--set", "first_name=Ada"},
			method: "POST", path: "/api/contacts", body: `{"first_name":"Ada"}`},
		{name: "contacts update", args: []string{"contacts", "update", "42", "--set", "last_name=Lovelace"},
			method: "PUT", path: "/api/contacts/42", body: `{"last_name":"Lovelace"}`},
		{name: "contacts set-status", args: []string{"contacts", "set-status", "42", "--set", "status_id=7"},
			method: "PUT", path: "/api/contacts/42/status", body: `{"status_id":7}`},
		{name: "contacts delete", args: []string{"contacts", "delete", "42"}, method: "DELETE", path: "/api/contacts/42"},
		{name: "contacts activities", args: []string{"contacts", "activities", "42"}, method: "GET", path: "/api/contacts/42/activities"},
		{name: "contacts immobilien", args: []string{"contacts", "immobilien", "42"}, method: "GET", path: "/api/contacts/42/immobilien"},

		// --- immobilien ---------------------------------------------------
		{name: "immobilien list", args: []string{"immobilien", "list"}, method: "GET", path: "/api/v2/immobilien"},
		{name: "immobilien search", args: []string{"immobilien", "search", "-q", "q=Köln"},
			method: "GET", path: "/api/v2/immobilien/search", query: url.Values{"q": {"Köln"}}},
		{name: "immobilien get", args: []string{"immobilien", "get", "5"}, method: "GET", path: "/api/v2/immobilien/5"},
		{name: "immobilien create", args: []string{"immobilien", "create", "--body", `{"name":"MFH"}`},
			method: "POST", path: "/api/v2/immobilien", body: `{"name":"MFH"}`},
		{name: "immobilien update", args: []string{"immobilien", "update", "5", "--set", "name=MFH"},
			method: "PUT", path: "/api/v2/immobilien/5", body: `{"name":"MFH"}`},
		{name: "immobilien patch", args: []string{"immobilien", "patch", "5", "--set", "kaufpreis=225000"},
			method: "PATCH", path: "/api/v2/immobilien/5", body: `{"kaufpreis":225000}`},
		{name: "immobilien delete", args: []string{"immobilien", "delete", "5"}, method: "DELETE", path: "/api/v2/immobilien/5"},
		{name: "immobilien contacts", args: []string{"immobilien", "contacts", "5"}, method: "GET", path: "/api/v2/immobilien/5/contacts"},
		{name: "immobilien duplicate", args: []string{"immobilien", "duplicate", "5"},
			method: "POST", path: "/api/v2/immobilien/5/duplicate", body: `{}`},

		// --- units --------------------------------------------------------
		{name: "units list", args: []string{"units", "list", "5"}, method: "GET", path: "/api/units/immobilie/5/units"},
		{name: "units create", args: []string{"units", "create", "5", "--set", "einheit=WE 1"},
			method: "POST", path: "/api/units/unit/5", body: `{"einheit":"WE 1"}`},
		{name: "units update", args: []string{"units", "update", "9", "--set", "ist_rent=780"},
			method: "PUT", path: "/api/units/unit/9", body: `{"ist_rent":780}`},
		{name: "units delete", args: []string{"units", "delete", "9"}, method: "DELETE", path: "/api/units/unit/9"},

		// --- activities ---------------------------------------------------
		{name: "activities list", args: []string{"activities", "list"}, method: "GET", path: "/api/activities/activities"},
		{name: "activities get", args: []string{"activities", "get", "3"}, method: "GET", path: "/api/activities/activities/3"},
		{name: "activities for-immobilie", args: []string{"activities", "for-immobilie", "5"},
			method: "GET", path: "/api/activities/activities/immobilie/5"},
		{name: "activities create", args: []string{"activities", "create", "--set", "title=Anruf", "--set", "type=ANRUF"},
			method: "POST", path: "/api/activities/activities", body: `{"title":"Anruf","type":"ANRUF"}`},
		{name: "activities update", args: []string{"activities", "update", "3", "--set", "status=Abgeschlossen"},
			method: "PUT", path: "/api/activities/activities/3", body: `{"status":"Abgeschlossen"}`},
		{name: "activities delete", args: []string{"activities", "delete", "3"},
			method: "DELETE", path: "/api/activities/activities/3"},

		// --- pipelines ({org} im Pfad) -------------------------------------
		{name: "pipelines list", args: []string{"pipelines", "list"}, method: "GET", path: "/api/pipelines/org-test/pipelines"},
		{name: "pipelines create", args: []string{"pipelines", "create", "--set", "name=Ankauf"},
			method: "POST", path: "/api/pipelines/org-test/pipelines", body: `{"name":"Ankauf"}`},
		{name: "pipelines get", args: []string{"pipelines", "get", "2"}, method: "GET", path: "/api/pipelines/pipelines/2"},
		{name: "pipelines update", args: []string{"pipelines", "update", "2", "--set", "name=Ankauf"},
			method: "PUT", path: "/api/pipelines/pipelines/2", body: `{"name":"Ankauf"}`},
		{name: "pipelines delete", args: []string{"pipelines", "delete", "2"}, method: "DELETE", path: "/api/pipelines/pipelines/2"},
		{name: "pipelines statuses", args: []string{"pipelines", "statuses", "2"}, method: "GET", path: "/api/pipelines/pipelines/2/statuses"},
		{name: "pipelines add-status", args: []string{"pipelines", "add-status", "2", "--set", "name=Neu"},
			method: "POST", path: "/api/pipelines/pipelines/2/statuses", body: `{"name":"Neu"}`},
		{name: "pipelines export", args: []string{"pipelines", "export", "2"}, method: "GET", path: "/api/pipelines/pipelines/2/export"},

		// --- statuses -----------------------------------------------------
		{name: "statuses list", args: []string{"statuses", "list"}, method: "GET", path: "/api/statuses/statuses"},
		{name: "statuses update", args: []string{"statuses", "update", "4", "--set", "name=Geprüft"},
			method: "PUT", path: "/api/statuses/statuses/4", body: `{"name":"Geprüft"}`},
		{name: "statuses delete", args: []string{"statuses", "delete", "4"}, method: "DELETE", path: "/api/statuses/statuses/4"},
		{name: "statuses swap", args: []string{"statuses", "swap", "4", "5", "--current-order", "1", "--target-order", "2"},
			method: "PUT", path: "/api/statuses/statuses/swap/4/5",
			body: `{"current_status_order":1,"target_status_order":2}`},
		{name: "statuses aliases", args: []string{"statuses", "aliases", "4"},
			method: "GET", path: "/api/statuses/statuses/4/inbound-aliases"},
		{name: "statuses add-alias", args: []string{"statuses", "add-alias", "4", "--set", "alias=neu@example.com"},
			method: "POST", path: "/api/statuses/statuses/4/inbound-aliases", body: `{"alias":"neu@example.com"}`},

		// --- templates ----------------------------------------------------
		{name: "templates list", args: []string{"templates", "list"},
			method: "GET", path: "/api/activity-templates/activity_templates"},
		{name: "templates recurring", args: []string{"templates", "recurring"},
			method: "GET", path: "/api/activity-templates/activity_templates/recurring"},
		{name: "templates by-status", args: []string{"templates", "by-status", "4"},
			method: "GET", path: "/api/activity-templates/activity_templates/status/4"},
		{name: "templates get", args: []string{"templates", "get", "8"},
			method: "GET", path: "/api/activity-templates/activity_templates/8"},
		{name: "templates create", args: []string{"templates", "create", "--set", "title=Exposé prüfen"},
			method: "POST", path: "/api/activity-templates/activity_templates", body: `{"title":"Exposé prüfen"}`},
		{name: "templates update", args: []string{"templates", "update", "8", "--set", "title=X"},
			method: "PUT", path: "/api/activity-templates/activity_templates/8", body: `{"title":"X"}`},
		{name: "templates delete", args: []string{"templates", "delete", "8"},
			method: "DELETE", path: "/api/activity-templates/activity_templates/8"},
		{name: "templates batch-move", args: []string{"templates", "batch-move", "--set", "from_status_id=1", "--set", "to_status_id=2"},
			method: "POST", path: "/api/activity-templates/activity_templates/status/batch_move",
			body: `{"from_status_id":1,"to_status_id":2}`},

		// --- documents ----------------------------------------------------
		{name: "documents list", args: []string{"documents", "list"}, method: "GET", path: "/api/documents/documents"},
		// Das Backend liest new_filename (document_routes.py) — nicht name.
		{name: "documents rename", args: []string{"documents", "rename", "11", "--name", "Exposé.pdf"},
			method: "PUT", path: "/api/documents/documents/11/rename", body: `{"new_filename":"Exposé.pdf"}`},
		{name: "documents delete", args: []string{"documents", "delete", "11"},
			method: "DELETE", path: "/api/documents/documents/11"},
		{name: "documents analyze", args: []string{"documents", "analyze", "11"},
			method: "POST", path: "/api/documents/documents/11/analyze", body: `{}`},
		{name: "documents analyze-details", args: []string{"documents", "analyze-details", "11"},
			method: "POST", path: "/api/documents/documents/11/analyze/details", body: `{}`},
		{name: "documents mark-reviewed", args: []string{"documents", "mark-reviewed", "11"},
			method: "POST", path: "/api/documents/documents/11/mark-reviewed", body: `{}`},
		{name: "documents analysis-results", args: []string{"documents", "analysis-results"},
			method: "GET", path: "/api/documents/analysis-results"},

		// --- tags ({org} im Pfad, Sonderfall set) --------------------------
		{name: "tags list", args: []string{"tags", "list", "-q", "for=contact"},
			method: "GET", path: "/api/org-test/tags", query: url.Values{"for": {"contact"}}},
		{name: "tags create", args: []string{"tags", "create", "--set", "name=Wichtig", "--set", "color=#ff0000"},
			method: "POST", path: "/api/org-test/tags", body: `{"name":"Wichtig","color":"#ff0000"}`},
		{name: "tags create ohne Farbe", args: []string{"tags", "create", "--set", "name=Wichtig"},
			method: "POST", path: "/api/org-test/tags", body: `{"name":"Wichtig"}`},
		{name: "tags update", args: []string{"tags", "update", "3", "--set", "name=Sehr wichtig"},
			method: "PUT", path: "/api/org-test/tags/3", body: `{"name":"Sehr wichtig"}`},
		{name: "tags delete", args: []string{"tags", "delete", "3"}, method: "DELETE", path: "/api/tags/3"},
		{name: "tags of", args: []string{"tags", "of", "contact", "42"}, method: "GET", path: "/api/tags/contact/42"},
		{name: "tags set", args: []string{"tags", "set", "contact", "42", "--tag-ids", "1,2"},
			method: "PUT", path: "/api/tags/contact/42", body: `["1","2"]`},
		{name: "tags set leer", args: []string{"tags", "set", "contact", "42", "--tag-ids", ""},
			method: "PUT", path: "/api/tags/contact/42", body: `[]`},

		// --- shares -------------------------------------------------------
		{name: "shares list", args: []string{"shares", "list"}, method: "GET", path: "/api/share-links"},
		{name: "shares list gefiltert", args: []string{"shares", "list", "--entity-type", "immobilie", "--entity-id", "5"},
			method: "GET", path: "/api/share-links",
			query: url.Values{"entity_type": {"immobilie"}, "entity_id": {"5"}}},
		{name: "shares create", args: []string{"shares", "create", "--immobilie", "5"},
			method: "POST", path: "/api/share-links",
			body: `{"items":[{"entity_type":"immobilie","entity_id":"5"}]}`},
		{name: "shares update", args: []string{"shares", "update", "7", "--title", "Neu"},
			method: "PATCH", path: "/api/share-links/7", body: `{"title":"Neu"}`},
		{name: "shares revoke", args: []string{"shares", "revoke", "7"},
			method: "DELETE", path: "/api/share-links/7"},

		// --- api (Escape-Hatch) -------------------------------------------
		{name: "api GET", args: []string{"api", "GET", "/api/deals"}, method: "GET", path: "/api/deals"},
		{name: "api GET mit Query", args: []string{"api", "GET", "/api/deals", "-q", "status=offen"},
			method: "GET", path: "/api/deals", query: url.Values{"status": {"offen"}}},
		{name: "api POST", args: []string{"api", "POST", "/api/deals", "--set", "title=Deal"},
			method: "POST", path: "/api/deals", body: `{"title":"Deal"}`},
		{name: "api Pfad ohne Slash", args: []string{"api", "GET", "api/deals"}, method: "GET", path: "/api/deals"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(tc.args...)
			if code != 0 {
				t.Fatalf("Exit 0 erwartet, got %d (stderr: %s)", code, stderr)
			}
			if h.last == nil {
				t.Fatal("kein HTTP-Request abgesetzt")
			}
			if h.last.Method != tc.method {
				t.Errorf("Methode %s erwartet, got %s", tc.method, h.last.Method)
			}
			if h.last.Path != tc.path {
				t.Errorf("Pfad %s erwartet, got %s", tc.path, h.last.Path)
			}
			for key, want := range tc.query {
				got := h.last.Query[key]
				if len(got) != len(want) || (len(want) > 0 && got[0] != want[0]) {
					t.Errorf("Query %s=%v erwartet, got %v", key, want, got)
				}
			}
			if len(tc.query) == 0 && len(h.last.Query) != 0 {
				t.Errorf("keine Query erwartet, got %v", h.last.Query)
			}
			if !sameJSON(t, h.last.Body, tc.body) {
				t.Errorf("Body\n got: %s\nwant: %s", h.last.Body, tc.body)
			}
			if h.last.Header.Get("Authorization") != "Bearer tok-test" {
				t.Errorf("Bearer-Header erwartet, got %q", h.last.Header.Get("Authorization"))
			}
			if h.last.Header.Get("X-Organisation-Id") != "org-test" {
				t.Errorf("X-Organisation-Id erwartet, got %q", h.last.Header.Get("X-Organisation-Id"))
			}
		})
	}
}

func TestPipelinesImportFromFile(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(t.TempDir(), "pipeline.yaml")
	yaml := "name: Ankauf\nstatuses:\n  - Neu\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	code, _, stderr := h.run("pipelines", "import", "--file", path)
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last.Method != "POST" || h.last.Path != "/api/pipelines/pipelines/import" {
		t.Errorf("POST /api/pipelines/pipelines/import erwartet, got %s %s", h.last.Method, h.last.Path)
	}
	if h.last.ContentType != "application/x-yaml" {
		t.Errorf("Content-Type application/x-yaml erwartet, got %q", h.last.ContentType)
	}
	if h.last.Body != yaml {
		t.Errorf("YAML unverändert erwartet, got %q", h.last.Body)
	}
}

func TestPipelinesImportFromStdin(t *testing.T) {
	h := newHarness(t)
	h.stdin = "name: Vertrieb\n"

	code, _, stderr := h.run("pipelines", "import")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last.Body != "name: Vertrieb\n" {
		t.Errorf("stdin als Body erwartet, got %q", h.last.Body)
	}
	if h.last.ContentType != "application/x-yaml" {
		t.Errorf("Content-Type application/x-yaml erwartet, got %q", h.last.ContentType)
	}
}

// TestPipelinesImportSendsOrganisationAsQuery: Die Import-Route liest die
// Organisation NUR aus dem Payload oder dem Query-String — den Header
// X-Organisation-Id ignoriert sie (pipeline_routes.py:import_pipeline).
func TestPipelinesImportSendsOrganisationAsQuery(t *testing.T) {
	h := newHarness(t)
	h.stdin = "name: Vertrieb\n"
	if code, _, stderr := h.run("pipelines", "import"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if got := h.last.Query.Get("organisation_id"); got != "org-test" {
		t.Errorf("?organisation_id=org-test erwartet, got %q (Query: %v)", got, h.last.Query)
	}

	// --org überschreibt.
	h2 := newHarness(t)
	h2.stdin = "name: Vertrieb\n"
	if code, _, stderr := h2.run("pipelines", "import", "--org", "org-anders"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if got := h2.last.Query.Get("organisation_id"); got != "org-anders" {
		t.Errorf("--org soll in die Query wandern, got %q", got)
	}
}

func TestPipelinesImportWithoutOrganisationExitsWith3(t *testing.T) {
	h := newHarness(t)
	h.stdin = "name: Vertrieb\n"
	delete(h.env, "IMMOJUMP_ORGANISATION_ID")
	code, _, stderr := h.run("pipelines", "import")
	if code != 3 {
		t.Fatalf("Exit 3 erwartet, got %d (%s)", code, stderr)
	}
	if h.last != nil {
		t.Error("ohne Organisation darf kein Request rausgehen")
	}
	msg, _ := errorLine(t, stderr)["message"].(string)
	if !strings.Contains(strings.ToLower(msg), "organisation") {
		t.Errorf("Meldung soll die Organisation benennen, got %q", msg)
	}
}

// TestStatusesSwapSendsOrdersAsJSONNumbers: Das Backend vergleicht die Werte
// als Zahlen — "1" als String läuft in einen Typfehler.
func TestStatusesSwapSendsOrdersAsJSONNumbers(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("statuses", "swap", "4", "5",
		"--current-order", "1", "--target-order", "2"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	for _, want := range []string{`"current_status_order":1`, `"target_status_order":2`} {
		if !strings.Contains(h.last.Body, want) {
			t.Errorf("Body soll %s enthalten (Zahl, kein String), got %s", want, h.last.Body)
		}
	}
}

func TestStatusesSwapRequiresBothOrders(t *testing.T) {
	cases := [][]string{
		{"statuses", "swap", "4", "5"},
		{"statuses", "swap", "4", "5", "--current-order", "1"},
		{"statuses", "swap", "4", "5", "--target-order", "2"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			h := newHarness(t)
			code, _, stderr := h.run(args...)
			if code != 2 {
				t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
			}
			if h.last != nil {
				t.Error("ohne Order-Werte darf kein Request rausgehen")
			}
		})
	}
}

func TestNumberFlagRejectsNonNumbers(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("statuses", "swap", "4", "5", "--current-order", "eins", "--target-order", "2")
	if code != 2 {
		t.Fatalf("Exit 2 erwartet, got %d (%s)", code, stderr)
	}
	msg, _ := errorLine(t, stderr)["message"].(string)
	if !strings.Contains(msg, "current-order") {
		t.Errorf("Meldung soll das Flag benennen, got %q", msg)
	}
}

// TestBodyKeepsNumericPrecision: encoding/json parst Zahlen sonst als float64
// — große IDs verlieren Stellen, 225000.00 wird zu 225000. Beides ändert die
// Daten, die beim Kunden landen.
func TestBodyKeepsNumericPrecision(t *testing.T) {
	h := newHarness(t)
	body := `{"externe_id":12345678901234567890,"kaufpreis":225000.00}`
	// Mit --set daneben, damit der Re-Marshal-Pfad genommen wird.
	if code, _, stderr := h.run("immobilien", "patch", "5", "--body", body, "--set", "name=MFH"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	for _, want := range []string{`"externe_id":12345678901234567890`, `"kaufpreis":225000.00`} {
		if !strings.Contains(h.last.Body, want) {
			t.Errorf("Zahl unverfälscht erwartet: %s\ngot: %s", want, h.last.Body)
		}
	}
}

func TestSetKeepsNumericPrecision(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("immobilien", "patch", "5",
		"--set", "externe_id=12345678901234567890", "--set", "kaufpreis=225000.00"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	for _, want := range []string{`"externe_id":12345678901234567890`, `"kaufpreis":225000.00`} {
		if !strings.Contains(h.last.Body, want) {
			t.Errorf("Zahl unverfälscht erwartet: %s\ngot: %s", want, h.last.Body)
		}
	}
}

// TestBodyWithoutOverlaysIsPassedThroughByteExact hält den einfachen Pfad fest.
func TestBodyWithoutOverlaysIsPassedThroughByteExact(t *testing.T) {
	h := newHarness(t)
	body := `{"externe_id":12345678901234567890,"kaufpreis":225000.00}`
	if code, _, stderr := h.run("immobilien", "patch", "5", "--body", body); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last.Body != body {
		t.Errorf("Body unverändert erwartet\n got: %s\nwant: %s", h.last.Body, body)
	}
}

func TestDocumentsUploadMultipart(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(t.TempDir(), "expose.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4 test"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	code, _, stderr := h.run("documents", "upload", path, "--immobilie-id", "5", "--allow-duplicate")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if h.last.Method != "POST" || h.last.Path != "/api/documents/documents/bulk-upload" {
		t.Errorf("POST bulk-upload erwartet, got %s %s", h.last.Method, h.last.Path)
	}
	if h.last.Multipart == nil {
		t.Fatal("Multipart-Formular erwartet")
	}
	files := h.last.Multipart.File["files[]"]
	if len(files) != 1 {
		t.Fatalf("eine Datei im Feld files[] erwartet, got %v", h.last.Multipart.File)
	}
	if files[0].Filename != "expose.pdf" {
		t.Errorf("Dateiname expose.pdf erwartet, got %q", files[0].Filename)
	}
	if got := h.last.Multipart.Value["organisation_id"]; len(got) != 1 || got[0] != "org-test" {
		t.Errorf("organisation_id=org-test erwartet, got %v", got)
	}
	if got := h.last.Multipart.Value["immobilien_id"]; len(got) != 1 || got[0] != "5" {
		t.Errorf("immobilien_id=5 erwartet, got %v", got)
	}
	if got := h.last.Multipart.Value["allow_duplicate_upload"]; len(got) != 1 || got[0] != "true" {
		t.Errorf("allow_duplicate_upload=true erwartet, got %v", got)
	}
}

func TestDocumentsUploadOmitsOptionalFields(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(t.TempDir(), "a.pdf")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if code, _, stderr := h.run("documents", "upload", path); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if _, ok := h.last.Multipart.Value["immobilien_id"]; ok {
		t.Error("ohne --immobilie-id kein Formfeld immobilien_id erwartet")
	}
	if _, ok := h.last.Multipart.Value["allow_duplicate_upload"]; ok {
		t.Error("ohne --allow-duplicate kein Formfeld allow_duplicate_upload erwartet")
	}
}

func TestBodyFromStdinAndFile(t *testing.T) {
	h := newHarness(t)
	h.stdin = `{"first_name":"Grace"}`
	if code, _, stderr := h.run("contacts", "create", "--body", "-"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if !sameJSON(t, h.last.Body, `{"first_name":"Grace"}`) {
		t.Errorf("Body aus stdin erwartet, got %s", h.last.Body)
	}

	h2 := newHarness(t)
	path := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(path, []byte(`{"first_name":"Ada"}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if code, _, stderr := h2.run("contacts", "create", "--body", "@"+path); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if !sameJSON(t, h2.last.Body, `{"first_name":"Ada"}`) {
		t.Errorf("Body aus Datei erwartet, got %s", h2.last.Body)
	}
}

func TestSetOverlaysBody(t *testing.T) {
	h := newHarness(t)
	code, _, stderr := h.run("contacts", "create",
		"--body", `{"first_name":"Ada","last_name":"L."}`,
		"--set", "last_name=Lovelace",
		"--set", "adresse.stadt=Köln",
		"--set", "aktiv=true",
		"--set", "notiz=null")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	want := `{"first_name":"Ada","last_name":"Lovelace","adresse":{"stadt":"Köln"},"aktiv":true,"notiz":null}`
	if !sameJSON(t, h.last.Body, want) {
		t.Errorf("Body\n got: %s\nwant: %s", h.last.Body, want)
	}
}

func TestIdempotencyKeyHeader(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("contacts", "create", "--set", "first_name=Ada", "--idempotency-key", "abc-1"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if got := h.last.Header.Get("Idempotency-Key"); got != "abc-1" {
		t.Errorf("Idempotency-Key abc-1 erwartet, got %q", got)
	}
}

func TestGlobalFlagsAcceptedBeforeAndAfterCommand(t *testing.T) {
	h := newHarness(t)
	if code, _, stderr := h.run("--org", "org-x", "contacts", "list"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if got := h.last.Header.Get("X-Organisation-Id"); got != "org-x" {
		t.Errorf("--org vor dem Befehl erwartet, got %q", got)
	}

	h2 := newHarness(t)
	if code, _, stderr := h2.run("contacts", "list", "--org", "org-y"); code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d (%s)", code, stderr)
	}
	if got := h2.last.Header.Get("X-Organisation-Id"); got != "org-y" {
		t.Errorf("--org nach dem Befehl erwartet, got %q", got)
	}
}

func TestRawResponsePassedThrough(t *testing.T) {
	h := newHarness(t)
	h.respCT = "application/x-yaml"
	h.respond = "name: Ankauf\n"
	code, stdout, _ := h.run("pipelines", "export", "2")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if stdout != "name: Ankauf\n" {
		t.Errorf("YAML roh auf stdout erwartet, got %q", stdout)
	}
}

func TestFieldsAndPrettyOnStdout(t *testing.T) {
	h := newHarness(t)
	h.respond = `[{"id":1,"titel":"A","x":1},{"id":2,"titel":"B","x":2}]`
	code, stdout, _ := h.run("immobilien", "list", "--fields", "id,titel")
	if code != 0 {
		t.Fatalf("Exit 0 erwartet, got %d", code)
	}
	if stdout != `[{"id":1,"titel":"A"},{"id":2,"titel":"B"}]`+"\n" {
		t.Errorf("Projektion erwartet, got %q", stdout)
	}
}
