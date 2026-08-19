package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"gitlab.com/smoothsics/ainp/internal/config"
	"gitlab.com/smoothsics/ainp/internal/equity"
	"gitlab.com/smoothsics/ainp/internal/game"
	"gitlab.com/smoothsics/ainp/internal/opponent"
	"gitlab.com/smoothsics/ainp/internal/personality"
	"gitlab.com/smoothsics/ainp/internal/poker"
	"gitlab.com/smoothsics/ainp/internal/protocol"
	"gitlab.com/smoothsics/ainp/internal/strategy"
)

const chipEpsilon = 1e-9

type EngineDecisionProvider struct {
	cfg       config.EngineConfig
	strategy  *strategy.Engine
	tracker   *opponent.Tracker
	semaphore chan struct{}
	adviseOn  map[protocol.Command]struct{}
	fallback  *MockDecisionProvider
	logger    *slog.Logger
}

func NewEngineDecisionProvider(cfg config.Config, logger *slog.Logger) *EngineDecisionProvider {
	tracker := opponent.NewTrackerWithLimits(cfg.Engine.OpponentModel.MaxPlayers, cfg.Engine.OpponentModel.DedupeWindow)
	return newEngineDecisionProvider(cfg, cfg.Engine, tracker, logger)
}

func newEngineDecisionProvider(cfg config.Config, engineCfg config.EngineConfig, tracker *opponent.Tracker, logger *slog.Logger) *EngineDecisionProvider {
	calculator := equity.NewCalculator()
	calculator.DefaultSamples = engineCfg.Equity.DefaultSamples
	calculator.DefaultPLO4Samples = engineCfg.Equity.PLO4Samples
	calculator.DefaultPLO5Samples = engineCfg.Equity.PLO5Samples
	calculator.DefaultPLO6Samples = engineCfg.Equity.PLO6Samples
	calculator.MaxExactOutcomes = engineCfg.Equity.MaxExactOutcomes
	calculator.PreflopLookupEnabled = engineCfg.Equity.PreflopLookupEnabled
	calculator.AutoExactEnabled = engineCfg.Equity.AutoExactEnabled
	if engineCfg.Equity.CacheEnabled {
		calculator.SetCacheCapacity(engineCfg.Equity.CacheCapacity)
	} else {
		calculator.SetCacheCapacity(0)
	}
	var options []strategy.Option
	if cfg.Log.Strategy {
		options = append(options, strategy.WithLogger(logger))
	}
	commands := make(map[protocol.Command]struct{}, len(engineCfg.AdviseOn))
	for _, command := range engineCfg.AdviseOn {
		commands[protocol.Command(command)] = struct{}{}
	}
	return &EngineDecisionProvider{
		cfg: engineCfg, strategy: strategy.NewEngine(calculator, options...),
		tracker:   tracker,
		semaphore: make(chan struct{}, engineCfg.MaxConcurrent), adviseOn: commands,
		fallback: NewMockDecisionProvider(cfg.Mock), logger: logger,
	}
}

func (p *EngineDecisionProvider) Name() string          { return "engine" }
func (p *EngineDecisionProvider) PolicyVersion() string { return p.cfg.PolicyVersion }

func (p *EngineDecisionProvider) Decide(ctx context.Context, input DecisionInput) (*protocol.AdviseResponse, error) {
	if input.State == nil {
		return p.fail(ctx, input, fmt.Errorf("missing normalized game state"))
	}
	p.observe(input.State)
	if _, enabled := p.adviseOn[input.Event.Cmd]; !enabled || !input.State.ShouldAdvise() {
		return nil, nil
	}
	request, err := p.strategyRequest(input)
	if err != nil {
		return p.fail(ctx, input, err)
	}
	select {
	case p.semaphore <- struct{}{}:
	case <-ctx.Done():
		return p.fail(ctx, input, ctx.Err())
	}
	decisionCtx, cancel := context.WithTimeout(ctx, p.cfg.DecisionTimeout)
	decision, err := p.strategy.Decide(decisionCtx, request)
	cancel()
	<-p.semaphore
	if err != nil {
		return p.fail(ctx, input, err)
	}
	if p.cfg.Personality.ApplyThinkTime && decision.ThinkTime > 0 {
		timer := time.NewTimer(decision.ThinkTime)
		select {
		case <-ctx.Done():
			timer.Stop()
			return p.fail(ctx, input, ctx.Err())
		case <-timer.C:
		}
	}
	advice := constrainToLegalActions(decisionToAdvice(decision), input.State)
	advice = collapseConstrainedNearAllIn(advice, input.State, p.cfg.Strategy.CollapseNearAllIn, p.cfg.Strategy.NearAllInRemainingChips)
	if request.NeverFold && advice != nil && advice.Type == protocol.ActionFold {
		advice = constrainNeverFoldToLegalActions(input.State)
	}
	return advice, nil
}

// collapseConstrainedNearAllIn runs after the server-authoritative legal-action
// clamp. The clamp may reduce a strategy Raise/Bet to e.g. 119.99 while All-in
// is 120, so doing this only inside the strategy guard can leave a 0.01 tail.
func collapseConstrainedNearAllIn(advice *protocol.AdviseResponse, state *game.State, enabled bool, threshold float64) *protocol.AdviseResponse {
	if !enabled || threshold <= 0 || advice == nil || advice.Value == nil || state == nil ||
		(advice.Type != protocol.ActionRaise && advice.Type != protocol.ActionBet) {
		return advice
	}
	var allIn protocol.LegalAction
	found := false
	for _, action := range state.LegalActions {
		if action.Type == protocol.ActionAllIn {
			allIn, found = action, true
			break
		}
	}
	if !found {
		return advice
	}
	allInAmount := allIn.Max
	if allInAmount <= 0 {
		allInAmount = allIn.Min
	}
	remaining := allInAmount - *advice.Value
	if allInAmount <= 0 || remaining <= 0 || remaining > threshold+chipEpsilon {
		return advice
	}
	return &protocol.AdviseResponse{Type: protocol.ActionAllIn, Value: &allInAmount, ValueMode: "increment"}
}

func constrainNeverFoldToLegalActions(state *game.State) *protocol.AdviseResponse {
	if state == nil {
		return nil
	}
	byType := make(map[protocol.ActionType]bool, len(state.LegalActions))
	for _, action := range state.LegalActions {
		byType[action.Type] = true
	}
	for _, actionType := range []protocol.ActionType{protocol.ActionCall, protocol.ActionCheck, protocol.ActionAllIn, protocol.ActionRaise, protocol.ActionBet} {
		if byType[actionType] {
			return constrainToLegalActions(&protocol.AdviseResponse{Type: actionType}, state)
		}
	}
	return nil
}

func (p *EngineDecisionProvider) fail(ctx context.Context, input DecisionInput, cause error) (*protocol.AdviseResponse, error) {
	p.logger.Error("engine_decision_failed", "request_id", input.RequestID, "decision_id", input.DecisionID, "error", cause)
	if p.cfg.FallbackToMock && p.fallback != nil {
		advice, fallbackErr := p.fallback.Decide(ctx, input)
		if fallbackErr == nil && advice != nil {
			p.logger.Warn("engine_decision_fallback", "request_id", input.RequestID, "decision_id", input.DecisionID, "provider", "mock")
			return advice, nil
		}
	}
	return nil, cause
}

func (p *EngineDecisionProvider) observe(state *game.State) {
	if !p.cfg.OpponentModel.Enabled || state.LastObservation == nil {
		return
	}
	item := state.LastObservation
	_ = p.tracker.Observe(opponent.Observation{
		ObservationID: item.ObservationID, PlayerID: item.PlayerID, HandID: item.HandID, Street: string(item.Street), Action: string(item.Action),
		Voluntary: item.Voluntary, PreflopOpportunity: item.PreflopOpportunity, PFROpportunity: item.PFROpportunity,
		ThreeBetOpportunity: item.ThreeBetOpportunity, CBetOpportunity: item.CBetOpportunity, FacingCBet: item.FacingCBet,
	})
}

func (p *EngineDecisionProvider) strategyRequest(input DecisionInput) (strategy.Request, error) {
	state := input.State
	gameType, err := p.resolveGame(state.GameType)
	if err != nil {
		return strategy.Request{}, err
	}
	hero := state.Players[state.HeroID]
	if hero == nil || len(state.HeroCards) == 0 {
		return strategy.Request{}, fmt.Errorf("hero state or cards missing")
	}
	// Keep opponent holes unknown unless they were legally revealed by show_cards.
	equityOpponents := make([][]poker.Card, 0, len(state.Players)-1)
	models := make([]opponent.Snapshot, 0, len(state.Players)-1)
	maxOpponentStack := 0.0
	for _, playerID := range state.Order {
		player := state.Players[playerID]
		if player == nil || playerID == state.HeroID || player.Folded {
			continue
		}
		hole := append([]poker.Card(nil), player.Cards...)
		equityOpponents = append(equityOpponents, hole)
		if player.Stack > maxOpponentStack {
			maxOpponentStack = player.Stack
		}
		if p.cfg.OpponentModel.Enabled {
			models = append(models, p.tracker.Snapshot(playerID))
		}
	}
	if len(equityOpponents) == 0 {
		return strategy.Request{}, fmt.Errorf("no active opponent")
	}
	activeOpponents := len(equityOpponents)
	if state.Street == game.Preflop {
		// Opening thresholds are calibrated to heads-up starting-hand strength.
		// Field size is applied separately as a small configurable penalty.
		equityOpponents = [][]poker.Card{{}}
	}
	toCall := state.CurrentBet - hero.StreetContribution
	if math.Abs(toCall) <= chipEpsilon {
		toCall = 0
	}
	toCall = math.Min(hero.Stack, math.Max(0, toCall))
	effective := math.Min(hero.Stack, maxOpponentStack)
	if effective <= 0 {
		effective = hero.Stack
	}
	profile := p.resolveBotProfile(state.AIProfile)
	return strategy.Request{
		DecisionID: input.DecisionID, RequestID: input.RequestID, PolicyVersion: p.cfg.PolicyVersion, AIProfile: state.AIProfile, ProfileSource: profile.Source, PlayerID: state.HeroID, TableID: state.TableID, HandID: state.HandID,
		Game: gameType, Street: strategyStreet(state.Street), Position: playerPosition(state), Hero: append([]poker.Card(nil), state.HeroCards...),
		Board: append([]poker.Card(nil), state.Board...), Opponents: equityOpponents, Pot: state.Pot, ToCall: toCall,
		Stack: hero.Stack, EffectiveStack: effective, BigBlind: state.BigBlind, RaisesFaced: state.Raises,
		WasPreflopAggressor: state.PreflopAggressor == state.HeroID, Level: profile.Level,
		TargetVPIP: profile.TargetVPIP, TargetPFR: profile.TargetPFR,
		BehaviorMode: profile.BehaviorMode, PreflopRaiseProbability: profile.PreflopRaiseProbability,
		PostflopAggressionChance: profile.PostflopAggressionChance, NeverFold: profile.NeverFold, AuditExempt: profile.AuditExempt,
		HeroPreflopVPIP: hero.PreflopVPIP, HeroPreflopPFR: hero.PreflopPFR, HeroPostflopCalls: hero.PostflopCalls,
		PostflopCallMargin: profile.PostflopCallMargin, LargePotThresholdBB: profile.LargePotThreshold, LargePotMinEquity: profile.LargePotMinEquity,
		ActiveOpponents: activeOpponents,
		PersonalityID:   profile.PersonalityID, OpponentModels: models, Seed: decisionSeed(input.DecisionID),
		LegalActions:       inferLegalActions(state, p.cfg.Strategy.MinRaiseBigBlinds),
		DisablePersonality: !p.cfg.Personality.Enabled, DisableHumanization: !p.cfg.Personality.HumanizationEnabled,
		DisableOpponentModel: !p.cfg.OpponentModel.Enabled, DisableThinkTime: !p.cfg.Personality.ThinkTimeEnabled,
		CollapseNearAllIn: p.cfg.Strategy.CollapseNearAllIn, NearAllInRemainingChips: p.cfg.Strategy.NearAllInRemainingChips,
		PreflopOpenCallGap: p.cfg.Strategy.PreflopOpenCallGap, PreflopReraiseEquity: p.cfg.Strategy.PreflopReraiseEquity,
		PreflopExtraRaisePenalty: p.cfg.Strategy.PreflopExtraRaisePenalty, PreflopMultiwayPenalty: p.cfg.Strategy.PreflopMultiwayPenalty,
		PreflopCallMargin: p.cfg.Strategy.PreflopCallMargin,
		FlopAirCallMargin: p.cfg.Strategy.FlopAirCallMargin, TurnAirCallMargin: p.cfg.Strategy.TurnAirCallMargin,
		RiverAirCallMargin: p.cfg.Strategy.RiverAirCallMargin, RepeatedAirCallPenalty: p.cfg.Strategy.RepeatedAirCallPenalty,
		RejectNegativeEVCalls: p.cfg.Strategy.RejectNegativeEVCalls,
	}, nil
}

func (p *EngineDecisionProvider) resolveGame(raw string) (equity.Game, error) {
	key := strings.ToUpper(strings.TrimSpace(raw))
	value, ok := p.cfg.GameAliases[key]
	if !ok {
		value = key
	}
	gameType := equity.Game(strings.ToUpper(value))
	switch gameType {
	case equity.GameNLH, equity.GamePLO4, equity.GamePLO5, equity.GamePLO6, equity.GameShortDeck, equity.GameShortDeckFixed:
		return gameType, nil
	default:
		return "", fmt.Errorf("unsupported game type %q", raw)
	}
}

type botProfileResolution struct {
	PersonalityID            string
	Level                    int
	TargetVPIP               float64
	TargetPFR                float64
	BehaviorMode             string
	PreflopRaiseProbability  float64
	PostflopAggressionChance float64
	NeverFold                bool
	AuditExempt              bool
	PostflopCallMargin       float64
	LargePotThreshold        float64
	LargePotMinEquity        float64
	Source                   string
}

func (p *EngineDecisionProvider) resolveBotProfile(aiProfile string) botProfileResolution {
	result := botProfileResolution{PersonalityID: p.cfg.Personality.Default, Level: p.cfg.DefaultLevel, Source: "default"}
	profileKey := strings.TrimSpace(aiProfile)
	if p.cfg.Personality.UseAIProfile {
		if configured, ok := p.cfg.Personality.Profiles[profileKey]; ok {
			result.PersonalityID, result.Level, result.TargetVPIP, result.TargetPFR = configured.Personality, configured.Level, configured.TargetVPIP, configured.TargetPFR
			result.PostflopCallMargin, result.LargePotThreshold, result.LargePotMinEquity, result.Source = configured.PostflopCallMargin, configured.LargePotThreshold, configured.LargePotMinEquity, "profiles"
			result.BehaviorMode, result.PreflopRaiseProbability, result.PostflopAggressionChance = configured.BehaviorMode, configured.PreflopRaiseProbability, configured.PostflopAggressionChance
			result.NeverFold, result.AuditExempt = configured.NeverFold, configured.AuditExempt
		} else if mapped := p.cfg.Personality.ProfileMap[profileKey]; mapped != "" {
			result.PersonalityID, result.Source = mapped, "profile_map"
		} else if _, err := personality.Resolve(profileKey); err == nil {
			result.PersonalityID, result.Source = profileKey, "builtin"
		}
	}
	if !p.cfg.Personality.Enabled {
		result.PersonalityID = ""
	}
	return result
}

func inferLegalActions(state *game.State, minRaiseBB float64) []strategy.LegalAction {
	hero := state.Players[state.HeroID]
	if hero == nil || hero.Stack <= 0 {
		return nil
	}
	toCall := state.CurrentBet - hero.StreetContribution
	if math.Abs(toCall) <= chipEpsilon {
		toCall = 0
	}
	toCall = math.Max(0, toCall)
	canRaise := opponentCanRespond(state)
	if toCall > 0 {
		actions := []strategy.LegalAction{{Action: strategy.Fold}}
		if hero.Stack <= toCall+1e-9 {
			return append(actions, strategy.LegalAction{Action: strategy.AllIn, Min: hero.Stack, Max: hero.Stack})
		}
		actions = append(actions, strategy.LegalAction{Action: strategy.Call, Min: toCall, Max: toCall})
		if canRaise {
			minimumRaise := toCall + math.Max(state.LastFullRaise, state.BigBlind*minRaiseBB)
			if minimumRaise < hero.Stack {
				actions = append(actions, strategy.LegalAction{Action: strategy.Raise, Min: minimumRaise, Max: hero.Stack})
			}
			actions = append(actions, strategy.LegalAction{Action: strategy.AllIn, Min: hero.Stack, Max: hero.Stack})
		}
		return actions
	}
	if state.Street == game.Preflop && state.CurrentBet > 0 {
		actions := []strategy.LegalAction{{Action: strategy.Check}}
		if !canRaise {
			return actions
		}
		minimumRaise := math.Max(state.LastFullRaise, state.BigBlind*minRaiseBB)
		if minimumRaise < hero.Stack {
			actions = append(actions, strategy.LegalAction{Action: strategy.Raise, Min: minimumRaise, Max: hero.Stack})
		}
		return append(actions, strategy.LegalAction{Action: strategy.AllIn, Min: hero.Stack, Max: hero.Stack})
	}
	actions := []strategy.LegalAction{{Action: strategy.Check}}
	if !canRaise {
		return actions
	}
	minimumBet := math.Min(hero.Stack, math.Max(state.BigBlind, state.BigBlind*minRaiseBB))
	if hero.Stack > minimumBet+1e-9 {
		actions = append(actions, strategy.LegalAction{Action: strategy.Bet, Min: minimumBet, Max: hero.Stack})
	}
	return append(actions, strategy.LegalAction{Action: strategy.AllIn, Min: hero.Stack, Max: hero.Stack})
}

func opponentCanRespond(state *game.State) bool {
	for id, player := range state.Players {
		if id != state.HeroID && player != nil && !player.Folded && !player.AllIn && player.Stack > 1e-9 {
			return true
		}
	}
	return false
}

func playerPosition(state *game.State) strategy.Position {
	hero := state.Players[state.HeroID]
	if hero == nil {
		return strategy.MP
	}
	switch hero.Role {
	case "sb":
		return strategy.SB
	case "bb":
		return strategy.BB
	case "bt", "dealer":
		return strategy.BTN
	}
	normals := make([]string, 0)
	for _, id := range state.Order {
		role := state.Players[id].Role
		if role != "sb" && role != "bb" && role != "st" && role != "bt" && role != "dealer" {
			normals = append(normals, id)
		}
	}
	for index, id := range normals {
		if id != state.HeroID {
			continue
		}
		remaining := len(normals) - index
		if remaining == 1 {
			return strategy.CO
		}
		if index == 0 {
			return strategy.UTG
		}
		return strategy.MP
	}
	return strategy.MP
}

func strategyStreet(street game.Street) strategy.Street {
	switch street {
	case game.Flop:
		return strategy.Flop
	case game.Turn:
		return strategy.Turn
	case game.River:
		return strategy.River
	default:
		return strategy.Preflop
	}
}

func decisionSeed(decisionID string) int64 {
	sum := sha256.Sum256([]byte(decisionID))
	return int64(binary.BigEndian.Uint64(sum[:8]) & math.MaxInt64)
}

func decisionToAdvice(decision strategy.Decision) *protocol.AdviseResponse {
	advice := &protocol.AdviseResponse{Type: protocol.ActionType(decision.Action)}
	if decision.Action == strategy.Call || decision.Action == strategy.Bet || decision.Action == strategy.Raise || decision.Action == strategy.AllIn {
		value := decision.Amount
		advice.Value = &value
	}
	return advice
}

func constrainToLegalActions(advice *protocol.AdviseResponse, state *game.State) *protocol.AdviseResponse {
	if advice == nil || state == nil || len(state.LegalActions) == 0 {
		return advice
	}
	byType := make(map[protocol.ActionType]protocol.LegalAction, len(state.LegalActions))
	for _, action := range state.LegalActions {
		byType[action.Type] = action
	}
	// The server's legal-actions list is authoritative. If Check is available,
	// a requested Fold is a free-option fold and must be converted to Check.
	if advice.Type == protocol.ActionFold {
		if check, ok := byType[protocol.ActionCheck]; ok {
			return &protocol.AdviseResponse{Type: check.Type, ValueMode: "increment"}
		}
	}
	preference := map[protocol.ActionType][]protocol.ActionType{
		protocol.ActionFold:  {protocol.ActionFold, protocol.ActionCheck, protocol.ActionCall},
		protocol.ActionCheck: {protocol.ActionCheck, protocol.ActionCall, protocol.ActionFold},
		protocol.ActionCall:  {protocol.ActionCall, protocol.ActionCheck, protocol.ActionFold},
		protocol.ActionBet:   {protocol.ActionBet, protocol.ActionRaise, protocol.ActionCheck, protocol.ActionFold},
		protocol.ActionRaise: {protocol.ActionRaise, protocol.ActionBet, protocol.ActionCall, protocol.ActionCheck, protocol.ActionFold},
		protocol.ActionAllIn: {protocol.ActionAllIn, protocol.ActionRaise, protocol.ActionCall, protocol.ActionCheck, protocol.ActionFold},
	}
	var selected protocol.LegalAction
	found := false
	for _, actionType := range preference[advice.Type] {
		if action, ok := byType[actionType]; ok {
			selected, found = action, true
			break
		}
	}
	if !found {
		return advice
	}
	// pokerbot's ActionLimit min/max values are the chips added by this action,
	// not the player's resulting total contribution on the street.
	result := &protocol.AdviseResponse{Type: selected.Type, ValueMode: "increment"}
	if selected.Type == protocol.ActionCall || selected.Type == protocol.ActionBet || selected.Type == protocol.ActionRaise || selected.Type == protocol.ActionAllIn {
		value := selected.Min
		if advice.Value != nil && selected.Type == advice.Type {
			desired := *advice.Value
			value = math.Max(selected.Min, math.Min(selected.Max, desired))
		}
		result.Value = &value
	}
	return result
}
