package protocol

import "encoding/json"

type Command string

const (
	CommandStartHand Command = "start_hand_extended"
	CommandDealCards Command = "deal_cards"
	CommandAction    Command = "action"
	CommandTourBonus Command = "tour_bonus"
	CommandShowCards Command = "show_cards"
	CommandFlop      Command = "flop"
	CommandTurn      Command = "turn"
	CommandRiver     Command = "river"
	CommandEndHand   Command = "end_hand"
	CommandTourExit  Command = "tour_exit"
)

type ActionType string

const (
	ActionCall  ActionType = "call"
	ActionRaise ActionType = "raise"
	ActionBet   ActionType = "bet"
	ActionAllIn ActionType = "allin"
	ActionFold  ActionType = "fold"
	ActionCheck ActionType = "check"
)

type EventRequest struct {
	SeqNum   uint64          `json:"seq_num"`
	PlayerID string          `json:"player_id"`
	RoomID   string          `json:"room_id"`
	TableID  string          `json:"table_id"`
	HandID   *string         `json:"hand_id,omitempty"`
	Cmd      Command         `json:"cmd"`
	Payload  json.RawMessage `json:"payload"`
}

type EventResponse struct {
	SeqNum    uint64             `json:"seq_num"`
	Advise    *AdviseResponse    `json:"advise,omitempty"`
	Deviation *DeviationResponse `json:"deviation,omitempty"`
}
type AdviseResponse struct {
	Type      ActionType `json:"type"`
	Value     *float64   `json:"value,omitempty"`
	ValueMode string     `json:"value_mode,omitempty"`
}
type DeviationResponse struct {
	ByType  *DeviationByType  `json:"by_type,omitempty"`
	ByValue *DeviationByValue `json:"by_value,omitempty"`
}
type DeviationByType struct {
	Actual   ActionType `json:"actual"`
	Expected ActionType `json:"expected"`
}
type DeviationByValue struct {
	Actual   float64 `json:"actual"`
	Expected float64 `json:"expected"`
}

type ErrorCode string

const (
	ErrorDecodeRequest ErrorCode = "Decode request body error"
	ErrorMissedField   ErrorCode = "Missed field"
	ErrorWrongValue    ErrorCode = "Wrong value"
	ErrorGameLogic     ErrorCode = "Game logic error"
	ErrorBrokenHand    ErrorCode = "Broken hand"
	ErrorWrongSeq      ErrorCode = "Wrong seq_num"
	ErrorServer        ErrorCode = "Server error"
)

type ErrorResponse struct {
	ErrorCode ErrorCode `json:"error_code"`
	Message   *string   `json:"message,omitempty"`
	RequestID string    `json:"request_id"`
}

type StartHandPayload struct {
	GameType   string            `json:"game_type"`
	ClubID     string            `json:"club_id"`
	AIProfile  string            `json:"ai_profile"`
	Time       uint64            `json:"time"`
	SmallBlind float64           `json:"small_blind"`
	BigBlind   float64           `json:"big_blind"`
	Straddle   *float64          `json:"straddle,omitempty"`
	Ante       *float64          `json:"ante,omitempty"`
	TimeToAct  uint64            `json:"time_to_act"`
	MaxSeat    uint32            `json:"max_seat"`
	Players    []PlayerStartHand `json:"players"`
	Antes      []Ante            `json:"antes,omitempty"`
	Blinds     []Blind           `json:"blinds,omitempty"`
}

type Ante struct {
	PlayerID string  `json:"player_id"`
	Value    float64 `json:"value"`
}

type Blind struct {
	PlayerID string  `json:"player_id"`
	Value    float64 `json:"value"`
	Type     string  `json:"type"`
}
type PlayerStartHand struct {
	PlayerID string  `json:"player_id"`
	Nick     string  `json:"nick"`
	Stack    float64 `json:"stack"`
	Role     *string `json:"role,omitempty"`
}
type CardsPayload struct {
	Cards        string         `json:"cards"`
	LegalActions *[]LegalAction `json:"legal_actions,omitempty"`
	NextPlayerID *string        `json:"next_player_id,omitempty"`
}
type LegalAction struct {
	Type ActionType `json:"type"`
	Min  float64    `json:"min"`
	Max  float64    `json:"max"`
}
type ActionPayload struct {
	Type     ActionType `json:"type"`
	PlayerID string     `json:"player_id"`
	Value    *float64   `json:"value,omitempty"`
	// ValueMode is optional. Legacy clients and current pokerbot send the amount
	// added by this action; street_total remains supported for compatibility.
	ValueMode    string         `json:"value_mode,omitempty"`
	LegalActions *[]LegalAction `json:"legal_actions,omitempty"`
	// StackAfter is the acting player's server-authoritative remaining stack.
	// It reconciles omitted forced contributions without replacing an explicit
	// action value.
	StackAfter   *float64 `json:"stack_after,omitempty"`
	NextPlayerID *string  `json:"next_player_id,omitempty"`
}

type ShowCardsPayload struct {
	PlayerID string `json:"player_id"`
	Cards    string `json:"cards"`
}

type EndHandPayload struct {
	Players []PlayerEndHand `json:"players"`
}

type PlayerEndHand struct {
	PlayerID string  `json:"player_id"`
	Profit   float64 `json:"profit"`
	Cards    *string `json:"cards,omitempty"`
}

func ValidCommand(cmd Command) bool {
	switch cmd {
	case CommandStartHand, CommandDealCards, CommandAction, CommandTourBonus, CommandShowCards, CommandFlop, CommandTurn, CommandRiver, CommandEndHand, CommandTourExit:
		return true
	default:
		return false
	}
}
func ValidAction(action ActionType) bool {
	switch action {
	case ActionCall, ActionRaise, ActionBet, ActionAllIn, ActionFold, ActionCheck:
		return true
	default:
		return false
	}
}
