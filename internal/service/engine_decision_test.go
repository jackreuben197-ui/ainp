package service

import (
	"io"
	"log/slog"
	"testing"

	"gitlab.com/smoothsics/ainp/internal/config"
	"gitlab.com/smoothsics/ainp/internal/game"
	"gitlab.com/smoothsics/ainp/internal/poker"
	"gitlab.com/smoothsics/ainp/internal/protocol"
	"gitlab.com/smoothsics/ainp/internal/strategy"
)

func TestInferLegalActionsUsesRaiseForPreflopBigBlindOption(t *testing.T) {
	state := &game.State{
		HeroID: "bot", Street: game.Preflop, BigBlind: 2, CurrentBet: 2, LastFullRaise: 2,
		Players: map[string]*game.Player{"bot": {ID: "bot", Stack: 98, StreetContribution: 2}, "villain": {ID: "villain", Stack: 99, StreetContribution: 1}},
	}
	actions := inferLegalActions(state, 1)
	foundRaise, foundBet := false, false
	for _, action := range actions {
		foundRaise = foundRaise || action.Action == strategy.Raise
		foundBet = foundBet || action.Action == strategy.Bet
	}
	if !foundRaise || foundBet {
		t.Fatalf("actions=%+v", actions)
	}
}

func TestResolveBotProfilePerHand(t *testing.T) {
	cfg := config.Default()
	cfg.Engine.Personality.Profiles["AICON_TAG_L2"] = config.BotProfileConfig{Personality: "tag", Level: 2, TargetVPIP: .30, TargetPFR: .15, PostflopSizings: []float64{.5, .66}}
	cfg.Engine.Personality.Profiles["AICON_LAG_L5"] = config.BotProfileConfig{Personality: "lag", Level: 5, TargetVPIP: .45, TargetPFR: .25}
	provider := NewEngineDecisionProvider(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	first := provider.resolveBotProfile("AICON_TAG_L2")
	second := provider.resolveBotProfile("AICON_LAG_L5")
	if first.PersonalityID != "tag" || first.Level != 2 || first.TargetVPIP != .30 || first.TargetPFR != .15 || len(first.PostflopSizings) != 2 || first.PostflopSizings[0] != .5 || first.Source != "profiles" || second.PersonalityID != "lag" || second.Level != 5 || second.TargetVPIP != .45 || second.TargetPFR != .25 || second.Source != "profiles" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	fallback := provider.resolveBotProfile("unknown-from-caller")
	if fallback.PersonalityID != cfg.Engine.Personality.Default || fallback.Level != cfg.Engine.DefaultLevel || fallback.Source != "default" {
		t.Fatalf("fallback=%+v", fallback)
	}
}

func TestStrategyRequestUsesCurrentHandsAIProfile(t *testing.T) {
	cfg := config.Default()
	cfg.Engine.Personality.Profiles["TAG_L2"] = config.BotProfileConfig{Personality: "tag", Level: 2, TargetVPIP: .30, TargetPFR: .15, PostflopSizings: []float64{.5, .66}}
	cfg.Engine.Personality.Profiles["LAG_L5"] = config.BotProfileConfig{Personality: "lag", Level: 5, TargetVPIP: .45, TargetPFR: .25}
	provider := NewEngineDecisionProvider(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	state := &game.State{
		HeroID: "bot", TableID: "table", HandID: "hand-1", GameType: "NLH", AIProfile: "TAG_L2", Street: game.Preflop,
		BigBlind: 2, Pot: 3, Players: map[string]*game.Player{"bot": {ID: "bot", Stack: 100}, "villain": {ID: "villain", Stack: 100}},
		Order: []string{"bot", "villain"}, HeroCards: poker.MustParseCards("AsKd"),
	}
	request, err := provider.strategyRequest(DecisionInput{State: state, DecisionID: "d1"})
	if err != nil {
		t.Fatal(err)
	}
	if request.AIProfile != "TAG_L2" || request.PersonalityID != "tag" || request.Level != 2 || request.TargetVPIP != .30 || request.TargetPFR != .15 || len(request.PostflopSizings) != 2 || request.PostflopSizings[1] != .66 {
		t.Fatalf("first request=%+v", request)
	}
	state.HandID, state.AIProfile = "hand-2", "LAG_L5"
	request, err = provider.strategyRequest(DecisionInput{State: state, DecisionID: "d2"})
	if err != nil {
		t.Fatal(err)
	}
	if request.AIProfile != "LAG_L5" || request.PersonalityID != "lag" || request.Level != 5 || request.TargetVPIP != .45 || request.TargetPFR != .25 {
		t.Fatalf("second request=%+v", request)
	}
}

func TestStrategyRequestUsesSpecialProfilePerHand(t *testing.T) {
	cfg := config.Default()
	cfg.Engine.Personality.Profiles["FPCH_100_50"] = config.BotProfileConfig{
		Personality: "lag", Level: 5, TargetVPIP: 1, TargetPFR: .5,
		BehaviorMode: "aggressive_never_fold", PreflopRaiseProbability: .5,
		PostflopAggressionChance: .75, NeverFold: true, AuditExempt: true,
	}
	provider := NewEngineDecisionProvider(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	state := &game.State{
		HeroID: "bot", TableID: "table", HandID: "hand", GameType: "NLH", AIProfile: "FPCH_100_50", Street: game.Preflop,
		BigBlind: 2, Pot: 3, Players: map[string]*game.Player{"bot": {ID: "bot", Stack: 100}, "villain": {ID: "villain", Stack: 100}},
		Order: []string{"bot", "villain"}, HeroCards: poker.MustParseCards("7s2d"),
	}
	request, err := provider.strategyRequest(DecisionInput{State: state, DecisionID: "special"})
	if err != nil {
		t.Fatal(err)
	}
	if request.BehaviorMode != "aggressive_never_fold" || request.PreflopRaiseProbability != .5 || request.PostflopAggressionChance != .75 || !request.NeverFold || !request.AuditExempt {
		t.Fatalf("request=%+v", request)
	}
}

func TestStrategyRequestNormalizesFloatingPointToCallResidue(t *testing.T) {
	provider := NewEngineDecisionProvider(config.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	state := &game.State{
		HeroID: "bot", TableID: "table", HandID: "hand", GameType: "NLH", Street: game.Preflop,
		BigBlind: 0.1, CurrentBet: 0.3, Pot: 0.45,
		Players: map[string]*game.Player{
			"bot":     {ID: "bot", Stack: 100, StreetContribution: 0.29999999999999993},
			"villain": {ID: "villain", Stack: 100, StreetContribution: 0.3},
		},
		Order: []string{"bot", "villain"}, HeroCards: poker.MustParseCards("7s2d"),
	}
	request, err := provider.strategyRequest(DecisionInput{State: state, DecisionID: "residue"})
	if err != nil {
		t.Fatal(err)
	}
	if request.ToCall != 0 {
		t.Fatalf("to_call=%v, want exact zero", request.ToCall)
	}
	if len(request.LegalActions) == 0 || request.LegalActions[0].Action != strategy.Check {
		t.Fatalf("legal_actions=%+v, want Check first", request.LegalActions)
	}
	for _, action := range request.LegalActions {
		if action.Action == strategy.Fold || action.Action == strategy.Call {
			t.Fatalf("legal_actions=%+v contain paid-response action for a free option", request.LegalActions)
		}
	}
}

func TestInferLegalActionsCannotRaiseAgainstLoneAllInOpponent(t *testing.T) {
	state := &game.State{
		HeroID: "bot", Street: game.Flop, BigBlind: 20, CurrentBet: 2560, LastFullRaise: 1960,
		Players: map[string]*game.Player{
			"bot":     {ID: "bot", Stack: 2550, StreetContribution: 1620},
			"villain": {ID: "villain", Stack: 0, StreetContribution: 2560, AllIn: true},
			"folded":  {ID: "folded", Stack: 100, Folded: true},
		},
	}
	actions := inferLegalActions(state, 1)
	if len(actions) != 2 || actions[0].Action != strategy.Fold || actions[1].Action != strategy.Call {
		t.Fatalf("facing lone all-in actions=%+v", actions)
	}
	state.CurrentBet = state.Players["bot"].StreetContribution
	actions = inferLegalActions(state, 1)
	if len(actions) != 1 || actions[0].Action != strategy.Check {
		t.Fatalf("checked-through all-in actions=%+v", actions)
	}
}

func TestConstrainToServerLegalActionsUsesIncrement(t *testing.T) {
	state := &game.State{HeroID: "bot", Players: map[string]*game.Player{"bot": {ID: "bot", StreetContribution: 10}}, LegalActions: []protocol.LegalAction{
		{Type: protocol.ActionFold, Min: 0, Max: 0}, {Type: protocol.ActionCall, Min: 10, Max: 10},
	}}
	increment := 10.0
	advice := constrainToLegalActions(&protocol.AdviseResponse{Type: protocol.ActionCall, Value: &increment}, state)
	if advice.Type != protocol.ActionCall || advice.Value == nil || *advice.Value != 10 || advice.ValueMode != "increment" {
		t.Fatalf("advice=%+v", advice)
	}
	advice = constrainToLegalActions(&protocol.AdviseResponse{Type: protocol.ActionRaise, Value: &increment}, state)
	if advice.Type != protocol.ActionCall || advice.Value == nil || *advice.Value != 10 {
		t.Fatalf("fallback advice=%+v", advice)
	}
}

func TestConstrainToServerLegalActionsConvertsFreeFoldToCheck(t *testing.T) {
	state := &game.State{LegalActions: []protocol.LegalAction{
		{Type: protocol.ActionFold},
		{Type: protocol.ActionCheck},
		{Type: protocol.ActionRaise, Min: 2, Max: 100},
	}}
	advice := constrainToLegalActions(&protocol.AdviseResponse{Type: protocol.ActionFold}, state)
	if advice.Type != protocol.ActionCheck || advice.Value != nil || advice.ValueMode != "increment" {
		t.Fatalf("advice=%+v", advice)
	}
}

func TestConstrainNeverFoldUsesAllInInsteadOfFold(t *testing.T) {
	state := &game.State{LegalActions: []protocol.LegalAction{
		{Type: protocol.ActionFold},
		{Type: protocol.ActionAllIn, Min: .01, Max: .01},
	}}
	advice := constrainNeverFoldToLegalActions(state)
	if advice == nil || advice.Type != protocol.ActionAllIn || advice.Value == nil || *advice.Value != .01 {
		t.Fatalf("advice=%+v", advice)
	}
}

func TestCollapseConstrainedNearAllInUsesServerAllInAmount(t *testing.T) {
	state := &game.State{LegalActions: []protocol.LegalAction{
		{Type: protocol.ActionCheck},
		{Type: protocol.ActionCall, Min: 119.99, Max: 119.99},
		{Type: protocol.ActionBet, Min: 20, Max: 119.99},
		{Type: protocol.ActionRaise, Min: 20, Max: 119.99},
		{Type: protocol.ActionAllIn, Min: 120, Max: 120},
	}}
	for _, actionType := range []protocol.ActionType{protocol.ActionCall, protocol.ActionBet, protocol.ActionRaise} {
		value := 119.99
		advice := collapseConstrainedNearAllIn(&protocol.AdviseResponse{Type: actionType, Value: &value}, state, true, .01)
		if advice.Type != protocol.ActionAllIn || advice.Value == nil || *advice.Value != 120 || advice.ValueMode != "increment" {
			t.Fatalf("%s advice=%+v", actionType, advice)
		}
	}

	value := 119.98
	advice := collapseConstrainedNearAllIn(&protocol.AdviseResponse{Type: protocol.ActionRaise, Value: &value}, state, true, .01)
	if advice.Type != protocol.ActionRaise || advice.Value == nil || *advice.Value != 119.98 {
		t.Fatalf("outside threshold advice=%+v", advice)
	}

	value = 119.99
	advice = collapseConstrainedNearAllIn(&protocol.AdviseResponse{Type: protocol.ActionRaise, Value: &value}, state, false, .01)
	if advice.Type != protocol.ActionRaise {
		t.Fatalf("disabled advice=%+v", advice)
	}
}

func TestCollapseConstrainedNearAllInUsesCallerPercentage(t *testing.T) {
	state := &game.State{NearAllInCallPercent: 10, LegalActions: []protocol.LegalAction{
		{Type: protocol.ActionCall, Min: 346.7, Max: 346.7},
		{Type: protocol.ActionAllIn, Min: 355, Max: 355},
	}}
	value := 346.7
	advice := collapseConstrainedNearAllIn(&protocol.AdviseResponse{Type: protocol.ActionCall, Value: &value}, state, true, .01)
	if advice.Type != protocol.ActionAllIn || advice.Value == nil || *advice.Value != 355 {
		t.Fatalf("2.34%% tail advice=%+v", advice)
	}
	state.NearAllInCallPercent = 2
	advice = collapseConstrainedNearAllIn(&protocol.AdviseResponse{Type: protocol.ActionCall, Value: &value}, state, true, .01)
	if advice.Type != protocol.ActionCall {
		t.Fatalf("percentage below 2.34%% must not collapse: %+v", advice)
	}
}

func TestFinalizeAdviceReportsNearAllInCollapseMetadata(t *testing.T) {
	state := &game.State{NearAllInCallPercent: 10, LegalActions: []protocol.LegalAction{
		{Type: protocol.ActionCall, Min: 346.7, Max: 346.7},
		{Type: protocol.ActionAllIn, Min: 355, Max: 355},
	}}
	value := 346.7
	advice, collapsed, originalAction, originalAmount := finalizeAdviceWithCollapse(
		&protocol.AdviseResponse{Type: protocol.ActionCall, Value: &value}, state, false, true, .01,
	)
	if !collapsed || originalAction != protocol.ActionCall || originalAmount != 346.7 || advice.Type != protocol.ActionAllIn {
		t.Fatalf("advice=%+v collapsed=%v original=%s/%v", advice, collapsed, originalAction, originalAmount)
	}
}

func TestFinalizeAdviceCollapsesNeverFoldFallbackCall(t *testing.T) {
	state := &game.State{LegalActions: []protocol.LegalAction{
		{Type: protocol.ActionFold},
		{Type: protocol.ActionCall, Min: 99.99, Max: 99.99},
		{Type: protocol.ActionAllIn, Min: 100, Max: 100},
	}}
	advice := finalizeAdvice(&protocol.AdviseResponse{Type: protocol.ActionFold}, state, true, true, .01)
	if advice == nil || advice.Type != protocol.ActionAllIn || advice.Value == nil || *advice.Value != 100 || advice.ValueMode != "increment" {
		t.Fatalf("advice=%+v", advice)
	}
}
