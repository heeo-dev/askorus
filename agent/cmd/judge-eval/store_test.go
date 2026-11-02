//go:build judgeeval

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := manifest{Tool: "judge-eval", Format: manifestFormat, Run: "r", Created: "2026-11-01T00:00:00Z", Set: setP06, Split: splitDev,
		Questions: []int{2, 12}, Engines: []string{"claude", "codex"}, Tier: "quick", Shape: "directed", Labels: "v1", Rules: "judge-v1",
		MaxUnits: 40, Parallel: 2, Checks: []string{"unsure"}, EngineInfo: map[string]engineInfo{"claude": {Model: "haiku", Effort: "medium", Family: "anthropic", Version: "2.1.316"}},
		SettingsIgnored: true, Data: dataHashes{Files: map[string]string{"questions.json": "ab"}, Perturb: "cd"},
		EvalFolder: `C:\x\eval`, CodexSchema: `C:\x\codex-schema`, Commit: "b01ba32",
		Sessions: []session{{Started: "2026-11-01T00:00:00Z", Versions: map[string]string{"claude": "2.1.316"}}}}
	if err := writeJSON(filepath.Join(dir, fileManifest), m); err != nil {
		t.Fatal(err)
	}
	got, err := readManifest(dir)
	if err != nil || !reflect.DeepEqual(got, m) {
		t.Errorf("round trip:\n got %+v\nwant %+v\n%v", got, m, err)
	}
	m.Tool = "other"
	writeJSON(filepath.Join(dir, fileManifest), m)
	if _, err := readManifest(dir); err == nil {
		t.Error("another tool's manifest was read")
	}
}

func TestRecordsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), fileRequests)
	line := func(key, status string) string {
		b, _ := json.Marshal(record{Key: key, Status: status})
		return string(b) + "\n"
	}
	os.WriteFile(path, []byte(line("a", statusFailed)+line("b", statusDone)+line("a", statusDone)), 0o600)
	recs, order, err := loadRecords(path, false)
	if err != nil || len(recs) != 2 || recs["a"].Status != statusDone || order[0] != "a" {
		t.Errorf("latest line wins: %v %v %v", recs, order, err)
	}
	os.WriteFile(path, []byte(line("a", statusDone)+"{oops\n"+line("b", statusDone)), 0o600)
	if _, _, err := loadRecords(path, false); err == nil {
		t.Error("a bad line in the middle is no error")
	}
	full := line("a", statusDone) + line("b", statusDone)
	os.WriteFile(path, []byte(full+`{"key":"c","sta`), 0o600)
	recs, _, err = loadRecords(path, true)
	if b, _ := os.ReadFile(path); err != nil || len(recs) != 2 || string(b) != full {
		t.Errorf("the torn line was not cut away: %q %v", b, err)
	}
	os.WriteFile(path, []byte(full+"{\"key\":\"c\"\n"), 0o600)
	if recs, _, err := loadRecords(path, false); err != nil || len(recs) != 2 {
		t.Errorf("a bad last line is torn: %v %v", recs, err)
	}
}

func TestResumeRefusesChangedData(t *testing.T) {
	data, out := writeMini(t, nil), t.TempDir()
	args := []string{"run", "-engines", "gold", "-run", "g", "-data", data, "-out", out}
	if code, _, stderr := cli(t, context.Background(), args...); code != exitOK {
		t.Fatalf("exit %d %s", code, stderr)
	}
	// Another tier or split is refused as a flag.
	if code, _, stderr := cli(t, context.Background(), append(args, "-resume", "-tier", "standard")...); code != exitFlags || !strings.Contains(stderr, "-tier quick") {
		t.Errorf("another tier: %d %q", code, stderr)
	}
	// A data file that changed is refused.
	q := filepath.Join(data, fileQuestions)
	b, _ := os.ReadFile(q)
	os.WriteFile(q, []byte(strings.Replace(string(b), `"test"`, `"changed"`, 1)), 0o600)
	if code, _, stderr := cli(t, context.Background(), append(args, "-resume")...); code != exitError || !strings.Contains(stderr, "questions.json changed") {
		t.Errorf("changed data: %d %q", code, stderr)
	}
	os.WriteFile(q, b, 0o600)
	// So is a record whose task is not the one the job sends now.
	path := filepath.Join(out, "g", fileRequests)
	rb, _ := os.ReadFile(path)
	var r record
	first, rest, _ := strings.Cut(string(rb), "\n")
	json.Unmarshal([]byte(first), &r)
	r.Task = strings.Repeat("0", 64)
	nb, _ := json.Marshal(r)
	os.WriteFile(path, []byte(string(nb)+"\n"+rest), 0o600)
	if code, _, stderr := cli(t, context.Background(), append(args, "-resume")...); code != exitError || !strings.Contains(stderr, "task of "+r.Key+" changed") {
		t.Errorf("changed task: %d %q", code, stderr)
	}
}
