package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"

	"gitlab.com/smoothsics/ainp/internal/config"
	"gitlab.com/smoothsics/ainp/internal/equity"
	"gitlab.com/smoothsics/ainp/internal/poker"
	"gitlab.com/smoothsics/ainp/internal/strategy"
)

type agreementStat struct {
	Evaluated       int     `json:"evaluated"`
	ExactAgreement  int     `json:"exact_agreement"`
	CoarseAgreement int     `json:"coarse_agreement"`
	ExactRate       float64 `json:"exact_rate"`
	CoarseRate      float64 `json:"coarse_rate"`
}

type disagreementExample struct {
	File            string         `json:"file"`
	Line            int            `json:"line"`
	GameID          string         `json:"game_id"`
	UID             string         `json:"uid"`
	Street          string         `json:"street"`
	Position        string         `json:"position"`
	StyleName       string         `json:"benapi_style"`
	PrivateHand     []string       `json:"private_hand"`
	CommunityCards  []string       `json:"community_cards"`
	Players         int            `json:"players"`
	ActiveOpponents int            `json:"active_opponents"`
	ToCallBB        float64        `json:"to_call_bb"`
	PotBB           float64        `json:"pot_bb"`
	BenAction       string         `json:"benapi_action"`
	BenChosenProb   float64        `json:"benapi_chosen_probability"`
	BenArgmaxAction string         `json:"benapi_argmax_action"`
	AinPAction      string         `json:"ainp_action"`
	AinPRule        string         `json:"ainp_rule"`
	AinPEquity      float64        `json:"ainp_equity"`
	AinPPotOdds     float64        `json:"ainp_pot_odds"`
	ActionHistory   map[string]int `json:"action_history"`
}

type profileComparison struct {
	Profile                        string                               `json:"profile"`
	Personality                    string                               `json:"personality"`
	Level                          int                                  `json:"level"`
	TargetVPIP                     float64                              `json:"target_vpip"`
	TargetPFR                      float64                              `json:"target_pfr"`
	Evaluated                      int                                  `json:"evaluated"`
	Errors                         int                                  `json:"errors"`
	ExactAgreement                 int                                  `json:"exact_agreement"`
	CoarseAgreement                int                                  `json:"coarse_agreement"`
	BenArgmaxAgreement             int                                  `json:"ben_argmax_agreement"`
	AinPActionSupported            int                                  `json:"ainp_action_supported_by_benapi"`
	ChosenMismatchButArgmaxMatch   int                                  `json:"chosen_mismatch_but_argmax_match"`
	ExactAgreementRate             float64                              `json:"exact_agreement_rate"`
	CoarseAgreementRate            float64                              `json:"coarse_agreement_rate"`
	BenArgmaxAgreementRate         float64                              `json:"ben_argmax_agreement_rate"`
	AinPActionSupportRate          float64                              `json:"ainp_action_support_rate"`
	MeanBenProbabilityOfAinPAction float64                              `json:"mean_benapi_probability_of_ainp_action"`
	ByStreet                       map[string]*agreementStat            `json:"by_street"`
	ByPosition                     map[string]*agreementStat            `json:"by_position"`
	ByBenStyle                     map[string]*agreementStat            `json:"by_benapi_style"`
	ActionMatrix                   map[string]map[string]int            `json:"action_matrix_benapi_to_ainp"`
	ActionMatrixByStreet           map[string]map[string]map[string]int `json:"action_matrix_by_street_benapi_to_ainp"`
	SizingEvaluated                int                                  `json:"sizing_evaluated"`
	SizingWithinQuarterPot         int                                  `json:"sizing_within_quarter_pot"`
	SizingWithinQuarterPotRate     float64                              `json:"sizing_within_quarter_pot_rate"`
	BenSizingPotRatio              distributionStat                     `json:"benapi_sizing_pot_ratio"`
	AinPSizingPotRatio             distributionStat                     `json:"ainp_sizing_pot_ratio"`
	SizingAbsolutePotDifference    distributionStat                     `json:"sizing_absolute_pot_difference"`
	DisagreementExamples           []disagreementExample                `json:"disagreement_examples,omitempty"`
	benSizingValues                []float64
	ainpSizingValues               []float64
	sizingDifferenceValues         []float64
}

func compareWithAinP(opts options, records []decisionRecord) (map[string]profileComparison, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load AinP config: %w", err)
	}
	result := make(map[string]profileComparison, len(opts.Profiles))
	for _, profileName := range opts.Profiles {
		profile, ok := cfg.Engine.Personality.Profiles[profileName]
		if !ok {
			return nil, fmt.Errorf("AinP profile %q is not configured", profileName)
		}
		calculator := equity.NewCalculator()
		calculator.DefaultSamples = opts.EquitySamples
		calculator.MaxExactOutcomes = cfg.Engine.Equity.MaxExactOutcomes
		calculator.PreflopLookupEnabled = cfg.Engine.Equity.PreflopLookupEnabled
		calculator.AutoExactEnabled = cfg.Engine.Equity.AutoExactEnabled
		calculator.SetCacheCapacity(cfg.Engine.Equity.CacheCapacity)
		engine := strategy.NewEngine(calculator)
		comparison := profileComparison{
			Profile: profileName, Personality: profile.Personality, Level: profile.Level, TargetVPIP: profile.TargetVPIP, TargetPFR: profile.TargetPFR,
			ByStreet: map[string]*agreementStat{}, ByPosition: map[string]*agreementStat{}, ByBenStyle: map[string]*agreementStat{},
			ActionMatrix: map[string]map[string]int{}, ActionMatrixByStreet: map[string]map[string]map[string]int{},
		}
		benProbabilityOfAinPSum := 0.0
		for _, record := range records {
			request, buildErr := strategyRequest(record, cfg, profileName, profile, opts.EquitySamples)
			if buildErr != nil {
				comparison.Errors++
				continue
			}
			decision, decideErr := engine.Decide(context.Background(), request)
			if decideErr != nil {
				comparison.Errors++
				continue
			}
			ainpAction := normalizeAinPAction(decision.Action, request.ToCall)
			exact := record.BenAction == ainpAction
			coarse := coarseAction(record.BenAction) == coarseAction(ainpAction)
			comparison.Evaluated++
			if exact {
				comparison.ExactAgreement++
			}
			if coarse {
				comparison.CoarseAgreement++
			}
			if ainpAction == record.Argmax {
				comparison.BenArgmaxAgreement++
				if !exact {
					comparison.ChosenMismatchButArgmaxMatch++
				}
			}
			probabilityMass := actionProbability(record.Response.Extra.ActionProbs, ainpAction, record.Response.Extra.CurrentToCall)
			benProbabilityOfAinPSum += probabilityMass
			if probabilityMass > 0.000001 {
				comparison.AinPActionSupported++
			}
			updateAgreement(comparison.ByStreet, record.Street, exact, coarse)
			updateAgreement(comparison.ByPosition, record.Position, exact, coarse)
			updateAgreement(comparison.ByBenStyle, fallback(record.Request.StyleName, "raw"), exact, coarse)
			incrementNested(comparison.ActionMatrix, record.BenAction, ainpAction)
			incrementActionMatrixByStreet(comparison.ActionMatrixByStreet, record.Street, record.BenAction, ainpAction)
			if exact && (ainpAction == "bet" || ainpAction == "raise") && request.Pot > 0 {
				benRatio := record.Response.Amount / request.Pot
				ainpRatio := decision.Amount / request.Pot
				difference := math.Abs(benRatio - ainpRatio)
				comparison.SizingEvaluated++
				comparison.benSizingValues = append(comparison.benSizingValues, benRatio)
				comparison.ainpSizingValues = append(comparison.ainpSizingValues, ainpRatio)
				comparison.sizingDifferenceValues = append(comparison.sizingDifferenceValues, difference)
				if difference <= .25 {
					comparison.SizingWithinQuarterPot++
				}
			}
			if !exact && len(comparison.DisagreementExamples) < opts.MaxExamples {
				comparison.DisagreementExamples = append(comparison.DisagreementExamples, makeDifference(record, request, decision, ainpAction))
			}
		}
		finalizeComparison(&comparison)
		if comparison.Evaluated > 0 {
			comparison.MeanBenProbabilityOfAinPAction = benProbabilityOfAinPSum / float64(comparison.Evaluated)
		}
		result[profileName] = comparison
	}
	return result, nil
}

func strategyRequest(record decisionRecord, cfg config.Config, profileName string, profile config.BotProfileConfig, samples int) (strategy.Request, error) {
	req := record.Request
	hero, err := parseCardList(req.PrivateHand)
	if err != nil {
		return strategy.Request{}, err
	}
	board, err := parseCardList(req.CommunityCards)
	if err != nil {
		return strategy.Request{}, err
	}
	folded := map[int]bool{}
	raises := 0
	heroVPIP, heroPFR := false, false
	for _, action := range req.ActionSequence {
		typeName := normalizeAction("", action.BetType, action.BetType, 0)
		if typeName == "fold" || strings.EqualFold(action.BetType, "leave") {
			folded[action.PlayerID] = true
		}
		if typeName == "raise" || typeName == "bet" {
			raises++
		}
		if record.Street == "preflop" && action.PlayerID == req.CurrentPlayerID {
			if typeName == "call" || typeName == "raise" || typeName == "bet" {
				heroVPIP = true
			}
			if typeName == "raise" || typeName == "bet" {
				heroPFR = true
			}
		}
	}
	if record.Street != "preflop" {
		// The cumulative sequence has no street delimiter. Counting all earlier
		// raises as current-street raises is known to be wrong.
		raises = 0
	}
	activePlayers := req.NPlayers - len(folded)
	activeOpponents := activePlayers - 1
	if activeOpponents < 1 {
		activeOpponents = 1
	}
	opponents := make([][]poker.Card, activeOpponents)
	if record.Street == "preflop" {
		opponents = [][]poker.Card{{}}
	}
	stack := record.Response.Extra.CurrentStack
	if stack <= 0 && req.CurrentPlayerID >= 0 && req.CurrentPlayerID < len(req.Stack) {
		stack = req.Stack[req.CurrentPlayerID]
	}
	toCall := math.Min(stack, math.Max(0, record.Response.Extra.CurrentToCall))
	pot := record.Response.Extra.CurrentPot
	if pot <= 0 {
		pot = approximatePot(req)
	}
	maxOpponentStack := 0.0
	for index, value := range req.Stack {
		if index != req.CurrentPlayerID && !folded[index] && value > maxOpponentStack {
			maxOpponentStack = value
		}
	}
	effective := math.Min(stack, maxOpponentStack)
	if effective <= 0 {
		effective = stack
	}
	street, position := strategy.Street(record.Street), strategy.Position(record.Position)
	if record.Position == "STR" {
		// AinP's current position model excludes the straddle role from named
		// early/late positions and falls back to MP.
		position = strategy.MP
	}
	return strategy.Request{
		DecisionID:    fmt.Sprintf("benapi:%s:%s:%d:%s", record.File, req.GameID, record.Line, profileName),
		PolicyVersion: cfg.Engine.PolicyVersion, AIProfile: profileName, ProfileSource: "profiles", PlayerID: record.UID, TableID: record.RoomID, HandID: record.HandNum,
		Game: equity.GameNLH, Street: street, Position: position, Hero: hero, Board: board, Opponents: opponents,
		Pot: pot, ToCall: toCall, Stack: stack, EffectiveStack: effective, BigBlind: req.BB, RaisesFaced: raises, ActiveOpponents: activeOpponents,
		Level: profile.Level, TargetVPIP: profile.TargetVPIP, TargetPFR: profile.TargetPFR, PostflopSizings: append([]float64(nil), profile.PostflopSizings...),
		BehaviorMode: profile.BehaviorMode, PreflopRaiseProbability: profile.PreflopRaiseProbability, PostflopAggressionChance: profile.PostflopAggressionChance,
		NeverFold: profile.NeverFold, AuditExempt: profile.AuditExempt, HeroPreflopVPIP: heroVPIP, HeroPreflopPFR: heroPFR,
		PostflopCallMargin: profile.PostflopCallMargin, LargePotThresholdBB: profile.LargePotThreshold, LargePotMinEquity: profile.LargePotMinEquity,
		PersonalityID: profile.Personality, DisablePersonality: !cfg.Engine.Personality.Enabled,
		DisableHumanization: !cfg.Engine.Personality.HumanizationEnabled, DisableOpponentModel: true, DisableThinkTime: true,
		Seed: stableSeed(record.File + req.GameID + record.UID + record.Street + profileName), EquitySamples: samples,
		LegalActions:      legalActions(record.Street, toCall, stack, req.BB, cfg.Engine.Strategy.MinRaiseBigBlinds),
		CollapseNearAllIn: cfg.Engine.Strategy.CollapseNearAllIn, NearAllInRemainingChips: cfg.Engine.Strategy.NearAllInRemainingChips,
		PreflopOpenCallGap: cfg.Engine.Strategy.PreflopOpenCallGap, PreflopReraiseEquity: cfg.Engine.Strategy.PreflopReraiseEquity, PreflopReraiseRangeFactor: cfg.Engine.Strategy.PreflopReraiseRangeFactor,
		PreflopExtraRaisePenalty: cfg.Engine.Strategy.PreflopExtraRaisePenalty, PreflopMultiwayPenalty: cfg.Engine.Strategy.PreflopMultiwayPenalty,
		PreflopCallMargin: cfg.Engine.Strategy.PreflopCallMargin, PreflopLargeCallBB: cfg.Engine.Strategy.PreflopLargeCallBB, FlopAirCallMargin: cfg.Engine.Strategy.FlopAirCallMargin,
		TurnAirCallMargin: cfg.Engine.Strategy.TurnAirCallMargin, RiverAirCallMargin: cfg.Engine.Strategy.RiverAirCallMargin,
		RepeatedAirCallPenalty: cfg.Engine.Strategy.RepeatedAirCallPenalty, UnderpairCallMargin: cfg.Engine.Strategy.UnderpairCallMargin, TurnWeakDrawCallMargin: cfg.Engine.Strategy.TurnWeakDrawCallMargin,
		RiverBoardPairCallMargin: cfg.Engine.Strategy.RiverBoardPairCallMargin, RiverMissedDrawMargin: cfg.Engine.Strategy.RiverMissedDrawMargin,
		RejectNegativeEVCalls: cfg.Engine.Strategy.RejectNegativeEVCalls,
	}, nil
}

func legalActions(street string, toCall, stack, bigBlind, minRaiseBB float64) []strategy.LegalAction {
	minimumRaiseSize := math.Max(bigBlind, bigBlind*minRaiseBB)
	if toCall > 0 {
		actions := []strategy.LegalAction{{Action: strategy.Fold}}
		if stack <= toCall+1e-9 {
			return append(actions, strategy.LegalAction{Action: strategy.AllIn, Min: stack, Max: stack})
		}
		actions = append(actions, strategy.LegalAction{Action: strategy.Call, Min: toCall, Max: toCall})
		minimumRaise := math.Min(stack, toCall+minimumRaiseSize)
		if minimumRaise < stack {
			actions = append(actions, strategy.LegalAction{Action: strategy.Raise, Min: minimumRaise, Max: stack})
		}
		return append(actions, strategy.LegalAction{Action: strategy.AllIn, Min: stack, Max: stack})
	}
	actions := []strategy.LegalAction{{Action: strategy.Check}}
	aggressive := strategy.Bet
	if street == "preflop" {
		aggressive = strategy.Raise
	}
	minimum := math.Min(stack, minimumRaiseSize)
	if minimum < stack {
		actions = append(actions, strategy.LegalAction{Action: aggressive, Min: minimum, Max: stack})
	}
	return append(actions, strategy.LegalAction{Action: strategy.AllIn, Min: stack, Max: stack})
}

func approximatePot(req benRequest) float64 {
	pot := req.SB + req.BB + req.Ante*float64(req.NPlayers) + req.Straddle
	for _, action := range req.ActionSequence {
		if action.Amount > 0 {
			pot += action.Amount
		}
	}
	return pot
}

func parseCardList(values []string) ([]poker.Card, error) {
	result := make([]poker.Card, 0, len(values))
	for _, value := range values {
		card, err := poker.ParseCard(value)
		if err != nil {
			return nil, err
		}
		result = append(result, card)
	}
	return result, nil
}

func normalizeAinPAction(action strategy.Action, toCall float64) string {
	if action == strategy.AllIn {
		if toCall > 0 {
			return "raise"
		}
		return "bet"
	}
	return string(action)
}

func coarseAction(action string) string {
	switch action {
	case "bet", "raise":
		return "aggressive"
	default:
		return action
	}
}

func updateAgreement(values map[string]*agreementStat, key string, exact, coarse bool) {
	item := values[key]
	if item == nil {
		item = &agreementStat{}
		values[key] = item
	}
	item.Evaluated++
	if exact {
		item.ExactAgreement++
	}
	if coarse {
		item.CoarseAgreement++
	}
}

func finalizeComparison(item *profileComparison) {
	if item.Evaluated > 0 {
		item.ExactAgreementRate = float64(item.ExactAgreement) / float64(item.Evaluated)
		item.CoarseAgreementRate = float64(item.CoarseAgreement) / float64(item.Evaluated)
		item.BenArgmaxAgreementRate = float64(item.BenArgmaxAgreement) / float64(item.Evaluated)
		item.AinPActionSupportRate = float64(item.AinPActionSupported) / float64(item.Evaluated)
	}
	if item.SizingEvaluated > 0 {
		item.SizingWithinQuarterPotRate = float64(item.SizingWithinQuarterPot) / float64(item.SizingEvaluated)
	}
	item.BenSizingPotRatio = summarizeDistribution(item.benSizingValues)
	item.AinPSizingPotRatio = summarizeDistribution(item.ainpSizingValues)
	item.SizingAbsolutePotDifference = summarizeDistribution(item.sizingDifferenceValues)
	for _, group := range []map[string]*agreementStat{item.ByStreet, item.ByPosition, item.ByBenStyle} {
		for _, stat := range group {
			if stat.Evaluated > 0 {
				stat.ExactRate = float64(stat.ExactAgreement) / float64(stat.Evaluated)
				stat.CoarseRate = float64(stat.CoarseAgreement) / float64(stat.Evaluated)
			}
		}
	}
}

func incrementActionMatrixByStreet(values map[string]map[string]map[string]int, street, benAction, ainpAction string) {
	if values[street] == nil {
		values[street] = map[string]map[string]int{}
	}
	incrementNested(values[street], benAction, ainpAction)
}

func actionProbability(candidates []benAction, action string, toCall float64) float64 {
	total := 0.0
	for _, candidate := range candidates {
		if normalizeAction("", candidate.BetType, candidate.BetType, toCall) == action {
			total += candidate.ActionProb
		}
	}
	return total
}

func makeDifference(record decisionRecord, req strategy.Request, decision strategy.Decision, ainpAction string) disagreementExample {
	history := map[string]int{}
	for _, action := range record.Request.ActionSequence {
		increment(history, normalizeAction("", action.BetType, action.BetType, 0))
	}
	bb := record.Request.BB
	if bb <= 0 {
		bb = 1
	}
	return disagreementExample{
		File: record.File, Line: record.Line, GameID: record.Request.GameID, UID: record.UID, Street: record.Street, Position: record.Position,
		StyleName: fallback(record.Request.StyleName, "raw"), PrivateHand: record.Request.PrivateHand, CommunityCards: record.Request.CommunityCards,
		Players: record.Request.NPlayers, ActiveOpponents: req.ActiveOpponents, ToCallBB: req.ToCall / bb, PotBB: req.Pot / bb,
		BenAction: record.BenAction, BenChosenProb: record.ChosenProb, BenArgmaxAction: record.Argmax,
		AinPAction: ainpAction, AinPRule: decision.RuleID, AinPEquity: decision.Equity.Equity, AinPPotOdds: decision.PotOdds, ActionHistory: history,
	}
}

func stableSeed(value string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(value))
	return int64(hash.Sum64() & math.MaxInt64)
}
