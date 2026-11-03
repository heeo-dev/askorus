//go:build syntheval

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// Question is one question of a set. ID is the file's name without its
// extension and the question's place among that file's questions, from 1
// (general-3): stable while the file only grows at its end.
type Question struct {
	ID   string `json:"id"`
	File string `json:"file"` // the file's base name
	Text string `json:"question"`
}

// maxQuestion bounds one question, as the phone's question box does not;
// the agent itself takes up to 32 KiB.
const maxQuestion = 4000

// readQuestions reads question files: one question per line; blank lines
// and lines that start with # are skipped. A question already read (same
// text, in any file) is skipped too, so a set can repeat nothing.
func readQuestions(paths []string) ([]Question, error) {
	var out []Question
	seen := map[string]bool{}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		stem := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 64*1024)
		n := 0
		line := 0
		for sc.Scan() {
			line++
			t := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\uFEFF"))
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if !utf8.ValidString(t) {
				f.Close()
				return nil, fmt.Errorf("%s line %d: not UTF-8", p, line)
			}
			if utf8.RuneCountInString(t) > maxQuestion {
				f.Close()
				return nil, fmt.Errorf("%s line %d: a question over %d characters", p, line, maxQuestion)
			}
			n++
			if seen[t] {
				continue
			}
			seen[t] = true
			out = append(out, Question{ID: fmt.Sprintf("%s-%d", stem, n), File: filepath.Base(p), Text: t})
		}
		err = sc.Err()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no questions in %s", strings.Join(paths, ", "))
	}
	return out, nil
}

// pick keeps the questions whose IDs only names, in the set's order; every
// name must be one of them. No names keeps them all.
func pick(qs []Question, only []string) ([]Question, error) {
	if len(only) == 0 {
		return qs, nil
	}
	for _, id := range only {
		if !slices.ContainsFunc(qs, func(q Question) bool { return q.ID == id }) {
			return nil, fmt.Errorf("no question %s", id)
		}
	}
	var out []Question
	for _, q := range qs {
		if slices.Contains(only, q.ID) {
			out = append(out, q)
		}
	}
	return out, nil
}
