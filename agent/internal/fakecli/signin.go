package fakecli

// The sign-in of the fake AIs (#60): `claude auth status --json`,
// `codex login status`, `opencode models`, and an ask that fails because
// the AI is not signed in.

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// EnvSignIn sets the sign-in state: "in" (the default), "out", or "soon"
// (claude: its sign-in ends within a day); for every AI or per AI, as
// "claude=out,codex=in".
const EnvSignIn = "ASKORUS_FAKECLI_SIGNIN"

// Sign-in states of ASKORUS_FAKECLI_SIGNIN.
const (
	SignedIn   = "in"
	SignedOut  = "out"
	SignInSoon = "soon"
)

// SignInModes lists the sign-in states.
var SignInModes = []string{SignedIn, SignedOut, SignInSoon}

// AskSignedOut is the ask mode where the AI is not signed in.
const AskSignedOut = "signedout"

// What claude printed on 2026-11-16 for a sign-in that had ended.
const SignedOutClaude = "Failed to authenticate: this login reached its time limit and cannot be renewed. Run `claude auth login` to sign in again, or use `claude setup-token` and set CLAUDE_CODE_OAUTH_TOKEN for unattended jobs"

// SignedOutCodex is what codex prints when it is not signed in.
const SignedOutCodex = "Error: Not logged in. Run `codex login` to sign in."

func signInMode(role string) (string, error) {
	return pick(os.Getenv(EnvSignIn), role, SignedIn, func(v string) bool { return slices.Contains(SignInModes, v) })
}

// claudeAuthStatus prints `claude auth status --json`: only the fields the
// agent may read and one it must drop.
func claudeAuthStatus() int {
	mode, err := signInMode(RoleClaude)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	switch mode {
	case SignedOut:
		printJSON(map[string]any{"loggedIn": false})
		return 1
	case SignInSoon:
		printJSON(map[string]any{"loggedIn": true, "email": "someone@example.com", "loginExpiresAt": time.Now().Add(20 * time.Hour).UTC().Format(time.RFC3339Nano)})
	default:
		printJSON(map[string]any{"loggedIn": true, "email": "someone@example.com", "loginExpiresAt": time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339Nano)})
	}
	return 0
}

// OpenCode's models (11-18, OpenCode 2.2.7): never signed in, it lists only
// its free models, all of the provider "opencode".
var (
	OpenCodeFreeModels = []string{"opencode/big-pickle", "opencode/deepseek-v4.1-flash-free"}
	openCodeModels     = []string{"opencode-go/deepseek-v4-pro", "opencode-go/deepseek-v4.1-flash", "opencode/deepseek-v4.1-flash"}
)

// SignedOutOpenCode is the error event's message for a model OpenCode
// cannot use, as for one that does not exist.
const SignedOutOpenCode = "Model unavailable: "

// What OpenCode said on 11-18 when its free models reached their usage
// limit, and about how long it said to wait.
const (
	FreeLimitOpenCode = "Free usage limit reached. Connect a provider or subscribe to OpenCode Go to keep going, or switch models."
	FreeLimitWait     = 7 * time.Hour
)

// opencodeModels prints `opencode models --standalone`.
func opencodeModels(args []string) int {
	if !slices.Equal(args, []string{"models", "--standalone"}) {
		return unexpected(RoleOpenCode)
	}
	mode, err := signInMode(RoleOpenCode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	list := OpenCodeFreeModels
	if mode != SignedOut {
		list = append(slices.Clone(openCodeModels), OpenCodeFreeModels...)
		if newer, _ := newerMode(RoleOpenCode); newer != NewerNone {
			list = append([]string{NewerOpenCode}, list...)
		}
	}
	for _, m := range list {
		fmt.Print(m + "\r\n")
	}
	return 0
}

// openCodeKnows tells whether a signed-out OpenCode can still use model:
// a free model, with or without its variant.
func openCodeKnows(model string) bool {
	base, _, _ := strings.Cut(model, "#")
	return slices.Contains(OpenCodeFreeModels, base)
}

// codexLoginStatus prints `codex login status`.
func codexLoginStatus() int {
	mode, err := signInMode(RoleCodex)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli:", err)
		return 2
	}
	if mode == SignedOut {
		fmt.Fprintln(os.Stderr, "Not logged in")
		return 1
	}
	fmt.Println("Logged in using ChatGPT")
	return 0
}
