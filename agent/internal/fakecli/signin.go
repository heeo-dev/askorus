package fakecli

// The sign-in of the fake AIs (#60): `claude auth status --json`,
// `codex login status`, and an ask that fails because the AI is not
// signed in.

import (
	"fmt"
	"os"
	"slices"
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
