package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"gitlab.com/smoothsics/ainp/internal/poker"
	"gitlab.com/smoothsics/ainp/internal/protocol"
)

var ErrInvalidTransition = errors.New("invalid game-state transition")

type Street string

const (
	Preflop Street = "preflop"
	Flop    Street = "flop"
	Turn    Street = "turn"
	River   Street = "river"
)

type Player struct {
	ID                 string
	Role               string
	InitialStack       float64
	Stack              float64
	StreetContribution float64
	TotalContribution  float64
	Folded             bool
	AllIn              bool
	Acted              bool
	PreflopActed       bool
	PreflopVPIP        bool
	PreflopPFR         bool
	PostflopCalls      int
	PostflopAggro      int
	Cards              []poker.Card
}

type Observation struct {
	ObservationID       string
	PlayerID            string
	HandID              string
	Street              Street
	Action              protocol.ActionType
	Voluntary           bool
	PreflopOpportunity  bool
	PFROpportunity      bool
	ThreeBetOpportunity bool
	CBetOpportunity     bool
	FacingCBet          bool
}

type State struct {
	HeroID                 string
	RoomID                 string
	TableID                string
	HandID                 string
	GameType               string
	AIProfile              string
	SmallBlind             float64
	BigBlind               float64
	Ante                   float64
	Street                 Street
	Players                map[string]*Player
	Order                  []string
	HeroCards              []poker.Card
	Board                  []poker.Card
	Pot                    float64
	CurrentBet             float64
	LastFullRaise          float64
	Raises                 int
	PreflopAggressor       string
	CurrentStreetAggressor string
	LastActor              string
	Ended                  bool
	LastObservation        *Observation
	LastNormalization      string
	PendingAdvice          *protocol.AdviseResponse
	Deviation              *protocol.DeviationResponse
	LegalActions           []protocol.LegalAction
	NearAllInCallPercent   float64
	NearAllInRaisePercent  float64
	// AuthoritativeNext is non-nil when the caller supplies the server's next
	// actor. An empty value means the betting round currently has no next actor.
	AuthoritativeNext *string
}

func New(req protocol.EventRequest) (*State, error) {
	if req.Cmd != protocol.CommandStartHand || req.HandID == nil {
		return nil, fmt.Errorf("%w: first event must start a hand", ErrInvalidTransition)
	}
	var payload protocol.StartHandPayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return nil, fmt.Errorf("%w: decode start hand: %v", ErrInvalidTransition, err)
	}
	state := &State{
		HeroID: req.PlayerID, RoomID: req.RoomID, TableID: req.TableID, HandID: *req.HandID,
		GameType: strings.ToUpper(strings.TrimSpace(payload.GameType)), AIProfile: payload.AIProfile,
		SmallBlind: payload.SmallBlind, BigBlind: payload.BigBlind, Street: Preflop,
		NearAllInCallPercent: payload.NearAllInCallPercent, NearAllInRaisePercent: payload.NearAllInRaisePercent,
		Players: make(map[string]*Player, len(payload.Players)), LastFullRaise: payload.BigBlind,
	}
	if state.BigBlind <= 0 {
		return nil, fmt.Errorf("%w: big_blind must be greater than zero", ErrInvalidTransition)
	}
	for _, item := range payload.Players {
		if item.PlayerID == "" || item.Stack < 0 {
			return nil, fmt.Errorf("%w: invalid player", ErrInvalidTransition)
		}
		if _, exists := state.Players[item.PlayerID]; exists {
			return nil, fmt.Errorf("%w: duplicate player %q", ErrInvalidTransition, item.PlayerID)
		}
		role := ""
		if item.Role != nil {
			role = strings.ToLower(*item.Role)
		}
		state.Players[item.PlayerID] = &Player{ID: item.PlayerID, Role: role, InitialStack: item.Stack, Stack: item.Stack}
		state.Order = append(state.Order, item.PlayerID)
	}
	if state.Players[state.HeroID] == nil {
		return nil, fmt.Errorf("%w: hero is not present at table", ErrInvalidTransition)
	}
	if payload.Ante != nil {
		state.Ante = *payload.Ante
	}
	if len(payload.Antes) > 0 {
		for _, ante := range payload.Antes {
			if err := state.postForced(ante.PlayerID, ante.Value, false); err != nil {
				return nil, err
			}
		}
	} else if state.Ante > 0 {
		for _, playerID := range state.Order {
			if err := state.postForced(playerID, state.Ante, false); err != nil {
				return nil, err
			}
		}
	}
	if len(payload.Blinds) > 0 {
		for _, blind := range payload.Blinds {
			if err := state.postForced(blind.PlayerID, blind.Value, true); err != nil {
				return nil, err
			}
		}
	} else {
		for _, playerID := range state.Order {
			player := state.Players[playerID]
			value := 0.0
			switch player.Role {
			case "sb":
				value = payload.SmallBlind
			case "bb":
				value = payload.BigBlind
			case "st":
				if payload.Straddle != nil {
					value = *payload.Straddle
				}
			}
			if value > 0 {
				if err := state.postForced(playerID, value, true); err != nil {
					return nil, err
				}
			}
		}
	}
	// A short all-in straddle still establishes the table's full preflop
	// bring-in. pokerbot reports the chips actually posted (for example 5, 10,
	// 20, 30 or 35 with a 20 big blind), while subsequent Call events use the
	// normal 2BB straddle target of 40. Treating the short post as CurrentBet
	// makes every following server-authoritative Call look too large.
	hasShortAllInStraddle := false
	for _, player := range state.Players {
		if player.Role == "st" && player.AllIn {
			hasShortAllInStraddle = true
			break
		}
	}
	if !hasShortAllInStraddle {
		for _, blind := range payload.Blinds {
			player := state.Players[blind.PlayerID]
			if blind.Type == "straddle" && player != nil && player.AllIn {
				hasShortAllInStraddle = true
				break
			}
		}
	}
	if hasShortAllInStraddle {
		state.CurrentBet = math.Max(state.CurrentBet, 2*state.BigBlind)
	}
	// A short all-in big blind does not reduce the preflop bring-in. Players
	// yet to act still face the configured full big blind even when the posted
	// blind was smaller because that player had insufficient chips.
	state.CurrentBet = math.Max(state.CurrentBet, state.BigBlind)
	state.LastFullRaise = math.Max(state.BigBlind, state.CurrentBet)
	for _, player := range state.Players {
		if player.Stack <= 1e-9 {
			player.Stack, player.AllIn, player.Acted = 0, true, true
		}
	}
	return state, nil
}

func (s *State) Apply(req protocol.EventRequest) error {
	s.LastObservation = nil
	s.LastNormalization = ""
	s.Deviation = nil
	if s.Ended && req.Cmd != protocol.CommandEndHand {
		return fmt.Errorf("%w: hand already ended", ErrInvalidTransition)
	}
	switch req.Cmd {
	case protocol.CommandDealCards:
		if len(s.HeroCards) > 0 {
			return fmt.Errorf("%w: hero cards already dealt", ErrInvalidTransition)
		}
		var payload protocol.CardsPayload
		_ = json.Unmarshal(req.Payload, &payload)
		cards, err := poker.ParseCards(payload.Cards)
		if err != nil {
			return fmt.Errorf("%w: hero cards: %v", ErrInvalidTransition, err)
		}
		s.HeroCards = cards
		s.Players[s.HeroID].Cards = append([]poker.Card(nil), cards...)
		if payload.LegalActions != nil {
			s.LegalActions = append([]protocol.LegalAction(nil), (*payload.LegalActions)...)
			s.reconcileHeroStackFromLegalActions()
		}
		s.setAuthoritativeNext(payload.NextPlayerID)
	case protocol.CommandFlop, protocol.CommandTurn, protocol.CommandRiver:
		if err := s.applyBoard(req); err != nil {
			return err
		}
	case protocol.CommandAction:
		hero := s.Players[s.HeroID]
		if hero == nil || !hero.Folded {
			if err := s.applyAction(req); err != nil {
				return err
			}
			break
		}
		// A folded hero can receive an incomplete spectator stream: some rooms
		// omit actions or public-card callbacks after that player leaves the pot,
		// then resume broadcasting later actions. Preserve strict validation while
		// the hero is active, but do not break an already decision-inactive stream.
		next := s.Clone()
		if err := next.applyAction(req); err == nil {
			*s = *next
			break
		}
		var payload protocol.ActionPayload
		_ = json.Unmarshal(req.Payload, &payload)
		s.LastObservation, s.Deviation, s.PendingAdvice = nil, nil, nil
		s.LegalActions = nil
		s.setAuthoritativeNext(payload.NextPlayerID)
		s.LastNormalization = "folded_hero_stale_action"
	case protocol.CommandShowCards:
		var payload protocol.ShowCardsPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return fmt.Errorf("%w: show cards payload", ErrInvalidTransition)
		}
		player := s.Players[payload.PlayerID]
		if player == nil {
			return fmt.Errorf("%w: unknown player %q", ErrInvalidTransition, payload.PlayerID)
		}
		cards, err := poker.ParseCards(payload.Cards)
		if err != nil {
			return fmt.Errorf("%w: shown cards: %v", ErrInvalidTransition, err)
		}
		player.Cards = cards
	case protocol.CommandEndHand:
		if s.Ended {
			return fmt.Errorf("%w: hand already ended", ErrInvalidTransition)
		}
		s.Ended = true
	case protocol.CommandStartHand:
		return fmt.Errorf("%w: duplicate start command", ErrInvalidTransition)
	}
	return nil
}

func (s *State) applyBoard(req protocol.EventRequest) error {
	var payload protocol.CardsPayload
	_ = json.Unmarshal(req.Payload, &payload)
	cards, err := poker.ParseCards(payload.Cards)
	if err != nil {
		return fmt.Errorf("%w: board cards: %v", ErrInvalidTransition, err)
	}
	target, street := 0, Preflop
	switch req.Cmd {
	case protocol.CommandFlop:
		if s.Street != Preflop {
			return fmt.Errorf("%w: flop cannot follow %s", ErrInvalidTransition, s.Street)
		}
		target, street = 3, Flop
	case protocol.CommandTurn:
		if s.Street != Flop {
			return fmt.Errorf("%w: turn cannot follow %s", ErrInvalidTransition, s.Street)
		}
		target, street = 4, Turn
	case protocol.CommandRiver:
		if s.Street != Turn {
			return fmt.Errorf("%w: river cannot follow %s", ErrInvalidTransition, s.Street)
		}
		target, street = 5, River
	}
	if len(cards) == target {
		if len(s.Board) > 0 && !sameCards(cards[:len(s.Board)], s.Board) {
			return fmt.Errorf("%w: board prefix changed", ErrInvalidTransition)
		}
		s.Board = cards
	} else if len(cards) == target-len(s.Board) {
		s.Board = append(s.Board, cards...)
	} else {
		return fmt.Errorf("%w: %s requires %d total board cards", ErrInvalidTransition, street, target)
	}
	if hasDuplicates(s.HeroCards, s.Board) {
		return fmt.Errorf("%w: duplicate hero and board card", ErrInvalidTransition)
	}
	s.Street = street
	if payload.LegalActions != nil {
		s.LegalActions = append([]protocol.LegalAction(nil), (*payload.LegalActions)...)
	} else {
		s.LegalActions = nil
	}
	s.setAuthoritativeNext(payload.NextPlayerID)
	s.CurrentBet, s.LastFullRaise, s.Raises, s.CurrentStreetAggressor, s.LastActor = 0, s.BigBlind, 0, "", ""
	for _, player := range s.Players {
		player.StreetContribution = 0
		player.Acted = player.Folded || player.AllIn
	}
	s.reconcileHeroStackFromLegalActions()
	return nil
}

func (s *State) applyAction(req protocol.EventRequest) error {
	var payload protocol.ActionPayload
	_ = json.Unmarshal(req.Payload, &payload)
	player := s.Players[payload.PlayerID]
	if player == nil || player.Folded || player.AllIn {
		return fmt.Errorf("%w: player %q cannot act", ErrInvalidTransition, payload.PlayerID)
	}
	if payload.NextPlayerID == nil {
		if expected := s.NextToAct(); expected != "" && expected != payload.PlayerID {
			return fmt.Errorf("%w: expected player %q, got %q", ErrInvalidTransition, expected, payload.PlayerID)
		}
	}
	value := 0.0
	hasValue := payload.Value != nil
	if payload.Value != nil {
		value = *payload.Value
	}
	reportedValue := value
	if payload.ValueMode != "" && payload.ValueMode != "increment" && payload.ValueMode != "street_total" {
		return fmt.Errorf("%w: unsupported action value_mode %q", ErrInvalidTransition, payload.ValueMode)
	}
	stackSpend := 0.0
	if payload.StackAfter != nil {
		if *payload.StackAfter < -1e-9 || *payload.StackAfter > player.Stack+.011 {
			return fmt.Errorf("%w: stack_after %.4f is invalid for stack %.4f", ErrInvalidTransition, *payload.StackAfter, player.Stack)
		}
		stackSpend = player.Stack - math.Max(0, *payload.StackAfter)
	}
	// Some tables report a free big-blind option as Call and put the already
	// posted street total in value. stack_after proves that this action spent no
	// chips, so keep Call for advice comparison/server compatibility but account
	// it as a zero-chip check. Without this normalization every player stream
	// rejects the shared action as "call amount 20, expected 0".
	freeCallTotalMarker := payload.Type == protocol.ActionCall && payload.StackAfter != nil &&
		math.Abs(stackSpend) <= .011 && s.CurrentBet-player.StreetContribution <= .011 && value > .011 &&
		(math.Abs(value-player.StreetContribution) <= .011 || math.Abs(value-s.CurrentBet) <= .011)
	if freeCallTotalMarker {
		value = 0
		s.LastNormalization = "free_call_street_total"
	}
	if !freeCallTotalMarker && payload.ValueMode == "street_total" && payload.Type != protocol.ActionFold && payload.Type != protocol.ActionCheck {
		rawValue := value
		value -= player.StreetContribution
		// AinP adapters deployed before 2026-08-09 labelled the server's
		// increment amount as street_total. stack_after lets us distinguish it
		// from a real street total without weakening correctly labelled clients:
		// a true street total is larger than the stack spent by the action.
		if payload.StackAfter != nil && rawValue <= stackSpend+.011 && rawValue > value+.011 {
			value = rawValue
		}
	} else if !hasValue && payload.StackAfter != nil && payload.Type != protocol.ActionFold && payload.Type != protocol.ActionCheck {
		value = stackSpend
	}
	payload.Value = &value
	untrackedSpend := math.Max(0, stackSpend-value)
	availableStack := math.Max(0, player.Stack-untrackedSpend)
	// Some table modes deduct a forced preflop contribution that also counts
	// toward the amount to call, but older start payloads omitted it. Infer only
	// the portion proven by the actual Check/Call; the remainder stays an ante.
	untrackedStreetSpend := 0.0
	if s.Street == Preflop && untrackedSpend > 0 {
		// The omitted forced amount used by these legacy tables counts toward
		// the preflop bring-in. For aggressive actions it precedes the action;
		// Check/Call further pin the exact prior contribution.
		targetContribution := math.Max(player.StreetContribution, s.CurrentBet)
		switch payload.Type {
		case protocol.ActionCheck:
			targetContribution = s.CurrentBet
		case protocol.ActionCall:
			targetContribution = math.Max(player.StreetContribution, s.CurrentBet-value)
		}
		untrackedStreetSpend = math.Min(untrackedSpend, math.Max(0, targetContribution-player.StreetContribution))
	}
	effectiveStreetContribution := player.StreetContribution + untrackedStreetSpend
	if value < 0 || value > availableStack+1e-9 {
		return fmt.Errorf("%w: action amount %.4f exceeds stack %.4f", ErrInvalidTransition, value, player.Stack)
	}
	if (payload.Type == protocol.ActionFold || payload.Type == protocol.ActionCheck) && value > .011 {
		return fmt.Errorf("%w: %s amount must be zero", ErrInvalidTransition, payload.Type)
	}
	if payload.Type == protocol.ActionAllIn && math.Abs(value-availableStack) > .011 {
		return fmt.Errorf("%w: allin amount %.4f, expected %.4f", ErrInvalidTransition, value, availableStack)
	}
	if payload.Type == protocol.ActionBet && (s.CurrentBet > 1e-9 || value <= 0) {
		return fmt.Errorf("%w: bet is invalid facing an existing bet", ErrInvalidTransition)
	}
	if payload.Type == protocol.ActionRaise && effectiveStreetContribution+value <= s.CurrentBet+1e-9 {
		return fmt.Errorf("%w: raise does not exceed current bet", ErrInvalidTransition)
	}
	beforeBet := s.CurrentBet
	if payload.PlayerID == s.HeroID && s.PendingAdvice != nil {
		comparison := payload
		if freeCallTotalMarker {
			// The wire value matched the advice; only chip accounting was
			// normalized, so this is not an action-value deviation.
			comparison.Value = &reportedValue
		} else if payload.Type == protocol.ActionFold || payload.Type == protocol.ActionCheck {
			comparison.Value = nil
		} else if s.PendingAdvice.ValueMode == "street_total" {
			actualStreetTotal := player.StreetContribution + value
			comparison.Value = &actualStreetTotal
		} else {
			comparison.Value = &value
		}
		s.Deviation = compareAdvice(comparison, *s.PendingAdvice)
		s.PendingAdvice = nil
	}
	if payload.LegalActions != nil {
		s.LegalActions = append([]protocol.LegalAction(nil), (*payload.LegalActions)...)
	} else {
		s.LegalActions = nil
	}
	preflopFirstAction := s.Street == Preflop && !player.PreflopActed
	observation := &Observation{
		ObservationID: fmt.Sprintf("%s:%d", s.HandID, req.SeqNum), PlayerID: player.ID, HandID: s.HandID,
		Street: s.Street, Action: payload.Type, Voluntary: true,
		PreflopOpportunity: preflopFirstAction, PFROpportunity: preflopFirstAction,
		ThreeBetOpportunity: preflopFirstAction && s.Raises > 0,
		CBetOpportunity:     s.Street != Preflop && player.ID == s.PreflopAggressor && s.CurrentBet == 0,
		FacingCBet:          s.Street != Preflop && s.CurrentStreetAggressor == s.PreflopAggressor && s.CurrentBet > player.StreetContribution,
	}
	if freeCallTotalMarker {
		observation.Action = protocol.ActionCheck
		observation.Voluntary = false
	}
	if payload.Type == protocol.ActionFold {
		player.Folded = true
	}
	if payload.Type == protocol.ActionCheck && s.CurrentBet > effectiveStreetContribution+1e-9 {
		return fmt.Errorf("%w: cannot check facing a bet", ErrInvalidTransition)
	}
	if payload.Type == protocol.ActionCall {
		toCall := math.Min(s.CurrentBet-effectiveStreetContribution, availableStack)
		if math.Abs(value-toCall) > .011 {
			return fmt.Errorf("%w: call amount %.4f, expected %.4f", ErrInvalidTransition, value, toCall)
		}
	}
	if untrackedSpend > 1e-9 {
		player.TotalContribution += untrackedSpend
		s.Pot += untrackedSpend
		player.StreetContribution += untrackedStreetSpend
	}
	if payload.Type == protocol.ActionBet || payload.Type == protocol.ActionRaise || payload.Type == protocol.ActionAllIn || payload.Type == protocol.ActionCall {
		if payload.StackAfter != nil {
			player.Stack = math.Max(0, *payload.StackAfter)
		} else {
			player.Stack = availableStack - value
		}
		player.StreetContribution += value
		player.TotalContribution += value
		s.Pot += value
		if player.Stack <= 1e-9 || payload.Type == protocol.ActionAllIn {
			player.Stack = 0
			player.AllIn = true
		}
	} else if payload.StackAfter != nil {
		// Fold/check spend no action chips, but stack_after may reveal a forced
		// contribution (for example an ante omitted from start_hand_extended).
		player.Stack = math.Max(0, *payload.StackAfter)
	}
	newBet := player.StreetContribution
	if payload.Type == protocol.ActionAllIn && newBet <= beforeBet+1e-9 {
		observation.Action = protocol.ActionCall
	}
	if newBet > beforeBet+1e-9 {
		raiseSize := newBet - beforeBet
		isAggressive := payload.Type == protocol.ActionBet || payload.Type == protocol.ActionRaise || payload.Type == protocol.ActionAllIn
		if isAggressive {
			if beforeBet > 0 {
				s.Raises++
			}
			if raiseSize >= s.LastFullRaise-1e-9 {
				s.LastFullRaise = raiseSize
				for _, other := range s.Players {
					if other.ID != player.ID && !other.Folded && !other.AllIn {
						other.Acted = false
					}
				}
			}
			s.CurrentStreetAggressor = player.ID
			if s.Street == Preflop {
				s.PreflopAggressor = player.ID
			}
		}
		s.CurrentBet = newBet
	}
	player.Acted = true
	s.LastActor = player.ID
	if s.Street == Preflop {
		player.PreflopActed = true
		if value > 0 && (payload.Type == protocol.ActionCall || payload.Type == protocol.ActionRaise || payload.Type == protocol.ActionBet || payload.Type == protocol.ActionAllIn) {
			player.PreflopVPIP = true
		}
		if newBet > beforeBet+1e-9 && (payload.Type == protocol.ActionRaise || payload.Type == protocol.ActionBet || payload.Type == protocol.ActionAllIn) {
			player.PreflopPFR = true
		}
	}
	if s.Street != Preflop && payload.Type == protocol.ActionCall {
		player.PostflopCalls++
	}
	if s.Street != Preflop && (payload.Type == protocol.ActionBet || payload.Type == protocol.ActionRaise || (payload.Type == protocol.ActionAllIn && newBet > beforeBet+1e-9)) {
		player.PostflopAggro++
	}
	s.LastObservation = observation
	s.setAuthoritativeNext(payload.NextPlayerID)
	s.reconcileHeroStackFromLegalActions()
	return nil
}

// reconcileHeroStackFromLegalActions absorbs forced chips that were omitted
// from start_hand_extended. The native adapter's AllIn maximum is the amount
// the hero can still add, so it is also an authoritative upper bound for the
// currently playable stack. The difference is a forced contribution (usually
// an ante), not part of the hero's next action or street contribution.
func (s *State) reconcileHeroStackFromLegalActions() {
	hero := s.Players[s.HeroID]
	if hero == nil || hero.Folded || hero.AllIn {
		return
	}
	for _, legal := range s.LegalActions {
		if legal.Type != protocol.ActionAllIn || legal.Max < 0 || legal.Max+.011 >= hero.Stack {
			continue
		}
		forced := hero.Stack - legal.Max
		forcedStreet := s.forcedStreetContributionFromLegalActions(hero, forced)
		hero.Stack = legal.Max
		hero.TotalContribution += forced
		hero.StreetContribution += forcedStreet
		s.Pot += forced
		return
	}
}

func (s *State) forcedStreetContributionFromLegalActions(hero *Player, forced float64) float64 {
	if s.Street != Preflop || forced <= 0 {
		return 0
	}
	target := hero.StreetContribution
	for _, legal := range s.LegalActions {
		switch legal.Type {
		case protocol.ActionCheck:
			target = math.Max(target, s.CurrentBet)
		case protocol.ActionCall:
			target = math.Max(target, s.CurrentBet-legal.Min)
		}
	}
	return math.Min(forced, math.Max(0, target-hero.StreetContribution))
}

func (s *State) postForced(playerID string, value float64, streetContribution bool) error {
	player := s.Players[playerID]
	if player == nil || value < 0 {
		return fmt.Errorf("%w: invalid forced bet for %q", ErrInvalidTransition, playerID)
	}
	value = math.Min(value, player.Stack)
	player.Stack -= value
	player.TotalContribution += value
	s.Pot += value
	if streetContribution {
		player.StreetContribution += value
		if player.StreetContribution > s.CurrentBet {
			s.CurrentBet = player.StreetContribution
		}
	}
	if player.Stack <= 1e-9 {
		player.Stack, player.AllIn, player.Acted = 0, true, true
	}
	return nil
}

func (s *State) ShouldAdvise() bool {
	active := 0
	for _, player := range s.Players {
		if !player.Folded {
			active++
		}
	}
	return active > 1 && !s.Ended && len(s.HeroCards) > 0 && s.NextToAct() == s.HeroID
}

func (s *State) NextToAct() string {
	if s.AuthoritativeNext != nil {
		return *s.AuthoritativeNext
	}
	order := s.actionOrder()
	start := 0
	if s.LastActor != "" {
		for index, playerID := range order {
			if playerID == s.LastActor {
				start = (index + 1) % len(order)
				break
			}
		}
	}
	for offset := range order {
		playerID := order[(start+offset)%len(order)]
		player := s.Players[playerID]
		if player == nil || player.Folded || player.AllIn {
			continue
		}
		if !player.Acted || player.StreetContribution+1e-9 < s.CurrentBet {
			return playerID
		}
	}
	return ""
}

func (s *State) setAuthoritativeNext(next *string) {
	if next == nil {
		return
	}
	value := *next
	s.AuthoritativeNext = &value
}

func (s *State) actionOrder() []string {
	if s.Street != Preflop {
		// Heads-up is the one positional exception: the dealer/small blind acts
		// first preflop, while the big blind acts first on every later street.
		if len(s.Order) == 2 {
			var smallBlind, bigBlind string
			for _, id := range s.Order {
				switch s.Players[id].Role {
				case "sb":
					smallBlind = id
				case "bb":
					bigBlind = id
				}
			}
			if smallBlind != "" && bigBlind != "" {
				return []string{bigBlind, smallBlind}
			}
		}
		return append([]string(nil), s.Order...)
	}
	order := make([]string, 0, len(s.Order))
	for _, id := range s.Order {
		role := s.Players[id].Role
		if role != "sb" && role != "bb" && role != "st" {
			order = append(order, id)
		}
	}
	for _, role := range []string{"sb", "bb", "st"} {
		for _, id := range s.Order {
			if s.Players[id].Role == role {
				order = append(order, id)
			}
		}
	}
	return order
}

func (s *State) Clone() *State {
	copyState := *s
	copyState.Order = append([]string(nil), s.Order...)
	copyState.HeroCards = append([]poker.Card(nil), s.HeroCards...)
	copyState.Board = append([]poker.Card(nil), s.Board...)
	copyState.LegalActions = append([]protocol.LegalAction(nil), s.LegalActions...)
	if s.AuthoritativeNext != nil {
		next := *s.AuthoritativeNext
		copyState.AuthoritativeNext = &next
	}
	copyState.Players = make(map[string]*Player, len(s.Players))
	for id, player := range s.Players {
		copyPlayer := *player
		copyPlayer.Cards = append([]poker.Card(nil), player.Cards...)
		copyState.Players[id] = &copyPlayer
	}
	if s.LastObservation != nil {
		observation := *s.LastObservation
		copyState.LastObservation = &observation
	}
	if s.PendingAdvice != nil {
		advice := *s.PendingAdvice
		if s.PendingAdvice.Value != nil {
			value := *s.PendingAdvice.Value
			advice.Value = &value
		}
		copyState.PendingAdvice = &advice
	}
	if s.Deviation != nil {
		deviation := *s.Deviation
		if s.Deviation.ByType != nil {
			byType := *s.Deviation.ByType
			deviation.ByType = &byType
		}
		if s.Deviation.ByValue != nil {
			byValue := *s.Deviation.ByValue
			deviation.ByValue = &byValue
		}
		copyState.Deviation = &deviation
	}
	return &copyState
}

func compareAdvice(actual protocol.ActionPayload, expected protocol.AdviseResponse) *protocol.DeviationResponse {
	deviation := &protocol.DeviationResponse{}
	if actual.Type != expected.Type {
		deviation.ByType = &protocol.DeviationByType{Actual: actual.Type, Expected: expected.Type}
	}
	actualValue := 0.0
	if actual.Value != nil {
		actualValue = *actual.Value
	}
	expectedValue := 0.0
	if expected.Value != nil {
		expectedValue = *expected.Value
	}
	if math.Abs(actualValue-expectedValue) > .011 {
		deviation.ByValue = &protocol.DeviationByValue{Actual: actualValue, Expected: expectedValue}
	}
	if deviation.ByType == nil && deviation.ByValue == nil {
		return nil
	}
	return deviation
}

func sameCards(left, right []poker.Card) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func hasDuplicates(groups ...[]poker.Card) bool {
	seen := uint64(0)
	for _, group := range groups {
		for _, card := range group {
			mask := uint64(1) << card
			if seen&mask != 0 {
				return true
			}
			seen |= mask
		}
	}
	return false
}
