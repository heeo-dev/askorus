//go:build judgeeval

// Command judge-eval measures comparison judging (판정 검증, P2-3 design
// part 9): it runs the product's judge path on the P0-6 set and on
// synthetic opposites, and reports what the phone would show. It uses the
// person's AI subscriptions, so it is started only when agreed, through
// scripts\judge-eval.ps1. It is built only with -tags judgeeval and is no
// part of the agent.
//
//	judge-eval run     -set p06|perturb -split dev|test|all [-questions 2,12] [-engines …] -expect N …
//	judge-eval report  -run NAME[,NAME] [-checks …] [-sweep-checks] [-texts] [-json]
//	judge-eval perturb [-n 60] [-out FILE] [-show N]
//	judge-eval data
//	judge-eval cleanup
//
// Exit codes: 0 done, 1 error, 2 bad flags, 3 stopped on a usage limit
// (run again with -resume), 4 interrupted, 5 call budget reached.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
)

// Exit codes.
const (
	exitOK          = 0
	exitError       = 1
	exitFlags       = 2
	exitUsageLimit  = 3
	exitInterrupted = 4
	exitBudget      = 5
	exitSuspect     = 6 // a judge reached for a tool or answered with another model
)

// selfElevated reports an elevated process; tests replace it.
var selfElevated = elevated

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := runMain(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

const usage = `judge-eval measures comparison judging (P2-3). It uses AI subscriptions.
  run      -set p06|perturb -split dev|test|all [-questions 2,12] [-engines claude,codex,opencode|gold]
           [-tier quick|standard] [-run NAME] [-resume] [-retry-failed] [-parallel 2] [-max-calls N] -expect N
  report   -run NAME[,NAME] [-checks unsure,...] [-sweep-checks] [-boot 2000] [-seed 20261101] [-texts] [-json]
  perturb  [-n 60] [-out FILE] [-show N]
  data     [-max-units 40]
  cleanup
Exit codes: 0 done, 1 error, 2 bad flags, 3 usage limit, 4 interrupted, 5 call budget.
`

// runMain runs one subcommand and returns its exit code.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		io.WriteString(stderr, usage)
		if len(args) == 0 {
			return exitFlags
		}
		return exitOK
	}
	if selfElevated() {
		fmt.Fprintln(stderr, "judge-eval refuses to run as administrator: run it from a normal PowerShell")
		return exitError
	}
	flagged := func(err error) int {
		var fe *flagError
		if errors.As(err, &fe) {
			if fe.msg != "help" {
				fmt.Fprintln(stderr, "judge-eval:", fe.msg)
			}
			return exitFlags
		}
		fmt.Fprintln(stderr, "judge-eval:", err)
		return exitError
	}
	switch args[0] {
	case "run":
		o, err := parseRun(args[1:], stderr)
		if err != nil {
			return flagged(err)
		}
		return cmdRun(ctx, o, stdout, stderr)
	case "report":
		o, err := parseReport(args[1:], stderr)
		if err != nil {
			return flagged(err)
		}
		return cmdReport(o, stdout, stderr)
	case "perturb":
		o, err := parsePerturb(args[1:], stderr)
		if err != nil {
			return flagged(err)
		}
		return cmdPerturb(o, stdout, stderr)
	case "data":
		o, err := parseData(args[1:], stderr)
		if err != nil {
			return flagged(err)
		}
		return cmdData(o, stdout, stderr)
	case "cleanup":
		if err := parseCleanup(args[1:], stderr); err != nil {
			return flagged(err)
		}
		return cmdCleanup(ctx, stdout, stderr)
	}
	fmt.Fprintf(stderr, "judge-eval: unknown command %q\n%s", args[0], usage)
	return exitFlags
}

// cmdCleanup deletes OpenCode judge conversations older than 10 minutes
// that a run left in the eval folder (CleanupJudge with no refs).
func cmdCleanup(ctx context.Context, stdout, stderr io.Writer) int {
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	cfg, err := config.Load(dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	ask, err := cfg.AskFolder()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	folder := filepath.Join(core.JudgeFolder(ask), evalFolderName)
	if _, err := os.Stat(folder); err != nil {
		fmt.Fprintln(stdout, "cleanup: no eval folder, nothing to do")
		return exitOK
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*core.JudgeCleanupTimeout)
	defer cancel()
	code := "ok"
	if err := opencode.New(opencode.Options{Path: cfg.OpenCodePath}).CleanupJudge(cctx, folder, nil); err != nil {
		if code = core.ErrorCode(err); code == "" {
			code = core.ErrCodeInternal
		}
	}
	fmt.Fprintf(stdout, "cleanup opencode: %s\n", code)
	if code != "ok" {
		return exitError
	}
	return exitOK
}
