package strategy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
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
	for _, expected := range []string{`"msg":"strategy_decision"`, `"decision_id":"decision-1"`, `"rule_id":"PREFLOP_OPEN"`, `"personality_id":"balanced"`, `"river_card_features_available":false`, `"pair_from_board_only":false`, `"one_pair_below_top_board":false`, `"missed_flush_draw":false`, `"missed_straight_draw":false`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s in %s", expected, text)
		}
	}
	if strings.Contains(text, "AsAh") {
		t.Fatalf("raw cards leaked in strategy log: %s", text)
	}
}

func TestRiverFeaturesIdentifyMissedStraightDrawWithBoardOnlyPair(t *testing.T) {
	features, err := buildFeatures(Request{
		Game: equity.GameNLH, Street: River,
		Hero: poker.MustParseCards("As4s"), Board: poker.MustParseCards("Kh3dQcJsKs"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if features.Category != poker.OnePair || features.Class != ClassAir || !features.RiverCardFeaturesAvailable || !features.PairFromBoardOnly || features.MissedFlushDraw || !features.MissedStraightDraw {
		t.Fatalf("features=%+v", features)
	}
}

func TestRiverFeaturesIdentifyMissedFlushDrawWithBoardOnlyPair(t *testing.T) {
	features, err := buildFeatures(Request{
		Game: equity.GameNLH, Street: River,
		Hero: poker.MustParseCards("AsKs"), Board: poker.MustParseCards("2s7s9h9c3d"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if features.Category != poker.OnePair || features.Class != ClassAir || !features.RiverCardFeaturesAvailable || !features.PairFromBoardOnly || !features.MissedFlushDraw {
		t.Fatalf("features=%+v", features)
	}

	features, err = buildFeatures(Request{
		Game: equity.GameNLH, Street: River,
		Hero: poker.MustParseCards("AhKd"), Board: poker.MustParseCards("2s7s9h9c3d"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !features.PairFromBoardOnly || features.MissedFlushDraw {
		t.Fatalf("non-flush-draw features=%+v", features)
	}

	features, err = buildFeatures(Request{
		Game: equity.GameNLH, Street: River,
		Hero: poker.MustParseCards("9sKs"), Board: poker.MustParseCards("2s7s9h9c3d"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if features.PairFromBoardOnly || features.Category != poker.ThreeOfAKind {
		t.Fatalf("hero-improved features=%+v", features)
	}
}

func TestFeaturesIdentifyPocketPairUnderBoard(t *testing.T) {
	features, err := buildFeatures(Request{
		Game: equity.GameNLH, Street: River,
		Hero: poker.MustParseCards("4s4h"), Board: poker.MustParseCards("6h2cJdKsQh"),
	})
	if err != nil || features.Class != ClassMade || !features.PocketPairUnderBoard {
		t.Fatalf("underpair features=%+v err=%v", features, err)
	}

	features, err = buildFeatures(Request{
		Game: equity.GameNLH, Street: Turn,
		Hero: poker.MustParseCards("QsQh"), Board: poker.MustParseCards("6h2cJd9s"),
	})
	if err != nil || features.PocketPairUnderBoard {
		t.Fatalf("overpair features=%+v err=%v", features, err)
	}
}

func TestFeaturesTreatBoardPairWithoutHoleCardAsAir(t *testing.T) {
	features, err := buildFeatures(Request{
		Game: equity.GameNLH, Street: Flop,
		Hero: poker.MustParseCards("5cKs"), Board: poker.MustParseCards("QsQd7s"),
	})
	if err != nil || features.Class != ClassAir || !features.PairFromBoardOnly {
		t.Fatalf("board-only pair features=%+v err=%v", features, err)
	}
}

func TestFeaturesIdentifyOnePairBelowTopBoard(t *testing.T) {
	features, err := buildFeatures(Request{
		Game: equity.GameNLH, Street: Flop,
		Hero: poker.MustParseCards("4cQh"), Board: poker.MustParseCards("AcQd2h"),
	})
	if err != nil || features.Class != ClassMade || !features.OnePairBelowTopBoard {
		t.Fatalf("second-pair features=%+v err=%v", features, err)
	}
}

func TestFeaturesIdentifyRiskyBoardMadeHandsWithoutDowngradingRealFlush(t *testing.T) {
	paired, err := buildFeatures(Request{
		Game: equity.GameNLH, Street: Turn,
		Hero: poker.MustParseCards("KsJd"), Board: poker.MustParseCards("KhTh5d5s"),
	})
	if err != nil || !paired.PairedBoardTwoPair || paired.Class != ClassMade {
		t.Fatalf("paired-board features=%+v err=%v", paired, err)
	}

	boardMade, err := buildFeatures(Request{
		Game: equity.GameNLH, Street: River,
		Hero: poker.MustParseCards("QsTs"), Board: poker.MustParseCards("2c6h2sJdJs"),
	})
	if err != nil || !boardMade.MadeCategoryFromBoard || boardMade.Class != ClassMade {
		t.Fatalf("board-made features=%+v err=%v", boardMade, err)
	}

	flush, err := buildFeatures(Request{
		Game: equity.GameNLH, Street: River,
		Hero: poker.MustParseCards("AhKc"), Board: poker.MustParseCards("2h4h6h8hTh"),
	})
	if err != nil || flush.MadeCategoryFromBoard || flush.Class != ClassMadeStrong || flush.Category != poker.Flush {
		t.Fatalf("improved-flush features=%+v err=%v", flush, err)
	}

	fourStraight, err := buildFeatures(Request{
		Game: equity.GameNLH, Street: Turn,
		Hero: poker.MustParseCards("KhKc"), Board: poker.MustParseCards("9d7h8s6h"),
	})
	if err != nil || !fourStraight.FourToStraightBoard {
		t.Fatalf("four-straight features=%+v err=%v", fourStraight, err)
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
		action, _, _, _, _ := chooseAggressiveNeverFold(req, Decision{})
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
		action, _, _, _, _ := chooseAggressiveNeverFold(req, Decision{SPR: 10})
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

func TestAggressiveNeverFoldCapsRepeatedRaises(t *testing.T) {
	preflop := Request{
		PlayerID: "bot", TableID: "98987274", HandID: "98", AIProfile: "FPCH_100_50",
		Street: Preflop, Pot: 1335, ToCall: 190, Stack: 5760.06, BigBlind: 20,
		HeroPreflopPFR: true, RaisesFaced: 2, PreflopRaiseProbability: .5,
	}
	action, amount, rule, _, aggressive := chooseAggressiveNeverFold(preflop, Decision{})
	if action != Call || amount != 190 || rule != "SPECIAL_PREFLOP_RERAISE_CAP_CALL" || aggressive {
		t.Fatalf("preflop action=%s amount=%v rule=%s aggressive=%v", action, amount, rule, aggressive)
	}

	// K5o in hand 8 had not raised yet, but was already facing two raises. The
	// profile's PFR must not turn that spot into a cold weak-hand 4-bet.
	preflop.HeroPreflopPFR = false
	preflop.HandID = "8"
	action, amount, rule, _, aggressive = chooseAggressiveNeverFold(preflop, Decision{})
	if action != Call || amount != 190 || rule != "SPECIAL_PREFLOP_RERAISE_CAP_CALL" || aggressive {
		t.Fatalf("cold reraise action=%s amount=%v rule=%s aggressive=%v", action, amount, rule, aggressive)
	}

	postflop := Request{Street: Flop, Pot: 410, ToCall: 170, RaisesFaced: 2, PostflopAggressionChance: 1}
	action, amount, rule, _, aggressive = chooseAggressiveNeverFold(postflop, Decision{SPR: 1.6})
	if action != Call || amount != 170 || rule != "SPECIAL_POSTFLOP_RERAISE_CAP_CALL" || aggressive {
		t.Fatalf("postflop action=%s amount=%v rule=%s aggressive=%v", action, amount, rule, aggressive)
	}
}

func TestAggressiveNeverFoldChecksLowSPRAir(t *testing.T) {
	req := Request{Street: Flop, Pot: 2945, Stack: 1832.18, PostflopAggressionChance: 1}
	action, amount, rule, _, aggressive := chooseAggressiveNeverFold(req, Decision{SPR: .217, Features: Features{Class: ClassAir}})
	if action != Check || amount != 0 || rule != "SPECIAL_POSTFLOP_LOW_SPR_AIR_CHECK" || aggressive {
		t.Fatalf("action=%s amount=%v rule=%s aggressive=%v", action, amount, rule, aggressive)
	}
}

func TestSecondPairCannotReraiseRepeatedFlopAggression(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	req := Request{Street: Flop, Pot: 410, ToCall: 170, Stack: 923.2, EffectiveStack: 683.2, BigBlind: 10, RaisesFaced: 2}
	decision := Decision{
		Equity: equity.Result{Equity: .76}, PotOdds: .293, CallEV: 318, SPR: 1.66,
		Features: Features{Category: poker.OnePair, Class: ClassMade, OnePairBelowTopBoard: true},
	}
	action, _, rule, _, aggressive := choosePostflopAdjusted(req, decision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "POSTFLOP_SECOND_PAIR_RERAISE_FOLD" || aggressive {
		t.Fatalf("action=%s rule=%s aggressive=%v", action, rule, aggressive)
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

func TestRiverNutsFromIncrementalBoardValueBets(t *testing.T) {
	engine := NewEngine(nil)
	decision, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: River, Position: UTG,
		Hero: poker.MustParseCards("Ah6h"), Board: poker.MustParseCards("6s3s7h5h4h"),
		Opponents: [][]poker.Card{{}, {}, {}}, ActiveOpponents: 3,
		Pot: 1003.14, Stack: 580.82, EffectiveStack: 580.82, BigBlind: 10, Level: 4,
		PersonalityID: "tag", DisableHumanization: true, DisableOpponentModel: true, DisableThinkTime: true,
		LegalActions: []LegalAction{{Action: Check}, {Action: Bet, Min: 10, Max: 580.81}, {Action: AllIn, Min: 580.82, Max: 580.82}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action == Check || decision.Action == Fold || decision.Features.Class != ClassMadeStrong || decision.Features.Category != poker.Flush {
		t.Fatalf("nuts river decision=%+v", decision)
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
			{Action: Call, Min: 99.99, Max: 99.99},
			{Action: Bet, Min: 2, Max: 99.99},
			{Action: Raise, Min: 2, Max: 99.99},
			{Action: AllIn, Min: 100, Max: 100},
		},
	}
	guard, err := newActionGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, desired := range []Action{Call, Bet, Raise} {
		action, amount, err := guard.finalize(desired, 99.99, desired != Call)
		if err != nil || action != AllIn || amount != 100 {
			t.Fatalf("dust %s action=%s amount=%v error=%v", desired, action, amount, err)
		}
	}

	request.NearAllInRemainingChips = .009
	guard, err = newActionGuard(request)
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err := guard.finalize(Raise, 99.99, true)
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

func TestLegalGuardCollapsesNeverFoldFallbackCall(t *testing.T) {
	guard, err := newActionGuard(Request{
		ToCall: 99.99, Stack: 100, NeverFold: true,
		CollapseNearAllIn: true, NearAllInRemainingChips: .01,
		LegalActions: []LegalAction{
			{Action: Fold},
			{Action: Call, Min: 99.99, Max: 99.99},
			{Action: AllIn, Min: 100, Max: 100},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	action, amount, err := guard.finalize(Fold, 0, false)
	if err != nil || action != AllIn || amount != 100 {
		t.Fatalf("never-fold dust call action=%s amount=%v error=%v", action, amount, err)
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

func TestPostflopSizingProfileSnapsBetsAndRaisesToAllowedFractions(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	req := Request{Street: Flop, Pot: 100, PostflopSizings: []float64{.33, .66}}
	action, amount, rule, _, _ := choosePostflopAdjusted(req, Decision{Equity: equity.Result{Equity: .80}, Features: Features{Class: ClassMadeStrong}}, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Bet || rule != "POSTFLOP_VALUE_BET" || amount != 66 {
		t.Fatalf("value bet action=%s amount=%v rule=%s", action, amount, rule)
	}
	req.ToCall, req.Stack, req.EffectiveStack = 10, 200, 200
	req.LegalActions = []LegalAction{{Action: Fold}, {Action: Call, Min: 10, Max: 10}, {Action: Raise, Min: 20, Max: 200}}
	action, amount, rule, _, _ = choosePostflopAdjusted(req, Decision{Equity: equity.Result{Equity: .80}, PotOdds: .09, CallEV: 70, SPR: 2, Features: Features{Class: ClassMadeStrong}}, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Raise || rule != "POSTFLOP_VALUE_RAISE" || amount != 76 {
		t.Fatalf("value raise action=%s amount=%v rule=%s", action, amount, rule)
	}
}

func TestPostflopSizingProfileDoesNotChangeUnconfiguredProfiles(t *testing.T) {
	req := Request{Pot: 100}
	if got := postflopAmount(req, .75, false); got != 75 {
		t.Fatalf("base bet amount=%v", got)
	}
	req.ToCall = 10
	if got := postflopAmount(req, 1, true); got != 110 {
		t.Fatalf("base raise amount=%v", got)
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

func TestRateControlledThreeBetRangeIsNarrowerThanPFR(t *testing.T) {
	req := Request{TargetVPIP: .30, TargetPFR: .15, PreflopReraiseRangeFactor: .4, RaisesFaced: 1, ToCall: 50, Pot: 120, BigBlind: 10, Stack: 1000, EffectiveStack: 1000, Seed: 7}
	action, _, rule, tags, _ := chooseRateControlledPreflop(req, .09)
	if action != Call || rule != "PREFLOP_PROFILE_CALL" || !slices.Contains(tags, "vpip_range") {
		t.Fatalf("9%% hand facing raise action=%s rule=%s tags=%v", action, rule, tags)
	}
	action, _, rule, tags, _ = chooseRateControlledPreflop(req, .05)
	if action != Raise || rule != "PREFLOP_PROFILE_RERAISE" || !slices.Contains(tags, "pfr_range") {
		t.Fatalf("5%% hand facing raise action=%s rule=%s tags=%v", action, rule, tags)
	}
}

func TestPreflopSizingVariesByDecisionSeed(t *testing.T) {
	req := Request{PlayerID: "bot", TableID: "table", HandID: "1", ToCall: 20, Pot: 80, BigBlind: 10, Seed: 1}
	first := preflopOpenAmount(req)
	req.Seed = 2
	second := preflopOpenAmount(req)
	if first == second || first < 20+25*.85 || first > 20+48*1.15 || second < 20+25*.85 || second > 20+48*1.15 {
		t.Fatalf("open sizing first=%v second=%v", first, second)
	}
}

func TestRateControlledLargePreflopCallUsesNarrowedContinuationRange(t *testing.T) {
	balanced, _ := personality.Resolve("balanced")
	base := Request{
		Game: equity.GameNLH, Position: MP, TargetVPIP: .39, TargetPFR: .14,
		HeroPreflopVPIP: true, ToCall: 180.65, Pot: 370.65, Stack: 6292.94,
		EffectiveStack: 3164.19, BigBlind: 10, RaisesFaced: 2, PreflopLargeCallBB: 10,
	}

	weak := base
	weak.Hero = poker.MustParseCards("Qs6s")
	action, _, rule, tags, _ := choosePreflopAdjusted(weak, .540725, .3276, 3, balanced, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "PREFLOP_PROFILE_LARGE_CALL_FOLD" || !slices.Contains(tags, "large_preflop_call") {
		t.Fatalf("weak large-call decision action=%s rule=%s tags=%v", action, rule, tags)
	}

	// The guard must also run on the hero's first voluntary decision. Previously
	// this path returned from profile VPIP selection before checking call size.
	firstDecision := weak
	firstDecision.HeroPreflopVPIP = false
	action, _, rule, _, _ = choosePreflopAdjusted(firstDecision, .540725, .3276, 3, balanced, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "PREFLOP_PROFILE_LARGE_CALL_FOLD" {
		t.Fatalf("first-decision large call action=%s rule=%s", action, rule)
	}

	strong := base
	strong.Hero = poker.MustParseCards("KsQh")
	action, _, rule, _, _ = choosePreflopAdjusted(strong, .618925, .20, 5, balanced, OpponentRead{BluffMultiplier: 1})
	if action == Fold || rule == "PREFLOP_PROFILE_LARGE_CALL_FOLD" {
		t.Fatalf("strong large-call decision action=%s rule=%s", action, rule)
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

func TestWeakDrawAndBoardPairGuardsRejectHand61Line(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	turnReq := Request{
		Street: Turn, Pot: 442, ToCall: 177, Stack: 577.41, EffectiveStack: 163, BigBlind: 10,
		TurnWeakDrawCallMargin: .18,
	}
	turnDecision := Decision{
		Equity: equity.Result{Equity: .4378}, PotOdds: .2859450726978998, CallEV: 93.9982,
		Features: Features{Class: ClassDraw, StraightDraw: true, DrawOuts: 4},
	}
	action, _, rule, tags, _ := choosePostflopAdjusted(turnReq, turnDecision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "TURN_WEAK_DRAW_FOLD" || !slices.Contains(tags, "clean_outs_guard") {
		t.Fatalf("turn action=%s rule=%s tags=%v", action, rule, tags)
	}

	riverReq := Request{
		Street: River, Pot: 782, ToCall: 163, Stack: 400.41, EffectiveStack: 163, BigBlind: 10,
		HeroPostflopCalls: 1, RepeatedAirCallPenalty: .08,
		RiverBoardPairCallMargin: .15, RiverMissedDrawMargin: .08,
	}
	riverDecision := Decision{
		Equity: equity.Result{Equity: .448989898989899}, PotOdds: .1724867724867725, CallEV: 261.29545454545456,
		Features: Features{Category: poker.OnePair, Class: ClassMade, PairFromBoardOnly: true, MissedStraightDraw: true},
	}
	action, _, rule, tags, _ = choosePostflopAdjusted(riverReq, riverDecision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "RIVER_BOARD_PAIR_BLUFF_CATCH_FOLD" || !slices.Contains(tags, "missed_draw") {
		t.Fatalf("river action=%s rule=%s tags=%v", action, rule, tags)
	}
}

func TestUnderpairGuardRejectsHand8TurnAndRiverCalls(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	baseFeatures := Features{Category: poker.OnePair, Class: ClassMade, PocketPairUnderBoard: true}

	turnReq := Request{Street: Turn, Pot: 315, ToCall: 140, BigBlind: 10, UnderpairCallMargin: .25}
	turnDecision := Decision{
		Equity: equity.Result{Equity: .5378}, PotOdds: .3076923076923077, CallEV: 104.699,
		Features: baseFeatures,
	}
	action, _, rule, tags, _ := choosePostflopAdjusted(turnReq, turnDecision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "POSTFLOP_UNDERPAIR_FOLD" || !slices.Contains(tags, "pocket_pair_under_board") {
		t.Fatalf("turn action=%s rule=%s tags=%v", action, rule, tags)
	}

	riverReq := Request{
		Street: River, Pot: 805, ToCall: 350, BigBlind: 10,
		HeroPostflopCalls: 1, UnderpairCallMargin: .25, RepeatedAirCallPenalty: .08,
	}
	riverDecision := Decision{
		Equity: equity.Result{Equity: .46111111111111114}, PotOdds: .30303030303030304, CallEV: 182.83333333333337,
		Features: baseFeatures,
	}
	action, _, rule, tags, _ = choosePostflopAdjusted(riverReq, riverDecision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Fold || rule != "POSTFLOP_UNDERPAIR_FOLD" || !slices.Contains(tags, "bluff_catch_guard") {
		t.Fatalf("river action=%s rule=%s tags=%v", action, rule, tags)
	}

	strong := riverDecision
	strong.Features = Features{Category: poker.TwoPair, Class: ClassMadeStrong, PocketPairUnderBoard: true}
	action, _, rule, _, _ = choosePostflopAdjusted(riverReq, strong, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action == Fold || rule == "POSTFLOP_UNDERPAIR_FOLD" {
		t.Fatalf("strong made hand action=%s rule=%s", action, rule)
	}
}

func TestWeakDrawGuardDoesNotPenalizeEightOutDraw(t *testing.T) {
	profile, _ := personality.Resolve("balanced")
	req := Request{Street: Turn, Pot: 100, ToCall: 25, TurnWeakDrawCallMargin: .18}
	decision := Decision{Equity: equity.Result{Equity: .35}, PotOdds: .20, CallEV: 10, Features: Features{Class: ClassDraw, DrawOuts: 8}}
	action, _, _, _, _ := choosePostflopAdjusted(req, decision, 5, profile, OpponentRead{BluffMultiplier: 1})
	if action != Call {
		t.Fatalf("action=%s, want call", action)
	}
}

func TestReportedCappedPairsUsePotControlAndFoldToPressure(t *testing.T) {
	engine := NewEngine(nil)
	tests := []struct {
		name               string
		hero               string
		board              string
		want               string
		previousAggression int
	}{
		{name: "hand50 second pair below king", hero: "JsQd", board: "4c3sKsJd5h", want: "POSTFLOP_CAPPED_PAIR_POT_CONTROL"},
		{name: "hand54 queens below ace", hero: "QcQh", board: "6cAsTd9d3h", want: "POSTFLOP_CAPPED_PAIR_POT_CONTROL"},
		{name: "hand90 paired board two pair", hero: "KsJd", board: "KhTh5d5sTc", want: "POSTFLOP_PAIRED_BOARD_POT_CONTROL", previousAggression: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision, err := engine.Decide(context.Background(), Request{
				Game: equity.GameNLH, Street: River, Position: BTN,
				Hero: poker.MustParseCards(test.hero), Board: poker.MustParseCards(test.board), Opponents: [][]poker.Card{{}},
				Pot: 100, Stack: 200, EffectiveStack: 200, BigBlind: 2, Level: 5,
				WasPreflopAggressor: true, DisableHumanization: true,
				HeroPostflopAggro: test.previousAggression,
				LegalActions:      []LegalAction{{Action: Check}, {Action: Bet, Min: 20, Max: 200}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if decision.Action != Check || decision.RuleID != test.want {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}

	pressure, err := engine.Decide(context.Background(), Request{
		Game: equity.GameNLH, Street: Turn, Position: BB,
		Hero: poker.MustParseCards("QcQh"), Board: poker.MustParseCards("6cAsTd9d"), Opponents: [][]poker.Card{{}, {}},
		Pot: 105.3, ToCall: 26.4, Stack: 141.3, EffectiveStack: 141.3, BigBlind: 1.2, Level: 5,
		ActiveOpponents: 2, DisableHumanization: true,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 26.4, Max: 26.4}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pressure.Action != Fold || pressure.RuleID != "POSTFLOP_UNDERPAIR_PRESSURE_FOLD" {
		t.Fatalf("pressure decision=%+v", pressure)
	}
}

func TestFourStraightBoardRejectsWeakMadeHandBarrelsAndCalls(t *testing.T) {
	engine := NewEngine(nil)
	turnReq := Request{
		Game: equity.GameNLH, Street: Turn, Position: BTN,
		Hero: poker.MustParseCards("KhKc"), Board: poker.MustParseCards("9d7h8s6h"), Opponents: [][]poker.Card{{}},
		Pot: 27.3, Stack: 92.95, EffectiveStack: 92.95, BigBlind: 1.2, Level: 5,
		WasPreflopAggressor: true, DisableHumanization: true,
		LegalActions: []LegalAction{{Action: Check}, {Action: Bet, Min: 2.4, Max: 92.95}},
	}
	turn, err := engine.Decide(context.Background(), turnReq)
	if err != nil {
		t.Fatal(err)
	}
	if turn.Action != Check || turn.RuleID != "POSTFLOP_FOUR_STRAIGHT_POT_CONTROL" || !turn.Features.FourToStraightBoard {
		t.Fatalf("turn decision=%+v", turn)
	}

	turnReq.ToCall, turnReq.Pot, turnReq.Stack, turnReq.EffectiveStack, turnReq.RaisesFaced = 54.9, 109.8, 79.15, 79.15, 1
	turnReq.LegalActions = []LegalAction{{Action: Fold}, {Action: Call, Min: 54.9, Max: 54.9}}
	decision, err := engine.Decide(context.Background(), turnReq)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != Fold || decision.RuleID != "POSTFLOP_FOUR_STRAIGHT_FOLD" {
		t.Fatalf("raised decision=%+v", decision)
	}

	hand70 := turnReq
	hand70.Hero = poker.MustParseCards("Kd7d")
	hand70.Board = poker.MustParseCards("9dKhJcQh")
	hand70.Opponents = [][]poker.Card{{}, {}, {}}
	hand70.Pot, hand70.ToCall, hand70.Stack, hand70.EffectiveStack = 232.2, 77.4, 478.35, 326.05
	hand70.RaisesFaced, hand70.ActiveOpponents = 0, 3
	hand70.LegalActions = []LegalAction{{Action: Fold}, {Action: Call, Min: 77.4, Max: 77.4}}
	decision, err = engine.Decide(context.Background(), hand70)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != Fold || decision.RuleID != "POSTFLOP_FOUR_STRAIGHT_FOLD" {
		t.Fatalf("hand70 decision=%+v", decision)
	}
}

func TestThreeFlushBoardControlsOnePair(t *testing.T) {
	engine := NewEngine(nil)
	base := Request{
		Game: equity.GameNLH, Street: Turn, Position: BTN,
		Hero: poker.MustParseCards("KcTc"), Board: poker.MustParseCards("6d2h8dKd"), Opponents: [][]poker.Card{{}},
		Pot: 40, Stack: 275.4, EffectiveStack: 275.4, BigBlind: .6, Level: 5,
		DisableHumanization: true,
	}
	checked := base
	checked.LegalActions = []LegalAction{{Action: Check}, {Action: Bet, Min: 1.2, Max: 275.4}}
	decision, err := engine.Decide(context.Background(), checked)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != Check || decision.RuleID != "POSTFLOP_THREE_FLUSH_PAIR_POT_CONTROL" || !decision.Features.ThreeToFlushBoard {
		t.Fatalf("checked decision=%+v", decision)
	}

	pressured := base
	pressured.Pot, pressured.ToCall, pressured.RaisesFaced = 155.4, 80.4, 1
	pressured.LegalActions = []LegalAction{{Action: Fold}, {Action: Call, Min: 80.4, Max: 80.4}, {Action: Raise, Min: 241.2, Max: 275.4}}
	decision, err = engine.Decide(context.Background(), pressured)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != Fold || decision.RuleID != "POSTFLOP_THREE_FLUSH_PAIR_FOLD" {
		t.Fatalf("pressured decision=%+v", decision)
	}
}

func TestRiverOnePairDoesNotValueReraise(t *testing.T) {
	engine := NewEngine(nil)
	base := Request{
		Game: equity.GameNLH, Street: River, Position: BTN,
		Hero: poker.MustParseCards("As9d"), Board: poker.MustParseCards("2d6h8sAc7c"), Opponents: [][]poker.Card{{}},
		Pot: 150, ToCall: 50, Stack: 760, EffectiveStack: 164, BigBlind: .6, Level: 5,
		DisableHumanization: true,
		LegalActions:        []LegalAction{{Action: Fold}, {Action: Call, Min: 50, Max: 50}, {Action: Raise, Min: 150, Max: 760}, {Action: AllIn, Min: 760, Max: 760}},
	}
	decision, err := engine.Decide(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != Call || decision.RuleID != "POSTFLOP_CALL" {
		t.Fatalf("single bet decision=%+v", decision)
	}

	base.RaisesFaced = 1
	decision, err = engine.Decide(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Action != Fold || decision.RuleID != "RIVER_ONE_PAIR_RERAISE_FOLD" {
		t.Fatalf("raised decision=%+v", decision)
	}
}

func TestSpecialProfileDoesNotTripleBarrelAir(t *testing.T) {
	req := Request{
		PlayerID: "97687418", TableID: "93326552", HandID: "116", AIProfile: "FPCH_100_50",
		Street: Turn, Pot: 15.6, Stack: 164.4, PostflopAggressionChance: 1,
		HeroPostflopAggro: 1,
	}
	action, amount, rule, tags, aggressive := chooseAggressiveNeverFold(req, Decision{SPR: 10, Features: Features{Class: ClassAir}})
	if action != Check || amount != 0 || rule != "SPECIAL_POSTFLOP_REPEAT_WEAK_BARREL_CHECK" || aggressive || !slices.Contains(tags, "repeat_weak_barrel") {
		t.Fatalf("action=%s amount=%v rule=%s tags=%v aggressive=%v", action, amount, rule, tags, aggressive)
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

func TestWeakDrawAndBoardPairFoldsAreProtectedFromHumanization(t *testing.T) {
	for _, ruleID := range []string{"TURN_WEAK_DRAW_FOLD", "POSTFLOP_UNDERPAIR_FOLD", "POSTFLOP_UNDERPAIR_PRESSURE_FOLD", "POSTFLOP_CAPPED_PAIR_FOLD", "POSTFLOP_FOUR_STRAIGHT_FOLD", "POSTFLOP_PAIRED_BOARD_RAISE_FOLD", "RIVER_BOARD_PAIR_BLUFF_CATCH_FOLD"} {
		if !isProtectedPostflopFold(ruleID) {
			t.Fatalf("rule %s must be protected from humanization", ruleID)
		}
	}
}

func TestHand61As4sCallDownIsRejectedEndToEnd(t *testing.T) {
	engine := NewEngine(nil)
	base := Request{
		Game: equity.GameNLH, Position: BB,
		Hero: poker.MustParseCards("As4s"), Opponents: [][]poker.Card{{}},
		Stack: 577.41, EffectiveStack: 163, BigBlind: 10, Level: 5, Seed: 61,
		TurnWeakDrawCallMargin: .18, RiverBoardPairCallMargin: .15,
		RiverMissedDrawMargin: .08, RepeatedAirCallPenalty: .04,
		LegalActions: []LegalAction{{Action: Fold}, {Action: Call, Min: 1, Max: 1000}},
	}

	turn := base
	turn.Street = Turn
	turn.Board = poker.MustParseCards("Kh3dQcJs")
	turn.Pot = 442
	turn.ToCall = 177
	turnDecision, err := engine.Decide(context.Background(), turn)
	if err != nil {
		t.Fatal(err)
	}
	if turnDecision.Action != Fold || turnDecision.RuleID != "TURN_WEAK_DRAW_FOLD" || turnDecision.Humanized {
		t.Fatalf("turn decision=%+v", turnDecision)
	}

	river := base
	river.Street = River
	river.Board = poker.MustParseCards("Kh3dQcJsKs")
	river.Pot = 782
	river.ToCall = 163
	river.HeroPostflopCalls = 1
	riverDecision, err := engine.Decide(context.Background(), river)
	if err != nil {
		t.Fatal(err)
	}
	if riverDecision.Action != Fold || riverDecision.RuleID != "RIVER_BOARD_PAIR_BLUFF_CATCH_FOLD" || riverDecision.Humanized {
		t.Fatalf("river decision=%+v", riverDecision)
	}
	if !riverDecision.Features.PairFromBoardOnly || !riverDecision.Features.MissedStraightDraw {
		t.Fatalf("river features=%+v", riverDecision.Features)
	}
}
