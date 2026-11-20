package fakecli

// The model lists (#64): `codex debug models` (not recorded, like
// --version), and a newer model of a line in each AI's list.

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// EnvNewer: "none" (the default) the lists hold the models the agent
// ships with; "list" a newer model of a line is listed too and answers;
// "refuse" it is listed but the AI refuses it when asked (codex
// model_not_found, OpenCode "Variant unavailable", agy not a known model).
// One value for every AI or per AI, as "codex=list,opencode=refuse".
const EnvNewer = "ASKORUS_FAKECLI_NEWER"

// Modes of ASKORUS_FAKECLI_NEWER.
const (
	NewerNone   = "none"
	NewerList   = "list"
	NewerRefuse = "refuse"
)

// NewerModes lists them.
var NewerModes = []string{NewerNone, NewerList, NewerRefuse}

// The newer model of a line in each list.
const (
	NewerCodex    = "gpt-6.2-sol"                     // Sol, after gpt-6.1-sol
	NewerOpenCode = "opencode-go/deepseek-v4.2-flash" // Flash, after v4.1
	NewerAgy      = "gemini-3.9-flash"                // Flash, after 3.8
)

func newerMode(role string) (string, error) {
	return pick(os.Getenv(EnvNewer), role, NewerNone, func(v string) bool { return slices.Contains(NewerModes, v) })
}

// refusesNewer reports whether role refuses model: it is role's newer
// model (with or without a variant or level) and the mode is refuse.
func refusesNewer(role, model string) bool {
	mode, _ := newerMode(role)
	if mode != NewerRefuse {
		return false
	}
	switch role {
	case RoleCodex:
		return model == NewerCodex
	case RoleOpenCode:
		base, _, _ := strings.Cut(model, "#")
		return base == NewerOpenCode
	case RoleAgy:
		return strings.HasPrefix(model, NewerAgy)
	}
	return false
}

// codexLevels are the efforts of a model in Codex's catalog.
func codexLevels(es ...string) []map[string]string {
	out := make([]map[string]string, len(es))
	for i, e := range es {
		out[i] = map[string]string{"effort": e, "description": "level " + e}
	}
	return out
}

// codexModels prints `codex debug models`: the catalog as Codex 0.171.0
// does, with the fields the agent reads and some it drops.
func codexModels() int {
	mode, err := newerMode(RoleCodex)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	all := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	model := func(slug, vis string, es ...string) map[string]any {
		return map[string]any{"slug": slug, "display_name": strings.ToUpper(slug), "visibility": vis,
			"supported_reasoning_levels": codexLevels(es...), "base_instructions": "You are Codex."}
	}
	var models []map[string]any
	if mode != NewerNone {
		models = append(models, model(NewerCodex, "list", all...))
	}
	models = append(models,
		model("gpt-6.2-astra", "list", all...), // the extra model (#65)
		model("gpt-6.1-terra", "list", all...),
		model("gpt-reserve", "hide", all[:5]...),
		model("gpt-6.1-sol", "list", all...),
		model("gpt-6-sol", "list", all...),
		model("gpt-6-luna", "list", all[:5]...),
		model("codex-auto-review", "hide", all[:5]...),
	)
	printJSON(map[string]any{"models": models})
	return 0
}

// codexRefused is the error codex prints for a model it does not take.
func codexRefused(model string) int {
	inner := fmt.Sprintf(`{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"The '%s' model is not supported when using Codex with a ChatGPT account.","code":"model_not_found"}}`, model)
	return codexFail(inner)
}

// agyModelLines are what `agy models` lists with the newer mode.
func agyModelLines() []string {
	mode, _ := newerMode(RoleAgy)
	if mode == NewerNone {
		return AgyModels
	}
	return append([]string{
		NewerAgy + "-high\tGemini 3.9 Flash (High)",
		NewerAgy + "-medium\tGemini 3.9 Flash (Medium)",
		NewerAgy + "-low\tGemini 3.9 Flash (Low)",
	}, AgyModels...)
}
