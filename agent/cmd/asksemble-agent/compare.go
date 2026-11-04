package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/asksemble/asksemble/agent/internal/adapter/claude"
	"github.com/asksemble/asksemble/agent/internal/adapter/codex"
	"github.com/asksemble/asksemble/agent/internal/adapter/opencode"
	"github.com/asksemble/asksemble/agent/internal/compares"
	"github.com/asksemble/asksemble/agent/internal/config"
	"github.com/asksemble/asksemble/agent/internal/core"
	"github.com/asksemble/asksemble/agent/internal/judge"
)

// `asksemble-agent compare` (P2-3): a comparison of answers the person
// gives as files, made in this process the way POST /v1/comparisons makes
// one, and judged by the AI programs on this PC; with -synth one AI then
// writes the combined answer (종합 답변, S5b). The running agent is not
// needed. The unit texts and the combined answer go to the person's own
// terminal only; nothing of the question or the answers is logged.

const (
	// compareOwner owns the CLI's comparisons.
	compareOwner = "cli"
	// compareFileMax bounds what is read of any one file; the limits of
	// each kind are below it.
	compareFileMax = 1 << 20
	compareUsageEn = "usage: asksemble-agent compare -q FILE -answer AI=FILE -answer AI=FILE ... [-model AI=MODEL] [-earlier FILE] [-judges A,B] [-synth [-writer AI] [-tier quick|standard|deep]] [-json]"
	compareUsageKo = "사용법: asksemble-agent compare -q 파일 -answer AI=파일 -answer AI=파일 ... [-model AI=모델] [-earlier 파일] [-judges A,B] [-synth [-writer AI] [-tier quick|standard|deep]] [-json]"
)

// compareLimit bounds a whole comparison of the CLI; a variable so a test
// can shorten it.
var compareLimit = 10 * time.Minute

// selfElevated is claude.SelfElevated; tests replace it.
var selfElevated = claude.SelfElevated

// pcAIs are the AI programs whose answers compare takes as PC answers.
var pcAIs = []string{claude.Name, codex.Name, opencode.Name}

var aiNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// compareFlags are compare's flags before any file is read.
type compareFlags struct {
	question string
	answers  []compareFile // in the order given
	models   map[string]string
	earlier  []string // file paths, oldest first
	judges   []string
	synth    *compares.SynthSpec // -synth with -writer and -tier; nil without
	asJSON   bool
}

// compareFile is one -answer: the AI and the file of its answer.
type compareFile struct {
	agent, via, path string
}

// compareInput is what compare compares, the files read.
type compareInput struct {
	question string
	earlier  []string
	answers  []compares.DirectAnswer // Model only as -model gave it
	judges   []string
	synth    *compares.SynthSpec
	asJSON   bool
}

func compareCmd(dir string, args []string, in io.Reader, out io.Writer) error {
	f, err := parseCompareFlags(args)
	if err != nil {
		return err
	}
	// N3: the judges would run with this terminal's rights.
	if selfElevated() {
		return errElevated()
	}
	ci, err := readCompareFiles(f, in)
	if err != nil {
		return err
	}
	cfg, err := config.Load(dir)
	if err != nil {
		return err
	}
	ca, err := newAdapter(cfg)
	if err != nil {
		return err
	}
	adapters := append([]core.Adapter{ca}, askOnlyAdapters(cfg)...)
	answers, notes, err := answerModels(adapters, ci.answers)
	if err != nil {
		return err
	}
	note := out
	if ci.asJSON {
		note = os.Stderr // out holds the JSON only
	}
	for _, n := range notes {
		say(note, n)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	spec := compares.DirectSpec{Owner: compareOwner, Question: ci.question, Earlier: ci.earlier, Answers: answers, Judges: ci.judges, Synth: ci.synth}
	return compareWith(ctx, adapters, judgeGate(cfg), spec, out, ci.asJSON)
}

// parseCompareFlags reads compare's flags and checks what it can without
// the files.
func parseCompareFlags(args []string) (compareFlags, error) {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	q := fs.String("q", "", "file with the question (- reads standard input)")
	var answers, models, earlier []string
	fs.Func("answer", "AI=FILE: the answer of claude, codex or opencode on this PC, or one pasted from the chatgpt or gemini app; 2 to 6 of them", func(v string) error {
		answers = append(answers, v)
		return nil
	})
	fs.Func("model", "AI=MODEL: the model that wrote AI's answer (a PC AI's default: the model of its standard tier)", func(v string) error {
		models = append(models, v)
		return nil
	})
	fs.Func("earlier", "FILE: an earlier question of a follow-up, oldest first; at most 3", func(v string) error {
		earlier = append(earlier, v)
		return nil
	})
	judges := fs.String("judges", "", "comma-separated AIs that may judge, in order of preference (default: every AI that judges here)")
	asJSON := fs.Bool("json", false, "print the comparison as JSON, as GET /v1/comparisons/{id} shows it")
	synth := fs.Bool("synth", false, "then have one AI write a combined answer (its company receives the question, every answer with the AIs' names and the comparison's results)")
	writer := fs.String("writer", claude.Name, "with -synth: the AI that writes the combined answer, claude, codex or opencode; another that may write takes over when it cannot")
	tier := fs.String("tier", core.TierStandard, "with -synth: the ask tier whose model writes: quick, standard or deep")
	if err := fs.Parse(args); err != nil {
		return compareFlags{}, err
	}
	if fs.NArg() > 0 || *q == "" {
		return compareFlags{}, fail(compareUsageEn, compareUsageKo)
	}
	f := compareFlags{question: *q, models: map[string]string{}, earlier: earlier, asJSON: *asJSON}
	withSynth := false
	fs.Visit(func(fl *flag.Flag) { withSynth = withSynth || fl.Name == "writer" || fl.Name == "tier" })
	switch {
	case !*synth && withSynth:
		return compareFlags{}, fail("-writer and -tier go with -synth", "-writer, -tier는 -synth와 함께 씁니다")
	case !slices.Contains(pcAIs, *writer):
		return compareFlags{}, fail("-writer must name claude, codex or opencode", "-writer에는 claude, codex, opencode 중 하나를 쓰세요")
	case !core.ValidTier(*tier):
		return compareFlags{}, fail("-tier must be quick, standard or deep", "-tier는 quick, standard, deep 중 하나입니다")
	case *synth:
		f.synth = &compares.SynthSpec{Agent: *writer, Tier: *tier}
	}
	if len(answers) < compares.MinAnswers || len(answers) > compares.MaxAnswers {
		return compareFlags{}, fail("give 2 to 6 answers, each with -answer AI=FILE", "-answer AI=파일로 답을 2~6개 주세요")
	}
	for _, v := range answers {
		agent, path, ok := strings.Cut(v, "=")
		if !ok || path == "" {
			return compareFlags{}, fail("-answer %q is not AI=FILE", "-answer %q가 AI=파일 꼴이 아닙니다", v)
		}
		via := ""
		switch {
		case slices.Contains(pcAIs, agent):
			via = judge.ViaPC
		case aiNameRe.MatchString(agent) && judge.HandoffFamily(agent) != "":
			via = judge.ViaHandoff
		default:
			return compareFlags{}, fail("unknown AI %q in -answer: name claude, codex or opencode on this PC, or the chatgpt or gemini app",
				"-answer의 %q는 모르는 AI입니다. 이 PC의 claude, codex, opencode 또는 chatgpt, gemini 앱을 쓰세요", agent)
		}
		if slices.ContainsFunc(f.answers, func(a compareFile) bool { return a.agent == agent }) {
			return compareFlags{}, fail("-answer names %s twice", "-answer에 %s가 두 번 있습니다", agent)
		}
		f.answers = append(f.answers, compareFile{agent: agent, via: via, path: path})
	}
	for _, v := range models {
		agent, model, ok := strings.Cut(v, "=")
		switch {
		case !ok || !core.ValidAskModel(model):
			return compareFlags{}, fail("-model %q is not AI=MODEL", "-model %q가 AI=모델 꼴이 아닙니다", v)
		case !slices.ContainsFunc(f.answers, func(a compareFile) bool { return a.agent == agent }):
			return compareFlags{}, fail("-model names %s, which has no -answer", "-model의 %s에는 -answer가 없습니다", agent)
		case f.models[agent] != "":
			return compareFlags{}, fail("-model names %s twice", "-model에 %s가 두 번 있습니다", agent)
		}
		f.models[agent] = model
	}
	if len(earlier) > judge.MaxEarlier {
		return compareFlags{}, fail("at most 3 -earlier questions", "-earlier는 3개까지입니다")
	}
	for _, j := range splitList(*judges) {
		if !slices.Contains(pcAIs, j) || slices.Contains(f.judges, j) {
			return compareFlags{}, fail("-judges must name claude, codex or opencode, each once", "-judges에는 claude, codex, opencode를 한 번씩만 쓰세요")
		}
		f.judges = append(f.judges, j)
	}
	stdin := 0
	for _, p := range slices.Concat([]string{f.question}, f.earlier) {
		if p == "-" {
			stdin++
		}
	}
	for _, a := range f.answers {
		if a.path == "-" {
			stdin++
		}
	}
	if stdin > 1 {
		return compareFlags{}, fail("only one file can be - (standard input)", "- (표준 입력)는 파일 하나에만 쓸 수 있습니다")
	}
	return f, nil
}

// readCompareFiles reads the files of f as UTF-8 text and checks their
// limits: the question 32 KiB without white space around it, each answer
// 64 KiB, each earlier question 4 KiB without white space around it.
func readCompareFiles(f compareFlags, in io.Reader) (compareInput, error) {
	ci := compareInput{judges: f.judges, synth: f.synth, asJSON: f.asJSON}
	text, over, err := readText(f.question, in)
	if err != nil {
		return ci, err
	}
	ci.question = strings.TrimSpace(text)
	switch {
	case over || len(ci.question) > compares.MaxQuestionBytes:
		return ci, fail("the question in %s is over 32 KiB", "%s의 질문이 32KiB를 넘습니다", f.question)
	case ci.question == "":
		return ci, fail("the question in %s is empty", "%s의 질문이 비어 있습니다", f.question)
	}
	for _, p := range f.earlier {
		text, over, err := readText(p, in)
		if err != nil {
			return ci, err
		}
		e := strings.TrimSpace(text)
		switch {
		case over || len(e) > judge.MaxEarlierBytes:
			return ci, fail("the earlier question in %s is over 4 KiB", "%s의 앞 질문이 4KiB를 넘습니다", p)
		case e == "":
			return ci, fail("the earlier question in %s is empty", "%s의 앞 질문이 비어 있습니다", p)
		}
		ci.earlier = append(ci.earlier, e)
	}
	for _, a := range f.answers {
		text, over, err := readText(a.path, in)
		if err != nil {
			return ci, err
		}
		switch {
		case over || len(text) > compares.MaxAnswerBytes:
			return ci, fail("the answer in %s is over 64 KiB", "%s의 답이 64KiB를 넘습니다", a.path)
		case strings.TrimSpace(text) == "":
			return ci, fail("the answer in %s is empty", "%s의 답이 비어 있습니다", a.path)
		}
		ci.answers = append(ci.answers, compares.DirectAnswer{ID: a.agent, Agent: a.agent, Via: a.via, Model: f.models[a.agent], Text: text})
	}
	return ci, nil
}

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// readText reads a file, or standard input for "-", as UTF-8 text without
// NUL, leaving out a byte order mark. over is true when the file holds
// more than compareFileMax bytes; text is then cut there.
func readText(path string, in io.Reader) (text string, over bool, err error) {
	r := in
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return "", false, fail("cannot read %s (%v)", "%s를 읽을 수 없습니다 (%v)", path, err)
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, compareFileMax+1))
	if err != nil {
		return "", false, fail("cannot read %s (%v)", "%s를 읽을 수 없습니다 (%v)", path, err)
	}
	if len(b) > compareFileMax {
		return "", true, nil
	}
	b = bytes.TrimPrefix(b, utf8BOM)
	if !utf8.Valid(b) || bytes.IndexByte(b, 0) >= 0 {
		return "", false, fail("%s is not UTF-8 text", "%s는 UTF-8 글이 아닙니다", path)
	}
	return string(b), false, nil
}

// answerModels fills in the model of each PC answer that -model left out:
// its AI's standard ask tier model, which a note says (S3 decision 6). The
// company of a PC answer comes from its model, so without one no judge
// could be picked for it. POST /v1/comparisons assumes nothing.
func answerModels(adapters []core.Adapter, answers []compares.DirectAnswer) ([]compares.DirectAnswer, []msg, error) {
	out := slices.Clone(answers)
	var notes []msg
	for i, a := range out {
		if a.Via != judge.ViaPC || a.Model != "" {
			continue
		}
		var tiers map[string]core.AskModel
		for _, ad := range adapters {
			if asker, ok := ad.(core.Asker); ok && ad.Agent() == a.Agent {
				tiers = asker.AskTiers()
			}
		}
		model := tiers[core.TierStandard].Model
		if model == "" {
			return nil, nil, fail("the model of %[1]s's answer is unknown: give -model %[1]s=MODEL", "%[1]s 답의 모델을 알 수 없습니다. -model %[1]s=모델을 주세요", a.Agent)
		}
		out[i].Model = model
		notes = append(notes, m("(model assumed: %[1]s %[2]s, its standard tier; if another model answered, give -model %[1]s=MODEL)",
			"(모델을 %[1]s %[2]s(표준 단계)로 봅니다. 다른 모델이 답했으면 -model %[1]s=모델을 주세요)", a.Agent, model))
	}
	return out, notes, nil
}

// compareWith runs one comparison in this process and prints it: the
// cards, the judge requests and the counts, then the combined answer when
// spec.Synth asks for one, or the comparison as JSON. A comparison that
// failed is an error with its code, and so is a combined answer that
// failed.
func compareWith(ctx context.Context, adapters []core.Adapter, gate compares.Gate, spec compares.DirectSpec, out io.Writer, asJSON bool) error {
	if gate.Settings.Off {
		return fail("comparison judging is off (agent.json judge.off)", "비교 판정이 꺼져 있습니다 (agent.json judge.off)")
	}
	mgr := compares.New(compares.Options{Adapters: adapters, Gate: gate, Logger: slog.New(slog.DiscardHandler)})
	defer mgr.Close() // ends judges still running
	if !asJSON {
		// The AIs that may judge, in -judges' order of preference.
		engines := mgr.Engines(ctx)
		if len(spec.Judges) > 0 {
			var picked []compares.Engine
			for _, j := range spec.Judges {
				if i := slices.IndexFunc(engines, func(e compares.Engine) bool { return e.Agent == j }); i >= 0 {
					picked = append(picked, engines[i])
				}
			}
			engines = picked
		}
		var judges []string
		for _, e := range engines {
			judges = append(judges, displayName(e.Agent)+" "+e.Model)
		}
		if len(judges) > 0 {
			list := strings.Join(judges, " · ")
			say(out, m("comparing %d answers; judges: %s", "답 %d개를 비교합니다. 판정: %s", len(spec.Answers), list))
		}
	}
	c, err := mgr.Start(spec)
	var se *compares.SpecError
	switch {
	case errors.Is(err, compares.ErrNoJudge):
		return fail("no AI judges on this PC, so the answers cannot be compared (asksemble-agent check)",
			"이 PC에는 판정하는 AI가 없어 답을 비교할 수 없습니다 (asksemble-agent check)")
	case errors.As(err, &se):
		return fail("the comparison was refused (%s): %s", "비교를 받지 않았습니다 (%s): %s", se.Code, se.Message)
	case err != nil:
		return err
	}
	wctx, cancel := context.WithTimeout(ctx, compareLimit)
	defer cancel()
	c, _ = mgr.WaitIdle(wctx, compareOwner, c.ID)
	synthBusy := c.Synthesis != nil && (c.Synthesis.Status == compares.SynthWaiting || c.Synthesis.Status == compares.SynthRunning)
	if c.Status == compares.Waiting || c.Status == compares.Running || synthBusy {
		if ctx.Err() != nil {
			return fail("cancelled", "취소했습니다")
		}
		return fail("the comparison did not end within %d minutes", "비교가 %d분 안에 끝나지 않았습니다", int(compareLimit/time.Minute))
	}
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(c); err != nil {
			return err
		}
	} else {
		printComparison(out, c)
		printSynthesis(out, c.Synthesis)
	}
	if c.Status == compares.Failed {
		e := judge.Error{Code: core.ErrCodeInternal}
		if c.Error != nil {
			e = *c.Error
		}
		return fail("the comparison failed (%s): %s", "비교에 실패했습니다 (%s): %s", e.Code, terminalText(e.Message))
	}
	if s := c.Synthesis; s != nil && s.Status == compares.SynthFailed {
		e := judge.Error{Code: core.ErrCodeInternal}
		if s.Error != nil {
			e = *s.Error
		}
		return fail("the combined answer failed (%s): %s", "종합 답변을 쓰지 못했습니다 (%s): %s", e.Code, terminalText(e.Message))
	}
	return nil
}

// printSynthesis prints the combined answer under its header: who wrote
// it with which model and in how long, and whether it was written without
// the comparison's results. The text keeps its line breaks and loses every
// other control character. A combined answer that failed prints its code
// and message; one that is off prints nothing.
func printSynthesis(out io.Writer, s *compares.Synthesis) {
	if s == nil || s.Status == compares.SynthOff {
		return
	}
	head := "no AI / AI 없음"
	if s.Agent != "" {
		head = displayName(s.Agent)
	}
	if s.Model != "" {
		head += " " + s.Model
	}
	if s.Ms > 0 {
		head += fmt.Sprintf(" · %.1f s", float64(s.Ms)/1000)
	}
	if s.Status == compares.SynthDone && !s.Judged {
		head += "; not compared / 비교 결과 없이"
	}
	fmt.Fprintf(out, "\n== Combined answer / 종합 답변 (%s)\n", terminalText(head))
	switch s.Status {
	case compares.SynthDone:
		fmt.Fprintln(out, terminalLines(s.Text))
		if s.UnnamedSides > 0 {
			fmt.Fprintln(out)
			say(out, m("(some places where the AIs differ may be missing from the combined answer: check the Differ card above)",
				"(AI 답이 다른 곳 일부가 종합 답변에 빠졌을 수 있습니다. 위의 다른 말 카드에서 확인하세요)"))
		}
	case compares.SynthFailed:
		code, message := core.ErrCodeInternal, ""
		if s.Error != nil {
			code, message = s.Error.Code, s.Error.Message
		}
		fmt.Fprintf(out, "  failed / 실패: %s %s\n", code, terminalText(message))
	default:
		fmt.Fprintf(out, "  %s\n", s.Status)
	}
}

// cardNames are the cards' headers, as the app names them.
var cardNames = map[string]msg{
	judge.CardDiffer:  {"Differ", "다른 말"},
	judge.CardCheck:   {"Check this", "확인 필요"},
	judge.CardAgree:   {"Agree", "같은 말"},
	judge.CardOnlyOne: {"Only one", "한 AI만"},
}

// checkNames say why an item is on the 확인 필요 card (compare.why.*).
var checkNames = map[string]msg{
	judge.CheckUnsure:         {"the judge was not sure", "확신 못 함"},
	judge.CheckCrossDirection: {"the two directions disagree", "두 방향이 안 맞음"},
	judge.CheckNoEvidence:     {"no sentence was picked as evidence", "근거가 된 문장을 고르지 않음"},
	judge.CheckNumberMismatch: {"the same, but the numbers differ", "같은 말인데 숫자가 다름"},
	judge.CheckNumbersAbsent:  {"not mentioned, but the other answer has other numbers", "언급 없음인데 다른 숫자가 있음"},
}

// printComparison prints the cards in judge.CardOrder, the judge requests
// and the counts.
func printComparison(out io.Writer, c compares.Comparison) {
	units := map[string]judge.Unit{}
	for _, a := range c.Answers {
		for _, u := range a.Units {
			units[u.ID] = u
		}
	}
	for _, kind := range judge.CardOrder {
		var items []judge.Item
		for _, it := range c.Items {
			if it.Kind == kind {
				items = append(items, it)
			}
		}
		name, ok := cardNames[kind]
		if !ok {
			name = msg{kind, kind}
		}
		fmt.Fprintf(out, "\n== %s / %s (%d)\n", name.en, name.ko, len(items))
		if len(items) == 0 {
			fmt.Fprintln(out, "  (none / 없음)")
		}
		for _, it := range items {
			printItem(out, it, units)
		}
	}
	// Items of a card this build does not know (an open string).
	for _, it := range c.Items {
		if !slices.Contains(judge.CardOrder, it.Kind) {
			fmt.Fprintf(out, "\n== %s\n", it.Kind)
			printItem(out, it, units)
		}
	}

	fmt.Fprintf(out, "\n== Judged by / 판정한 AI (%d)\n", len(c.Requests))
	if len(c.Requests) == 0 {
		fmt.Fprintln(out, "  (none / 없음)")
	}
	for _, r := range c.Requests {
		fmt.Fprintln(out, "  "+requestLine(r))
	}
	fmt.Fprintln(out)
	n := c.Counts
	say(out, m("differ %d · check this %d · agree %d · only one %d · not compared %d · not judged %d",
		"다른 말 %d · 확인 필요 %d · 같은 말 %d · 한 AI만 %d · 비교 못 한 문장 %d · 판정 안 한 문장 %d",
		n.Differ, n.Check, n.Agree, n.OnlyOne, n.NotCompared, n.NotJudged))
}

// printItem prints one card item: each side as "[answers] its first
// unit", then what the item notes.
func printItem(out io.Writer, it judge.Item, units map[string]judge.Unit) {
	for i, p := range it.Positions {
		lead := "  - "
		if i > 0 {
			lead = "    ↔ "
		}
		text := ""
		if len(p.Units) > 0 {
			text = p.Units[0]
			if u, ok := units[p.Units[0]]; ok {
				text = u.Text
			}
		}
		more := ""
		if n := len(p.Units) - 1; n > 0 {
			more = fmt.Sprintf(" (+%d more / %d개 더)", n, n)
		}
		fmt.Fprintf(out, "%s[%s] %s%s\n", lead, strings.Join(p.Agents, ", "), terminalText(text), more)
	}
	if it.NumbersDiffer {
		fmt.Fprintln(out, "    numbers differ / 숫자가 다릅니다")
	}
	if len(it.Check) > 0 {
		var en, ko []string
		for _, ch := range it.Check {
			x, ok := checkNames[ch]
			if !ok {
				x = msg{ch, ch}
			}
			en, ko = append(en, x.en), append(ko, x.ko)
		}
		fmt.Fprintf(out, "    why: %s / 까닭: %s\n", strings.Join(en, "; "), strings.Join(ko, "; "))
	}
	if len(it.Missing) > 0 {
		names := strings.Join(it.Missing, ", ")
		fmt.Fprintf(out, "    not compared with %s / %s 답과는 비교하지 못했습니다\n", names, names)
	}
}

// requestLine is one judge request: "r1 opencode (judge only): claude →
// codex · done · 9.8 s · model", or "… · failed: code message".
func requestLine(r judge.Request) string {
	who := r.Judge
	if who == "" {
		who = "(no judge / 판정할 AI 없음)"
	}
	if r.JudgeOnly {
		who += " (judge only / 판정만)"
	}
	line := fmt.Sprintf("%s %s: %s → %s", r.ID, who, r.From, r.Against)
	if r.Status == judge.RequestFailed && r.Error != nil {
		line += " · failed: " + r.Error.Code
		if r.Error.Message != "" {
			line += " " + terminalText(r.Error.Message)
		}
	} else {
		line += " · " + r.Status
	}
	if r.Ms > 0 {
		line += fmt.Sprintf(" · %.1f s", float64(r.Ms)/1000)
	}
	if r.Model != "" {
		line += " · " + r.Model
	}
	if r.Attempt > 1 {
		line += fmt.Sprintf(" · attempt %d / %d번째", r.Attempt, r.Attempt)
	}
	return line
}

// terminalText is s on one line without control characters, so an AI's
// words cannot move the cursor or recolour the person's terminal.
func terminalText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
}

// terminalLines is s with its line breaks kept, a CR LF or a lone CR as
// a line break, and every other control character, U+2028 and U+2029 as
// a space, so a combined answer cannot move the cursor or recolour the
// person's terminal.
func terminalLines(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case unicode.IsControl(r), r == '\u2028', r == '\u2029':
			return ' '
		}
		return r
	}, s)
}

// errElevated refuses to run as administrator: what the agent starts
// would get full rights (N3).
func errElevated() error {
	return fail("refusing to run as administrator: sessions started from here would get full rights. Start the agent from a normal terminal",
		"관리자 권한으로는 실행하지 않습니다. 여기서 켠 세션이 모든 권한을 갖게 됩니다. 일반 터미널에서 에이전트를 켜세요")
}
