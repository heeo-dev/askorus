package logx

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRedaction(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, slog.LevelInfo)
	l.Info("request",
		"device", "phone",
		"route", "GET /v1/info",
		"Authorization", "Bearer ask_abcdef",
		"token", "ask_abcdef",
		"lastPrompt", "please read my diary",
		slog.Group("req", "pair_code", "123456"),
	)
	out := buf.String()
	for _, leak := range []string{"ask_abcdef", "diary", "123456"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaks %q: %s", leak, out)
		}
	}
	for _, keep := range []string{`"device":"phone"`, `"route":"GET /v1/info"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("log lost %s: %s", keep, out)
		}
	}
}

func TestRedactsComparisonContent(t *testing.T) {
	var buf bytes.Buffer
	l := New(&buf, slog.LevelInfo)
	l.Info("comparison",
		"answers", 3, "sentences", 4, "apiKey", "x-key-1", "api_key", "x-key-2", "evidenceCount", 2,
		"question", "q-leak", "earlier", "e-leak", "claim", "c-leak", "reason", "r-leak", "answerText", "a-leak",
	)
	out := buf.String()
	for _, key := range []string{"answers", "sentences", "apiKey", "api_key", "evidenceCount", "question", "earlier", "claim", "reason", "answerText"} {
		if !strings.Contains(out, `"`+key+`":"`+Redacted+`"`) {
			t.Errorf("%s not redacted: %s", key, out)
		}
	}
	for _, leak := range []string{"x-key-1", "x-key-2", "q-leak", "e-leak", "c-leak", "r-leak", "a-leak"} {
		if strings.Contains(out, leak) {
			t.Errorf("log leaks %q: %s", leak, out)
		}
	}
}

func TestKeepsCountKeys(t *testing.T) {
	// The keys comparisons log, and those the agent logged before P2-3.
	keys := []string{
		"ais", "requests", "judges", "noJudge", "differ", "check", "agree", "onlyOne", "notCompared", "notJudged",
		"attempt", "ms", "code", "status", "judge", "source", "comparison",
		"mode", "agent", "agents", "tier", "followUp", "kind", "detail", "err", "folder",
	}
	var buf bytes.Buffer
	l := New(&buf, slog.LevelInfo)
	var args []any
	for _, k := range keys {
		if IsSensitive(k) {
			t.Errorf("%s is blocked", k)
		}
		args = append(args, k, "v")
	}
	l.Info("counts", args...)
	for _, k := range keys {
		if !strings.Contains(buf.String(), `"`+k+`":"v"`) {
			t.Errorf("%s lost: %s", k, buf.String())
		}
	}
	if strings.Contains(buf.String(), Redacted) {
		t.Errorf("a count key was redacted: %s", buf.String())
	}
}
