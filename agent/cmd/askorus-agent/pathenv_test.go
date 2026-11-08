package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests work on PATH strings only and never on the real user PATH.

func fakeExpand(s string) string {
	return strings.ReplaceAll(s, "%LOCALAPPDATA%", `C:\Users\u\AppData\Local`)
}

func TestPathListWithAppendsOnce(t *testing.T) {
	list := `C:\Windows;%LOCALAPPDATA%\Microsoft\WindowsApps`
	got, changed := pathListWith(list, `D:\Tools\Askorus`, fakeExpand)
	if !changed || got != list+`;D:\Tools\Askorus` {
		t.Fatalf("with = %q, %v", got, changed)
	}
	// Same folder spelled differently: case, trailing backslash, quotes.
	for _, again := range []string{`d:\tools\askorus`, `D:\Tools\Askorus\`, `"D:\Tools\Askorus"`} {
		if g, c := pathListWith(got, again, fakeExpand); c || g != got {
			t.Errorf("adding %q again changed PATH to %q", again, g)
		}
	}
}

func TestPathListKeepsOtherEntriesAsWritten(t *testing.T) {
	list := `%LOCALAPPDATA%\Programs\Askorus;C:\Go\bin;`
	if !pathListHas(list, `C:\Users\u\AppData\Local\Programs\Askorus`, fakeExpand) {
		t.Fatal("an entry with %LOCALAPPDATA% was not recognised")
	}
	got, changed := pathListWithout(list, `C:\Users\u\AppData\Local\Programs\Askorus`, fakeExpand)
	if !changed || got != `C:\Go\bin` {
		t.Fatalf("without = %q, %v", got, changed)
	}
	if g, c := pathListWithout(got, `D:\Other`, fakeExpand); c || g != got {
		t.Errorf("removing a missing folder changed PATH to %q", g)
	}
}

func TestPathListWithIntoEmpty(t *testing.T) {
	if got, changed := pathListWith("", `D:\A`, nil); !changed || got != `D:\A` {
		t.Errorf("with = %q, %v", got, changed)
	}
}

func TestLooksTemporary(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	if !looksTemporary(filepath.Join(home, "Downloads")) || !looksTemporary(filepath.Join(home, "Downloads", "x")) {
		t.Error("Downloads is not seen as temporary")
	}
	if !looksTemporary(filepath.Join(os.TempDir(), "askorus")) {
		t.Error("the temp folder is not seen as temporary")
	}
	if looksTemporary(filepath.Join(home, "DownloadsArchive")) || looksTemporary(filepath.Join(home, "AppData", "Local", "Programs", "Askorus")) {
		t.Error("a kept folder is seen as temporary")
	}
}

func TestPathUsage(t *testing.T) {
	if err := pathCmd([]string{"maybe"}, &strings.Builder{}); err == nil || !strings.Contains(err.Error(), "path on | off | status") {
		t.Errorf("err = %v", err)
	}
}
