package proc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the command being run. ASKORUS_PROC_HELPER:
//
//	sleep  sleep 30 s
//	exit3  print to stdout and stderr, exit 3
//	big    print 9 MiB
//	echo   print stdin and ASKORUS_PROC_X
//	spawn  start a sleeping copy that holds stdout, write its pid to
//	       ASKORUS_PROC_PIDFILE, and end at once
func TestMain(m *testing.M) {
	switch os.Getenv("ASKORUS_PROC_HELPER") {
	case "":
		os.Exit(m.Run())
	case "sleep":
		time.Sleep(30 * time.Second)
	case "exit3":
		fmt.Print("out")
		fmt.Fprint(os.Stderr, "err")
		os.Exit(3)
	case "big":
		chunk := strings.Repeat("x", 1<<20)
		for i := 0; i < 9; i++ {
			fmt.Print(chunk)
		}
	case "echo":
		b, _ := io.ReadAll(os.Stdin)
		fmt.Printf("%s|%s", b, os.Getenv("ASKORUS_PROC_X"))
	case "spawn":
		exe, _ := os.Executable()
		child := exec.Command(exe)
		child.Env = append(os.Environ(), "ASKORUS_PROC_HELPER=sleep")
		child.Stdout = os.Stdout // holds the pipe open after this one ends
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		os.WriteFile(os.Getenv("ASKORUS_PROC_PIDFILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600)
	}
	os.Exit(0)
}

func helper(t *testing.T, mode string, extra map[string]string) Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"ASKORUS_PROC_HELPER": mode}
	for k, v := range extra {
		env[k] = v
	}
	return Cmd{Exe: exe, Dir: t.TempDir(), Env: env}
}

func TestRunOutputExitCodeAndStdin(t *testing.T) {
	res, err := Run(context.Background(), helper(t, "exit3", nil))
	if err != nil || res.ExitCode != 3 || string(res.Stdout) != "out" || string(res.Stderr) != "err" {
		t.Errorf("exit3 = %+v, %v", res, err)
	}
	c := helper(t, "echo", map[string]string{"ASKORUS_PROC_X": "x1"})
	c.Stdin = "[Askorus] 머리말\n\n질문 100% \"따옴표\" & 끝"
	res, err = Run(context.Background(), c)
	if err != nil || string(res.Stdout) != c.Stdin+"|x1" {
		t.Errorf("echo = %q, %v", res.Stdout, err)
	}
	res, err = Run(context.Background(), helper(t, "big", nil))
	if err != nil || len(res.Stdout) != maxStdout {
		t.Errorf("big = %d bytes, %v", len(res.Stdout), err)
	}
}

func TestRunTimeLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, helper(t, "sleep", nil))
	if !errors.Is(err, ErrTimeout) || time.Since(start) > 10*time.Second {
		t.Errorf("err = %v after %s", err, time.Since(start))
	}
}

// A process the command started and left running, holding its output
// open, neither keeps Run waiting nor outlives it (the job object on
// Windows, the process group on macOS and Linux, #52).
func TestRunLeavesNoChildren(t *testing.T) {
	for _, limit := range []time.Duration{0, 20 * time.Second} {
		pidFile := filepath.Join(t.TempDir(), "pid")
		ctx := context.Background()
		if limit > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, limit)
			defer cancel()
		}
		start := time.Now()
		if _, err := Run(ctx, helper(t, "spawn", map[string]string{"ASKORUS_PROC_PIDFILE": pidFile})); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took > 10*time.Second {
			t.Errorf("Run waited %s for the child's output", took)
		}
		b, _ := os.ReadFile(pidFile)
		pid, _ := strconv.Atoi(string(b))
		if pid == 0 {
			t.Fatal("no child pid")
		}
		if processRunning(pid) {
			t.Errorf("child %d still runs after Run", pid)
			if p, err := os.FindProcess(pid); err == nil {
				p.Kill()
			}
		}
	}
}

func TestCheckExe(t *testing.T) {
	exe, _ := os.Executable()
	if err := CheckExe(exe); err != nil {
		t.Errorf("test binary refused: %v", err)
	}
	dir := t.TempDir()
	for _, name := range []string{"x.cmd", "x.bat", "x.ps1", "x.sh", "x.js"} {
		p := filepath.Join(dir, name)
		os.WriteFile(p, nil, 0o700)
		if err := CheckExe(p); !errors.Is(err, ErrScript) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := CheckExe("codex.exe"); err == nil {
		t.Error("relative path accepted")
	}
	if err := CheckExe(dir); err == nil {
		t.Error("folder accepted")
	}
	if err := CheckExe(filepath.Join(dir, "missing.exe")); err == nil {
		t.Error("missing file accepted")
	}
	if _, err := Run(context.Background(), Cmd{Exe: filepath.Join(dir, "x.cmd"), Dir: dir}); !errors.Is(err, ErrScript) {
		t.Errorf("Run of a .cmd: %v", err)
	}
	if _, err := Run(context.Background(), Cmd{Exe: exe, Dir: "relative"}); err == nil {
		t.Error("relative folder accepted")
	}
}

func TestLookExe(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the .exe and .cmd rules are for Windows")
	}
	cmdDir, exeDir := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(cmdDir, "tool.cmd"), []byte("@echo off\r\n"), 0o600)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	t.Setenv("PATH", cmdDir)
	if _, shim, err := LookExe("tool"); !errors.Is(err, ErrScript) || shim != filepath.Join(cmdDir, "tool.cmd") {
		t.Errorf("only a .cmd: shim %q, %v", shim, err)
	}
	// A real exe later on PATH wins over the .cmd first on PATH.
	os.WriteFile(filepath.Join(exeDir, "tool.exe"), []byte("MZ"), 0o700)
	t.Setenv("PATH", cmdDir+string(os.PathListSeparator)+exeDir)
	if exe, _, err := LookExe("tool"); err != nil || exe != filepath.Join(exeDir, "tool.exe") {
		t.Errorf("exe = %q, %v", exe, err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, _, err := LookExe("tool"); !errors.Is(err, ErrNotFound) {
		t.Errorf("nothing: %v", err)
	}
}

func TestMergeEnv(t *testing.T) {
	got := mergeEnv([]string{"Path=a", "X=1", "=C:=C:\\"}, map[string]string{"PATH": "b", "NEW": "n"})
	slices.Sort(got)
	want := []string{"=C:=C:\\", "NEW=n", "X=1"}
	if runtime.GOOS == "windows" {
		want = append(want, "PATH=b")
	} else {
		want = append(want, "PATH=b", "Path=a") // names differ by case elsewhere
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("env = %q, want %q", got, want)
	}
}
