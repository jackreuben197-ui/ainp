package strategy

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math"
	"math/rand"
	"time"

	"gitlab.com/smoothsics/ainp/internal/equity"
	"gitlab.com/smoothsics/ainp/internal/personality"
)

var ErrInvalidStrategyInput = errors.New("invalid strategy input")

type Engine struct {
	equity *equity.Calculator
	logger *slog.Logger
}

type Option func(*Engine)

func WithLogger(logger *slog.Logger) Option {
	return func(engine *Engine) { engine.logger = logger }
}

func NewEngine(calculator *equity.Calculator, options ...Option) *Engine {
	if calculator == nil {
		calculator = equity.NewCalculator()
	}
	engine := &Engine{equity: calculator}
	for _, option := range options {
		option(engine)
	}
	return engine
}

func (e *Engine) Decide(ctx context.Context, req Request) (Decision, error) {
	started := time.Now()
	if math.Abs(req.ToCall) <= chipEpsilon {
		req.ToCall = 0
	}
	if req.EffectiveStack == 0 {
		req.EffectiveStack = req.Stack
	}
	if req.ToCall > req.Stack {
		req.ToCall = req.Stack
	}
	if err := validateStrategyRequest(req); err != nil {
		return Decision{}, err
	}
	profile := personality.Neutral()
	if !req.DisablePersonality {
		resolved, resolveErr := personality.Resolve(req.PersonalityID)
		if resolveErr != nil {
			return Decision{}, fmt.Errorf("resolve personality: %w", resolveErr)
		}
		profile = resolved
	}
	guard, err := newActionGuard(req)
	if err != nil {
		return Decision{}, err
	}
	features, err := buildFeatures(req)
	if err != nil {
		return Decision{}, fmt.Errorf("build features: %w", err)
	}
	equityResult, err := e.equity.Calculate(ctx, equity.Request{
		Game: req.Game, Hero: req.Hero, Board: req.Board, Opponents: req.Opponents, Dead: req.Dead,
		Samples: req.EquitySamples, Seed: req.Seed, MaxExactOutcomes: req.MaxExactOutcomes,
	})
	if err != nil {
		return Decision{}, fmt.Errorf("calculate equity: %w", err)
	}

	read := OpponentRead{BluffMultiplier: 1}
	if !req.DisableOpponentModel {
		read = analyzeOpponents(req)
	}
	decision := Decision{Equity: equityResult, Features: features, PersonalityID: profile.ID, OpponentRead: read}
	if req.ToCall > 0 {
		decision.PotOdds = req.ToCall / (req.Pot + req.ToCall)
	}
	decision.CallEV = equityResult.Equity*(req.Pot+req.ToCall) - req.ToCall
	if req.Pot > 0 {
		decision.SPR = req.EffectiveStack / req.Pot
	}

	desired, amount, ruleID, tags, aggressive := chooseRule(req, decision, profile)
	humanSeed := req.Seed
	if humanSeed == 0 {
		humanSeed = equityResult.Seed
	}
	if humanSeed == 0 {
		humanSeed = stableHumanSeed(req)
	}
	rateControlledPreflop := req.Street == Preflop && req.Game == equity.GameNLH && req.TargetVPIP > 0
	specialBehavior := req.BehaviorMode == "aggressive_never_fold"
	protectedFold := desired == Fold && isProtectedPostflopFold(ruleID)
	if !req.DisableHumanization && !rateControlledPreflop && !protectedFold && !specialBehavior {
		desired, amount, tags, decision.Humanized = applyHumanization(req, decision, profile, humanSeed, desired, amount, tags)
	}
	action, finalAmount, err := guard.finalize(desired, amount, aggressive)
	if err != nil {
		return Decision{}, err
	}
	decision.Action = action
	decision.Amount = finalAmount
	decision.RuleID = ruleID
	decision.Tags = append(tags, "personality:"+profile.ID, "legal_guard_passed")
	complexity := decisionComplexity(req, action)
	if !req.DisableThinkTime {
		decision.ThinkTime = personality.ThinkTime(profile, humanSeed+17, complexity)
	}
	e.logDecision(req, decision, time.Since(started))
	return decision, nil
}

func isProtectedPostflopFold(ruleID string) bool {
	switch ruleID {
	case "POSTFLOP_NEGATIVE_EV_FOLD",
		"POSTFLOP_AIR_MARGIN_FOLD",
		"TURN_WEAK_DRAW_FOLD",
		"POSTFLOP_UNDERPAIR_FOLD",
		"RIVER_BOARD_PAIR_BLUFF_CATCH_FOLD":
		return true
	default:
		return false
	}
}

func stableHumanSeed(req Request) int64 {
	identity := req.DecisionID
	if identity == "" {
		identity = req.RequestID
	}
	if identity == "" {
		return 1
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(identity))
	return int64(hash.Sum64() & math.MaxInt64)
}

func (e *Engine) logDecision(req Request, decision Decision, elapsed time.Duration) {
	if e.logger == nil {
		return
	}
	heroHandClass := ""
	if req.Game == equity.GameNLH {
		heroHandClass, _ = equity.StartingHandClass(req.Hero)
	}
	preflopClass := ""
	if req.Street == Preflop {
		preflopClass = heroHandClass
	}
	activeOpponents := req.ActiveOpponents
	if activeOpponents == 0 {
		activeOpponents = len(req.Opponents)
	}
	e.logger.Info("strategy_decision",
		"request_id", req.RequestID,
		"decision_id", req.DecisionID,
		"policy_version", req.PolicyVersion,
		"ai_profile", req.AIProfile,
		"profile_source", req.ProfileSource,
		"strategy_level", req.Level,
		"target_vpip", req.TargetVPIP,
		"target_pfr", req.TargetPFR,
		"postflop_sizings", req.PostflopSizings,
		"behavior_mode", req.BehaviorMode,
		"audit_exempt", req.AuditExempt,
		"postflop_call_margin", req.PostflopCallMargin,
		"large_pot_threshold_bb", req.LargePotThresholdBB,
		"large_pot_min_equity", req.LargePotMinEquity,
		"player_id", req.PlayerID,
		"table_id", req.TableID,
		"hand_id", req.HandID,
		"game", req.Game,
		"street", req.Street,
		"position", req.Position,
		"preflop_hand_class", preflopClass,
		"hero_hand_class", heroHandClass,
		"active_opponents", activeOpponents,
		"raises_faced", req.RaisesFaced,
		"preflop_large_call_outside_range", isPreflopLargeCallOutsideRange(req),
		"personality_id", decision.PersonalityID,
		"opponent_archetypes", decision.OpponentRead.Archetypes,
		"opponent_hands", decision.OpponentRead.ObservedHands,
		"pot", req.Pot,
		"to_call", req.ToCall,
		"effective_stack", req.EffectiveStack,
		"equity", decision.Equity.Equity,
		"equity_method", decision.Equity.Method,
		"equity_trials", decision.Equity.Trials,
		"hand_category", decision.Features.Category,
		"hand_class", decision.Features.Class,
		"river_card_features_available", decision.Features.RiverCardFeaturesAvailable,
		"pair_from_board_only", decision.Features.PairFromBoardOnly,
		"pocket_pair_under_board", decision.Features.PocketPairUnderBoard,
		"one_pair_below_top_board", decision.Features.OnePairBelowTopBoard,
		"missed_flush_draw", decision.Features.MissedFlushDraw,
		"missed_straight_draw", decision.Features.MissedStraightDraw,
		"action", decision.Action,
		"amount", decision.Amount,
		"rule_id", decision.RuleID,
		"tags", decision.Tags,
		"pot_odds", decision.PotOdds,
		"call_ev", decision.CallEV,
		"spr", decision.SPR,
		"hero_postflop_calls", req.HeroPostflopCalls,
		"humanized", decision.Humanized,
		"think_time_ms", decision.ThinkTime.Milliseconds(),
		"latency_us", elapsed.Microseconds(),
	)
}

func chooseRule(req Request, decision Decision, profile personality.Profile) (Action, float64, string, []string, bool) {
	level := req.Level
	if level == 0 {
		level = 3
	}
	eq := decision.Equity.Equity
	if req.BehaviorMode == "aggressive_never_fold" {
		return chooseAggressiveNeverFold(req, decision)
	}
	if req.Street == Preflop {
		return choosePreflopAdjusted(req, eq, decision.PotOdds, level, profile, decision.OpponentRead)
	}
	return choosePostflopAdjusted(req, decision, level, profile, decision.OpponentRead)
}

func chooseAggressiveNeverFold(req Request, decision Decision) (Action, float64, string, []string, bool) {
	tags := []string{"special_profile", "aggressive_never_fold", "ignore_equity_ev"}
	if req.Street == Preflop {
		// The configured PFR controls the opening raise frequency; it is not a
		// weak-hand 3-bet/4-bet range. Once a raise has been faced (or this hero
		// has already raised), continue passively instead of starting a raise war.
		if req.HeroPreflopPFR || req.RaisesFaced > 0 {
			if req.ToCall > 0 {
				return Call, req.ToCall, "SPECIAL_PREFLOP_RERAISE_CAP_CALL", append(tags, "no_preflop_reraise"), false
			}
			return Check, 0, "SPECIAL_PREFLOP_RERAISE_CAP_CHECK", append(tags, "no_preflop_reraise"), false
		}
		if probabilityRoll(stableSpecialBehaviorSeed(req), 101) < req.PreflopRaiseProbability {
			base := math.Max(3*req.BigBlind, req.Pot)
			amount := req.ToCall + base*preflopSizingFactor(req, 197, .85, 1.15)
			return Raise, amount, "SPECIAL_PREFLOP_RAISE", append(tags, "probability_raise"), true
		}
		if req.ToCall > 0 {
			return Call, req.ToCall, "SPECIAL_PREFLOP_CALL", append(tags, "always_continue"), false
		}
		return Check, 0, "SPECIAL_PREFLOP_CHECK", append(tags, "free_option"), false
	}
	// A paired board that does not use either hole card is not a made hand.
	// At low SPR, a pot-sized stab would be converted into an artificial air
	// shove, which was the K5o failure in table 98987274 hand 8.
	if req.ToCall == 0 && decision.Features.Class == ClassAir && decision.SPR <= 1 {
		return Check, 0, "SPECIAL_POSTFLOP_LOW_SPR_AIR_CHECK", append(tags, "low_spr", "air", "pot_control"), false
	}
	// One aggressive response per street is sufficient for this profile. A
	// subsequent reraise is continued passively, avoiding deterministic 5-bet
	// loops caused by using the same per-hand probability roll repeatedly.
	if req.ToCall > 0 && req.RaisesFaced > 0 {
		return Call, req.ToCall, "SPECIAL_POSTFLOP_RERAISE_CAP_CALL", append(tags, "one_raise_per_street"), false
	}
	if probabilityRoll(stableSpecialBehaviorSeed(req), 211+int64(len(req.Board))) < req.PostflopAggressionChance {
		if req.ToCall > 0 {
			return Raise, postflopAmount(req, .5, true), "SPECIAL_POSTFLOP_RAISE", append(tags, "probability_aggression", "bounded_sizing"), true
		}
		return Bet, postflopAmount(req, .5, false), "SPECIAL_POSTFLOP_BET", append(tags, "probability_aggression", "bounded_sizing"), true
	}
	if req.ToCall > 0 {
		return Call, req.ToCall, "SPECIAL_POSTFLOP_CALL", append(tags, "always_continue"), false
	}
	return Check, 0, "SPECIAL_POSTFLOP_CHECK", append(tags, "free_option"), false
}

func stableSpecialBehaviorSeed(req Request) int64 {
	h := fnv.New64a()
	_, _ = fmt.Fprintf(h, "%s|%s|%s|%s", req.PlayerID, req.TableID, req.HandID, req.AIProfile)
	return int64(h.Sum64())
}

func probabilityRoll(seed, salt int64) float64 {
	return rand.New(rand.NewSource(seed ^ (salt * 0x5DEECE66D))).Float64()
}

func choosePreflop(req Request, eq, potOdds float64, level int) (Action, float64, string, []string, bool) {
	profile, _ := personality.Resolve("balanced")
	return choosePreflopAdjusted(req, eq, potOdds, level, profile, OpponentRead{BluffMultiplier: 1})
}

func choosePreflopAdjusted(req Request, eq, potOdds float64, level int, profile personality.Profile, read OpponentRead) (Action, float64, string, []string, bool) {
	percentile, rateControlled := 0.0, false
	if req.Game == equity.GameNLH && req.TargetVPIP > 0 {
		if value, err := equity.StartingHandPercentile(req.Hero); err == nil {
			percentile, rateControlled = value, true
			if exceedsLargePreflopCallRange(req, percentile) {
				continuationRange := preflopLargeCallRange(req)
				return Fold, 0, "PREFLOP_PROFILE_LARGE_CALL_FOLD", []string{
					"profile_rate_controlled",
					fmt.Sprintf("range_percentile:%.6f", percentile),
					fmt.Sprintf("large_call_range:%.6f", continuationRange),
					"large_preflop_call",
				}, false
			}
			if !req.HeroPreflopVPIP && !req.HeroPreflopPFR {
				return chooseRateControlledPreflop(req, percentile)
			}
		}
	}
	action, amount, rule, tags, aggressive := chooseLegacyPreflop(req, eq, potOdds, level, profile, read)
	if rateControlled && req.HeroPreflopVPIP && !req.HeroPreflopPFR && percentile > req.TargetPFR && aggressive {
		if req.ToCall > 0 {
			return Call, req.ToCall, "PREFLOP_PROFILE_PFR_CAP", append(tags, "profile_pfr_cap", "continue_without_raise"), false
		}
		return Check, 0, "PREFLOP_PROFILE_PFR_CAP", append(tags, "profile_pfr_cap", "check_without_raise"), false
	}
	return action, amount, rule, tags, aggressive
}

// Large preflop calls must use a range-aware guard. The lookup equity is the
// hero hand's strength against one random hand; it is intentionally useful for
// opening decisions, but it overstates weak hands against large reraises and
// all-ins. Preserve each profile's configured VPIP/PFR shape by narrowing the
// continuation range as the number of raises grows.
func exceedsLargePreflopCallRange(req Request, percentile float64) bool {
	thresholdBB := req.PreflopLargeCallBB
	if thresholdBB == 0 {
		thresholdBB = 10
	}
	return req.BigBlind > 0 && req.ToCall >= thresholdBB*req.BigBlind && req.RaisesFaced > 0 && percentile > preflopLargeCallRange(req)
}

func preflopLargeCallRange(req Request) float64 {
	raises := max(1, req.RaisesFaced)
	return req.TargetPFR + (req.TargetVPIP-req.TargetPFR)/float64(raises)
}

func isPreflopLargeCallOutsideRange(req Request) bool {
	if req.Street != Preflop {
		return false
	}
	percentile, err := equity.StartingHandPercentile(req.Hero)
	return err == nil && exceedsLargePreflopCallRange(req, percentile)
}

func chooseLegacyPreflop(req Request, eq, potOdds float64, level int, profile personality.Profile, read OpponentRead) (Action, float64, string, []string, bool) {
	openThresholds := map[Position]float64{UTG: .61, MP: .58, CO: .55, BTN: .52, SB: .56, BB: .58}
	threshold := openThresholds[req.Position]
	if threshold == 0 {
		threshold = .58
	}
	fieldSize := req.ActiveOpponents
	if fieldSize == 0 {
		fieldSize = len(req.Opponents)
	}
	multiwayPenalty := req.PreflopMultiwayPenalty
	if multiwayPenalty == 0 {
		multiwayPenalty = .005
	}
	fieldPenalty := math.Min(.03, float64(max(0, fieldSize-1))*multiwayPenalty)
	threshold += profile.OpenThresholdDelta - float64(level-3)*.01 + fieldPenalty
	if req.ToCall == 0 {
		if eq >= threshold {
			return Raise, preflopOpenAmount(req), "PREFLOP_OPEN", []string{"position_range", "value_raise"}, true
		}
		return Check, 0, "PREFLOP_CHECK", []string{"below_open_threshold"}, false
	}

	openCallGap := req.PreflopOpenCallGap
	if openCallGap == 0 {
		openCallGap = .06
	}
	callMargin := req.PreflopCallMargin
	if callMargin == 0 {
		callMargin = .035
	}
	callThreshold := math.Max(potOdds+callMargin+profile.CallMarginDelta+read.CallMargin, threshold-openCallGap)
	if req.RaisesFaced == 0 {
		if eq >= threshold {
			return Raise, preflopOpenAmount(req), "PREFLOP_OPEN", []string{"position_range", "open_raise"}, true
		}
		if eq >= callThreshold {
			return Call, req.ToCall, "PREFLOP_LIMP_CALL", []string{"priced_entry", "below_raise_threshold"}, false
		}
		return Fold, 0, "PREFLOP_FOLD", []string{"strength_below_entry_range"}, false
	}
	extraRaisePenalty := req.PreflopExtraRaisePenalty
	if extraRaisePenalty == 0 {
		extraRaisePenalty = .025
	}
	callThreshold += math.Min(.06, float64(max(0, req.RaisesFaced-1))*.015)
	if eq < callThreshold {
		return Fold, 0, "PREFLOP_FOLD", []string{"strength_below_call_range"}, false
	}
	reraiseThreshold := req.PreflopReraiseEquity
	if reraiseThreshold == 0 {
		reraiseThreshold = .76
	}
	reraiseThreshold += profile.AggressionDelta - float64(level-3)*.01 + fieldPenalty + float64(max(0, req.RaisesFaced-1))*extraRaisePenalty
	if eq >= reraiseThreshold {
		if req.EffectiveStack <= req.Pot+2*req.ToCall {
			return AllIn, req.Stack, "PREFLOP_ALLIN_VALUE", []string{"premium", "low_spr"}, true
		}
		return Raise, preflopReraiseAmount(req), "PREFLOP_RERAISE", []string{"premium", "value_reraise"}, true
	}
	return Call, req.ToCall, "PREFLOP_CALL", []string{"priced_call", "continue_vs_raise"}, false
}

func chooseRateControlledPreflop(req Request, percentile float64) (Action, float64, string, []string, bool) {
	tags := []string{"profile_rate_controlled", fmt.Sprintf("range_percentile:%.6f", percentile)}
	aggressionRange := req.TargetPFR
	if req.RaisesFaced > 0 {
		factor := req.PreflopReraiseRangeFactor
		if factor == 0 {
			factor = .4
		}
		aggressionRange *= factor
		tags = append(tags, fmt.Sprintf("three_bet_range:%.6f", aggressionRange))
	}
	if req.ToCall == 0 {
		if percentile <= aggressionRange {
			return Raise, preflopOpenAmount(req), "PREFLOP_PROFILE_RAISE", append(tags, "pfr_range"), true
		}
		return Check, 0, "PREFLOP_PROFILE_CHECK", append(tags, "no_vpip_opportunity"), false
	}
	if percentile <= aggressionRange {
		if req.RaisesFaced == 0 {
			return Raise, preflopOpenAmount(req), "PREFLOP_PROFILE_RAISE", append(tags, "pfr_range"), true
		}
		if req.EffectiveStack <= req.Pot+2*req.ToCall {
			return AllIn, req.Stack, "PREFLOP_PROFILE_ALLIN", append(tags, "pfr_range", "low_spr"), true
		}
		return Raise, preflopReraiseAmount(req), "PREFLOP_PROFILE_RERAISE", append(tags, "pfr_range"), true
	}
	if percentile <= req.TargetVPIP {
		return Call, req.ToCall, "PREFLOP_PROFILE_CALL", append(tags, "vpip_range"), false
	}
	return Fold, 0, "PREFLOP_PROFILE_FOLD", append(tags, "outside_vpip_range"), false
}

func preflopOpenAmount(req Request) float64 {
	base := math.Max(2.5*req.BigBlind, .6*req.Pot)
	return req.ToCall + base*preflopSizingFactor(req, 211, .85, 1.15)
}

func preflopReraiseAmount(req Request) float64 {
	base := math.Max(2*req.ToCall, .75*req.Pot)
	return req.ToCall + base*preflopSizingFactor(req, 223, .85, 1.15)
}

func preflopSizingFactor(req Request, salt int64, minimum, maximum float64) float64 {
	seed := req.Seed
	if seed == 0 {
		seed = stableSpecialBehaviorSeed(req)
	}
	return minimum + (maximum-minimum)*probabilityRoll(seed, salt)
}

func choosePostflop(req Request, decision Decision, level int) (Action, float64, string, []string, bool) {
	profile, _ := personality.Resolve("balanced")
	return choosePostflopAdjusted(req, decision, level, profile, OpponentRead{BluffMultiplier: 1})
}

func choosePostflopAdjusted(req Request, decision Decision, level int, profile personality.Profile, read OpponentRead) (Action, float64, string, []string, bool) {
	eq := decision.Equity.Equity
	class := decision.Features.Class
	largePot := req.LargePotThresholdBB > 0 && req.BigBlind > 0 && req.Pot >= req.LargePotThresholdBB*req.BigBlind
	betMultiplier := profile.BetSizeMultiplier
	if betMultiplier == 0 {
		betMultiplier = 1
	}
	if req.ToCall == 0 {
		if largePot && req.LargePotMinEquity > 0 && eq < req.LargePotMinEquity {
			return Check, 0, "POSTFLOP_LARGE_POT_CONTROL", []string{"large_pot", "equity_guard", "pot_control"}, false
		}
		switch {
		case eq >= .70+profile.ValueThresholdDelta+read.ValueThreshold || class == ClassMadeStrong && eq >= .62+profile.ValueThresholdDelta+read.ValueThreshold:
			return Bet, postflopAmount(req, .75*betMultiplier, false), "POSTFLOP_VALUE_BET", []string{"strong_made", "value"}, true
		case (class == ClassDraw || class == ClassMadeDraw) && eq >= .40+profile.AggressionDelta:
			return Bet, postflopAmount(req, .5*betMultiplier, false), "POSTFLOP_SEMIBLUFF", []string{"draw", "semi_bluff"}, true
		case class == ClassMade && eq >= .52 && req.WasPreflopAggressor:
			return Bet, postflopAmount(req, .5*betMultiplier, false), "POSTFLOP_THIN_VALUE", []string{"made_hand", "initiative"}, true
		case class == ClassAir && req.Street == Flop && req.WasPreflopAggressor && bluffRollAdjusted(req.Seed, level, req.Position, profile.BluffMultiplier*read.BluffMultiplier):
			return Bet, postflopAmount(req, .34*betMultiplier, false), "FLOP_CBET_BLUFF", []string{"air", "cbet", "controlled_bluff"}, true
		default:
			return Check, 0, "POSTFLOP_CHECK", []string{string(class), "pot_control"}, false
		}
	}

	required := decision.PotOdds + .02 + profile.CallMarginDelta + read.CallMargin + req.PostflopCallMargin - float64(level-3)*.005
	if largePot && req.LargePotMinEquity > required {
		required = req.LargePotMinEquity
	}
	if req.RejectNegativeEVCalls && decision.CallEV < -1e-9 {
		return Fold, 0, "POSTFLOP_NEGATIVE_EV_FOLD", []string{string(class), "negative_call_ev", "call_ev_guard"}, false
	}
	if class == ClassAir {
		airMargin := req.FlopAirCallMargin
		switch req.Street {
		case Turn:
			airMargin = req.TurnAirCallMargin
		case River:
			airMargin = req.RiverAirCallMargin
		}
		required += airMargin + float64(req.HeroPostflopCalls)*req.RepeatedAirCallPenalty
	}
	weakTurnDraw := req.Street == Turn && class == ClassDraw && decision.Features.DrawOuts > 0 && decision.Features.DrawOuts <= 4
	if weakTurnDraw {
		required += req.TurnWeakDrawCallMargin
	}
	boardPairBluffCatch := req.Street == River && decision.Features.PairFromBoardOnly
	if boardPairBluffCatch {
		required += req.RiverBoardPairCallMargin + float64(req.HeroPostflopCalls)*req.RepeatedAirCallPenalty
		if decision.Features.MissedFlushDraw || decision.Features.MissedStraightDraw {
			required += req.RiverMissedDrawMargin
		}
	}
	underpairBluffCatch := (req.Street == Turn || req.Street == River) && class == ClassMade && decision.Features.PocketPairUnderBoard
	if underpairBluffCatch {
		required += req.UnderpairCallMargin + float64(req.HeroPostflopCalls)*req.RepeatedAirCallPenalty
	}
	if eq < required {
		rule := "POSTFLOP_FOLD"
		tags := []string{"equity_below_pot_odds", string(class)}
		switch {
		case underpairBluffCatch:
			rule = "POSTFLOP_UNDERPAIR_FOLD"
			tags = append(tags, "pocket_pair_under_board", "bluff_catch_guard")
		case boardPairBluffCatch:
			rule = "RIVER_BOARD_PAIR_BLUFF_CATCH_FOLD"
			tags = append(tags, "pair_from_board_only", "bluff_catch_guard")
			if decision.Features.MissedFlushDraw || decision.Features.MissedStraightDraw {
				tags = append(tags, "missed_draw")
			}
		case weakTurnDraw:
			rule = "TURN_WEAK_DRAW_FOLD"
			tags = append(tags, "weak_draw", "clean_outs_guard")
		case class == ClassAir:
			rule = "POSTFLOP_AIR_MARGIN_FOLD"
			tags = append(tags, "bluff_catch_guard")
		}
		return Fold, 0, rule, tags, false
	}
	if class == ClassMade && decision.Features.OnePairBelowTopBoard && req.RaisesFaced >= 2 {
		return Fold, 0, "POSTFLOP_SECOND_PAIR_RERAISE_FOLD", []string{"one_pair_below_top_board", "repeated_raise", "range_guard"}, false
	}
	// Raise only when both hero and an opponent can cover the increment above a
	// call required by the server's smallest aggressive action. This prevents a
	// nearly all-in opponent from turning low SPR into a pointless oversized
	// shove, without suppressing a normal value raise at equal starting stacks.
	canRaiseForValue := hasCallableRaise(req)
	if canRaiseForValue && (class == ClassDraw || class == ClassMadeDraw) && eq >= .68+profile.AggressionDelta {
		if decision.SPR <= 1.2 {
			return AllIn, req.Stack, "POSTFLOP_DRAW_ALLIN", []string{"draw", "semi_bluff", "low_spr"}, true
		}
		return Raise, postflopAmount(req, betMultiplier, true), "POSTFLOP_DRAW_RAISE", []string{"draw", "semi_bluff"}, true
	}
	if canRaiseForValue && !decision.Features.OnePairBelowTopBoard && (eq >= .74+profile.AggressionDelta || class == ClassMadeStrong && eq >= .50 && eq-decision.PotOdds >= .18+profile.AggressionDelta) {
		if decision.SPR <= 1.2 {
			return AllIn, req.Stack, "POSTFLOP_ALLIN_VALUE", []string{"strong_made", "low_spr"}, true
		}
		return Raise, postflopAmount(req, betMultiplier, true), "POSTFLOP_VALUE_RAISE", []string{"strong_made", "value_raise"}, true
	}
	if class == ClassDraw || class == ClassMadeDraw {
		return Call, req.ToCall, "POSTFLOP_DRAW_CALL", []string{"draw", "priced_call"}, false
	}
	return Call, req.ToCall, "POSTFLOP_CALL", []string{string(class), "equity_call"}, false
}

// postflopAmount snaps the strategy's normal target to the nearest sizing
// explicitly allowed by the current AiProfile. Facing a bet, the selected pot
// fraction controls the raise increment above the call while preserving the
// minimum two-times-to-call value used by the base strategy.
func postflopAmount(req Request, target float64, facingBet bool) float64 {
	fraction := target
	if len(req.PostflopSizings) > 0 {
		fraction = req.PostflopSizings[0]
		bestDistance := math.Abs(fraction - target)
		for _, candidate := range req.PostflopSizings[1:] {
			distance := math.Abs(candidate - target)
			if distance < bestDistance {
				fraction, bestDistance = candidate, distance
			}
		}
	}
	potAmount := fraction * req.Pot
	if facingBet {
		return req.ToCall + math.Max(potAmount, 2*req.ToCall)
	}
	return potAmount
}

func hasCallableRaise(req Request) bool {
	if len(req.LegalActions) == 0 {
		return req.EffectiveStack > req.ToCall+1e-9
	}
	minimumExtra := math.Inf(1)
	for _, legal := range req.LegalActions {
		if legal.Action != Raise && legal.Action != AllIn {
			continue
		}
		extra := legal.Min - req.ToCall
		if extra > 1e-9 && extra < minimumExtra {
			minimumExtra = extra
		}
	}
	if math.IsInf(minimumExtra, 1) {
		return false
	}
	callableExtra := math.Min(math.Max(0, req.Stack-req.ToCall), math.Max(0, req.EffectiveStack))
	return callableExtra+1e-9 >= minimumExtra
}

func bluffRoll(seed int64, level int, position Position) bool {
	return bluffRollAdjusted(seed, level, position, 1)
}

func bluffRollAdjusted(seed int64, level int, position Position, multiplier float64) bool {
	if seed == 0 {
		return false
	}
	frequency := .08 + float64(level)*.025
	if position == CO || position == BTN {
		frequency += .08
	}
	frequency *= multiplier
	if frequency > .45 {
		frequency = .45
	}
	return rand.New(rand.NewSource(seed)).Float64() < frequency
}

func analyzeOpponents(req Request) OpponentRead {
	read := OpponentRead{BluffMultiplier: 1}
	if len(req.OpponentModels) == 0 {
		return read
	}
	seenArchetype := make(map[string]bool)
	for _, model := range req.OpponentModels {
		read.AverageVPIP += model.VPIP
		read.Aggression += model.Aggression
		read.FoldToCBet += model.FoldToCBet
		read.ObservedHands += model.Hands
		if model.Archetype != "" && !seenArchetype[model.Archetype] {
			seenArchetype[model.Archetype] = true
			read.Archetypes = append(read.Archetypes, model.Archetype)
		}
	}
	count := float64(len(req.OpponentModels))
	read.AverageVPIP /= count
	read.Aggression /= count
	read.FoldToCBet /= count
	if read.AverageVPIP < .20 {
		read.CallMargin += .02
	}
	if read.Aggression > 2 {
		read.CallMargin -= .015
	}
	read.BluffMultiplier = math.Max(.5, math.Min(1.5, read.FoldToCBet/.5))
	for _, archetype := range read.Archetypes {
		if archetype == "calling_station" || archetype == "loose_passive" {
			read.BluffMultiplier *= .35
			read.ValueThreshold = -.03
			break
		}
	}
	return read
}

func applyHumanization(req Request, decision Decision, profile personality.Profile, seed int64, desired Action, amount float64, tags []string) (Action, float64, []string, bool) {
	rng := rand.New(rand.NewSource(seed + 101))
	if (desired == Bet || desired == Raise) && decision.Equity.Equity >= .65 && rng.Float64() < profile.SlowPlayRate {
		if req.ToCall == 0 {
			return Check, 0, append(tags, "human_slowplay"), true
		}
		return Call, req.ToCall, append(tags, "human_slowplay"), true
	}
	if rng.Float64() >= profile.MistakeRate {
		return desired, amount, tags, false
	}
	edge := decision.Equity.Equity - decision.PotOdds
	switch {
	case desired == Fold && edge >= -.03:
		return Call, req.ToCall, append(tags, "bounded_loose_call"), true
	case desired == Bet && decision.Equity.Equity >= .45 && decision.Equity.Equity < .65:
		return Check, 0, append(tags, "bounded_missed_bet"), true
	case desired == Raise && decision.Equity.Equity < .72:
		return Call, req.ToCall, append(tags, "bounded_passive_line"), true
	default:
		return desired, amount, tags, false
	}
}

func decisionComplexity(req Request, action Action) float64 {
	complexity := .25
	if req.Street != Preflop {
		complexity = .5
	}
	if req.ToCall > 0 {
		complexity += .2
	}
	if action == Raise || action == AllIn {
		complexity += .2
	}
	if complexity > 1 {
		return 1
	}
	return complexity
}

func validateStrategyRequest(req Request) error {
	if req.Pot < 0 || req.ToCall < 0 || req.Stack <= 0 || req.EffectiveStack <= 0 || req.EffectiveStack > req.Stack || req.BigBlind <= 0 || req.ToCall > req.Stack {
		return fmt.Errorf("%w: invalid pot, call, stack or blind", ErrInvalidStrategyInput)
	}
	if req.Level < 0 || req.Level > 5 {
		return fmt.Errorf("%w: level must be 0 (default) or between 1 and 5", ErrInvalidStrategyInput)
	}
	if req.TargetVPIP < 0 || req.TargetVPIP > 1 || req.TargetPFR < 0 || req.TargetPFR > req.TargetVPIP {
		return fmt.Errorf("%w: target VPIP/PFR must satisfy 0 <= PFR <= VPIP <= 1", ErrInvalidStrategyInput)
	}
	switch req.Street {
	case Preflop, Flop, Turn, River:
	default:
		return fmt.Errorf("%w: unsupported street %q", ErrInvalidStrategyInput, req.Street)
	}
	return nil
}
