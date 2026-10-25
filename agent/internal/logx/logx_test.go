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
