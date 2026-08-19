package strategy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"testing"

	"gitlab.com/smoothsics/ainp/internal/equity"
	"gitlab.com/smoothsics/ainp/internal/opponent"
	"gitlab.com/smoothsics/ainp/internal/personality"
	"gitlab.com/smoothsics/ainp/internal/poker"
)

func TestStrategyDecisionLogIsStructuredAndDoesNotContainCards(t *testing.T) {
	var logs bytes.Buffer
	engine := NewEngine(nil, WithLogger(slog.New(slog.NewJSONHandler(&logs, nil))))
	_, err := engine.Decide(context.Background(), Request{
		DecisionID: "decision-1", RequestID: "request-1", PlayerID: "bot", TableID: "table", HandID: "hand",
		Game: equity.GameNLH, Street: Preflop, Position: BTN, Hero: poker.MustParseCards("AsAh"), Opponents: [][]poker.Card{{}},
		Pot: 1.5, Stack: 100, BigBlind: 1, Seed: 42, LegalActions: []LegalAction{{Action: Check}, {Action: Raise, Min: 2, Max: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := logs.String()
	for _, expected := range []string{`"msg":"strategy_decision"`, `"decision_id":"decision-1"`, `"rule_id":"PREFLOP_OPEN"`, `"personality_id":"balanced"`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s in %s", expected, text)
		}
	}
	if strings.Contains(text, "AsAh") {
		t.Fatalf("raw cards leaked in strategy log: %s", text)
	}
}

func TestPreflopOpenAndFold(t *testing.T) {
	engine := NewEngine(nil)
	open, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: Preflop, Position: UTG, Hero: poker.MustParseCards("AsAh"),
		Opponents: [][]poker.Card{{}}, Pot: 1.5, Stack: 100, BigBlind: 1, Level: 3, Seed: 1,
		LegalActions: []LegalAction{{Action: Check}, {Action: Raise, Min: 2, Max: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if open.Action != Raise || open.Amount < 2 || open.RuleID != "PREFLOP_OPEN" {
		t.Fatalf("open decision=%+v", open)
	}

	fold, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: Preflop, Position: UTG, Hero: poker.MustParseCards("7s2h"),
		Opponents: [][]poker.Card{{}}, Pot: 3, ToCall: 10, Stack: 100, BigBlind: 1, Level: 3,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 10, Max: 10}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fold.Action != Fold || fold.Amount != 0 {
		t.Fatalf("fold decision=%+v", fold)
	}
}

func TestAggressiveNeverFoldProfileRatesAndContinuation(t *testing.T) {
	const hands = 100000
	raises := 0
	for hand := 0; hand < hands; hand++ {
		req := Request{
			PlayerID: "bot", TableID: "table", HandID: fmt.Sprint(hand), AIProfile: "FPCH_100_50",
			Street: Preflop, Pot: 3, ToCall: 2, Stack: 100, BigBlind: 2,
			BehaviorMode: "aggressive_never_fold", PreflopRaiseProbability: .5,
			PostflopAggressionChance: .75, NeverFold: true,
		}
		action, _, _, _, _ := chooseAggressiveNeverFold(req)
		if action == Fold {
			t.Fatalf("hand %d unexpectedly folded", hand)
		}
		if action == Raise {
			raises++
		}
	}
	rate := float64(raises) / hands
	if math.Abs(rate-.5) > .01 {
		t.Fatalf("preflop raise rate=%f, want 0.50±0.01", rate)
	}

	postflopAggressive := 0
	for hand := 0; hand < hands; hand++ {
		req := Request{
			PlayerID: "bot", TableID: "table", HandID: fmt.Sprint(hand), AIProfile: "FPCH_100_50",
			Street: Flop, Pot: 10, ToCall: 2, Stack: 100,
			BehaviorMode: "aggressive_never_fold", PostflopAggressionChance: .75, NeverFold: true,
		}
		action, _, _, _, _ := chooseAggressiveNeverFold(req)
		if action == Fold {
			t.Fatalf("postflop hand %d unexpectedly folded", hand)
		}
		if action == Raise {
			postflopAggressive++
		}
	}
	rate = float64(postflopAggressive) / hands
	if math.Abs(rate-.75) > .01 {
		t.Fatalf("postflop aggression rate=%f, want 0.75±0.01", rate)
	}
}

func TestPocketJacksContinueAgainstPreflopRaise(t *testing.T) {
	engine := NewEngine(nil)
	decision, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: Preflop, Position: BB, Hero: poker.MustParseCards("JsJh"),
		Opponents: [][]poker.Card{{}}, ActiveOpponents: 4, Pot: 75, ToCall: 25, Stack: 190,
		EffectiveStack: 190, BigBlind: 20, RaisesFaced: 2, Level: 3, PersonalityID: "tag", Seed: 404,
		PreflopOpenCallGap: .06, PreflopReraiseEquity: .76, PreflopExtraRaisePenalty: .025,
		PreflopMultiwayPenalty: .005, PreflopCallMargin: .035,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 25, Max: 25}, {Action: Raise, Min: 65, Max: 190}, {Action: AllIn, Min: 190, Max: 190}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action == Fold || decision.Equity.Equity < .70 {
		t.Fatalf("JJ must continue using heads-up starting strength: %+v", decision)
	}
}

func TestShortStackFacingOversizedBetUsesAllInPrice(t *testing.T) {
	engine := NewEngine(nil)
	decision, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: Preflop, Position: BB, Hero: poker.MustParseCards("AsAh"),
		Opponents: [][]poker.Card{{}}, Pot: 100, ToCall: 200, Stack: 40, EffectiveStack: 40,
		BigBlind: 10, RaisesFaced: 1, Seed: 1,
		LegalActions: []LegalAction{{Action: Fold}, {Action: AllIn, Min: 40, Max: 40}},
	})
	if err != nil || decision.Action != AllIn || decision.Amount != 40 || decision.PotOdds != float64(40)/140 {
		t.Fatalf("short-stack decision=%+v error=%v", decision, err)
	}
}

func TestPreflopEntryRangeIsNotPathologicallyTight(t *testing.T) {
	engine := NewEngine(nil)
	deck := poker.FullDeck()
	folds, decisions := 0, 0
	for left := 0; left < len(deck); left++ {
		for right := left + 1; right < len(deck); right++ {
			decision, err := engine.Decide(context.Background(), Request{
				Game: equity.GameNLH, Street: Preflop, Position: MP, Hero: []poker.Card{deck[left], deck[right]},
				Opponents: [][]poker.Card{{}}, ActiveOpponents: 5, Pot: 3, ToCall: 2, Stack: 100,
				BigBlind: 2, Level: 3, PersonalityID: "tag", Seed: int64(left*52 + right + 1),
				DisableHumanization: true, DisableOpponentModel: true, DisableThinkTime: true,
				PreflopOpenCallGap: .06, PreflopReraiseEquity: .76, PreflopExtraRaisePenalty: .025,
				PreflopMultiwayPenalty: .005, PreflopCallMargin: .035,
				LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 2, Max: 2}, {Action: Raise, Min: 6, Max: 100}, {Action: AllIn, Min: 100, Max: 100}},
			})
			if err != nil {
				t.Fatal(err)
			}
			decisions++
			if decision.Action == Fold {
				folds++
			}
		}
	}
	foldRate := float64(folds) / float64(decisions)
	if foldRate < .40 || foldRate > .78 {
		t.Fatalf("unexpected TAG MP unopened fold rate %.3f (%d/%d)", foldRate, folds, decisions)
	}
	t.Logf("TAG MP five-opponent unopened fold rate %.3f (%d/%d)", foldRate, folds, decisions)
}

func TestPostflopValueDrawAndWeakFold(t *testing.T) {
	engine := NewEngine(nil)
	value, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: River, Position: BTN, Hero: poker.MustParseCards("AsKs"), Board: poker.MustParseCards("QsJsTs2d3c"),
		Opponents: [][]poker.Card{{}}, Pot: 40, Stack: 100, BigBlind: 1, Level: 3,
		LegalActions: []LegalAction{{Action: Check}, {Action: Bet, Min: 10, Max: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if value.Action != Bet || value.Features.Class != ClassMadeStrong || value.Amount != 30 {
		t.Fatalf("value decision=%+v", value)
	}

	draw, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: Flop, Position: CO, Hero: poker.MustParseCards("AsKs"), Board: poker.MustParseCards("QsJs2d"),
		Opponents: [][]poker.Card{{}}, Pot: 50, ToCall: 10, Stack: 100, BigBlind: 1, Level: 3, Seed: 42, EquitySamples: 2_000,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 10, Max: 10}, {Action: Raise, Min: 30, Max: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if draw.Action != Raise || draw.RuleID != "POSTFLOP_DRAW_RAISE" || (draw.Features.Class != ClassDraw && draw.Features.Class != ClassMadeDraw) {
		t.Fatalf("draw decision=%+v", draw)
	}

	weak, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: River, Position: BB, Hero: poker.MustParseCards("2c3c"), Board: poker.MustParseCards("AhKdQsJc9d"),
		Opponents: [][]poker.Card{poker.MustParseCards("Tc8c")}, Pot: 10, ToCall: 50, Stack: 100, BigBlind: 1, Level: 3,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 50, Max: 50}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if weak.Action != Fold || weak.CallEV >= 0 {
		t.Fatalf("weak decision=%+v", weak)
	}
}

func TestPLOAndShortDeckStrategy(t *testing.T) {
	engine := NewEngine(nil)
	plo, err := engine.Decide(context.Background(), Request{
		Game: equity.GamePLO4, Street: Flop, Position: BTN, Hero: poker.MustParseCards("AsKsQdJc"), Board: poker.MustParseCards("Ts9s8s"),
		Opponents: [][]poker.Card{poker.MustParseCards("AhAdKhKd")}, Pot: 30, Stack: 100, BigBlind: 1, Level: 3,
		LegalActions: []LegalAction{{Action: Check}, {Action: Bet, Min: 8, Max: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if plo.Action != Bet || plo.Features.Category != poker.Flush {
		t.Fatalf("PLO decision=%+v", plo)
	}

	short, err := engine.Decide(context.Background(), Request{
		Game: equity.GameShortDeck, Street: Preflop, Position: BTN, Hero: poker.MustParseCards("AsKs"),
		Opponents: [][]poker.Card{{}}, Pot: 3, Stack: 100, BigBlind: 1, Level: 3, PersonalityID: "tag", Seed: 42, EquitySamples: 500,
		LegalActions: []LegalAction{{Action: Check}, {Action: Raise, Min: 3, Max: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if short.Action != Raise {
		t.Fatalf("short-deck decision=%+v", short)
	}
	plo6, err := engine.Decide(context.Background(), Request{
		Game: equity.GamePLO6, Street: Flop, Position: BTN, Hero: poker.MustParseCards("AsKsQdJc5h4h"), Board: poker.MustParseCards("Ts9s8s"),
		Opponents: [][]poker.Card{poker.MustParseCards("AhAdKhKd6c7c")}, Pot: 30, Stack: 100, BigBlind: 1, Level: 3,
		LegalActions: []LegalAction{{Action: Check}, {Action: Bet, Min: 8, Max: 100}},
	})
	if err != nil || plo6.Action != Bet || plo6.Features.Category != poker.Flush {
		t.Fatalf("PLO6 decision=%+v error=%v", plo6, err)
	}
}

func TestLegalGuardClampsAndUsesSafeFallback(t *testing.T) {
	guard, err := newActionGuard(Request{ToCall: 5, Stack: 100, BigBlind: 1, LegalActions: []LegalAction{{Action: Call, Min: 5, Max: 5}, {Action: Fold}}})
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err := guard.finalize(Raise, 50, true)
	if err != nil || action != Call || amount != 5 {
		t.Fatalf("fallback action=%s amount=%v error=%v", action, amount, err)
	}
	if _, err := newActionGuard(Request{}); !errors.Is(err, ErrNoLegalActions) {
		t.Fatalf("error=%v", err)
	}
	allInOnly, err := newActionGuard(Request{ToCall: .01, Stack: .01, BigBlind: 10, LegalActions: []LegalAction{{Action: Fold}, {Action: AllIn, Min: .01, Max: .01}}})
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err = allInOnly.finalize(Call, .01, false)
	if err != nil || action != AllIn || amount != .01 {
		t.Fatalf("call-to-allin action=%s amount=%v error=%v", action, amount, err)
	}
	action, amount, err = allInOnly.finalize(Raise, 30, true)
	if err != nil || action != AllIn || amount != .01 {
		t.Fatalf("raise-to-allin action=%s amount=%v error=%v", action, amount, err)
	}
	shortValue, err := newActionGuard(Request{Stack: 5, BigBlind: .1, LegalActions: []LegalAction{{Action: Check}, {Action: AllIn, Min: .01, Max: .01}}})
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err = shortValue.finalize(Bet, 3, true)
	if err != nil || action != AllIn || amount != .01 {
		t.Fatalf("short value bet action=%s amount=%v error=%v", action, amount, err)
	}
	deepValue, err := newActionGuard(Request{Stack: 100, BigBlind: 1, LegalActions: []LegalAction{{Action: Check}, {Action: AllIn, Min: 100, Max: 100}}})
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err = deepValue.finalize(Bet, 3, true)
	if err != nil || action != Check || amount != 0 {
		t.Fatalf("deep all-in fallback action=%s amount=%v error=%v", action, amount, err)
	}
}

func TestLegalGuardNeverFoldsWhenCheckIsAvailable(t *testing.T) {
	guard, err := newActionGuard(Request{
		ToCall: 5.551115123125783e-17,
		Stack:  100,
		LegalActions: []LegalAction{
			{Action: Fold},
			{Action: Check},
			{Action: Raise, Min: 2, Max: 100},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err := guard.finalize(Fold, 0, false)
	if err != nil || action != Check || amount != 0 {
		t.Fatalf("free-option fold action=%s amount=%v error=%v", action, amount, err)
	}
}

func TestLegalGuardNeverFoldProfileFallsBackToCall(t *testing.T) {
	guard, err := newActionGuard(Request{ToCall: 10, Stack: 100, NeverFold: true, LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 10, Max: 10}}})
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err := guard.finalize(Fold, 0, false)
	if err != nil || action != Call || amount != 10 {
		t.Fatalf("never-fold fallback action=%s amount=%v error=%v", action, amount, err)
	}
}

func TestLegalGuardCollapsesDustRemainderIntoAllIn(t *testing.T) {
	request := Request{
		Stack: 100, BigBlind: 1,
		CollapseNearAllIn: true, NearAllInRemainingChips: .01,
		LegalActions: []LegalAction{
			{Action: Raise, Min: 2, Max: 99.99},
			{Action: AllIn, Min: 100, Max: 100},
		},
	}
	guard, err := newActionGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err := guard.finalize(Raise, 99.99, true)
	if err != nil || action != AllIn || amount != 100 {
		t.Fatalf("dust raise action=%s amount=%v error=%v", action, amount, err)
	}

	request.NearAllInRemainingChips = .009
	guard, err = newActionGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err = guard.finalize(Raise, 99.99, true)
	if err != nil || action != Raise || amount != 99.99 {
		t.Fatalf("above threshold action=%s amount=%v error=%v", action, amount, err)
	}

	request.CollapseNearAllIn = false
	request.NearAllInRemainingChips = .01
	guard, err = newActionGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err = guard.finalize(Raise, 99.99, true)
	if err != nil || action != Raise || amount != 99.99 {
		t.Fatalf("disabled collapse action=%s amount=%v error=%v", action, amount, err)
	}

	request.CollapseNearAllIn = true
	request.LegalActions = []LegalAction{{Action: Raise, Min: 2, Max: 99.99}}
	guard, err = newActionGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err = guard.finalize(Raise, 99.99, true)
	if err != nil || action != Raise || amount != 99.99 {
		t.Fatalf("all-in unavailable action=%s amount=%v error=%v", action, amount, err)
	}
}

func TestPostflopDoesNotRaiseWhenCallCoversEffectiveStack(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	req := Request{
		Street: River, Pot: 3.39, ToCall: .59, Stack: 4.44, EffectiveStack: .01, BigBlind: .1,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: .59, Max: .59}, {Action: Raise, Min: 1.18, Max: 4.43}, {Action: AllIn, Min: 4.44, Max: 4.44}},
	}
	decision := Decision{Equity: equity.Result{Equity: .34}, PotOdds: .15, CallEV: .7, SPR: .003, Features: Features{Class: ClassMadeStrong}}
	action, amount, rule, _, aggressive := choosePostflopAdjusted(req, decision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Call || amount != .59 || rule != "POSTFLOP_CALL" || aggressive {
		t.Fatalf("action=%s amount=%v rule=%s aggressive=%t", action, amount, rule, aggressive)
	}
}

func TestPostflopAllowsRaiseWhenOpponentCanCoverMinimumExtra(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	req := Request{
		Street: Turn, Pot: 300, ToCall: 100, Stack: 1000, EffectiveStack: 100, BigBlind: 10,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 100, Max: 100}, {Action: Raise, Min: 200, Max: 1000}, {Action: AllIn, Min: 1000, Max: 1000}},
	}
	decision := Decision{Equity: equity.Result{Equity: .80}, PotOdds: .25, CallEV: 200, SPR: 1, Features: Features{Class: ClassMadeStrong}}
	action, _, rule, _, aggressive := choosePostflopAdjusted(req, decision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != AllIn || rule != "POSTFLOP_ALLIN_VALUE" || !aggressive {
		t.Fatalf("action=%s rule=%s aggressive=%t", action, rule, aggressive)
	}
}

func TestLowAbsoluteEquityStrongMadeHandDoesNotValueShove(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	req := Request{
		Street: Turn, Pot: 14.28, ToCall: .01, Stack: 10, EffectiveStack: 1.71, BigBlind: .1,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: .01, Max: .01}, {Action: Raise, Min: .02, Max: 10}, {Action: AllIn, Min: 10, Max: 10}},
	}
	decision := Decision{Equity: equity.Result{Equity: .1864}, PotOdds: .0007, CallEV: 2.65, SPR: .12, Features: Features{Class: ClassMadeStrong}}
	action, _, rule, _, _ := choosePostflopAdjusted(req, decision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Call || rule != "POSTFLOP_CALL" {
		t.Fatalf("low-equity strong made action=%s rule=%s", action, rule)
	}
}

func TestControlledCBetStrengthAndAllInFallback(t *testing.T) {
	seed := int64(1)
	for !bluffRoll(seed, 3, BTN) {
		seed++
	}
	action, _, rule, _, _ := choosePostflop(Request{Street: Flop, Position: BTN, Pot: 30, Seed: seed, WasPreflopAggressor: true}, Decision{Equity: equity.Result{Equity: .20}, Features: Features{Class: ClassAir}}, 3)
	if action != Bet || rule != "FLOP_CBET_BLUFF" {
		t.Fatalf("c-bet action=%s rule=%s seed=%d", action, rule, seed)
	}

	lowAction, _, _, _, _ := choosePreflop(Request{Position: BTN}, .53, 0, 1)
	highAction, _, _, _, _ := choosePreflop(Request{Position: BTN}, .53, 0, 5)
	if lowAction != Check || highAction != Raise {
		t.Fatalf("level actions low=%s high=%s", lowAction, highAction)
	}

	engine := NewEngine(nil)
	decision, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: Preflop, Position: BTN, Hero: poker.MustParseCards("AsAh"),
		Opponents: [][]poker.Card{{}}, Pot: 50, ToCall: 50, Stack: 50, EffectiveStack: 50, BigBlind: 1, Level: 3,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 50, Max: 50}},
	})
	if err != nil || decision.Action != Call || decision.Amount != 50 {
		t.Fatalf("all-in fallback decision=%+v error=%v", decision, err)
	}
}

func TestPersonalityAndOpponentAdjustments(t *testing.T) {
	balanced, _ := personality.Resolve("balanced")
	tight, _ := personality.Resolve("tight_passive")
	balancedAction, _, _, _, _ := choosePreflopAdjusted(Request{Position: BTN}, .54, 0, 3, balanced, OpponentRead{BluffMultiplier: 1})
	tightAction, _, _, _, _ := choosePreflopAdjusted(Request{Position: BTN}, .54, 0, 3, tight, OpponentRead{BluffMultiplier: 1})
	if balancedAction != Raise || tightAction != Check {
		t.Fatalf("personality actions balanced=%s tight=%s", balancedAction, tightAction)
	}

	read := analyzeOpponents(Request{OpponentModels: []opponent.Snapshot{{Hands: 100, VPIP: .45, Aggression: .4, FoldToCBet: .2, Archetype: "calling_station"}}})
	if read.BluffMultiplier >= .5 || read.ValueThreshold >= 0 {
		t.Fatalf("opponent read=%+v", read)
	}

	custom := balanced
	custom.MistakeRate = 1
	action, _, tags, changed := applyHumanization(Request{ToCall: 5}, Decision{Equity: equity.Result{Equity: .28}, PotOdds: .30}, custom, 8, Fold, 0, nil)
	if !changed || action != Call || len(tags) == 0 {
		t.Fatalf("humanization action=%s tags=%v changed=%v", action, tags, changed)
	}
}

func TestRateControlledPreflopMatchesTargetAcrossAllCombos(t *testing.T) {
	targets := []struct{ vpip, pfr float64 }{{.30, .15}, {.39, .14}, {.54, .11}, {.60, .05}, {.60, .10}, {.90, .05}}
	deck := poker.FullDeck()
	for _, target := range targets {
		hands, voluntary, raises := 0, 0, 0
		for left := 0; left < len(deck); left++ {
			for right := left + 1; right < len(deck); right++ {
				cards := []poker.Card{deck[left], deck[right]}
				percentile, err := equity.StartingHandPercentile(cards)
				if err != nil {
					t.Fatal(err)
				}
				action, _, _, _, _ := chooseRateControlledPreflop(Request{TargetVPIP: target.vpip, TargetPFR: target.pfr, ToCall: 1, Pot: 3, Stack: 100, EffectiveStack: 100, BigBlind: 2}, percentile)
				hands++
				if action == Call || action == Raise || action == AllIn {
					voluntary++
				}
				if action == Raise || action == AllIn {
					raises++
				}
			}
		}
		vpip, pfr := float64(voluntary)/float64(hands), float64(raises)/float64(hands)
		if math.Abs(vpip-target.vpip) > 1.0/1326 || math.Abs(pfr-target.pfr) > 1.0/1326 {
			t.Errorf("target %.2f/%.2f actual %.6f/%.6f", target.vpip, target.pfr, vpip, pfr)
		}
	}
}

func TestRateControlOnlySelectsFirstEntryAndCapsLaterPFR(t *testing.T) {
	balanced, _ := personality.Resolve("balanced")
	cards := poker.MustParseCards("As5s")
	percentile, err := equity.StartingHandPercentile(cards)
	if err != nil || percentile <= .15 || percentile > .30 {
		t.Fatalf("As5s percentile=%f err=%v", percentile, err)
	}
	req := Request{
		Game: equity.GameNLH, Position: BTN, Hero: cards, TargetVPIP: .30, TargetPFR: .15,
		HeroPreflopVPIP: true, ToCall: 5, Pot: 20, Stack: 100, EffectiveStack: 100, BigBlind: 2,
	}
	action, amount, rule, tags, aggressive := choosePreflopAdjusted(req, .80, .20, 5, balanced, OpponentRead{BluffMultiplier: 1})
	if action != Call || amount != 5 || rule != "PREFLOP_PROFILE_PFR_CAP" || aggressive || len(tags) == 0 {
		t.Fatalf("action=%s amount=%v rule=%s tags=%v aggressive=%t", action, amount, rule, tags, aggressive)
	}
	req.HeroPreflopPFR = true
	action, _, rule, _, _ = choosePreflopAdjusted(req, .80, .20, 5, balanced, OpponentRead{BluffMultiplier: 1})
	if action != Raise || rule == "PREFLOP_PROFILE_PFR_CAP" {
		t.Fatalf("already-PFR action=%s rule=%s", action, rule)
	}
}

func TestLargePotRiskControlFoldsMarginalEquity(t *testing.T) {
	calling, _ := personality.Resolve("calling_station")
	req := Request{Street: Turn, Pot: 20, ToCall: 5, BigBlind: 1, LargePotThresholdBB: 12, LargePotMinEquity: .68, PostflopCallMargin: .10}
	action, _, rule, _, _ := choosePostflopAdjusted(req, Decision{Equity: equity.Result{Equity: .60}, PotOdds: .20, Features: Features{Class: ClassMade}}, 1, calling, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "POSTFLOP_FOLD" {
		t.Fatalf("marginal large-pot action=%s rule=%s", action, rule)
	}
	req.ToCall = 0
	action, _, rule, _, _ = choosePostflopAdjusted(req, Decision{Equity: equity.Result{Equity: .60}, Features: Features{Class: ClassMade}}, 1, calling, OpponentRead{BluffMultiplier: 1})
	if action != Check || rule != "POSTFLOP_LARGE_POT_CONTROL" {
		t.Fatalf("unchecked large-pot action=%s rule=%s", action, rule)
	}
}

func TestPersonalityThinkTimeIsReturned(t *testing.T) {
	engine := NewEngine(nil)
	decision, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: Preflop, Position: BTN, Hero: poker.MustParseCards("AsAh"),
		Opponents: [][]poker.Card{{}}, Pot: 3, Stack: 100, BigBlind: 1, PersonalityID: "tricky", Seed: 88,
		LegalActions: []LegalAction{{Action: Check}, {Action: Raise, Min: 3, Max: 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, _ := personality.Resolve("tricky")
	if decision.PersonalityID != "tricky" || decision.ThinkTime < profile.ThinkMin || decision.ThinkTime > profile.ThinkMax {
		t.Fatalf("decision=%+v profile=%+v", decision, profile)
	}
}

func TestAirCallGuardRejectsNegativeEVAndRepeatedRiverCall(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	base := Request{
		Street: River, Pot: 10, ToCall: 5, Stack: 50, EffectiveStack: 50, BigBlind: 1,
		RiverAirCallMargin: .15, RepeatedAirCallPenalty: .08, RejectNegativeEVCalls: true,
	}
	negative := Decision{Equity: equity.Result{Equity: .30}, PotOdds: 1.0 / 3, CallEV: -1, Features: Features{Class: ClassAir}}
	action, _, rule, _, _ := choosePostflopAdjusted(base, negative, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "POSTFLOP_NEGATIVE_EV_FOLD" {
		t.Fatalf("negative-EV action=%s rule=%s", action, rule)
	}

	base.HeroPostflopCalls = 2
	marginal := Decision{Equity: equity.Result{Equity: .55}, PotOdds: .25, CallEV: 3, Features: Features{Class: ClassAir}}
	action, _, rule, _, _ = choosePostflopAdjusted(base, marginal, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "POSTFLOP_AIR_MARGIN_FOLD" {
		t.Fatalf("repeated river action=%s rule=%s", action, rule)
	}
}

func TestAirCallGuardCanBeDisabledForLegacyBehavior(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	req := Request{Street: River, Pot: 10, ToCall: 2.5, Stack: 50, EffectiveStack: 50, BigBlind: 1}
	decision := Decision{Equity: equity.Result{Equity: .30}, PotOdds: .20, CallEV: 1, Features: Features{Class: ClassAir}}
	action, _, _, _, _ := choosePostflopAdjusted(req, decision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Call {
		t.Fatalf("legacy action=%s, want call", action)
	}
}

func TestHumanizationCannotOverrideProtectedPostflopFold(t *testing.T) {
	engine := NewEngine(nil)
	decision, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: Flop, Position: BB,
		Hero: poker.MustParseCards("Jc4d"), Board: poker.MustParseCards("Jh9s8s"),
		Opponents: [][]poker.Card{poker.MustParseCards("QsQd")}, Pot: 100, ToCall: 40,
		Stack: 4000, EffectiveStack: 4000, BigBlind: 20, Level: 5, Seed: 8,
		PersonalityID: "balanced", RejectNegativeEVCalls: true,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 40, Max: 40}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != Fold || decision.RuleID != "POSTFLOP_NEGATIVE_EV_FOLD" || decision.Humanized {
		t.Fatalf("protected decision=%+v", decision)
	}
}
