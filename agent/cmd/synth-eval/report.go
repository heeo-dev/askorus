//go:build syntheval

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Summary is a run in a few numbers.
type Summary struct {
	Run         string  `json:"run"`
	Refine      string  `json:"refine"`
	Tier        string  `json:"tier"`
	Writer      string  `json:"writer"` // who wrote, at which tier, with which prompt
	From        string  `json:"from,omitempty"`
	Questions   int     `json:"questions"`
	Stopped     int     `json:"stopped"`
	SynthOK     int     `json:"synthOk"`
	Refined     int     `json:"refined"`
	MedianRef   float64 `json:"medianRefineS"`
	MedianAsk   float64 `json:"medianAskS"`
	MedianJudge float64 `json:"medianCompareS"`
	MedianSynth float64 `json:"medianSynthS"`
	MedianTotal float64 `json:"medianTotalS"`
	MedianChars int     `json:"medianChars"`
	Tables      int     `json:"tables"`
	MedianFirst int     `json:"medianFirstSentence"`
	StartsHead  int     `json:"startsWithHeading"`
	Korean      int     `json:"korean"`
	MeanLatin   float64 `json:"meanLatinShare"` // over the Korean questions with a combined answer
	Extras      int     `json:"extras"`
	AnsExtras   int     `json:"answerExtras"`
	Unnamed     int     `json:"unnamedSides"`
	Differ      int     `json:"differ"`
	Check       int     `json:"check"`
}

func summarize(name string, rs []Record) (Summary, []Metrics) {
	s := Summary{Run: name, Questions: len(rs)}
	if len(rs) > 0 {
		s.Refine, s.Tier = rs[0].Run.Refine, rs[0].Run.Tier
		s.Writer, s.From = rs[0].Run.writerLabel(), rs[0].Run.From
	}
	var ms []Metrics
	var ask, cmp, syn, tot, ref []float64
	var chars, first []int
	var latin []float64
	for _, r := range rs {
		m := metricsOf(r)
		ms = append(ms, m)
		if m.Stop != "" {
			s.Stopped++
			continue
		}
		ask = append(ask, float64(m.AskMs)/1000)
		if m.Kind != "" {
			s.Refined++
			ref = append(ref, float64(m.RefineMs)/1000)
		}
		cmp = append(cmp, float64(m.CompareMs)/1000)
		s.Differ += m.Differ
		s.Check += m.Check
		s.AnsExtras += m.AnswerExtras
		if !m.SynthOK {
			continue
		}
		s.SynthOK++
		syn = append(syn, float64(m.SynthMs)/1000)
		tot = append(tot, float64(m.TotalMs)/1000)
		chars = append(chars, m.Chars)
		first = append(first, m.FirstSentence)
		if m.Table {
			s.Tables++
		}
		if m.StartsHeading {
			s.StartsHead++
		}
		if m.Extras {
			s.Extras++
		}
		s.Unnamed += m.Unnamed
		if m.Korean {
			s.Korean++
			latin = append(latin, m.LatinShare)
		}
	}
	s.MedianAsk, s.MedianJudge, s.MedianSynth, s.MedianTotal = median(ask), median(cmp), median(syn), median(tot)
	s.MedianRef = median(ref)
	s.MedianChars, s.MedianFirst = int(medianInt(chars)), int(medianInt(first))
	for _, l := range latin {
		s.MeanLatin += l / float64(len(latin))
	}
	return s, ms
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	ys := slices.Clone(xs)
	slices.Sort(ys)
	if n := len(ys); n%2 == 1 {
		return ys[n/2]
	} else {
		return (ys[n/2-1] + ys[n/2]) / 2
	}
}

func medianInt(xs []int) float64 {
	fs := make([]float64, len(xs))
	for i, x := range xs {
		fs[i] = float64(x)
	}
	return median(fs)
}

// summaryRows are the summary's lines: a label and how to print a run's value.
var summaryRows = []struct {
	label string
	value func(Summary) string
}{
	{"questions (stopped)", func(s Summary) string { return fmt.Sprintf("%d (%d)", s.Questions, s.Stopped) }},
	{"combined answers written", func(s Summary) string { return fmt.Sprintf("%d", s.SynthOK) }},
	{"refined (median s)", func(s Summary) string { return fmt.Sprintf("%d (%.1f)", s.Refined, s.MedianRef) }},
	{"median answers s", func(s Summary) string { return fmt.Sprintf("%.1f", s.MedianAsk) }},
	{"median judging s", func(s Summary) string { return fmt.Sprintf("%.1f", s.MedianJudge) }},
	{"median combined s", func(s Summary) string { return fmt.Sprintf("%.1f", s.MedianSynth) }},
	{"median total s", func(s Summary) string { return fmt.Sprintf("%.1f", s.MedianTotal) }},
	{"median length (chars)", func(s Summary) string { return fmt.Sprintf("%d", s.MedianChars) }},
	{"with a table", func(s Summary) string { return fmt.Sprintf("%d", s.Tables) }},
	{"median first sentence", func(s Summary) string { return fmt.Sprintf("%d", s.MedianFirst) }},
	{"starts with a heading", func(s Summary) string { return fmt.Sprintf("%d", s.StartsHead) }},
	{"Latin share (Korean qs)", func(s Summary) string { return fmt.Sprintf("%.1f%% of %d", 100*s.MeanLatin, s.Korean) }},
	{"extras in combined", func(s Summary) string { return fmt.Sprintf("%d", s.Extras) }},
	{"extras in answers", func(s Summary) string { return fmt.Sprintf("%d", s.AnsExtras) }},
	{"unnamed differ sides", func(s Summary) string { return fmt.Sprintf("%d", s.Unnamed) }},
	{"differ / check items", func(s Summary) string { return fmt.Sprintf("%d / %d", s.Differ, s.Check) }},
}

// writeReport prints the runs' summaries side by side, then each run's
// questions.
func writeReport(out io.Writer, names []string, runs [][]Record) {
	var sums []Summary
	var all [][]Metrics
	for i, rs := range runs {
		s, ms := summarize(names[i], rs)
		sums = append(sums, s)
		all = append(all, ms)
	}
	head := fmt.Sprintf("%-26s", "")
	for _, s := range sums {
		head += fmt.Sprintf(" %18s", s.Run)
	}
	fmt.Fprintln(out, head)
	line := fmt.Sprintf("%-26s", "refine · tier")
	for _, s := range sums {
		line += fmt.Sprintf(" %18s", s.Refine+" · "+s.Tier)
	}
	fmt.Fprintln(out, line)
	line = fmt.Sprintf("%-26s", "writer · tier · prompt")
	for _, s := range sums {
		line += fmt.Sprintf(" %18s", s.Writer)
	}
	fmt.Fprintln(out, line)
	for _, row := range summaryRows {
		line := fmt.Sprintf("%-26s", row.label)
		for _, s := range sums {
			line += fmt.Sprintf(" %18s", row.value(s))
		}
		fmt.Fprintln(out, line)
	}
	for i, ms := range all {
		fmt.Fprintf(out, "\n== %s\n", names[i])
		fmt.Fprintf(out, "%-12s %-12s %5s %6s %6s %6s %6s %5s %5s %6s %4s %4s %s\n", "question", "kind", "ans", "ask s", "judge", "synth", "chars", "table", "first", "latin", "ext", "unn", "differ/check")
		for _, m := range ms {
			if m.Stop != "" {
				fmt.Fprintf(out, "%-12s stopped: %s\n", m.ID, m.Stop)
				continue
			}
			latin := "-"
			if m.Korean && m.SynthOK {
				latin = fmt.Sprintf("%.1f%%", 100*m.LatinShare)
			}
			synth := fmt.Sprintf("%.1f", float64(m.SynthMs)/1000)
			if !m.SynthOK {
				synth = m.SynthStatus
			}
			kind := "-"
			if m.Kind != "" {
				kind = m.Kind + "/" + m.Depth
			}
			fmt.Fprintf(out, "%-12s %-12s %2d/%-2d %6.1f %6.1f %6s %6d %5s %5d %6s %4s %4d %d/%d\n",
				m.ID, kind, m.Answered, m.Asked, float64(m.AskMs)/1000, float64(m.CompareMs)/1000, synth, m.Chars,
				yes(m.Table), m.FirstSentence, latin, yes(m.Extras), m.Unnamed, m.Differ, m.Check)
		}
	}
}

func yes(b bool) string {
	if b {
		return "yes"
	}
	return "-"
}

// writeReportJSON prints the summaries and every question's metrics.
func writeReportJSON(out io.Writer, names []string, runs [][]Record) error {
	type runOut struct {
		Summary   Summary   `json:"summary"`
		Questions []Metrics `json:"questions"`
	}
	var res []runOut
	for i, rs := range runs {
		s, ms := summarize(names[i], rs)
		res = append(res, runOut{s, ms})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

// writeTexts prints each question's combined answer, for a person to read.
func writeTexts(out io.Writer, name string, rs []Record) {
	for _, r := range rs {
		fmt.Fprintf(out, "\n== %s · %s\n%s\n", name, r.Question.ID, r.Question.Text)
		if r.Synth != nil && r.Synth.Status == "done" {
			fmt.Fprintf(out, "-- combined (%s %s, %.1fs)\n%s\n", r.Synth.Agent, r.Synth.Model, float64(r.Synth.Ms)/1000, strings.TrimSpace(r.Synth.Text))
		} else if r.Synth != nil {
			fmt.Fprintf(out, "-- combined %s %s\n", r.Synth.Status, r.Synth.Error)
		}
	}
}
