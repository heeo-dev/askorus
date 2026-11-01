package codex

// featuresOff lists the codex 0.164.0 features that every codex run of the
// agent turns off with --disable, an ask (askArgs) as well as a judge
// (judgeArgs). The 11-01 probe found that by default even an exec with the
// person's config left out has their ChatGPT apps such as Gmail, plugins,
// hooks, sub-agents, the browser and computer use (P2-3 S2 decision 4).
// Commands go with shell_tool and unified_exec.
//
// Web search is not among them: an ask keeps the native web_search tool
// that --search turns on, and a judge turns it off on its own with
// -c web_search="disabled".
//
// A name codex does not know fails the run ("Unknown feature flag"), so a
// codex that dropped one neither asks nor judges until this list is fixed.
var featuresOff = []string{
	"shell_tool", "unified_exec", "hooks", "apps", "plugins", "remote_plugin",
	"multi_agent", "browser_use", "browser_use_external", "in_app_browser", "computer_use",
	"image_generation", "view_image", "goals", "sleep_tool", "tool_suggest", "skill_search",
	"skill_mcp_dependency_install",
}

// disableArgs returns "--disable", name for each of featuresOff, in list
// order. It returns a new slice on every call.
func disableArgs() []string {
	args := make([]string, 0, 2*len(featuresOff))
	for _, f := range featuresOff {
		args = append(args, "--disable", f)
	}
	return args
}
