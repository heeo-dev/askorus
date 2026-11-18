package agy

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/fakecli"
)

func synthesize(t *testing.T, a *Adapter, req core.SynthRequest) (core.SynthResult, error) {
	t.Helper()
	if req.Prompt == "" {
		req.Prompt = "Write the combined answer of the answers below."
	}
	if req.Model == "" {
		req.Model = "gemini-3.8-flash"
	}
	return a.Synthesize(context.Background(), req)
}

func TestSynthArgs(t *testing.T) {
	ok := synthArgs(synthAgent, "gemini-3.8-flash-medium", core.SynthSchemaV1, askSettings)
	want := []string{"--agent", "askorus-synth", "--model", "gemini-3.8-flash-medium", "--json-schema", core.SynthSchemaV1,
		"--input-format", "stream-json", "--output-format", "stream-json", "--disable-slash-commands", "--settings", wantAskSettings}
	if !slices.Equal(ok, want) {
		t.Fatalf("synthArgs = %q", ok)
	}
	for _, args := range [][]string{ok, synthArgs(synthAgent, "gemini-3.8-flash-low", core.RefineSchemaV1, askSettings),
		synthArgs(synthPhotoAgent, "gemini-3.8-flash-low", core.SynthSchemaV1, photoSettings)} {
		if err := checkSynthArgs(args); err != nil {
			t.Errorf("checkSynthArgs(%q) = %v", args, err)
		}
	}
	for name, args := range map[string][]string{
		"other schema":         synthArgs(synthAgent, "gemini-3.8-flash-low", `{"type":"object"}`, askSettings),
		"photo agent no reads": synthArgs(synthPhotoAgent, "gemini-3.8-flash-low", core.SynthSchemaV1, askSettings),
		"synth with reads":     synthArgs(synthAgent, "gemini-3.8-flash-low", core.SynthSchemaV1, photoSettings),
		"ask agent":            synthArgs(askAgent, "gemini-3.8-flash-low", core.SynthSchemaV1, askSettings),
		"claude model":         synthArgs(synthAgent, "claude-sonnet-4-6", core.SynthSchemaV1, askSettings),
		"extra":                append(slices.Clone(ok), "--dangerously-skip-permissions"),
	} {
		if err := checkSynthArgs(args); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if !strings.Contains(synthAgentFile, "\ntools: [finish]\n") || !strings.Contains(synthPhotoAgentFile, "\ntools: [finish, view_file]\n") {
		t.Error("synth agent files")
	}
	a := &Adapter{}
	for m, want := range map[string]string{"gemini-3.8-flash": "google", "claude-opus-4-6-thinking": "anthropic", "gpt-oss-120b-medium": "openai", "llama-3": ""} {
		if got := a.Family(m); got != want {
			t.Errorf("Family(%q) = %q", m, got)
		}
	}
}

func TestSynthesize(t *testing.T) {
	a, ask := fakeAgy(t)
	judgeDir := core.JudgeFolder(ask)
	if err := os.MkdirAll(judgeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := synthesize(t, a, core.SynthRequest{Folder: judgeDir})
	if err != nil {
		t.Fatal(err)
	}
	var v struct{ Answer string }
	if json.Unmarshal(r.Output, &v) != nil || v.Answer != fakecli.SynthLead || r.Model != "gemini-3.8-flash-medium" || r.Cleanup != "" {
		t.Errorf("result = %s %+v", r.Output, r)
	}
	c := fakecli.Calls(t)[0]
	if c.Dir != filepath.Join(judgeDir, FolderName) || !slices.Equal(c.Args, synthArgs(synthAgent, "gemini-3.8-flash-medium", core.SynthSchemaV1, askSettings)) {
		t.Errorf("call %q in %q", c.Args, c.Dir)
	}
	if c.Stdin != stdinMessage("Write the combined answer of the answers below.") {
		t.Errorf("stdin = %q", c.Stdin)
	}

	// Refining: the refine schema and the quick level.
	r, err = synthesize(t, a, core.SynthRequest{Folder: judgeDir, Refine: true, Prompt: "[Askorus refine v1] the question"})
	if err != nil || string(r.Output) != fakecli.RefineSpec || r.Model != "gemini-3.8-flash-low" {
		t.Errorf("refine = %s %+v %v", r.Output, r, err)
	}
	if c := fakecli.Calls(t)[1]; !slices.Contains(c.Args, core.RefineSchemaV1) {
		t.Errorf("refine args %q", c.Args)
	}
	// A model of the settings' own keeps its level.
	if r, err := synthesize(t, a, core.SynthRequest{Folder: judgeDir, Model: "gemini-3.8-flash-high"}); err != nil || r.Model != "gemini-3.8-flash-high" {
		t.Errorf("own level = %+v %v", r, err)
	}

	// The question's photos, for a moderator whose model sees them.
	r, err = synthesize(t, a, core.SynthRequest{Folder: judgeDir, Images: keptPhotos(t)})
	if err != nil || r.Output == nil {
		t.Fatalf("with photos = %+v %v", r, err)
	}
	c = fakecli.Calls(t)[3]
	if filepath.Dir(c.Dir) != filepath.Join(judgeDir, PhotoFolderName) || !slices.Equal(c.Args, synthArgs(synthPhotoAgent, "gemini-3.8-flash-medium", core.SynthSchemaV1, photoSettings)) || c.Photos != 2 {
		t.Errorf("photo call %q in %q, %d photos", c.Args, c.Dir, c.Photos)
	}
	if _, err := os.Stat(c.Dir); !os.IsNotExist(err) {
		t.Error("the moderator's photo folder outlived it")
	}
	if _, err := synthesize(t, a, core.SynthRequest{Folder: judgeDir, Refine: true, Images: keptPhotos(t)}); core.ErrorCode(err) != core.ErrCodeInternal {
		t.Errorf("refine with photos = %v", err)
	}
}

func TestSynthesizeModes(t *testing.T) {
	for mode, code := range map[string]string{
		fakecli.SynthProse:            core.ErrCodeJudgeBadOutput,
		fakecli.SynthToolUse:          core.ErrCodeJudgeUsedTool,
		fakecli.SynthUsageLimit:       core.ErrCodeJudgeFailed,
		fakecli.SynthBadModel:         core.ErrCodeBadModel,
		fakecli.SynthFailedWithOutput: core.ErrCodeJudgeFailed,
	} {
		t.Run(mode, func(t *testing.T) {
			a, ask := fakeAgy(t)
			judgeDir := core.JudgeFolder(ask)
			os.MkdirAll(judgeDir, 0o700)
			t.Setenv(fakecli.EnvSynth, mode)
			r, err := synthesize(t, a, core.SynthRequest{Folder: judgeDir})
			if core.ErrorCode(err) != code || r.Output != nil {
				t.Errorf("= %s %v, want %s", r.Output, err, code)
			}
		})
	}
	t.Run("lock", func(t *testing.T) {
		a, ask := fakeAgy(t)
		judgeDir := core.JudgeFolder(ask)
		os.MkdirAll(judgeDir, 0o700)
		t.Setenv(fakecli.EnvAgy, fakecli.AgyOpenMode)
		if r, err := synthesize(t, a, core.SynthRequest{Folder: judgeDir}); core.ErrorCode(err) != core.ErrCodeInternal || r.Output != nil {
			t.Errorf("= %s %v", r.Output, err)
		}
	})
	t.Run("slow", func(t *testing.T) {
		a, ask := fakeAgy(t)
		judgeDir := core.JudgeFolder(ask)
		os.MkdirAll(judgeDir, 0o700)
		t.Setenv(fakecli.EnvSynth, fakecli.SynthSlow)
		t.Setenv(fakecli.EnvSleep, "5s")
		old := synthTimeout
		synthTimeout = 300 * time.Millisecond
		defer func() { synthTimeout = old }()
		if _, err := synthesize(t, a, core.SynthRequest{Folder: judgeDir}); core.ErrorCode(err) != core.ErrCodeJudgeTimeout {
			t.Errorf("= %v", err)
		}
	})
	t.Run("refused", func(t *testing.T) {
		a, ask := fakeAgy(t)
		for name, req := range map[string]core.SynthRequest{
			"no folder":    {Folder: filepath.Join(ask, "missing")},
			"empty prompt": {Folder: ask, Prompt: " "},
			"claude model": {Folder: ask, Model: "claude-sonnet-4-6"},
		} {
			if req.Prompt == "" {
				req.Prompt = "x"
			}
			if _, err := synthesize(t, a, req); err == nil {
				t.Errorf("%s accepted", name)
			}
		}
		if n := len(fakecli.Calls(t)); n != 0 {
			t.Errorf("%d runs", n)
		}
	})
}
