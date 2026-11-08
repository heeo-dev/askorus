package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/runs"
)

// askCmd is `askorus-agent ask`: the ask the phone sends (POST /v1/runs,
// mode ask), run from the PC terminal to try it. Answers print as each AI
// finishes.
func askCmd(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	agents := fs.String("agents", "", "comma-separated AIs (default: every AI that can answer here)")
	tier := fs.String("tier", core.TierStandard, "quick, standard or deep")
	follow := fs.String("follow", "", "comma-separated sessions to carry on, e.g. claude:<id>,codex:<id>")
	if err := fs.Parse(args); err != nil {
		return err
	}
	question := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if question == "" {
		return fail("usage: askorus-agent ask [-agents A,B] [-tier T] [-follow ID,ID] QUESTION",
			"사용법: askorus-agent ask [-agents A,B] [-tier 단계] [-follow ID,ID] 질문")
	}
	if !core.ValidTier(*tier) {
		return fail("tier must be quick, standard or deep", "단계는 quick, standard, deep 중 하나입니다")
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	folder, err := cfg.AskFolder()
	if err != nil {
		return err
	}
	ca, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	ctx := context.Background()
	askers := map[string]core.Asker{}
	var order []string
	for _, a := range append([]core.Adapter{ca}, askOnlyAdapters(cfg)...) {
		info := a.Info(ctx)
		if asker, ok := a.(core.Asker); ok && info.Available && slices.Contains(info.Capabilities, core.CapAsk) {
			askers[a.Agent()] = asker
			order = append(order, a.Agent())
		}
	}
	resume := map[string]string{}
	var want []string
	for _, s := range splitList(*follow) {
		agent, native, err := core.ParseID(s)
		if err != nil {
			return fail("session %q is not <agent>:<id>", "세션 %q가 <agent>:<id> 꼴이 아닙니다", s)
		}
		resume[agent] = native
		want = append(want, agent)
	}
	want = append(want, splitList(*agents)...)
	if len(want) == 0 {
		want = order
	}
	var tasks []runs.Task
	for _, name := range want {
		asker, ok := askers[name]
		if !ok {
			return fail("%s cannot answer on this PC (askorus-agent info)", "%s는 이 PC에서 답할 수 없습니다 (askorus-agent info)", name)
		}
		if slices.ContainsFunc(tasks, func(t runs.Task) bool { return t.Agent == name }) {
			continue
		}
		req := core.AskRequest{Folder: folder, Prompt: core.AskPrompt(question), Tier: *tier, Resume: resume[name]}
		session := ""
		if resume[name] != "" {
			session = core.MakeID(name, resume[name])
		}
		tasks = append(tasks, runs.Task{Agent: name, Session: session, Ask: func(ctx context.Context) (core.AskResult, error) {
			return asker.Ask(ctx, req)
		}})
	}
	if len(tasks) == 0 {
		return fail("no AI on this PC can answer (askorus-agent info)", "이 PC에서 답할 수 있는 AI가 없습니다 (askorus-agent info)")
	}
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return err
	}
	mgr := runs.NewManager()
	run, err := mgr.Start(runs.Spec{Owner: "cli", Mode: core.ModeAsk, Tier: *tier, Tasks: tasks})
	if err != nil {
		return err
	}
	say(out, m("asking %s (%s) in %s …", "%s에게 묻는 중 (%s), 폴더 %s …", strings.Join(want, ", "), *tier, folder))
	shown := map[int]bool{}
	for {
		r, _ := mgr.Get("cli", run.ID)
		for i, p := range r.Parts {
			if p.Status == runs.Running || shown[i] {
				continue
			}
			shown[i] = true
			head := fmt.Sprintf("== %s · %s · %.1f s", p.Agent, p.Model, float64(p.Ms)/1000)
			if p.Session != "" {
				head += " · " + p.Session
			}
			fmt.Fprintln(out, head)
			if p.Status == runs.Done {
				fmt.Fprintln(out, p.Answer)
			} else {
				fmt.Fprintf(out, "failed: %s %s\n", p.Error.Code, p.Error.Message)
			}
			fmt.Fprintln(out)
		}
		switch r.Status {
		case runs.Failed:
			return fail("no AI answered", "답한 AI가 없습니다")
		case runs.Done:
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
