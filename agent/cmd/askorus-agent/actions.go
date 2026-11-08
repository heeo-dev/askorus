package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/heeo-dev/askorus/agent/internal/config"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/hidden"
)

// These commands do on the PC what the phone does through the API, so the
// agent can be tried from a terminal alone (A3 gate).

// nativeID accepts "claude:<uuid>" or a bare uuid.
func nativeID(arg string) (string, error) {
	if agent, native, err := core.ParseID(arg); err == nil {
		if agent != "claude" {
			return "", fail("only claude sessions are supported", "지금은 claude 세션만 됩니다")
		}
		return native, nil
	}
	return arg, nil
}

func printOpen(out io.Writer, res core.OpenResult) {
	s := res.Session
	actions := map[string]msg{
		core.ActionNone:    m("already reachable, nothing was run", "이미 열려 있어 아무것도 실행하지 않았습니다"),
		core.ActionResumed: m("woke the session", "세션을 깨웠습니다"),
		core.ActionStarted: m("started a new session", "새 세션을 시작했습니다"),
	}
	what := actions[res.Action]
	title := firstLine(s.Title, s.Name, s.ID)
	say(out, msg{what.en + ": " + title + " (" + string(s.State) + ")", what.ko + ": " + title + " (" + string(s.State) + ")"})
	if res.RemoteURL != "" {
		fmt.Fprintf(out, "Remote link / 원격 링크: %s\n", res.RemoteURL)
	}
	if res.Hint == core.HintUseRCInTerminal {
		say(out, m("This session is open in a terminal on the PC; type /rc there to reach it from the phone.",
			"이 세션은 PC 터미널에서 열려 있습니다. 폰에서 쓰려면 그 터미널에 /rc를 입력하세요."))
	}
	if s.AttachCommand != "" {
		fmt.Fprintf(out, "On the PC / PC에서: %s\n", s.AttachCommand)
	}
}

func firstLine(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// explain turns adapter error codes into bilingual text.
func explain(err error) error {
	var ae *core.ActionError
	if !errors.As(err, &ae) {
		return err
	}
	texts := map[string]msg{
		core.ErrCodeNotFound:         m("no such session", "그런 세션이 없습니다"),
		core.ErrCodeBusy:             m("another action on this session is still running", "이 세션에 다른 동작이 진행 중입니다"),
		core.ErrCodeNotBackground:    m("only background sessions can be stopped", "백그라운드 세션만 멈출 수 있습니다"),
		core.ErrCodeStateUnknown:     m("could not read claude agents, so the session state is unknown", "claude agents를 읽지 못해 세션 상태를 모릅니다"),
		core.ErrCodeUntrusted:        m("the folder is not trusted yet: run claude in it once and accept the trust prompt", "아직 신뢰하지 않은 폴더입니다. 그 폴더에서 claude를 한 번 실행해 신뢰를 허용하세요"),
		core.ErrCodeLinkTimeout:      m("the session started but no remote link appeared", "세션은 떴지만 원격 링크가 생기지 않았습니다"),
		core.ErrCodeLaunchFailed:     m("claude failed", "claude 실행이 실패했습니다"),
		core.ErrCodeFolderNotFound:   m("the folder does not exist", "폴더가 없습니다"),
		core.ErrCodeAgentUnavailable: m("claude was not found", "claude를 찾지 못했습니다"),
		core.ErrCodeServiceElevated: m("the Claude background service runs as administrator; stop it from an administrator terminal: claude daemon stop --any",
			"Claude 백그라운드 서비스가 관리자 권한으로 돌고 있습니다. 관리자 터미널에서 멈추세요: claude daemon stop --any"),
	}
	x, ok := texts[ae.Code]
	if !ok {
		x = m("%s", "%s", ae.Code)
	}
	if ae.Detail != "" {
		x.en += " (" + ae.Detail + ")"
		x.ko += " (" + ae.Detail + ")"
	}
	return biError{x}
}

func openCmd(dir string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fail("usage: askorus-agent open SESSION_ID", "사용법: askorus-agent open 세션_ID")
	}
	id, err := nativeID(args[0])
	if err != nil {
		return err
	}
	a, err := loadAdapter(dir)
	if err != nil {
		return err
	}
	res, err := a.Open(context.Background(), id)
	if err != nil {
		return explain(err)
	}
	printOpen(out, res)
	return nil
}

func newCmd(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	folder := fs.String("folder", "", "folder id from `askorus-agent folders`")
	name := fs.String("name", "", "session name")
	model := fs.String("model", "", "model (default: Claude Code's)")
	effort := fs.String("effort", "", "effort: low, medium, high, xhigh, max (default: Claude Code's)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	f, ok := cfg.FolderByID(*folder)
	if !ok {
		return fail("unknown folder %q; see askorus-agent folders", "모르는 폴더 %q입니다. askorus-agent folders로 확인하세요", *folder)
	}
	if *model != "" && !config.ValidModel(*model) {
		return fail("bad model %q", "모델 이름이 잘못됐습니다: %q", *model)
	}
	if *effort != "" && !config.ValidEffort(*effort) {
		return fail("effort must be low, medium, high, xhigh or max", "노력 수준은 low, medium, high, xhigh, max 중 하나입니다")
	}
	a, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	res, err := a.Start(context.Background(), core.StartRequest{Folder: f.Path, Name: *name, Model: *model, Effort: *effort})
	if err != nil {
		return explain(err)
	}
	printOpen(out, res)
	return nil
}

func stopCmd(dir string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fail("usage: askorus-agent stop SESSION_ID", "사용법: askorus-agent stop 세션_ID")
	}
	id, err := nativeID(args[0])
	if err != nil {
		return err
	}
	a, err := loadAdapter(dir)
	if err != nil {
		return err
	}
	s, err := a.Stop(context.Background(), id)
	if err != nil {
		return explain(err)
	}
	say(out, m("stopped: %s (%s)", "멈춤: %s (%s)", firstLine(s.Title, s.Name, s.ID), s.State))
	return nil
}

func rmCmd(dir string, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fail("usage: askorus-agent rm SESSION_ID", "사용법: askorus-agent rm 세션_ID")
	}
	id, err := nativeID(args[0])
	if err != nil {
		return err
	}
	a, err := loadAdapter(dir)
	if err != nil {
		return err
	}
	if err := a.Remove(context.Background(), id); err != nil {
		return explain(err)
	}
	hid, err := hidden.Open(dir)
	if err != nil {
		return err
	}
	full := core.MakeID("claude", id)
	if err := hid.Add(full); err != nil {
		return err
	}
	say(out, m("removed %s from the list (the transcript stays; undo: askorus-agent hidden unhide %[1]s)",
		"%s를 목록에서 지웠습니다 (대화 기록은 남습니다. 되돌리기: askorus-agent hidden unhide %[1]s)", full))
	return nil
}

func hiddenCmd(dir string, args []string, out io.Writer) error {
	hid, err := hidden.Open(dir)
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "list" {
		list, err := hid.List()
		if err != nil {
			return err
		}
		for _, id := range list {
			fmt.Fprintln(out, id)
		}
		say(out, m("%d hidden sessions", "숨긴 세션 %d개", len(list)))
		return nil
	}
	if args[0] == "unhide" && len(args) == 2 {
		id := args[1]
		if !strings.Contains(id, ":") {
			id = core.MakeID("claude", id)
		}
		ok, err := hid.Remove(id)
		if err != nil {
			return err
		}
		if !ok {
			return fail("%s is not hidden", "%s는 숨긴 세션이 아닙니다", id)
		}
		say(out, m("%s is back in the list", "%s를 목록에 다시 보입니다", id))
		return nil
	}
	return fail("usage: askorus-agent hidden [list | unhide SESSION_ID]", "사용법: askorus-agent hidden [list | unhide 세션_ID]")
}

func foldersCmd(dir string, args []string, out io.Writer) error {
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "list" {
		fmt.Fprintln(out, "(id · 경로 · 열어 두기)")
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tPATH\tSERVER")
		for _, f := range cfg.Folders {
			srv := "off"
			if f.Server.Enabled {
				srv = fmt.Sprintf("on (%s, %d)", f.Server.Spawn, f.Server.Capacity)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", f.ID, f.Path, srv)
		}
		return tw.Flush()
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("folders add", flag.ContinueOnError)
		id := fs.String("id", "", "short id (default: from the folder name)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fail("usage: askorus-agent folders add [-id ID] PATH", "사용법: askorus-agent folders add [-id ID] 경로")
		}
		path, err := filepath.Abs(fs.Arg(0))
		if err != nil {
			return err
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return fail("%s is not a folder", "%s는 폴더가 아닙니다", path)
		}
		added, isNew := cfg.AddFolder(path, *id)
		if !isNew {
			return fail("%s is already allowed (id %s)", "%s는 이미 허용돼 있습니다 (id %s)", path, added.ID)
		}
		if err := config.Save(dir, cfg); err != nil {
			return err
		}
		say(out, m("allowed %s as %q. New sessions there need the folder to be trusted: run claude in it once.",
			"%s를 %q로 허용했습니다. 이 폴더에서 새 세션을 열려면 먼저 그 폴더에서 claude를 한 번 실행해 신뢰를 허용하세요.", added.Path, added.ID))
		return nil
	case "serve":
		fs := flag.NewFlagSet("folders serve", flag.ContinueOnError)
		spawn := fs.String("spawn", "", "same-dir or worktree (git repositories only)")
		capacity := fs.Int("capacity", 0, "concurrent sessions, 1-32")
		if len(args) < 3 || (args[2] != "on" && args[2] != "off") {
			return fail("usage: askorus-agent folders serve ID on|off [-spawn same-dir|worktree] [-capacity N]",
				"사용법: askorus-agent folders serve ID on|off [-spawn same-dir|worktree] [-capacity N]")
		}
		if err := fs.Parse(args[3:]); err != nil {
			return err
		}
		f, ok := cfg.FolderByID(args[1])
		if !ok {
			return fail("no folder %q", "%q 폴더가 없습니다", args[1])
		}
		srv := f.Server
		srv.Enabled = args[2] == "on"
		if *spawn != "" {
			srv.Spawn = *spawn
		}
		if *capacity != 0 {
			srv.Capacity = *capacity
		}
		if srv.Spawn == config.SpawnWorktree {
			if _, err := os.Stat(filepath.Join(f.Path, ".git")); err != nil {
				return fail("worktree spawn needs a git repository", "worktree 방식은 git 저장소에서만 됩니다")
			}
		}
		saved, err := setFolderServer(dir)(f.ID, srv)
		if err != nil {
			return err
		}
		if saved.Server.Enabled {
			say(out, m("folder %q will be kept open (%s, up to %d sessions). The running agent applies it within %s.",
				"%q 폴더를 열어 둡니다 (%s, 세션 최대 %d개). 실행 중인 에이전트가 %s 안에 반영합니다.",
				saved.ID, saved.Server.Spawn, saved.Server.Capacity, reconcileEvery))
		} else {
			say(out, m("folder %q will no longer be kept open; the running agent stops its server within %s.",
				"%q 폴더 열어 두기를 껐습니다. 실행 중인 에이전트가 %s 안에 서버를 멈춥니다.", saved.ID, reconcileEvery))
		}
		return nil
	case "remove":
		if len(args) != 2 {
			return fail("usage: askorus-agent folders remove ID", "사용법: askorus-agent folders remove ID")
		}
		kept := cfg.Folders[:0]
		found := false
		for _, f := range cfg.Folders {
			if f.ID == args[1] {
				found = true
				continue
			}
			kept = append(kept, f)
		}
		if !found {
			return fail("no folder %q", "%q 폴더가 없습니다", args[1])
		}
		cfg.Folders = kept
		if err := config.Save(dir, cfg); err != nil {
			return err
		}
		say(out, m("removed folder %q", "%q 폴더를 뺐습니다", args[1]))
		return nil
	}
	return fail("unknown folders command %q", "모르는 folders 명령 %q입니다", args[0])
}
