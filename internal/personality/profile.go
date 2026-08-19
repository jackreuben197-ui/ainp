package personality

import (
	"fmt"
	"math/rand"
	"time"
)

type Profile struct {
	ID                  string
	OpenThresholdDelta  float64
	CallMarginDelta     float64
	AggressionDelta     float64
	ValueThresholdDelta float64
	BluffMultiplier     float64
	BetSizeMultiplier   float64
	MistakeRate         float64
	SlowPlayRate        float64
	ThinkMin            time.Duration
	ThinkMax            time.Duration
}

var builtins = map[string]Profile{
	"balanced": {
		ID: "balanced", BluffMultiplier: 1, BetSizeMultiplier: 1, MistakeRate: .015, SlowPlayRate: .04,
		ThinkMin: 600 * time.Millisecond, ThinkMax: 2200 * time.Millisecond,
	},
	"tight_passive": {
		ID: "tight_passive", OpenThresholdDelta: .05, CallMarginDelta: .02, AggressionDelta: .04,
		ValueThresholdDelta: .03, BluffMultiplier: .35, BetSizeMultiplier: .85, MistakeRate: .03, SlowPlayRate: .02,
		ThinkMin: 900 * time.Millisecond, ThinkMax: 2800 * time.Millisecond,
	},
	"tag": {
		ID: "tag", OpenThresholdDelta: .02, AggressionDelta: -.01, ValueThresholdDelta: -.01,
		BluffMultiplier: .7, BetSizeMultiplier: 1, MistakeRate: .01, SlowPlayRate: .03,
		ThinkMin: 650 * time.Millisecond, ThinkMax: 2100 * time.Millisecond,
	},
	"lag": {
		ID: "lag", OpenThresholdDelta: -.05, CallMarginDelta: -.02, AggressionDelta: -.05,
		ValueThresholdDelta: -.02, BluffMultiplier: 1.5, BetSizeMultiplier: 1.1, MistakeRate: .035, SlowPlayRate: .05,
		ThinkMin: 450 * time.Millisecond, ThinkMax: 1800 * time.Millisecond,
	},
	"calling_station": {
		ID: "calling_station", OpenThresholdDelta: -.02, CallMarginDelta: -.06, AggressionDelta: .05,
		BluffMultiplier: .25, BetSizeMultiplier: .8, MistakeRate: .05, SlowPlayRate: .02,
		ThinkMin: 500 * time.Millisecond, ThinkMax: 1700 * time.Millisecond,
	},
	"tricky": {
		ID: "tricky", OpenThresholdDelta: -.01, AggressionDelta: -.03, ValueThresholdDelta: -.02,
		BluffMultiplier: 1.25, BetSizeMultiplier: .9, MistakeRate: .025, SlowPlayRate: .15,
		ThinkMin: 800 * time.Millisecond, ThinkMax: 3200 * time.Millisecond,
	},
}

func Resolve(id string) (Profile, error) {
	if id == "" {
		id = "balanced"
	}
	profile, ok := builtins[id]
	if !ok {
		return Profile{}, fmt.Errorf("unknown personality %q", id)
	}
	return profile, nil
}

func Neutral() Profile {
	return Profile{ID: "neutral", BluffMultiplier: 1, BetSizeMultiplier: 1}
}

func Builtins() []Profile {
	ids := []string{"balanced", "tight_passive", "tag", "lag", "calling_station", "tricky"}
	profiles := make([]Profile, 0, len(ids))
	for _, id := range ids {
		profiles = append(profiles, builtins[id])
	}
	return profiles
}

func ThinkTime(profile Profile, seed int64, complexity float64) time.Duration {
	if profile.ThinkMax <= profile.ThinkMin {
		return profile.ThinkMin
	}
	if complexity < 0 {
		complexity = 0
	}
	if complexity > 1 {
		complexity = 1
	}
	rng := rand.New(rand.NewSource(seed))
	randomFactor := (rng.Float64() + rng.Float64()) / 2
	factor := .35*randomFactor + .65*complexity
	return profile.ThinkMin + time.Duration(float64(profile.ThinkMax-profile.ThinkMin)*factor)
}
