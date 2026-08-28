package strategy

import (
	"time"

	"gitlab.com/smoothsics/ainp/internal/equity"
	"gitlab.com/smoothsics/ainp/internal/opponent"
	"gitlab.com/smoothsics/ainp/internal/poker"
)

type Action string

const (
	Fold  Action = "fold"
	Check Action = "check"
	Call  Action = "call"
	Bet   Action = "bet"
	Raise Action = "raise"
	AllIn Action = "allin"
)

type Street string

const (
	Preflop Street = "preflop"
	Flop    Street = "flop"
	Turn    Street = "turn"
	River   Street = "river"
)

type Position string

const (
	UTG Position = "UTG"
	MP  Position = "MP"
	CO  Position = "CO"
	BTN Position = "BTN"
	SB  Position = "SB"
	BB  Position = "BB"
)

type LegalAction struct {
	Action Action
	Min    float64
	Max    float64
}

type Request struct {
	DecisionID                string
	RequestID                 string
	PolicyVersion             string
	AIProfile                 string
	ProfileSource             string
	PlayerID                  string
	TableID                   string
	HandID                    string
	Game                      equity.Game
	Street                    Street
	Position                  Position
	Hero                      []poker.Card
	Board                     []poker.Card
	Opponents                 [][]poker.Card
	Dead                      []poker.Card
	Pot                       float64
	ToCall                    float64
	Stack                     float64
	EffectiveStack            float64
	BigBlind                  float64
	RaisesFaced               int
	ActiveOpponents           int
	WasPreflopAggressor       bool
	Level                     int
	TargetVPIP                float64
	TargetPFR                 float64
	PostflopSizings           []float64
	BehaviorMode              string
	PreflopRaiseProbability   float64
	PostflopAggressionChance  float64
	NeverFold                 bool
	AuditExempt               bool
	HeroPreflopVPIP           bool
	HeroPreflopPFR            bool
	HeroPostflopCalls         int
	PostflopCallMargin        float64
	LargePotThresholdBB       float64
	LargePotMinEquity         float64
	PersonalityID             string
	OpponentModels            []opponent.Snapshot
	DisablePersonality        bool
	DisableHumanization       bool
	DisableOpponentModel      bool
	DisableThinkTime          bool
	Seed                      int64
	EquitySamples             int
	MaxExactOutcomes          uint64
	LegalActions              []LegalAction
	CollapseNearAllIn         bool
	NearAllInRemainingChips   float64
	PreflopOpenCallGap        float64
	PreflopReraiseEquity      float64
	PreflopReraiseRangeFactor float64
	PreflopExtraRaisePenalty  float64
	PreflopMultiwayPenalty    float64
	PreflopCallMargin         float64
	PreflopLargeCallBB        float64
	FlopAirCallMargin         float64
	TurnAirCallMargin         float64
	RiverAirCallMargin        float64
	RepeatedAirCallPenalty    float64
	UnderpairCallMargin       float64
	TurnWeakDrawCallMargin    float64
	RiverBoardPairCallMargin  float64
	RiverMissedDrawMargin     float64
	RejectNegativeEVCalls     bool
}

type HandClass string

const (
	ClassAir        HandClass = "air"
	ClassDraw       HandClass = "draw"
	ClassMade       HandClass = "made"
	ClassMadeDraw   HandClass = "made_draw"
	ClassMadeStrong HandClass = "made_strong"
)

type Features struct {
	Category                   poker.Category
	Class                      HandClass
	FlushDraw                  bool
	StraightDraw               bool
	DrawOuts                   int
	RiverCardFeaturesAvailable bool
	PairFromBoardOnly          bool
	PocketPairUnderBoard       bool
	OnePairBelowTopBoard       bool
	MissedFlushDraw            bool
	MissedStraightDraw         bool
}

type Decision struct {
	Action        Action
	Amount        float64
	RuleID        string
	Tags          []string
	Equity        equity.Result
	PotOdds       float64
	CallEV        float64
	SPR           float64
	Features      Features
	PersonalityID string
	OpponentRead  OpponentRead
	Humanized     bool
	ThinkTime     time.Duration
}

type OpponentRead struct {
	AverageVPIP     float64
	Aggression      float64
	FoldToCBet      float64
	ObservedHands   uint64
	Archetypes      []string
	CallMargin      float64
	BluffMultiplier float64
	ValueThreshold  float64
}
