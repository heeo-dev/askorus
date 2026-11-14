package main

// The question on the PC's screen about a phone that asks to pair without a
// code (#58). The agent runs in the background with no window of its own,
// so the question is a small dialog (internal/dialog). It names the phone,
// the two digits the phone shows, and where the request came from.

import (
	"context"
	"fmt"
	"os"

	"github.com/heeo-dev/askorus/agent/internal/dialog"
	"github.com/heeo-dev/askorus/agent/internal/pairing"
)

// screenConfirm is what the running agent gives the pairer to ask with. In
// the run without a Tailscale node (the install and start tests) nothing
// can reach the agent, and no window is ever to pop up: nil.
func screenConfirm() func(context.Context, pairing.Question) (allow, shown bool) {
	if os.Getenv(noNodeEnv) == "1" {
		return nil
	}
	return func(ctx context.Context, q pairing.Question) (bool, bool) {
		return dialog.Confirm(ctx, pairQuestion(q))
	}
}

// pairQuestion words the dialog, in both languages as the commands do.
// Windows shows its own Yes and No buttons, so the text says which is which.
func pairQuestion(q pairing.Question) dialog.Question {
	text := fmt.Sprintf("A phone asks to connect to this PC.\n\n"+
		"    %s\n    number %s\n    Tailscale account %s, address %s\n\n"+
		"Allow it only if you just asked for this on your own phone and the phone shows the same number.\n"+
		"Yes allows it. No refuses.\n\n"+
		"폰이 이 PC에 연결하려고 합니다.\n"+
		"방금 내 폰에서 요청했고 폰에 같은 숫자(%[2]s)가 보일 때만 허용하세요.\n"+
		"예: 허용, 아니요: 거절",
		q.DeviceName, q.Match, q.Login, q.RemoteIP)
	return dialog.Question{Title: "Askorus", Text: text, Allow: "허용 Allow", Deny: "거절 Refuse"}
}
