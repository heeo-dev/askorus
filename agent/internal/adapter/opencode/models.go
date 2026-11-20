package opencode

// The newest model of each tier's line (#64). `opencode models` lists
// the models OpenCode can use now, one "provider/model" a line (login.go
// reads the same list for the sign-in), but not the variants each takes:
// a newer model's variant is known only once it is tried. So a newer
// model that OpenCode refuses ("Variant unavailable", or a model it does
// not know) is asked again with the shipped one and left out until the
// next list; and a question with photos keeps the shipped model of the
// line, the one known to see them (2026-11-18 probe), until a newer one
// is found to see them too.

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/agent/internal/core"
	"github.com/heeo-dev/askorus/agent/internal/proc"
)

const modelsTimeout = 30 * time.Second

// lines are the lines the tiers follow: DeepSeek Flash (quick and
// standard) and DeepSeek Pro (deep) of OpenCode Go.
var lines = []core.Line{
	{Base: "opencode-go/deepseek-v4.1-flash", Re: regexp.MustCompile(`^opencode-go/deepseek-v([0-9]+(?:\.[0-9]+)*)-flash$`)},
	{Base: "opencode-go/deepseek-v4-pro", Re: regexp.MustCompile(`^opencode-go/deepseek-v([0-9]+(?:\.[0-9]+)*)-pro$`)},
}

// listedModels reads `opencode models`: the names, without variants.
func listedModels(out []byte) []core.ListedModel {
	var list []core.ListedModel
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line) // lines end with \r\n on Windows
		if modelRe.MatchString(line) && !strings.Contains(line, "#") && core.ValidAskModel(line) {
			list = append(list, core.ListedModel{Name: line})
		}
	}
	return list
}

// listModels runs the list in opencode's own folder, as the sign-in
// check does.
func (a *Adapter) listModels(ctx context.Context) ([]core.ListedModel, error) {
	if a.exeErr != nil {
		return nil, a.exeErr
	}
	ctx, cancel := context.WithTimeout(ctx, modelsTimeout)
	defer cancel()
	dir := filepath.Dir(a.exe)
	res, err := proc.Run(ctx, proc.Cmd{Exe: a.exe, Dir: dir, Args: modelsArgs, Env: map[string]string{"PWD": dir}})
	if err != nil || res.ExitCode != 0 {
		return nil, err
	}
	return listedModels(res.Stdout), nil
}

// Follow returns the model to run for a model the phone picked
// (core.Following). A variant named in the model ("…#high") stays on the
// newer one.
func (a *Adapter) Follow(model string) string {
	base, variant := splitVariant(model)
	return core.FollowPick(a.AskTiers(), a.follow, base) + variant
}

// splitVariant parts "provider/model#variant" into the model and
// "#variant" ("" when there is none).
func splitVariant(model string) (string, string) {
	if base, v, ok := strings.Cut(model, "#"); ok {
		return base, "#" + v
	}
	return model, ""
}

// askCheck is the agent's own check of a model and variant, before
// OpenCode runs. A request it refuses never leaves a newer model out
// (2ho's 0.5.1 review); only OpenCode's own refusal does.
func askCheck(model, effort string) error {
	switch {
	case effort != "" && !variantRe.MatchString(effort):
		return core.Fail(core.ErrCodeBadModel, "variant "+effort)
	case !modelRe.MatchString(modelArg(model, effort)):
		return core.Fail(core.ErrCodeBadModel, model)
	}
	return nil
}

// Newer maps each shipped tier model to the newer one a tier uses now
// (core.Following).
func (a *Adapter) Newer() map[string]string { return core.NewerTiers(a.AskTiers(), a.follow) }

// WaitModels waits for a model list that runs (Info starts one), or until
// ctx ends: for a command that runs once, and for tests.
func (a *Adapter) WaitModels(ctx context.Context) { a.follow.Wait(ctx) }

// refused reports whether OpenCode refused the model or its variant.
func refused(err error) bool {
	return core.ErrorCode(err) == core.ErrCodeBadModel ||
		strings.Contains(strings.ToLower(core.ErrorDetail(err)), "variant unavailable")
}

// photoModel is the model a run with photos uses for model (with or
// without "#variant"): model when it is known to see photos, else the
// shipped model of its line when that one is, else "".
func (a *Adapter) photoModel(model string) string {
	base, variant, has := strings.Cut(model, "#")
	pick := base
	if !slices.Contains(imageModels, pick) {
		pick = a.follow.Base(base)
		if pick == "" || !slices.Contains(imageModels, pick) {
			return ""
		}
	}
	if has {
		pick += "#" + variant
	}
	return pick
}
