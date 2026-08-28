package game

import (
	"encoding/json"
	"math"
	"testing"

	"gitlab.com/smoothsics/ainp/internal/protocol"
)

func TestStateTracksTurnOrderBetsAndBoard(t *testing.T) {
	state, err := New(gameEvent(1, "bot", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 1, "big_blind": 2,
		"time_to_act": 12000, "max_seat": 4,
		"players": []map[string]any{
			{"player_id": "sb", "nick": "sb", "stack": 100, "role": "sb"},
			{"player_id": "bb", "nick": "bb", "stack": 100, "role": "bb"},
			{"player_id": "bot", "nick": "bot", "stack": 100},
			{"player_id": "btn", "nick": "btn", "stack": 100, "role": "bt"},
		},
		"blinds": []map[string]any{{"player_id": "sb", "value": 1, "type": "small_blind"}, {"player_id": "bb", "value": 2, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if state.AIProfile != "tag" || state.Pot != 3 || state.NextToAct() != "bot" {
		t.Fatalf("initial state=%+v next=%s", state, state.NextToAct())
	}
	if err := state.Apply(gameEvent(2, "bot", protocol.CommandDealCards, map[string]any{"cards": "AsAh"})); err != nil {
		t.Fatal(err)
	}
	if !state.ShouldAdvise() {
		t.Fatal("hero should act after deal")
	}
	if err := state.Apply(gameEvent(3, "bot", protocol.CommandAction, map[string]any{"player_id": "bot", "type": "raise", "value": 6})); err != nil {
		t.Fatal(err)
	}
	if !state.Players["bot"].PreflopVPIP || !state.Players["bot"].PreflopPFR || state.Pot != 9 || state.CurrentBet != 6 || state.NextToAct() != "btn" {
		t.Fatalf("after hero raise pot=%v bet=%v next=%s", state.Pot, state.CurrentBet, state.NextToAct())
	}
	if err := state.Apply(gameEvent(4, "bot", protocol.CommandAction, map[string]any{"player_id": "btn", "type": "raise", "value": 12})); err != nil {
		t.Fatal(err)
	}
	if state.NextToAct() != "sb" {
		t.Fatalf("raise must rotate after actor, next=%s", state.NextToAct())
	}
	if err := state.Apply(gameEvent(5, "bot", protocol.CommandFlop, map[string]any{"cards": "2c3d7h"})); err != nil {
		t.Fatal(err)
	}
	if state.Street != Flop || len(state.Board) != 3 || state.CurrentBet != 0 || state.NextToAct() != "sb" {
		t.Fatalf("flop state=%+v", state)
	}
	if err := state.Apply(gameEvent(6, "bot", protocol.CommandTurn, map[string]any{"cards": "2c3d7hKs"})); err != nil {
		t.Fatal(err)
	}
	if len(state.Board) != 4 || state.Board[3].String() != "Ks" {
		t.Fatalf("turn board=%v", state.Board)
	}
}

func TestStateComparesPendingAdvice(t *testing.T) {
	state, err := New(gameEvent(1, "bot", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "balanced", "time": 1, "small_blind": 1, "big_blind": 2,
		"time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "bot", "nick": "bot", "stack": 100, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 100, "role": "bb"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	value := 2.0
	state.PendingAdvice = &protocol.AdviseResponse{Type: protocol.ActionCall, Value: &value}
	if err := state.Apply(gameEvent(2, "bot", protocol.CommandAction, map[string]any{"player_id": "bot", "type": "fold", "value": 0})); err != nil {
		t.Fatal(err)
	}
	if state.Deviation == nil || state.Deviation.ByType == nil || state.Deviation.ByValue == nil {
		t.Fatalf("deviation=%+v", state.Deviation)
	}
}

func TestHeadsUpBigBlindActsFirstPostflop(t *testing.T) {
	state, err := New(gameEvent(1, "bb", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 1, "big_blind": 2,
		"time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 100, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 100, "role": "bb"}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 1, "type": "small_blind"}, {"player_id": "bb", "value": 2, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if state.NextToAct() != "sb" {
		t.Fatalf("preflop next=%s", state.NextToAct())
	}
	if err := state.Apply(gameEvent(2, "bb", protocol.CommandDealCards, map[string]any{"cards": "AsKd"})); err != nil {
		t.Fatal(err)
	}
	if err := state.Apply(gameEvent(3, "bb", protocol.CommandAction, map[string]any{"player_id": "sb", "type": "call", "value": 1})); err != nil {
		t.Fatal(err)
	}
	if err := state.Apply(gameEvent(4, "bb", protocol.CommandAction, map[string]any{"player_id": "bb", "type": "check", "value": 0})); err != nil {
		t.Fatal(err)
	}
	if err := state.Apply(gameEvent(5, "bb", protocol.CommandFlop, map[string]any{"cards": "2c3d7h"})); err != nil {
		t.Fatal(err)
	}
	if state.NextToAct() != "bb" || !state.ShouldAdvise() {
		t.Fatalf("postflop next=%s advise=%t", state.NextToAct(), state.ShouldAdvise())
	}
	if err := state.Apply(gameEvent(6, "bb", protocol.CommandAction, map[string]any{"player_id": "bb", "type": "check", "value": 0})); err != nil {
		t.Fatal(err)
	}
}

func TestActionStreetTotalIsNormalizedToIncrement(t *testing.T) {
	state, err := New(gameEvent(1, "bb", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20,
		"time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 1000, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 1000, "role": "bb"}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Apply(gameEvent(2, "bb", protocol.CommandAction, map[string]any{"player_id": "sb", "type": "call", "value": 20, "value_mode": "street_total"})); err != nil {
		t.Fatal(err)
	}
	if got := state.Players["sb"].StreetContribution; got != 20 {
		t.Fatalf("street contribution=%v want 20", got)
	}
	if got := state.Players["sb"].Stack; got != 980 {
		t.Fatalf("stack=%v want 980", got)
	}
}

func TestStreetTotalAdviceComparesAgainstRawServerAmount(t *testing.T) {
	state, err := New(gameEvent(1, "sb", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 1000, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 1000, "role": "bb"}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	value := 20.0
	state.PendingAdvice = &protocol.AdviseResponse{Type: protocol.ActionCall, Value: &value, ValueMode: "street_total"}
	if err := state.Apply(gameEvent(2, "sb", protocol.CommandAction, map[string]any{"player_id": "sb", "type": "call", "value": 20, "value_mode": "street_total"})); err != nil {
		t.Fatal(err)
	}
	if state.Deviation != nil {
		t.Fatalf("deviation=%+v", state.Deviation)
	}
}

func TestServerAuthoritativeActionUsesStackAfterAndNextPlayer(t *testing.T) {
	state, err := New(gameEvent(1, "bot", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 3,
		"players": []map[string]any{
			{"player_id": "sb", "nick": "sb", "stack": 1000, "role": "sb"},
			{"player_id": "bb", "nick": "bb", "stack": 1000, "role": "bb"},
			{"player_id": "bot", "nick": "bot", "stack": 1000},
		},
		"blinds": []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	// The locally inferred actor is bot. The integration can nevertheless use
	// the server event as authority and derive the 10-chip call from stack_after,
	// irrespective of the legacy value field's meaning.
	if got := state.NextToAct(); got != "bot" {
		t.Fatalf("inferred next=%s", got)
	}
	if err := state.Apply(gameEvent(2, "bot", protocol.CommandAction, map[string]any{
		"player_id": "sb", "type": "call", "value": 10, "value_mode": "street_total",
		"stack_after": 980, "next_player_id": "bot",
	})); err != nil {
		t.Fatal(err)
	}
	if got := state.Players["sb"].StreetContribution; got != 20 {
		t.Fatalf("street contribution=%v want 20", got)
	}
	if got := state.Players["sb"].Stack; got != 980 {
		t.Fatalf("stack=%v want 980", got)
	}
	if state.NextToAct() != "bot" {
		t.Fatalf("authoritative next=%s", state.NextToAct())
	}
	if err := state.Apply(gameEvent(3, "bot", protocol.CommandDealCards, map[string]any{
		"cards": "AsKd", "next_player_id": "bot",
	})); err != nil {
		t.Fatal(err)
	}
	if !state.ShouldAdvise() {
		t.Fatal("server-authoritative hero turn should advise")
	}
}

func TestFreeBigBlindCallWithStreetTotalIsNormalizedToCheck(t *testing.T) {
	state, err := New(gameEvent(1, "bb", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 100, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 100, "role": "bb"}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	beforePot := state.Pot
	callValue := 20.0
	state.PendingAdvice = &protocol.AdviseResponse{Type: protocol.ActionCall, Value: &callValue, ValueMode: "increment"}
	if err := state.Apply(gameEvent(2, "bb", protocol.CommandAction, map[string]any{
		"player_id": "bb", "type": "call", "value": 20, "value_mode": "increment", "stack_after": 80, "next_player_id": "",
	})); err != nil {
		t.Fatal(err)
	}
	bb := state.Players["bb"]
	if bb.Stack != 80 || bb.StreetContribution != 20 || state.Pot != beforePot {
		t.Fatalf("free option changed chips: player=%+v pot=%v want pot=%v", bb, state.Pot, beforePot)
	}
	if state.LastObservation == nil || state.LastObservation.Action != protocol.ActionCheck || state.LastObservation.Voluntary {
		t.Fatalf("observation=%+v, want non-voluntary check", state.LastObservation)
	}
	if state.Deviation != nil {
		t.Fatalf("deviation=%+v, want wire-level advice match", state.Deviation)
	}
	if state.LastNormalization != "free_call_street_total" {
		t.Fatalf("normalization=%q", state.LastNormalization)
	}
}

func TestServerAuthoritativeAllInIgnoresAmbiguousLegacyValue(t *testing.T) {
	state, err := New(gameEvent(1, "sb", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 100, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 100, "role": "bb"}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Apply(gameEvent(2, "sb", protocol.CommandAction, map[string]any{
		"player_id": "sb", "type": "allin", "value": 100, "value_mode": "street_total", "stack_after": 0, "next_player_id": "bb",
	})); err != nil {
		t.Fatal(err)
	}
	if !state.Players["sb"].AllIn || state.Players["sb"].Stack != 0 || state.Players["sb"].StreetContribution != 100 {
		t.Fatalf("all-in player=%+v", state.Players["sb"])
	}
}

func TestZeroCostActionToleratesStackRoundingAndHasNoValueDeviation(t *testing.T) {
	state, err := New(gameEvent(1, "sb", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 0.1, "big_blind": 0.2, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 2.3, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 2.3, "role": "bb"}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 0.1, "type": "small_blind"}, {"player_id": "bb", "value": 0.2, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	state.PendingAdvice = &protocol.AdviseResponse{Type: protocol.ActionFold, ValueMode: "street_total"}
	if err := state.Apply(gameEvent(2, "sb", protocol.CommandAction, map[string]any{
		"player_id": "sb", "type": "fold", "value": 0, "value_mode": "street_total",
		"stack_after": 2.1999999999999997, "next_player_id": "bb",
	})); err != nil {
		t.Fatal(err)
	}
	if state.Deviation != nil {
		t.Fatalf("zero-cost action deviation=%+v", state.Deviation)
	}
}

func TestIncrementAdviceComparesWithStackDerivedActionIncrement(t *testing.T) {
	state, err := New(gameEvent(1, "sb", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 0.1, "big_blind": 0.2, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 2.3, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 2.3, "role": "bb"}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 0.1, "type": "small_blind"}, {"player_id": "bb", "value": 0.2, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	call := 0.1
	state.PendingAdvice = &protocol.AdviseResponse{Type: protocol.ActionCall, Value: &call, ValueMode: "increment"}
	if err := state.Apply(gameEvent(2, "sb", protocol.CommandAction, map[string]any{
		"player_id": "sb", "type": "call", "value": 0.2, "value_mode": "street_total",
		"stack_after": 2.1, "next_player_id": "bb",
	})); err != nil {
		t.Fatal(err)
	}
	if state.Deviation != nil {
		t.Fatalf("increment action deviation=%+v", state.Deviation)
	}
}

func TestShortBigBlindKeepsFullPreflopBringIn(t *testing.T) {
	state, err := New(gameEvent(1, "btn", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 3,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 100, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 9.99, "role": "bb"}, {"player_id": "btn", "nick": "btn", "stack": 100, "role": "bt"}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 9.99, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentBet != 20 || math.Abs(state.Pot-19.99) > 1e-9 {
		t.Fatalf("current_bet=%v pot=%v", state.CurrentBet, state.Pot)
	}
	if err := state.Apply(gameEvent(2, "btn", protocol.CommandAction, map[string]any{
		"player_id": "btn", "type": "call", "value": 20, "value_mode": "increment", "stack_after": 80, "next_player_id": "sb",
	})); err != nil {
		t.Fatal(err)
	}
	if state.Players["btn"].StreetContribution != 20 {
		t.Fatalf("button=%+v", state.Players["btn"])
	}
}

func TestLegacyMislabeledIncrementAndOmittedAnteAreReconciled(t *testing.T) {
	state, err := New(gameEvent(1, "hero", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 3,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 200, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 200, "role": "bb"}, {"player_id": "hero", "nick": "hero", "stack": 200}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Apply(gameEvent(2, "hero", protocol.CommandDealCards, map[string]any{
		"cards": "AsKd", "next_player_id": "hero",
		"legal_actions": []map[string]any{{"type": "check", "min": 0, "max": 0}, {"type": "fold", "min": 0, "max": 0}, {"type": "raise", "min": 20, "max": 179.99}, {"type": "allin", "min": 180, "max": 180}},
	})); err != nil {
		t.Fatal(err)
	}
	if state.Players["hero"].Stack != 180 || state.Players["hero"].StreetContribution != 20 || state.Pot != 50 {
		t.Fatalf("reconciled hero=%+v pot=%v", state.Players["hero"], state.Pot)
	}
	if err := state.Apply(gameEvent(3, "hero", protocol.CommandAction, map[string]any{
		"player_id": "hero", "type": "fold", "value": 0, "value_mode": "street_total", "stack_after": 180, "next_player_id": "sb",
	})); err != nil {
		t.Fatal(err)
	}

	sbState, err := New(gameEvent(1, "sb", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 2,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 100, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 100, "role": "bb"}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	// Older pokerbot/ainp versions sent the increment (10) but labelled it
	// street_total. stack_after proves that another 10 chips were spent.
	if err := sbState.Apply(gameEvent(2, "sb", protocol.CommandAction, map[string]any{
		"player_id": "sb", "type": "call", "value": 10, "value_mode": "street_total", "stack_after": 80, "next_player_id": "bb",
	})); err != nil {
		t.Fatal(err)
	}
	if sbState.Players["sb"].StreetContribution != 20 || sbState.Players["sb"].Stack != 80 {
		t.Fatalf("legacy call=%+v", sbState.Players["sb"])
	}
}

func TestActionInfersOmittedForcedStreetContribution(t *testing.T) {
	state, err := New(gameEvent(1, "hero", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 3,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 200, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 200, "role": "bb"}, {"player_id": "hero", "nick": "hero", "stack": 200}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Apply(gameEvent(2, "hero", protocol.CommandAction, map[string]any{
		"player_id": "hero", "type": "check", "value": 0, "value_mode": "street_total", "stack_after": 180, "next_player_id": "sb",
	})); err != nil {
		t.Fatal(err)
	}
	if state.Players["hero"].StreetContribution != 20 || state.Players["hero"].Stack != 180 {
		t.Fatalf("check=%+v", state.Players["hero"])
	}

	callState, err := New(gameEvent(1, "hero", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 3,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 200, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 200, "role": "bb"}, {"player_id": "hero", "nick": "hero", "stack": 200}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	callState.CurrentBet = 80
	if err := callState.Apply(gameEvent(2, "hero", protocol.CommandAction, map[string]any{
		"player_id": "hero", "type": "call", "value": 60, "value_mode": "increment", "stack_after": 120, "next_player_id": "sb",
	})); err != nil {
		t.Fatal(err)
	}
	if callState.Players["hero"].StreetContribution != 80 || callState.Players["hero"].Stack != 120 {
		t.Fatalf("call=%+v", callState.Players["hero"])
	}

	raiseState, err := New(gameEvent(1, "hero", protocol.CommandStartHand, map[string]any{
		"game_type": "NLH", "club_id": "1", "ai_profile": "tag", "time": 1, "small_blind": 10, "big_blind": 20, "time_to_act": 12000, "max_seat": 3,
		"players": []map[string]any{{"player_id": "sb", "nick": "sb", "stack": 200, "role": "sb"}, {"player_id": "bb", "nick": "bb", "stack": 200, "role": "bb"}, {"player_id": "hero", "nick": "hero", "stack": 200}},
		"blinds":  []map[string]any{{"player_id": "sb", "value": 10, "type": "small_blind"}, {"player_id": "bb", "value": 20, "type": "big_blind"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := raiseState.Apply(gameEvent(2, "hero", protocol.CommandAction, map[string]any{
		"player_id": "hero", "type": "raise", "value": 80, "value_mode": "increment", "stack_after": 100, "next_player_id": "sb",
	})); err != nil {
		t.Fatal(err)
	}
	if raiseState.Players["hero"].StreetContribution != 100 || raiseState.CurrentBet != 100 || raiseState.Players["hero"].Stack != 100 {
		t.Fatalf("raise=%+v current_bet=%v", raiseState.Players["hero"], raiseState.CurrentBet)
	}
}

func gameEvent(sequence uint64, hero string, command protocol.Command, payload any) protocol.EventRequest {
	handID := "hand"
	body, _ := json.Marshal(payload)
	return protocol.EventRequest{SeqNum: sequence, PlayerID: hero, RoomID: "fishcn", TableID: "table", HandID: &handID, Cmd: command, Payload: body}
}
