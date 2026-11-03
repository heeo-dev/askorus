//go:build syntheval

// Command synth-eval measures the combined answer (종합 답변; milestone C,
// design C-9, #12). It asks a fixed set of questions the way the phone
// does (POST /v1/runs with a comparison and a combined answer, made in
// this process), keeps each question's answers, comparison and combined
// answer under %LOCALAPPDATA%\Asksemble\synth-eval\<run>, and reports what
// can be counted: times, the combined answer's shape, English mixed into a
// Korean answer, extras left in, 다른 말 sides it does not name. It uses the
// person's AI subscriptions, so a run is started only when agreed, through
// scripts\synth-eval.ps1. It is built only with -tags syntheval and is no
// part of the agent.
//
//	synth-eval questions -q FILE[,FILE] [-only ID,ID]
//	synth-eval run       -q FILE[,FILE] [-only ID,ID] -run NAME [-tier quick] [-writer claude] [-refine off|on] [-resume] -expect N
//	synth-eval report    -run NAME[,NAME] [-json] [-texts]
//	synth-eval resynth   -from RUN -run NAME [-only ID,ID] [-tier quick] [-writer claude] [-synth product] [-resume] -expect N
//
// resynth and the commands after it are the quality experiments (C4, E0 in
// "마일스톤 C" of docs/phase2-plan.md): they make their own writer calls.
//
// Exit codes: 0 done, 1 error, 2 bad flags, 3 stopped on a usage limit
// (run again with -resume), 4 interrupted.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/core"
)

const (
	exitOK          = 0
	exitError       = 1
	exitFlags       = 2
	exitUsageLimit  = 3
	exitInterrupted = 4
)

// selfElevated reports an elevated process; tests replace it. The AIs run
// with normal rights (구현 전 테스트 6).
var selfElevated = claude.SelfElevated

// newPipeline makes the agent's pipeline; tests replace it.
var newPipeline = func(ctx context.Context) (pipeline, func(), error) {
	p, err := newAgentPipeline(ctx)
	if err != nil {
		return nil, nil, err
	}
	return p, p.Close, nil
}

const usage = `synth-eval measures the combined answer (milestone C). A run uses AI subscriptions.
  questions -q FILE[,FILE] [-only ID,ID]
  run       -q FILE[,FILE] [-only ID,ID] -run NAME [-tier quick|standard|deep] [-writer claude|codex|opencode]
            [-refine off|on] [-resume] -expect N
  report    -run NAME[,NAME] [-json] [-texts]
  resynth   -from RUN -run NAME [-only ID,ID] [-tier quick|standard|deep] [-writer claude|codex|opencode]
            [-synth product] [-resume] -expect N
            writes RUN's combined answers again from the same answers and judgments (up to 2 calls each)
A question file has one question per line; blank lines and lines starting with # are skipped.
-expect is the number of questions the run will ask (each uses the PC's AIs up to 10 times, 12 with -refine on).
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := runMain(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func runMain(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(errOut, usage)
		return exitFlags
	}
	switch args[0] {
	case "questions":
		return questionsCmd(args[1:], out, errOut)
	case "run":
		return runCmd(ctx, args[1:], out, errOut)
	case "report":
		return reportCmd(args[1:], out, errOut)
	case "resynth":
		return resynthCmd(ctx, args[1:], out, errOut)
	}
	fmt.Fprint(errOut, usage)
	return exitFlags
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

// questionFlags are -q and -only, shared by questions and run.
func questionFlags(fs *flag.FlagSet) (*string, *string) {
	return fs.String("q", "", "question files, comma-separated"), fs.String("only", "", "question IDs to keep, comma-separated")
}

func loadPicked(files, only string) ([]Question, error) {
	if files == "" {
		return nil, errors.New("give -q FILE")
	}
	qs, err := readQuestions(splitList(files))
	if err != nil {
		return nil, err
	}
	return pick(qs, splitList(only))
}

func questionsCmd(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("questions", flag.ContinueOnError)
	fs.SetOutput(errOut)
	files, only := questionFlags(fs)
	if err := fs.Parse(args); err != nil {
		return exitFlags
	}
	qs, err := loadPicked(*files, *only)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	for _, q := range qs {
		fmt.Fprintf(out, "%-12s %s\n", q.ID, q.Text)
	}
	fmt.Fprintf(out, "%d questions; a run asks the PC's AIs up to %d times\n", len(qs), len(qs)*callsPerQuestion)
	return exitOK
}

func runCmd(ctx context.Context, args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(errOut)
	files, only := questionFlags(fs)
	name := fs.String("run", "", "run name (a-z, 0-9, -)")
	tier := fs.String("tier", core.TierQuick, "quick, standard or deep")
	writer := fs.String("writer", "claude", "the AI that writes the combined answer")
	refine := fs.String("refine", "off", "off, or on: the writer refines each question first (C2)")
	resume := fs.Bool("resume", false, "skip the questions the run already has")
	expect := fs.Int("expect", -1, "the number of questions this run will ask")
	if err := fs.Parse(args); err != nil {
		return exitFlags
	}
	switch {
	case !core.ValidTier(*tier):
		fmt.Fprintln(errOut, "-tier must be quick, standard or deep")
		return exitFlags
	case *refine != "off" && *refine != "on":
		fmt.Fprintln(errOut, "-refine must be off or on")
		return exitFlags
	case !validWriter(*writer):
		fmt.Fprintln(errOut, "-writer must be claude, codex or opencode")
		return exitFlags
	}
	dir, err := runDir(*name)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	qs, err := loadPicked(*files, *only)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	todo := 0
	for _, q := range qs {
		if !*resume || !done(dir, q.ID) {
			todo++
		}
	}
	if *expect != todo {
		fmt.Fprintf(errOut, "this run would ask %d questions (up to %d AI calls): give -expect %d\n", todo, todo*callsPerQuestion, todo)
		return exitFlags
	}
	if !*resume {
		if _, err := os.Stat(dir); err == nil {
			fmt.Fprintf(errOut, "run %s exists: pick another name, or -resume\n", *name)
			return exitFlags
		}
	}
	if selfElevated() {
		fmt.Fprintln(errOut, "synth-eval runs with normal rights: use a terminal that is not run as administrator")
		return exitError
	}
	if todo == 0 {
		fmt.Fprintln(out, "nothing left to ask")
		return exitOK
	}
	p, closeP, err := newPipeline(ctx)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitError
	}
	defer closeP()
	run := RunInfo{Name: *name, Tier: *tier, Writer: *writer, Refine: *refine}
	fmt.Fprintf(out, "run %s: %d questions, tier %s, writer %s, refine %s\n", *name, todo, *tier, *writer, *refine)
	return runQuestions(ctx, p, qs, run, dir, *resume, time.Now, out)
}

func reportCmd(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(errOut)
	names := fs.String("run", "", "run names, comma-separated")
	asJSON := fs.Bool("json", false, "print JSON")
	texts := fs.Bool("texts", false, "print each combined answer too")
	if err := fs.Parse(args); err != nil {
		return exitFlags
	}
	list := splitList(*names)
	if len(list) == 0 {
		fmt.Fprintln(errOut, "give -run NAME")
		return exitFlags
	}
	var runs [][]Record
	for _, n := range list {
		dir, err := runDir(n)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return exitFlags
		}
		rs, err := loadRecords(dir)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return exitError
		}
		runs = append(runs, rs)
	}
	if *asJSON {
		if err := writeReportJSON(out, list, runs); err != nil {
			fmt.Fprintln(errOut, err)
			return exitError
		}
		return exitOK
	}
	writeReport(out, list, runs)
	if *texts {
		for i, rs := range runs {
			writeTexts(out, list[i], rs)
		}
	}
	return exitOK
}
