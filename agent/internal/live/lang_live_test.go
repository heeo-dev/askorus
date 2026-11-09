//go:build live

package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/heeo-dev/askorus/agent/internal/adapter/claude"
	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/judge"
)

// LT-14 The question's language (#39) with the real Claude Code on the
// standard tier: on an English question Claude now and then wrote Korean
// (a debater's turn with its own Korean answer in the prompt; the plan's
// topics and questions and a point's outcome line with English data, 11-09
// on the S10). Four cases, each ASKORUS_LT14_N times (default 3): the
// moderator's plan (English answers), its last check with English turns and
// with Korean turns, and a debater's turn whose own answer is Korean. Every
// reply must have no Korean letters. ASKORUS_LT14_OLD=1 also runs the
// prompts as they were before #39 and only reports, to compare. 4·N AI
// calls (8·N with the old prompts).
func TestLT14QuestionLanguage(t *testing.T) {
	claudeDir, err := claude.DefaultClaudeDir()
	if err != nil {
		t.Fatal(err)
	}
	a := claude.New(claude.Options{ClaudeDir: claudeDir})
	judgeDir := core.JudgeFolder(askFolder(t))
	if err := os.MkdirAll(judgeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	model := a.AskTiers()[core.TierStandard].Model
	n := 3
	if v, err := strconv.Atoi(os.Getenv("ASKORUS_LT14_N")); err == nil && v > 0 {
		n = v
	}

	q := "Which season is best for a first trip to Japan? Pick one season and answer in two sentences."
	task := judge.DebateTask{SynthTask: judge.SynthTask{Question: q, Answers: []judge.SynthAnswer{
		{AI: "Claude", Text: "Autumn, from late October to November, is the best pick: mild, dry weather and maple foliage in Kyoto and Nikko. Crowds are lighter than at cherry blossom time."},
		{AI: "Codex", Text: "Spring is the best season for a first trip: late March to early April brings cherry blossoms and mild weather, though popular spots are busy."},
		{AI: "OpenCode", Text: "Spring, for the cherry blossoms; book early because it is peak season."},
	}}, Combined: "The AIs differ: Claude picks autumn for the foliage and dry weather, Codex and OpenCode pick spring for the cherry blossoms."}
	point := func(turns []judge.DebateTurn) judge.DebatePoint {
		return judge.DebatePoint{ID: "p1", Topic: "Best season for a first trip: autumn or spring", Nature: judge.NatureJudgment, Debate: true,
			Goal:  "which season fits which traveler",
			Sides: []judge.DebateSide{{AIs: []string{"Claude"}, Says: "Autumn: dry weather and foliage."}, {AIs: []string{"Codex", "OpenCode"}, Says: "Spring: cherry blossoms."}},
			Turns: turns}
	}
	enTurns := point([]judge.DebateTurn{
		{AI: "Codex", Ask: "Why is spring better despite the crowds?", Text: "Cherry blossoms are the most memorable sight for a first visit; booking early handles the crowds.", Stance: judge.StanceKeep},
		{AI: "Claude", Ask: "Does autumn still win if blossoms matter most?", Text: "No: for someone who wants blossoms, spring fits; for steady weather and fewer crowds, autumn fits.", Stance: judge.StancePartly},
	})
	koTurns := point([]judge.DebateTurn{
		{AI: "Codex", Ask: "Why is spring better despite the crowds?", Text: "첫 방문이라면 벚꽃이 가장 기억에 남습니다. 일찍 예약하면 붐비는 문제는 줄일 수 있습니다.", Stance: judge.StanceKeep},
		{AI: "Claude", Ask: "Does autumn still win if blossoms matter most?", Text: "벚꽃이 가장 중요하다면 봄이 맞습니다. 날씨와 한산함을 원하면 가을이 맞습니다.", Stance: judge.StancePartly},
	})
	ownKo := "가을(10월 말~11월)이 가장 좋습니다. 날씨가 건조하고 온화하며 교토와 닛코의 단풍이 아름답고, 벚꽃 철보다 덜 붐빕니다."
	nonce := judge.DebateNonce(q, task.Combined, ownKo, "p1")

	type tc struct {
		name, prompt string
		turn         bool
	}
	cases := func(old bool) []tc {
		plan := judge.DebatePlanPrompt(task, nonce)
		checkEN := judge.DebateCheckPrompt(q, enTurns, true, nonce)
		checkKO := judge.DebateCheckPrompt(q, koTurns, true, nonce)
		turn := core.AskPromptIn(core.QuestionLanguage(q), judge.DebateTurnPrompt(q, "Claude", ownKo, enTurns, "Codex says spring is better for a first trip. Why autumn?", nonce))
		if old {
			plan, checkEN, checkKO = before39(plan), before39(checkEN), before39(checkKO)
			turn = core.AskPreamble + "\n\n" + before39(judge.DebateTurnPrompt(q, "Claude", ownKo, enTurns, "Codex says spring is better for a first trip. Why autumn?", nonce))
		}
		cs := []tc{{"plan", plan, false}, {"check, English turns", checkEN, false}, {"check, Korean turns", checkKO, false}, {"turn, Korean own answer", turn, true}}
		if os.Getenv("ASKORUS_LT14_SYNTH") == "1" {
			// The combined answer and the answer after the debate (unchanged by #39: their
			// language line was made firm in #27), with one Korean answer among the data.
			st := task.SynthTask
			st.Answers = append(append([]judge.SynthAnswer{}, st.Answers...), judge.SynthAnswer{AI: "Gemini", Text: ownKo})
			closed := koTurns
			closed.Result, closed.Line = judge.OutcomeConditional, "Spring fits a traveler who wants cherry blossoms; autumn fits one who wants steady weather."
			cs = append(cs, tc{"synth, one Korean answer", judge.SynthPrompt(st, nonce), false}, tc{"after the debate, Korean turns", judge.AfterDebatePrompt(st, []judge.DebatePoint{closed}, nonce), false})
		}
		return cs
	}

	run := func(label string, cs []tc) (bad int, total int, samples []string) {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 3)
		for _, c := range cs {
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					text, err := ask(t, a, judgeDir, model, c.prompt, c.turn)
					mu.Lock()
					defer mu.Unlock()
					total++
					if err != nil {
						samples = append(samples, fmt.Sprintf("%s %s #%d: error %v", label, c.name, i+1, err))
						return
					}
					if hasHangul(text) {
						bad++
						samples = append(samples, fmt.Sprintf("%s %s #%d KOREAN: %.160q", label, c.name, i+1, text))
					}
				}()
			}
		}
		wg.Wait()
		return bad, total, samples
	}

	if os.Getenv("ASKORUS_LT14_OLD") == "1" {
		bad, total, samples := run("old", cases(true))
		t.Logf("before #39: %d of %d replies had Korean", bad, total)
		for _, s := range samples {
			t.Log(s)
		}
	}
	bad, total, samples := run("new", cases(false))
	t.Logf("now: %d of %d replies had Korean (model %s)", bad, total, model)
	for _, s := range samples {
		t.Log(s)
	}
	if bad > 0 {
		t.Errorf("%d of %d replies to an English question had Korean", bad, total)
	}
}

// ask sends one prompt the way a debate does: the moderator's through the
// locked-down writer command ({"answer": text}), a debater's as a one-off ask.
func ask(t *testing.T, a *claude.Adapter, dir, model, prompt string, turn bool) (string, error) {
	t.Helper()
	ctx := context.Background()
	if turn {
		res, err := a.Ask(ctx, core.AskRequest{Folder: dir, Prompt: prompt, Tier: core.TierStandard, OneOff: true})
		return res.Answer, err
	}
	res, err := a.Synthesize(ctx, core.SynthRequest{Folder: dir, Prompt: prompt, Model: model})
	if err != nil {
		return "", err
	}
	var out struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(res.Output, &out); err != nil {
		return string(res.Output), nil
	}
	return out.Answer, nil
}

// before39 turns a prompt back into its wording before #39: no language
// line, the general "in the language of the QUESTION", and the Korean
// wording rule in the check.
func before39(p string) string {
	for _, l := range []string{"Korean", "English"} {
		p = strings.ReplaceAll(p, "Write in "+l+": the QUESTION is in "+l+", whatever language the answers and the rest of the data are in.\n", "")
		p = strings.ReplaceAll(p, "in "+l+", the language of the QUESTION", "in the language of the QUESTION")
	}
	p = strings.ReplaceAll(p, ", even if YOUR ANSWER is in another language,", ",")
	if strings.Contains(p, "[Askorus debate-check") && !strings.Contains(p, "never 갈리다") {
		p = strings.Replace(p, "a better or new one from the debate first.", "a better or new one from the debate first. In Korean write 다르다 (다릅니다), never 갈리다.", 1)
	}
	return p
}

func hasHangul(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}
