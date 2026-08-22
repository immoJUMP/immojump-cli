package cli

import (
	"strings"

	"github.com/immoJUMP/immojump-cli/internal/api"
	"github.com/immoJUMP/immojump-cli/internal/config"
	"github.com/immoJUMP/immojump-cli/internal/output"
)

// emit schreibt ein Ergebnis als JSON nach stdout — durch dieselbe
// Output-Schicht wie API-Antworten, damit --pretty und --fields wirken.
func (r *runner) emit(value any, flags *flagValues) int {
	raw, err := output.Marshal(value)
	if err != nil {
		return r.fail(usageErr("Ausgabe nicht serialisierbar: %v", err))
	}
	if err := r.render(raw, "application/json", flags); err != nil {
		return r.fail(err)
	}
	return 0
}

// runAuthLogin legt einen Context an bzw. aktualisiert ihn und prüft ihn
// gegen die Instanz. Gespeichert wird erst nach erfolgreicher Prüfung.
func (r *runner) runAuthLogin(spec Spec, flags *flagValues) int {
	path, file, err := r.loadConfigFile()
	if err != nil {
		return r.fail(err)
	}
	// Ohne Speicherort gar nicht erst prüfen — sonst kostet der Aufruf einen
	// Request und scheitert danach trotzdem.
	if path == "" {
		return r.fail(configErr(
			"Kein Config-Pfad ermittelbar — %s setzen (oder HOME bzw. XDG_CONFIG_HOME)", config.EnvConfig))
	}

	if flags.has("token") && flags.has("token-env") {
		return r.fail(usageErr("--token und --token-env schließen sich aus"))
	}

	name := config.FirstNonEmpty(flags.get("context"), r.getenv(config.EnvContext), file.CurrentContext, "default")
	ctx := file.Contexts[name]

	if base := config.FirstNonEmpty(flags.get("base-url"), r.getenv(config.EnvBaseURL), ctx.BaseURL, config.DefaultBaseURL); base != "" {
		ctx.BaseURL = config.NormalizeBaseURL(base)
	}
	if org := config.FirstNonEmpty(flags.get("organisation"), flags.get("org"), r.getenv(config.EnvOrg), ctx.OrganisationID); org != "" {
		ctx.OrganisationID = org
	}
	if tokenEnv := strings.TrimSpace(flags.get("token-env")); tokenEnv != "" {
		ctx.TokenEnv, ctx.Token = tokenEnv, ""
	}
	if token := flags.get("token"); token != "" {
		ctx.Token, ctx.TokenEnv = token, ""
	}

	var token string
	tokenFromEnv := false
	if requested := strings.TrimSpace(flags.get("token-env")); requested != "" {
		// Wer --token-env sagt, meint genau diese Variable. Ein stiller
		// Rückfall auf IMMOJUMP_TOKEN würde einen Context speichern, der
		// später wortlos ein fremdes Token benutzt.
		token = r.getenv(requested)
		if token == "" {
			return r.fail(configErr(
				"Umgebungsvariable %s ist nicht gesetzt — nichts gespeichert.", requested))
		}
	} else {
		token = config.ContextToken(ctx, r.getenv)
		if token == "" {
			token = r.getenv(config.EnvToken)
			tokenFromEnv = token != ""
		}
	}
	if token == "" {
		return r.fail(configErr(
			"Kein Token angegeben. Nutze --token <token> oder --token-env <VARIABLE> (Token unter Einstellungen → API-Zugang)."))
	}

	// Die Allowlist prüft api.Client.Do ohnehin, bevor ein Byte rausgeht.
	client, err := r.newClient(config.Resolved{BaseURL: ctx.BaseURL, Org: ctx.OrganisationID, Token: token}, flags)
	if err != nil {
		return r.fail(err)
	}
	response, err := client.Do(api.Request{Method: spec.Method, Path: spec.Path})
	if err != nil {
		return r.fail(err)
	}

	if file.Contexts == nil {
		file.Contexts = map[string]config.Context{}
	}
	file.Contexts[name] = ctx
	file.CurrentContext = name
	if err := config.Save(path, file); err != nil {
		return r.fail(configErr("%s", err.Error()))
	}

	result := map[string]any{
		"context":         name,
		"config":          path,
		"base_url":        ctx.BaseURL,
		"organisation_id": ctx.OrganisationID,
		"token":           config.MaskToken(token),
		"token_source":    tokenSource(ctx, tokenFromEnv),
	}
	addUser(result, response.Body, ctx.OrganisationID, flags)
	return r.emit(result, flags)
}

// addUser hängt die Nutzerdaten an die Anmeldebestätigung — standardmäßig nur
// id und username plus die Rolle in der aktiven Organisation.
//
// Das komplette /api/user/me-Objekt (Abo-Daten, Login-Zähler, alle
// Organisationen) sind ~1.400 Zeichen für eine Anmeldebestätigung — und das
// im Befehl, den jeder Agent als Erstes ausführt. Wer alles braucht, sagt
// --full.
func addUser(result map[string]any, body []byte, org string, flags *flagValues) {
	var user any
	if err := decodeJSONExact(body, &user); err != nil {
		return
	}
	if flags.bool("full") {
		result["user"] = user
		return
	}
	payload, ok := user.(map[string]any)
	if !ok {
		result["user"] = user
		return
	}
	compact := map[string]any{}
	for _, key := range []string{"id", "username"} {
		if value, ok := payload[key]; ok {
			compact[key] = value
		}
	}
	result["user"] = compact
	if role := organisationRole(payload, org); role != "" {
		result["organisation_role"] = role
	}
}

// organisationRole sucht die Mitgliedsrolle in der aktiven Organisation.
// Ohne Treffer wird keine erfunden.
func organisationRole(payload map[string]any, org string) string {
	if org == "" {
		return ""
	}
	entries, _ := payload["organisation_access"].([]any)
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := item["organisationId"].(string); id != org {
			continue
		}
		role, _ := item["membershipRole"].(string)
		return role
	}
	return ""
}

func tokenSource(ctx config.Context, fromEnv bool) string {
	switch {
	case ctx.TokenEnv != "":
		return "env:" + ctx.TokenEnv
	case ctx.Token != "":
		return "context"
	case fromEnv:
		return "env:" + config.EnvToken
	default:
		return "unbekannt"
	}
}

// runAuthStatus zeigt die aufgelöste Konfiguration und prüft sie live.
func (r *runner) runAuthStatus(spec Spec, resolved config.Resolved, flags *flagValues) int {
	if resolved.Token == "" {
		return r.fail(configErr(
			"Kein API-Token gefunden. Setze IMMOJUMP_TOKEN oder lege einen Context an: immojump auth login --context <name> --token <token>"))
	}
	client, err := r.newClient(resolved, flags)
	if err != nil {
		return r.fail(err)
	}
	response, err := client.Do(api.Request{Method: spec.Method, Path: spec.Path})
	if err != nil {
		return r.fail(err)
	}
	result := map[string]any{
		"context":         resolved.ContextName,
		"config":          config.Path(r.getenv),
		"base_url":        resolved.BaseURL,
		"organisation_id": resolved.Org,
		"token":           config.MaskToken(resolved.Token),
		"token_source":    resolved.TokenSource,
	}
	addUser(result, response.Body, resolved.Org, flags)
	return r.emit(result, flags)
}

// runContext bedient die rein lokalen Context-Befehle.
func (r *runner) runContext(spec Spec, args []string, flags *flagValues) int {
	path, file, err := r.loadConfigFile()
	if err != nil {
		return r.fail(err)
	}

	switch spec.Special {
	case SpecialContextList:
		contexts := []map[string]any{}
		for _, name := range sortedKeys(file.Contexts) {
			ctx := file.Contexts[name]
			contexts = append(contexts, map[string]any{
				"name":            name,
				"base_url":        ctx.BaseURL,
				"organisation_id": ctx.OrganisationID,
				"token":           config.MaskToken(config.ContextToken(ctx, r.getenv)),
				"token_env":       ctx.TokenEnv,
				"current":         name == file.CurrentContext,
			})
		}
		return r.emit(map[string]any{
			"config":          path,
			"current_context": file.CurrentContext,
			"contexts":        contexts,
		}, flags)

	case SpecialContextCurrent:
		if file.CurrentContext == "" {
			return r.emit(map[string]any{
				"config":          path,
				"current_context": "",
				"hinweis":         "Kein Context aktiv. Anlegen: immojump auth login --context <name> …",
			}, flags)
		}
		ctx := file.Contexts[file.CurrentContext]
		return r.emit(map[string]any{
			"config":          path,
			"current_context": file.CurrentContext,
			"base_url":        ctx.BaseURL,
			"organisation_id": ctx.OrganisationID,
			"token":           config.MaskToken(config.ContextToken(ctx, r.getenv)),
		}, flags)

	case SpecialContextUse:
		name := args[0]
		if _, ok := file.Contexts[name]; !ok {
			return r.fail(configErr("Context %q ist nicht konfiguriert. Vorhandene: %s",
				name, joinOrNone(sortedKeys(file.Contexts))))
		}
		file.CurrentContext = name
		if err := config.Save(path, file); err != nil {
			return r.fail(configErr("%s", err.Error()))
		}
		return r.emit(map[string]any{"config": path, "current_context": name}, flags)

	case SpecialContextDelete:
		name := args[0]
		if _, ok := file.Contexts[name]; !ok {
			return r.fail(configErr("Context %q ist nicht konfiguriert. Vorhandene: %s",
				name, joinOrNone(sortedKeys(file.Contexts))))
		}
		delete(file.Contexts, name)
		if file.CurrentContext == name {
			file.CurrentContext = ""
		}
		if err := config.Save(path, file); err != nil {
			return r.fail(configErr("%s", err.Error()))
		}
		return r.emit(map[string]any{
			"config":          path,
			"deleted":         name,
			"current_context": file.CurrentContext,
		}, flags)
	}
	return r.fail(usageErr("Befehl %q ist nicht implementiert", spec.Name()))
}
