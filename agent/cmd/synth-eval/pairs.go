//go:build syntheval

package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// pairs and picks are the person's blind A/B (C4, E0): pairs puts two
// runs' combined answers side by side for the A/B page with the sides
// drawn at random and kept apart in key.json, which the page never gets;
// picks reads what the person picked on the page (exported from the
// page's store) and maps it back to the runs. The person's picks are the
// final measure; score is checked against them.

// PairSet is what the page shows: no run names, no question IDs.
type PairSet struct {
	V     int        `json:"v"` // 1
	Name  string     `json:"name"`
	Made  string     `json:"made"`
	Pairs []PagePair `json:"pairs"`
}

// PagePair is one question with its two combined answers.
type PagePair struct {
	ID       string `json:"id"` // p01, p02, …
	Question string `json:"question"`
	Left     string `json:"left"`
	Right    string `json:"right"`
}

// PairKey says which run each side is. It stays on this PC.
type PairKey struct {
	V     int                `json:"v"`
	A     string             `json:"a"`
	B     string             `json:"b"`
	Sides map[string]KeySide `json:"sides"` // pair ID → its question and the run on the left
}

// KeySide is one pair's question and the run ("a" or "b") on its left.
type KeySide struct {
	Question string `json:"question"`
	Left     string `json:"left"`
}

func pairDir(name string) (string, error) {
	if !runNameRe.MatchString(name) {
		return "", fmt.Errorf("pairs name %q: use a-z, 0-9 and -, up to 64", name)
	}
	root, err := rootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "_pairs", name), nil
}

// coin draws a or b; tests replace it.
var coin = func() string {
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil || b[0]&1 == 0 {
		return pickA
	}
	return pickB
}

func pairsCmd(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("pairs", flag.ContinueOnError)
	fs.SetOutput(errOut)
	a := fs.String("a", "", "run A")
	b := fs.String("b", "", "run B")
	name := fs.String("name", "", "the pair set's name (a-z, 0-9, -)")
	only := fs.String("only", "", "question IDs to keep, comma-separated")
	if err := fs.Parse(args); err != nil {
		return exitFlags
	}
	if *a == "" || *a == *b {
		fmt.Fprintln(errOut, "give two runs, -a and -b")
		return exitFlags
	}
	dir, err := pairDir(*name)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	if _, err := os.Stat(dir); err == nil {
		fmt.Fprintf(errOut, "pair set %s exists: pick another name\n", *name)
		return exitFlags
	}
	both, err := comparable(*a, *b, splitList(*only), out)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	if len(both) == 0 {
		fmt.Fprintln(errOut, "no question has a combined answer in both runs")
		return exitFlags
	}
	set, key := makePairs(*name, *a, *b, both, time.Now())
	if err := saveJSON(filepath.Join(dir, "key.json"), key); err != nil {
		fmt.Fprintln(errOut, err)
		return exitError
	}
	if err := saveJSON(filepath.Join(dir, "pairs.json"), set); err != nil {
		fmt.Fprintln(errOut, err)
		return exitError
	}
	fmt.Fprintf(out, "pair set %s: %d pairs of %s and %s\npage data: %s\n", *name, len(set.Pairs), *a, *b, filepath.Join(dir, "pairs.json"))
	return exitOK
}

// makePairs puts each question's two combined answers on sides drawn at
// random.
func makePairs(name, a, b string, both [][2]Record, now time.Time) (PairSet, PairKey) {
	set := PairSet{V: 1, Name: name, Made: now.UTC().Format(time.RFC3339)}
	key := PairKey{V: 1, A: a, B: b, Sides: map[string]KeySide{}}
	for i, p := range both {
		id := fmt.Sprintf("p%02d", i+1)
		left := coin()
		pp := PagePair{ID: id, Question: p[0].Question.Text, Left: p[0].Synth.Text, Right: p[1].Synth.Text}
		if left == pickB {
			pp.Left, pp.Right = pp.Right, pp.Left
		}
		set.Pairs = append(set.Pairs, pp)
		key.Sides[id] = KeySide{Question: p[0].Question.ID, Left: left}
	}
	return set, key
}

// Pick is one pair as the person picked it on the page: each of overall
// and the optional criteria is left, right or same ("" when not picked).
type Pick struct {
	ID       string `json:"id"`
	Overall  string `json:"overall"`
	Intent   string `json:"intent,omitempty"`
	Readable string `json:"readable,omitempty"`
	Quality  string `json:"quality,omitempty"`
	Note     string `json:"note,omitempty"`
}

func picksCmd(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("picks", flag.ContinueOnError)
	fs.SetOutput(errOut)
	name := fs.String("name", "", "the pair set's name")
	in := fs.String("in", "", "the picks exported from the page (JSON array)")
	score := fs.String("score", "", "a scoring of the same runs, to see how far it agrees with the person")
	if err := fs.Parse(args); err != nil {
		return exitFlags
	}
	dir, err := pairDir(*name)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	var key PairKey
	if err := readJSON(filepath.Join(dir, "key.json"), &key); err != nil {
		fmt.Fprintln(errOut, err)
		return exitError
	}
	var picks []Pick
	if err := readJSON(*in, &picks); err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	var scores []ScoreRec
	if *score != "" {
		sd, err := scoreDir(*score)
		if err != nil {
			fmt.Fprintln(errOut, err)
			return exitFlags
		}
		if scores, err = loadScores(sd); err != nil {
			fmt.Fprintln(errOut, err)
			return exitError
		}
		if scores[0].A != key.A || scores[0].B != key.B {
			fmt.Fprintf(errOut, "scoring %s compares %s and %s, not %s and %s\n", *score, scores[0].A, scores[0].B, key.A, key.B)
			return exitFlags
		}
	}
	if err := writePicks(out, *name, key, picks, scores); err != nil {
		fmt.Fprintln(errOut, err)
		return exitFlags
	}
	return exitOK
}

func readJSON(path string, v any) error {
	if path == "" {
		return errors.New("give the file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

// toRun maps a side the person picked back to the run: a, b or same.
func toRun(side string, k KeySide) (string, error) {
	switch side {
	case "same":
		return pickSame, nil
	case "left", "right":
		if (side == "left") == (k.Left == pickA) {
			return pickA, nil
		}
		return pickB, nil
	}
	return "", fmt.Errorf("pick %q", side)
}

// writePicks prints the person's picks mapped to the runs: per criterion
// the wins of A and B and the ties, each question with its note, and with
// a scoring, how often each judge's overall pick was the person's.
func writePicks(out io.Writer, name string, key PairKey, picks []Pick, scores []ScoreRec) error {
	type row struct {
		qid      string
		criteria map[string]string // overall, intent, readable, quality → a, b, same
		note     string
	}
	var rows []row
	for _, p := range picks {
		k, ok := key.Sides[p.ID]
		if !ok {
			return fmt.Errorf("pair %q is not in set %s", p.ID, name)
		}
		if p.Overall == "" {
			continue // looked at, not picked
		}
		r := row{qid: k.Question, criteria: map[string]string{}, note: p.Note}
		for c, side := range map[string]string{"overall": p.Overall, "intent": p.Intent, "readable": p.Readable, "quality": p.Quality} {
			if side == "" {
				continue
			}
			v, err := toRun(side, k)
			if err != nil {
				return fmt.Errorf("pair %s: %w", p.ID, err)
			}
			r.criteria[c] = v
		}
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(x, y row) int { return strings.Compare(x.qid, y.qid) })
	fmt.Fprintf(out, "pair set %s: A = %s, B = %s; the person picked %d of %d pairs\n", name, key.A, key.B, len(rows), len(key.Sides))
	fmt.Fprintf(out, "%-10s %6s %6s %6s\n", "criterion", "A", "B", "same")
	for _, c := range []string{"overall", "intent", "readable", "quality"} {
		a, b, same := 0, 0, 0
		for _, r := range rows {
			switch r.criteria[c] {
			case pickA:
				a++
			case pickB:
				b++
			case pickSame:
				same++
			}
		}
		fmt.Fprintf(out, "%-10s %6d %6d %6d\n", c, a, b, same)
	}
	if len(scores) > 0 {
		agree := map[string][2]int{} // judge → matches, compared
		var judges []string
		for _, r := range rows {
			i := slices.IndexFunc(scores, func(s ScoreRec) bool { return s.Question.ID == r.qid })
			if i < 0 {
				continue
			}
			for _, pr := range resultsOf(scores[i]) {
				if pr.Failed {
					continue
				}
				if _, ok := agree[pr.Judge]; !ok {
					judges = append(judges, pr.Judge)
				}
				m := agree[pr.Judge]
				m[1]++
				if pr.Picks["overall"] == r.criteria["overall"] {
					m[0]++
				}
				agree[pr.Judge] = m
			}
		}
		for _, j := range judges {
			fmt.Fprintf(out, "%s picked as the person did: %d/%d\n", j, agree[j][0], agree[j][1])
		}
	}
	for _, r := range rows {
		line := fmt.Sprintf("%-12s overall %s", r.qid, strings.ToUpper(r.criteria["overall"]))
		for _, c := range []string{"intent", "readable", "quality"} {
			if v := r.criteria[c]; v != "" {
				line += fmt.Sprintf(", %s %s", c, strings.ToUpper(v))
			}
		}
		if r.note != "" {
			line += " · " + clip(r.note, 200)
		}
		fmt.Fprintln(out, line)
	}
	return nil
}
