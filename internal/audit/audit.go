package audit

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gitlab.com/smoothsics/ainp/internal/poker"
)

type Expectations struct {
	MinimumStrategyDecisions   int     `yaml:"minimum_strategy_decisions" json:"minimum_strategy_decisions"`
	MinPreflopFoldRate         float64 `yaml:"min_preflop_fold_rate" json:"min_preflop_fold_rate"`
	MaxPreflopFoldRate         float64 `yaml:"max_preflop_fold_rate" json:"max_preflop_fold_rate"`
	MinPreflopAggressionRate   float64 `yaml:"min_preflop_aggression_rate" json:"min_preflop_aggression_rate"`
	MaxPreflopAggressionRate   float64 `yaml:"max_preflop_aggression_rate" json:"max_preflop_aggression_rate"`
	MaxHTTPErrorRate           float64 `yaml:"max_http_error_rate" json:"max_http_error_rate"`
	MaxEngineFailures          int     `yaml:"max_engine_failures" json:"max_engine_failures"`
	MaxIntentConflicts         int     `yaml:"max_intent_conflicts" json:"max_intent_conflicts"`
	MaxHighEquityPreflopFolds  int     `yaml:"max_high_equity_preflop_folds" json:"max_high_equity_preflop_folds"`
	HighEquityThreshold        float64 `yaml:"high_equity_threshold" json:"high_equity_threshold"`
	MaxStrategyP95US           int64   `yaml:"max_strategy_p95_us" json:"max_strategy_p95_us"`
	MaxHTTPP95US               int64   `yaml:"max_http_p95_us" json:"max_http_p95_us"`
	MaxGrayCandidateErrors     int     `yaml:"max_gray_candidate_errors" json:"max_gray_candidate_errors"`
	MaxGameLogicErrors         int     `yaml:"max_game_logic_errors" json:"max_game_logic_errors"`
	MaxWrongSequenceErrors     int     `yaml:"max_wrong_sequence_errors" json:"max_wrong_sequence_errors"`
	MaxActionTypeDeviations    int     `yaml:"max_action_type_deviations" json:"max_action_type_deviations"`
	MaxDefaultProfileFallbacks int     `yaml:"max_default_profile_fallbacks" json:"max_default_profile_fallbacks"`
	StartupBoundaryGraceMS     int64   `yaml:"startup_boundary_grace_ms" json:"startup_boundary_grace_ms"`
	LateStreamAfterEndMS       int64   `yaml:"late_stream_after_end_threshold_ms" json:"late_stream_after_end_threshold_ms"`
	DelayedDealThresholdMS     int64   `yaml:"delayed_deal_threshold_ms" json:"delayed_deal_threshold_ms"`
	MinRiverAirCallEquityEdge  float64 `yaml:"min_river_air_call_equity_edge" json:"min_river_air_call_equity_edge"`
	MinRiverPairCallEquityEdge float64 `yaml:"min_river_pair_call_equity_edge" json:"min_river_pair_call_equity_edge"`
	MaxDelayedDealP95US        *int64  `yaml:"max_delayed_deal_decision_p95_us" json:"max_delayed_deal_decision_p95_us,omitempty"`
	MaxRejectedDealCards       *int    `yaml:"max_rejected_deal_cards" json:"max_rejected_deal_cards,omitempty"`
	MaxDelayedDealsNoAdvice    *int    `yaml:"max_delayed_deals_without_advice" json:"max_delayed_deals_without_advice,omitempty"`
	MaxNegativeEVCalls         *int    `yaml:"max_negative_ev_calls" json:"max_negative_ev_calls,omitempty"`
	MaxQuestionableAirCalls    *int    `yaml:"max_questionable_air_calls" json:"max_questionable_air_calls,omitempty"`
	MaxAirCallDownHands        *int    `yaml:"max_air_call_down_hands" json:"max_air_call_down_hands,omitempty"`
	MaxFreeOptionFolds         *int    `yaml:"max_free_option_folds" json:"max_free_option_folds,omitempty"`
	MinimumProfileHands        int     `yaml:"minimum_profile_hands" json:"minimum_profile_hands"`
	MaxVPIPDeviation           float64 `yaml:"max_vpip_deviation" json:"max_vpip_deviation"`
	MaxPFRDeviation            float64 `yaml:"max_pfr_deviation" json:"max_pfr_deviation"`
	EnforceProfileRates        bool    `yaml:"enforce_profile_rates" json:"enforce_profile_rates"`
	LowEquityAllInThreshold    float64 `yaml:"low_equity_allin_threshold" json:"low_equity_allin_threshold"`
	RiverHighEquityThreshold   float64 `yaml:"river_high_equity_threshold" json:"river_high_equity_threshold"`
}

type Issue struct {
	Check    string `json:"check"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type Example struct {
	DecisionID                       string  `json:"decision_id"`
	TableID                          string  `json:"table_id"`
	HandID                           string  `json:"hand_id"`
	PlayerID                         string  `json:"player_id"`
	AIProfile                        string  `json:"ai_profile,omitempty"`
	Street                           string  `json:"street,omitempty"`
	HeroClass                        string  `json:"hero_hand_class,omitempty"`
	HandClass                        string  `json:"hand_class,omitempty"`
	Action                           string  `json:"action"`
	RuleID                           string  `json:"rule_id"`
	Equity                           float64 `json:"equity"`
	Stack                            float64 `json:"effective_stack"`
	ToCall                           float64 `json:"to_call"`
	Pot                              float64 `json:"pot"`
	PotOdds                          float64 `json:"pot_odds"`
	CallEV                           float64 `json:"call_ev"`
	PairFromBoardOnly                bool    `json:"pair_from_board_only,omitempty"`
	PocketPairUnderBoard             bool    `json:"pocket_pair_under_board,omitempty"`
	UnderpairFeatureAvailable        bool    `json:"underpair_feature_available,omitempty"`
	PreflopLargeCallFeatureAvailable bool    `json:"preflop_large_call_feature_available,omitempty"`
	PreflopLargeCallOutsideRange     bool    `json:"preflop_large_call_outside_range,omitempty"`
	MissedFlushDraw                  bool    `json:"missed_flush_draw,omitempty"`
	MissedStraightDraw               bool    `json:"missed_straight_draw,omitempty"`
	HeroPostflopCalls                int     `json:"hero_postflop_calls,omitempty"`
	RaisesFaced                      int     `json:"raises_faced,omitempty"`
	FeatureAvailable                 bool    `json:"river_card_features_available,omitempty"`
}

type ErrorExample struct {
	EventFingerprint string `json:"event_fingerprint,omitempty"`
	DecisionID       string `json:"decision_id"`
	TableID          string `json:"table_id"`
	HandID           string `json:"hand_id"`
	PlayerID         string `json:"player_id"`
	SeqNum           uint64 `json:"seq_num"`
	Command          string `json:"command"`
	Message          string `json:"message"`
	Occurrences      int    `json:"occurrences"`
}

type DeviationExample struct {
	DecisionID       string  `json:"decision_id"`
	AdviceDecisionID string  `json:"advice_decision_id,omitempty"`
	AdviceCommand    string  `json:"advice_command,omitempty"`
	AdviceAgeMS      int64   `json:"advice_age_ms,omitempty"`
	LateStreamReason string  `json:"late_stream_reason,omitempty"`
	StartDelayMS     int64   `json:"start_delay_ms,omitempty"`
	StartAfterEndMS  int64   `json:"start_after_end_ms,omitempty"`
	TableSeqAtStart  uint64  `json:"table_seq_at_start,omitempty"`
	TableID          string  `json:"table_id"`
	HandID           string  `json:"hand_id"`
	PlayerID         string  `json:"player_id"`
	SeqNum           uint64  `json:"seq_num"`
	ActualAction     string  `json:"actual_action,omitempty"`
	ExpectedAction   string  `json:"expected_action,omitempty"`
	ActualValue      float64 `json:"actual_value,omitempty"`
	ExpectedValue    float64 `json:"expected_value,omitempty"`
}

type LateStreamExample struct {
	DecisionID      string `json:"decision_id"`
	TableID         string `json:"table_id"`
	HandID          string `json:"hand_id"`
	PlayerID        string `json:"player_id"`
	Reason          string `json:"reason"`
	StartDelayMS    int64  `json:"start_delay_ms"`
	StartAfterEndMS int64  `json:"start_after_end_ms,omitempty"`
	TableSeqAtStart uint64 `json:"table_seq_at_start,omitempty"`
}

type lateStreamContext struct {
	reason          string
	startDelayMS    int64
	startAfterEndMS int64
	tableSeqAtStart uint64
}

type tableHandProgress struct {
	startedAt time.Time
	maxSeq    uint64
}

type adviceContext struct {
	time       time.Time
	decisionID string
	command    string
}

type CallDownExample struct {
	TableID   string    `json:"table_id"`
	HandID    string    `json:"hand_id"`
	PlayerID  string    `json:"player_id"`
	AIProfile string    `json:"ai_profile,omitempty"`
	Calls     []Example `json:"calls"`
}

type Report struct {
	GeneratedAt                      time.Time                        `json:"generated_at"`
	Input                            string                           `json:"input"`
	Passed                           bool                             `json:"passed"`
	Lines                            int                              `json:"lines"`
	MalformedLines                   int                              `json:"malformed_lines"`
	MalformedLineExamples            []int                            `json:"malformed_line_examples,omitempty"`
	HTTPRequests                     int                              `json:"http_requests"`
	HTTPErrors                       int                              `json:"http_errors"`
	HTTPErrorRate                    float64                          `json:"http_error_rate"`
	DecisionOutcomes                 map[string]int                   `json:"decision_outcomes"`
	DecisionErrors                   map[string]int                   `json:"decision_errors"`
	GameLogicErrors                  int                              `json:"game_logic_errors"`
	RecoveredGameLogicErrors         int                              `json:"recovered_game_logic_errors"`
	FreeCallTotalNormalizations      int                              `json:"free_call_total_normalizations"`
	UniqueGameLogicErrors            int                              `json:"unique_game_logic_errors"`
	WrongSequenceErrors              int                              `json:"wrong_sequence_errors"`
	ActionTypeDeviations             int                              `json:"action_type_deviations"`
	ActionValueDeviations            int                              `json:"action_value_deviations"`
	LatePlayerStreams                int                              `json:"late_player_streams"`
	LateStreamsAfterEnd              int                              `json:"late_streams_after_end"`
	LateStreamsAfterTableProgress    int                              `json:"late_streams_after_table_progress"`
	LateStreamTypeDeviations         int                              `json:"late_stream_action_type_deviations"`
	LateStreamValueDeviations        int                              `json:"late_stream_action_value_deviations"`
	DefaultProfileFallbacks          int                              `json:"default_profile_fallbacks"`
	ErrorMessages                    map[string]int                   `json:"error_messages"`
	StartupBoundaryMessages          map[string]int                   `json:"startup_boundary_error_messages"`
	DealCardsEvents                  int                              `json:"deal_cards_events"`
	RejectedDealCards                int                              `json:"rejected_deal_cards"`
	StartupBoundaryErrors            int                              `json:"startup_boundary_errors"`
	StartupRejectedDealCards         int                              `json:"startup_rejected_deal_cards"`
	DelayedDealCards                 int                              `json:"delayed_deal_cards"`
	DelayedDealsWithAdvice           int                              `json:"delayed_deals_with_advice"`
	DelayedDealsNoAdvice             int                              `json:"delayed_deals_without_advice"`
	DelayedDealP95US                 int64                            `json:"delayed_deal_decision_p95_us"`
	DealAfterStartP95MS              int64                            `json:"deal_after_start_p95_ms"`
	DealAfterStartMaxMS              int64                            `json:"deal_after_start_max_ms"`
	StrategyDecisions                int                              `json:"strategy_decisions"`
	PolicyVersions                   map[string]int                   `json:"policy_versions"`
	StreetCounts                     map[string]int                   `json:"street_counts"`
	ActionCounts                     map[string]int                   `json:"action_counts"`
	PreflopActions                   map[string]int                   `json:"preflop_actions"`
	PreflopFoldRate                  float64                          `json:"preflop_fold_rate"`
	PreflopAggressionRate            float64                          `json:"preflop_aggression_rate"`
	Humanized                        int                              `json:"humanized"`
	GrayRoutes                       map[string]int                   `json:"gray_routes"`
	GrayCompared                     int                              `json:"gray_compared"`
	GraySameAction                   int                              `json:"gray_same_action"`
	GrayActionAgreement              float64                          `json:"gray_action_agreement"`
	GrayCandidateErrors              int                              `json:"gray_candidate_errors"`
	IntentConflicts                  int                              `json:"intent_conflicts"`
	HighEquityPreflopFolds           int                              `json:"high_equity_preflop_folds"`
	PreflopLargeCallFeatureDecisions int                              `json:"preflop_large_call_feature_decisions"`
	PreflopLargeCallMisses           int                              `json:"preflop_large_call_misses"`
	PreflopLargeCallMissHands        int                              `json:"preflop_large_call_miss_hands"`
	PreflopLargeCallGuardFolds       int                              `json:"preflop_large_call_guard_folds"`
	PreflopLargeCallGuardFoldHands   int                              `json:"preflop_large_call_guard_fold_hands"`
	LegacyReraisedPreflopCalls       int                              `json:"legacy_reraised_preflop_calls"`
	LegacyReraisedPreflopCallHands   int                              `json:"legacy_reraised_preflop_call_hands"`
	PostflopAirCalls                 int                              `json:"postflop_air_calls"`
	RiverAirCalls                    int                              `json:"river_air_calls"`
	RiverCardFeatureDecisions        int                              `json:"river_card_feature_decisions"`
	RiverOnePairCalls                int                              `json:"river_one_pair_calls"`
	RiverWeakOnePairCalls            int                              `json:"river_weak_one_pair_calls"`
	RiverMissedFlushBoardCalls       int                              `json:"river_missed_flush_board_pair_calls"`
	RiverWeakMissedFlushCalls        int                              `json:"river_weak_missed_flush_board_pair_calls"`
	RiverBoardPairCalls              int                              `json:"river_board_pair_calls"`
	RiverMissedStraightBoardCalls    int                              `json:"river_missed_straight_board_pair_calls"`
	RiverMissedDrawBoardCalls        int                              `json:"river_missed_draw_board_pair_calls"`
	RiverRepeatedMissedDrawCalls     int                              `json:"river_repeated_missed_draw_board_pair_calls"`
	UnderpairFeatureDecisions        int                              `json:"underpair_feature_decisions"`
	UnderpairCalls                   int                              `json:"underpair_calls"`
	UnderpairCallHands               int                              `json:"underpair_call_hands"`
	UnderpairGuardFolds              int                              `json:"underpair_guard_folds"`
	UnderpairGuardFoldHands          int                              `json:"underpair_guard_fold_hands"`
	LegacyUnderpairCallCandidates    int                              `json:"legacy_underpair_call_candidates"`
	LegacyUnderpairCandidateHands    int                              `json:"legacy_underpair_candidate_hands"`
	RiverOnePairEdgeBuckets          map[string]int                   `json:"river_one_pair_call_edge_buckets"`
	NegativeEVCalls                  int                              `json:"negative_ev_calls"`
	QuestionableAirCalls             int                              `json:"questionable_air_calls"`
	AirCallDownHands                 int                              `json:"air_call_down_hands"`
	AKHighCardCalls                  int                              `json:"ak_high_card_calls"`
	AKHighCardCallDownHands          int                              `json:"ak_high_card_call_down_hands"`
	HighCardCalls                    int                              `json:"high_card_calls"`
	HighCardMultiStreetHands         int                              `json:"high_card_multi_street_call_hands"`
	HighCardThreeStreetHands         int                              `json:"high_card_three_street_call_hands"`
	FreeOptionFolds                  int                              `json:"free_option_folds"`
	LowEquityAllIns                  int                              `json:"low_equity_allins"`
	RiverHighEquityChecks            int                              `json:"river_high_equity_checks"`
	ProfileRateViolations            int                              `json:"profile_rate_violations"`
	AdvisedAllIns                    int                              `json:"advised_allins"`
	AdvisedAllInHands                int                              `json:"advised_allin_hands"`
	SecondPairFeatureDecisions       int                              `json:"second_pair_feature_decisions"`
	SecondPairRepeatedRaises         int                              `json:"second_pair_repeated_raises"`
	SecondPairRepeatedRaiseFolds     int                              `json:"second_pair_repeated_raise_folds"`
	NearAllInCollapses               int                              `json:"near_allin_collapses"`
	NearAllInCallCollapses           int                              `json:"near_allin_call_collapses"`
	NearAllInRaiseCollapses          int                              `json:"near_allin_raise_collapses"`
	IncompleteShowdownHands          int                              `json:"incomplete_showdown_hands"`
	ConflictExamples                 []Example                        `json:"conflict_examples,omitempty"`
	HighEquityFoldExamples           []Example                        `json:"high_equity_fold_examples,omitempty"`
	PreflopLargeCallMissExamples     []Example                        `json:"preflop_large_call_miss_examples,omitempty"`
	PreflopLargeCallGuardExamples    []Example                        `json:"preflop_large_call_guard_fold_examples,omitempty"`
	LegacyReraisedPreflopExamples    []Example                        `json:"legacy_reraised_preflop_call_examples,omitempty"`
	QuestionableCallExamples         []Example                        `json:"questionable_call_examples,omitempty"`
	RiverWeakPairExamples            []Example                        `json:"river_weak_one_pair_call_examples,omitempty"`
	RiverMissedFlushExamples         []Example                        `json:"river_missed_flush_board_pair_call_examples,omitempty"`
	RiverWeakMissedFlushExamples     []Example                        `json:"river_weak_missed_flush_board_pair_call_examples,omitempty"`
	RiverMissedDrawExamples          []Example                        `json:"river_missed_draw_board_pair_call_examples,omitempty"`
	RiverRepeatedMissedDrawExamples  []Example                        `json:"river_repeated_missed_draw_board_pair_call_examples,omitempty"`
	UnderpairCallExamples            []Example                        `json:"underpair_call_examples,omitempty"`
	UnderpairGuardFoldExamples       []Example                        `json:"underpair_guard_fold_examples,omitempty"`
	LegacyUnderpairExamples          []Example                        `json:"legacy_underpair_call_candidate_examples,omitempty"`
	FreeOptionFoldExamples           []Example                        `json:"free_option_fold_examples,omitempty"`
	GameLogicErrorExamples           []ErrorExample                   `json:"game_logic_error_examples,omitempty"`
	IncompleteShowdownExamples       []ErrorExample                   `json:"incomplete_showdown_examples,omitempty"`
	ActionDeviationExamples          []DeviationExample               `json:"action_deviation_examples,omitempty"`
	LateStreamExamples               []LateStreamExample              `json:"late_stream_examples,omitempty"`
	LateStreamDeviationExamples      []DeviationExample               `json:"late_stream_deviation_examples,omitempty"`
	StartupBoundaryExamples          []ErrorExample                   `json:"startup_boundary_examples,omitempty"`
	AirCallDownExamples              []CallDownExample                `json:"air_call_down_examples,omitempty"`
	StrategyP95US                    int64                            `json:"strategy_p95_us"`
	HTTPP95US                        int64                            `json:"http_p95_us"`
	Issues                           []Issue                          `json:"issues"`
	ProfileMetrics                   map[string]*ProfileMetric        `json:"profile_metrics"`
	SpecialProfileMetrics            map[string]*SpecialProfileMetric `json:"special_profile_metrics"`
}

type SpecialProfileMetric struct {
	AIProfile           string               `json:"ai_profile"`
	Hands               int                  `json:"hands"`
	Decisions           int                  `json:"decisions"`
	StreetDecisions     map[string]int       `json:"street_decisions"`
	StreetReached       map[string]int       `json:"street_reached"`
	StreetReachRates    map[string]float64   `json:"street_reach_rates"`
	ActionCounts        map[string]int       `json:"action_counts"`
	Wins                int                  `json:"wins"`
	Losses              int                  `json:"losses"`
	Ties                int                  `json:"ties"`
	WinRate             float64              `json:"win_rate"`
	NetProfit           float64              `json:"net_profit"`
	AverageProfit       float64              `json:"average_profit"`
	FacingPreflopRaise  int                  `json:"facing_preflop_raise"`
	PreflopReraises     int                  `json:"preflop_reraises"`
	FacingPostflopRaise int                  `json:"facing_postflop_raise"`
	PostflopReraises    int                  `json:"postflop_reraises"`
	LowEquityAllIns     int                  `json:"low_equity_allins"`
	LossDetails         []SpecialProfileLoss `json:"loss_details,omitempty"`
	handKeys            map[string]bool
	handDecisions       map[string][]SpecialProfileDecision
}

type SpecialProfileLoss struct {
	TableID       string                   `json:"table_id"`
	HandID        string                   `json:"hand_id"`
	PlayerID      string                   `json:"player_id"`
	ReachedStreet string                   `json:"reached_street"`
	Profit        float64                  `json:"profit"`
	Decisions     []SpecialProfileDecision `json:"decisions,omitempty"`
}

type SpecialProfileDecision struct {
	Street        string  `json:"street"`
	Action        string  `json:"action"`
	Amount        float64 `json:"amount"`
	RuleID        string  `json:"rule_id,omitempty"`
	HeroHandClass string  `json:"hero_hand_class,omitempty"`
	HandClass     string  `json:"hand_class,omitempty"`
	Equity        float64 `json:"equity"`
	Pot           float64 `json:"pot"`
	ToCall        float64 `json:"to_call"`
	PotOdds       float64 `json:"pot_odds"`
	CallEV        float64 `json:"call_ev"`
}

type ProfileMetric struct {
	AIProfile             string  `json:"ai_profile"`
	PersonalityID         string  `json:"personality_id"`
	StrategyLevel         int     `json:"strategy_level"`
	ProfileSource         string  `json:"profile_source"`
	TargetVPIP            float64 `json:"target_vpip"`
	TargetPFR             float64 `json:"target_pfr"`
	PreflopHands          int     `json:"preflop_hands"`
	VPIPRate              float64 `json:"vpip_rate"`
	PFRRate               float64 `json:"pfr_rate"`
	Decisions             int     `json:"decisions"`
	PreflopDecisions      int     `json:"preflop_decisions"`
	PreflopFoldRate       float64 `json:"preflop_fold_rate"`
	PreflopAggressionRate float64 `json:"preflop_aggression_rate"`
	PreflopCallRate       float64 `json:"preflop_call_rate"`
	FacingPreflopRaise    int     `json:"facing_preflop_raise"`
	PreflopReraiseRate    float64 `json:"preflop_reraise_rate"`
	FacingPostflopBet     int     `json:"facing_postflop_bet"`
	PostflopFoldRate      float64 `json:"postflop_fold_rate"`
	PostflopCallAllInRate float64 `json:"postflop_call_allin_rate"`
	PostflopRaiseRate     float64 `json:"postflop_raise_rate"`
	HumanizedRate         float64 `json:"humanized_rate"`
	preflopFold           int
	preflopAggressive     int
	preflopCall           int
	preflopReraise        int
	postflopFold          int
	postflopCallAllIn     int
	postflopRaise         int
	humanized             int
	hands                 map[string]*profileHand
}

type profileHand struct{ vpip, pfr bool }

type entry struct {
	Time                         time.Time       `json:"time"`
	Msg                          string          `json:"msg"`
	Status                       int             `json:"status"`
	Outcome                      string          `json:"outcome"`
	ErrorCode                    string          `json:"error_code"`
	ErrorMessage                 string          `json:"error_message"`
	Cmd                          string          `json:"cmd"`
	SeqNum                       uint64          `json:"seq_num"`
	Advise                       json.RawMessage `json:"advise"`
	PolicyVersion                string          `json:"policy_version"`
	AIProfile                    string          `json:"ai_profile"`
	PersonalityID                string          `json:"personality_id"`
	StrategyLevel                int             `json:"strategy_level"`
	ProfileSource                string          `json:"profile_source"`
	TargetVPIP                   float64         `json:"target_vpip"`
	TargetPFR                    float64         `json:"target_pfr"`
	Street                       string          `json:"street"`
	Action                       string          `json:"action"`
	Amount                       float64         `json:"amount"`
	RuleID                       string          `json:"rule_id"`
	Tags                         []string        `json:"tags"`
	DecisionID                   string          `json:"decision_id"`
	EventFingerprint             string          `json:"event_fingerprint"`
	HandID                       string          `json:"hand_id"`
	TableID                      string          `json:"table_id"`
	PlayerID                     string          `json:"player_id"`
	PreflopHandClass             string          `json:"preflop_hand_class"`
	HeroHandClass                string          `json:"hero_hand_class"`
	RaisesFaced                  int             `json:"raises_faced"`
	HandClass                    string          `json:"hand_class"`
	HandCategory                 poker.Category  `json:"hand_category"`
	RiverCardFeaturesAvailable   *bool           `json:"river_card_features_available"`
	PairFromBoardOnly            *bool           `json:"pair_from_board_only"`
	PocketPairUnderBoard         *bool           `json:"pocket_pair_under_board"`
	OnePairBelowTopBoard         *bool           `json:"one_pair_below_top_board"`
	PreflopLargeCallOutsideRange *bool           `json:"preflop_large_call_outside_range"`
	MissedFlushDraw              *bool           `json:"missed_flush_draw"`
	MissedStraightDraw           *bool           `json:"missed_straight_draw"`
	Equity                       float64         `json:"equity"`
	Pot                          float64         `json:"pot"`
	PotOdds                      float64         `json:"pot_odds"`
	CallEV                       float64         `json:"call_ev"`
	HeroPostflopCalls            int             `json:"hero_postflop_calls"`
	EffectiveStack               float64         `json:"effective_stack"`
	ToCall                       float64         `json:"to_call"`
	Humanized                    bool            `json:"humanized"`
	OriginalAction               string          `json:"original_action"`
	Normalization                string          `json:"normalization"`
	LatencyUS                    int64           `json:"latency_us"`
	Route                        string          `json:"route"`
	SameAction                   *bool           `json:"same_action"`
	CandidateError               string          `json:"candidate_error"`
	AuditExempt                  bool            `json:"audit_exempt"`
	Profit                       float64         `json:"profit"`
	ReachedStreet                string          `json:"reached_street"`
	BoardCardCount               int             `json:"board_card_count"`
	ShowdownPlayers              int             `json:"showdown_players"`
	Deviation                    *struct {
		ByType  *struct{ Actual, Expected string }  `json:"by_type"`
		ByValue *struct{ Actual, Expected float64 } `json:"by_value"`
	} `json:"deviation"`
}

func Analyze(path string, expected Expectations) (Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	defer file.Close()
	r := Report{GeneratedAt: time.Now(), Input: path, Passed: true, DecisionOutcomes: map[string]int{}, DecisionErrors: map[string]int{}, ErrorMessages: map[string]int{}, StartupBoundaryMessages: map[string]int{}, PolicyVersions: map[string]int{}, StreetCounts: map[string]int{}, ActionCounts: map[string]int{}, PreflopActions: map[string]int{}, GrayRoutes: map[string]int{}, ProfileMetrics: map[string]*ProfileMetric{}, SpecialProfileMetrics: map[string]*SpecialProfileMetric{}, RiverOnePairEdgeBuckets: map[string]int{}}
	var strategyLatency, httpLatency, delayedDealLatency, dealAfterStartMS []int64
	startTimes := make(map[string]time.Time)
	airCallStreets := make(map[string]map[string]bool)
	airCallExamples := make(map[string]map[string]Example)
	underpairCallHands := make(map[string]bool)
	underpairGuardHands := make(map[string]bool)
	legacyUnderpairHands := make(map[string]bool)
	preflopLargeCallMissHands := make(map[string]bool)
	preflopLargeCallGuardHands := make(map[string]bool)
	legacyReraisedPreflopHands := make(map[string]bool)
	advisedAllInHands := make(map[string]bool)
	akCallStreets := make(map[string]map[string]bool) // legacy metric, retained for old report consumers
	logicErrorExamples := make(map[string]*ErrorExample)
	pendingLogicErrors := make(map[string][]string)
	lastAdvice := make(map[string]adviceContext)
	tableHandEndedAt := make(map[string]time.Time)
	latePlayerStreams := make(map[string]lateStreamContext)
	tableProgress := make(map[string]tableHandProgress)
	specialHands := make(map[string]string)
	incompleteShowdownHands := make(map[string]bool)
	var serviceStartedAt time.Time
	startupGrace := time.Duration(expected.StartupBoundaryGraceMS) * time.Millisecond
	if startupGrace <= 0 {
		startupGrace = time.Minute
	}
	lateStreamThreshold := time.Duration(expected.LateStreamAfterEndMS) * time.Millisecond
	if lateStreamThreshold <= 0 {
		lateStreamThreshold = time.Second
	}
	delayedThreshold := expected.DelayedDealThresholdMS
	if delayedThreshold <= 0 {
		delayedThreshold = 1000
	}
	riverAirEdge := expected.MinRiverAirCallEquityEdge
	if riverAirEdge <= 0 {
		riverAirEdge = .10
	}
	riverPairEdge := expected.MinRiverPairCallEquityEdge
	if riverPairEdge <= 0 {
		riverPairEdge = .10
	}
	lowEquityAllIn := expected.LowEquityAllInThreshold
	if lowEquityAllIn <= 0 {
		lowEquityAllIn = .35
	}
	riverHighEquity := expected.RiverHighEquityThreshold
	if riverHighEquity <= 0 {
		riverHighEquity = .80
	}
	preflop := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		r.Lines++
		var item entry
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			r.MalformedLines++
			if len(r.MalformedLineExamples) < 20 {
				r.MalformedLineExamples = append(r.MalformedLineExamples, r.Lines)
			}
			continue
		}
		switch item.Msg {
		case "ainp started":
			serviceStartedAt = item.Time
		case "http_access":
			r.HTTPRequests++
			httpLatency = append(httpLatency, item.LatencyUS)
			if item.Status >= 400 {
				r.HTTPErrors++
			}
		case "decision_event":
			startupBoundary := isStartupBoundary(item, serviceStartedAt, startupGrace)
			if startupBoundary {
				r.StartupBoundaryErrors++
				if len(r.StartupBoundaryExamples) < 20 {
					r.StartupBoundaryExamples = append(r.StartupBoundaryExamples, ErrorExample{
						EventFingerprint: item.EventFingerprint, DecisionID: item.DecisionID, TableID: item.TableID,
						HandID: item.HandID, PlayerID: item.PlayerID, SeqNum: item.SeqNum, Command: item.Cmd,
						Message: item.ErrorMessage, Occurrences: 1,
					})
				}
			}
			r.DecisionOutcomes[item.Outcome]++
			if item.ErrorCode != "" {
				r.DecisionErrors[item.ErrorCode]++
			}
			if item.ErrorMessage != "" {
				if startupBoundary {
					r.StartupBoundaryMessages[item.ErrorMessage]++
				} else {
					r.ErrorMessages[item.ErrorMessage]++
				}
			}
			key := eventHandKey(item)
			tableKey := eventTableHandKey(item)
			sequenceKey := eventSequenceKey(item)
			// A rejected event does not advance the stream. If the caller replaces
			// it with an accepted event at the same sequence number, retain the raw
			// rejection in DecisionErrors but do not fail the actionable state gate.
			if item.Outcome == "applied" {
				for _, exampleKey := range pendingLogicErrors[sequenceKey] {
					if r.GameLogicErrors > 0 {
						r.GameLogicErrors--
					}
					r.RecoveredGameLogicErrors++
					if example := logicErrorExamples[exampleKey]; example != nil {
						example.Occurrences--
						if example.Occurrences == 0 {
							delete(logicErrorExamples, exampleKey)
						}
					}
				}
				delete(pendingLogicErrors, sequenceKey)
			}
			if item.Cmd == "start_hand_extended" && item.Outcome == "applied" && !item.Time.IsZero() {
				late := lateStreamContext{}
				if endedAt, ok := tableHandEndedAt[tableKey]; ok {
					delta := item.Time.Sub(endedAt)
					if delta >= lateStreamThreshold {
						late = lateStreamContext{reason: "after_end", startDelayMS: delta.Milliseconds(), startAfterEndMS: delta.Milliseconds()}
					}
				}
				if late.reason == "" {
					if progress, ok := tableProgress[tableKey]; ok && progress.maxSeq >= 2 && !progress.startedAt.IsZero() {
						delta := item.Time.Sub(progress.startedAt)
						if delta >= lateStreamThreshold {
							late = lateStreamContext{reason: "after_table_progress", startDelayMS: delta.Milliseconds(), tableSeqAtStart: progress.maxSeq}
						}
					}
				}
				if late.reason != "" {
					latePlayerStreams[key] = late
					r.LatePlayerStreams++
					if late.reason == "after_end" {
						r.LateStreamsAfterEnd++
					} else {
						r.LateStreamsAfterTableProgress++
					}
					if len(r.LateStreamExamples) < 20 {
						r.LateStreamExamples = append(r.LateStreamExamples, LateStreamExample{
							DecisionID: item.DecisionID, TableID: item.TableID, HandID: item.HandID, PlayerID: item.PlayerID,
							Reason: late.reason, StartDelayMS: late.startDelayMS, StartAfterEndMS: late.startAfterEndMS, TableSeqAtStart: late.tableSeqAtStart,
						})
					}
				}
			}
			lateStream := latePlayerStreams[key]
			lateStreamDelayMS := lateStream.startDelayMS
			if item.Outcome == "applied" && !item.Time.IsZero() {
				progress := tableProgress[tableKey]
				if progress.startedAt.IsZero() && item.Cmd == "start_hand_extended" {
					progress.startedAt = item.Time
				}
				if item.SeqNum > progress.maxSeq {
					progress.maxSeq = item.SeqNum
				}
				tableProgress[tableKey] = progress
			}
			if item.Outcome == "applied" && hasAdvice(item.Advise) {
				lastAdvice[key] = adviceContext{time: item.Time, decisionID: item.DecisionID, command: item.Cmd}
				if adviceType(item.Advise) == "allin" {
					r.AdvisedAllIns++
					if !advisedAllInHands[key] {
						advisedAllInHands[key] = true
						r.AdvisedAllInHands++
					}
				}
			}
			if item.Cmd == "start_hand_extended" && item.Outcome == "applied" && !item.Time.IsZero() {
				startTimes[key] = item.Time
			}
			if item.Cmd == "deal_cards" {
				r.DealCardsEvents++
				if item.Outcome != "applied" {
					if startupBoundary {
						r.StartupRejectedDealCards++
					} else {
						r.RejectedDealCards++
					}
				}
				delayMS := int64(0)
				if started, ok := startTimes[key]; ok && !item.Time.IsZero() {
					delayMS = item.Time.Sub(started).Milliseconds()
					if delayMS >= 0 {
						dealAfterStartMS = append(dealAfterStartMS, delayMS)
					}
				}
				if item.SeqNum > 2 || delayMS >= delayedThreshold {
					r.DelayedDealCards++
					delayedDealLatency = append(delayedDealLatency, item.LatencyUS)
					if item.Outcome == "applied" {
						if hasAdvice(item.Advise) {
							r.DelayedDealsWithAdvice++
						} else {
							r.DelayedDealsNoAdvice++
						}
					}
				}
			}
			if item.ErrorCode == "Game logic error" {
				r.GameLogicErrors++
				exampleKey := item.EventFingerprint + "|" + item.ErrorMessage
				if item.EventFingerprint == "" {
					exampleKey = item.DecisionID + "|" + item.ErrorMessage
				}
				example := logicErrorExamples[exampleKey]
				if example == nil {
					example = &ErrorExample{EventFingerprint: item.EventFingerprint, DecisionID: item.DecisionID, TableID: item.TableID, HandID: item.HandID, PlayerID: item.PlayerID, SeqNum: item.SeqNum, Command: item.Cmd, Message: item.ErrorMessage}
					logicErrorExamples[exampleKey] = example
				}
				example.Occurrences++
				pendingLogicErrors[sequenceKey] = append(pendingLogicErrors[sequenceKey], exampleKey)
			}
			if item.ErrorCode == "Wrong seq_num" {
				r.WrongSequenceErrors++
			}
			specialProfile := specialHands[key]
			if item.Deviation != nil && item.Deviation.ByType != nil && specialProfile == "" {
				if lateStreamDelayMS > 0 {
					r.LateStreamTypeDeviations++
				} else {
					r.ActionTypeDeviations++
				}
			}
			if item.Deviation != nil && item.Deviation.ByValue != nil && specialProfile == "" {
				if lateStreamDelayMS > 0 {
					r.LateStreamValueDeviations++
				} else {
					r.ActionValueDeviations++
				}
			}
			if item.Deviation != nil && specialProfile == "" {
				example := DeviationExample{DecisionID: item.DecisionID, TableID: item.TableID, HandID: item.HandID, PlayerID: item.PlayerID, SeqNum: item.SeqNum}
				example.LateStreamReason = lateStream.reason
				example.StartDelayMS = lateStream.startDelayMS
				example.StartAfterEndMS = lateStream.startAfterEndMS
				example.TableSeqAtStart = lateStream.tableSeqAtStart
				if advice, ok := lastAdvice[key]; ok {
					example.AdviceDecisionID, example.AdviceCommand = advice.decisionID, advice.command
					if !item.Time.IsZero() && !advice.time.IsZero() {
						example.AdviceAgeMS = item.Time.Sub(advice.time).Milliseconds()
					}
				}
				if item.Deviation.ByType != nil {
					example.ActualAction, example.ExpectedAction = item.Deviation.ByType.Actual, item.Deviation.ByType.Expected
				}
				if item.Deviation.ByValue != nil {
					example.ActualValue, example.ExpectedValue = item.Deviation.ByValue.Actual, item.Deviation.ByValue.Expected
				}
				if lateStreamDelayMS > 0 {
					if len(r.LateStreamDeviationExamples) < 20 {
						r.LateStreamDeviationExamples = append(r.LateStreamDeviationExamples, example)
					}
				} else if len(r.ActionDeviationExamples) < 20 {
					r.ActionDeviationExamples = append(r.ActionDeviationExamples, example)
				}
			}
			if item.Cmd == "end_hand" && item.Outcome == "applied" {
				if endedAt, ok := tableHandEndedAt[tableKey]; !ok || (!item.Time.IsZero() && item.Time.Before(endedAt)) {
					tableHandEndedAt[tableKey] = item.Time
				}
				delete(lastAdvice, key)
				delete(startTimes, key)
				delete(latePlayerStreams, key)
			}
		case "engine_decision_failed":
			r.DecisionOutcomes["engine_failed"]++
		case "game_state_normalized":
			if item.Normalization == "free_call_street_total" {
				r.FreeCallTotalNormalizations++
			}
		case "near_allin_collapsed":
			r.NearAllInCollapses++
			if item.OriginalAction == "call" {
				r.NearAllInCallCollapses++
			} else {
				r.NearAllInRaiseCollapses++
			}
		case "hand_result":
			key := eventHandKey(item)
			if item.ShowdownPlayers >= 2 && item.BoardCardCount < 5 {
				tableHandKey := eventTableHandKey(item)
				if !incompleteShowdownHands[tableHandKey] {
					incompleteShowdownHands[tableHandKey] = true
					r.IncompleteShowdownHands++
					if len(r.IncompleteShowdownExamples) < 20 {
						r.IncompleteShowdownExamples = append(r.IncompleteShowdownExamples, ErrorExample{
							TableID: item.TableID, HandID: item.HandID, PlayerID: item.PlayerID,
							Message: fmt.Sprintf("showdown has %d players but only %d board cards reached AinP", item.ShowdownPlayers, item.BoardCardCount),
						})
					}
				}
			}
			// Only a preceding audit_exempt strategy decision classifies a hand
			// as special. Every hand_result carries ai_profile, so using that
			// field alone would pollute this block with all normal profiles.
			profile := specialHands[key]
			if profile != "" {
				updateSpecialHandResult(r.SpecialProfileMetrics, profile, item)
			}
			delete(specialHands, key)
		case "strategy_decision":
			if item.OnePairBelowTopBoard != nil {
				r.SecondPairFeatureDecisions++
				if *item.OnePairBelowTopBoard && item.RaisesFaced >= 2 {
					if item.Action == "raise" || (item.Action == "allin" && isActiveAllIn(item)) {
						r.SecondPairRepeatedRaises++
					}
					if item.Action == "fold" && item.RuleID == "POSTFLOP_SECOND_PAIR_RERAISE_FOLD" {
						r.SecondPairRepeatedRaiseFolds++
					}
				}
			}
			if item.AuditExempt {
				key := eventHandKey(item)
				specialHands[key] = item.AIProfile
				updateSpecialDecision(r.SpecialProfileMetrics, item)
				continue
			}
			r.StrategyDecisions++
			strategyLatency = append(strategyLatency, item.LatencyUS)
			r.PolicyVersions[item.PolicyVersion]++
			r.StreetCounts[item.Street]++
			r.ActionCounts[item.Action]++
			if item.Humanized {
				r.Humanized++
			}
			updateProfileMetric(r.ProfileMetrics, item)
			if item.Street == "preflop" && item.PreflopLargeCallOutsideRange != nil {
				r.PreflopLargeCallFeatureDecisions++
				if *item.PreflopLargeCallOutsideRange && isCallEquivalent(item) {
					r.PreflopLargeCallMisses++
					key := eventHandKey(item)
					if !preflopLargeCallMissHands[key] {
						preflopLargeCallMissHands[key] = true
						r.PreflopLargeCallMissHands++
					}
					appendExample(&r.PreflopLargeCallMissExamples, item)
				}
				if *item.PreflopLargeCallOutsideRange && item.Action == "fold" && item.RuleID == "PREFLOP_PROFILE_LARGE_CALL_FOLD" {
					r.PreflopLargeCallGuardFolds++
					key := eventHandKey(item)
					if !preflopLargeCallGuardHands[key] {
						preflopLargeCallGuardHands[key] = true
						r.PreflopLargeCallGuardFoldHands++
					}
					appendExample(&r.PreflopLargeCallGuardExamples, item)
				}
			} else if isLegacyReraisedPreflopCall(item) {
				r.LegacyReraisedPreflopCalls++
				key := eventHandKey(item)
				if !legacyReraisedPreflopHands[key] {
					legacyReraisedPreflopHands[key] = true
					r.LegacyReraisedPreflopCallHands++
				}
				appendExample(&r.LegacyReraisedPreflopExamples, item)
			}
			if (item.Street == "turn" || item.Street == "river") && item.PocketPairUnderBoard != nil {
				r.UnderpairFeatureDecisions++
				if *item.PocketPairUnderBoard && item.HandClass == "made" && isCallEquivalent(item) {
					r.UnderpairCalls++
					key := eventHandKey(item)
					if !underpairCallHands[key] {
						underpairCallHands[key] = true
						r.UnderpairCallHands++
					}
					appendExample(&r.UnderpairCallExamples, item)
				}
				if *item.PocketPairUnderBoard && item.Action == "fold" && item.RuleID == "POSTFLOP_UNDERPAIR_FOLD" {
					r.UnderpairGuardFolds++
					key := eventHandKey(item)
					if !underpairGuardHands[key] {
						underpairGuardHands[key] = true
						r.UnderpairGuardFoldHands++
					}
					appendExample(&r.UnderpairGuardFoldExamples, item)
				}
			} else if isLegacyUnderpairCallCandidate(item) {
				r.LegacyUnderpairCallCandidates++
				key := eventHandKey(item)
				if !legacyUnderpairHands[key] {
					legacyUnderpairHands[key] = true
					r.LegacyUnderpairCandidateHands++
				}
				appendExample(&r.LegacyUnderpairExamples, item)
			}
			if item.Street == "river" && item.RiverCardFeaturesAvailable != nil && *item.RiverCardFeaturesAvailable {
				r.RiverCardFeatureDecisions++
			}
			if item.ProfileSource == "default" {
				r.DefaultProfileFallbacks++
			}
			if item.Street == "preflop" {
				preflop++
				r.PreflopActions[item.Action]++
				if item.Action == "fold" && item.Equity >= expected.HighEquityThreshold {
					r.HighEquityPreflopFolds++
					appendExample(&r.HighEquityFoldExamples, item)
				}
			}
			if item.Action == "fold" && item.ToCall <= 1e-9 {
				r.FreeOptionFolds++
				appendExample(&r.FreeOptionFoldExamples, item)
			}
			if item.Action == "allin" && item.Equity < lowEquityAllIn && isActiveAllIn(item) {
				r.LowEquityAllIns++
			}
			if item.Street == "river" && item.Action == "check" && item.ToCall <= 1e-9 && item.Equity >= riverHighEquity && !aggressiveRule(item.RuleID) {
				r.RiverHighEquityChecks++
			}
			if item.Street != "preflop" && item.Action == "call" {
				if item.CallEV < -1e-9 {
					r.NegativeEVCalls++
					appendExample(&r.QuestionableCallExamples, item)
				}
				if item.HandClass == "air" {
					r.PostflopAirCalls++
					r.HighCardCalls++
					key := eventHandKey(item)
					if airCallStreets[key] == nil {
						airCallStreets[key] = make(map[string]bool)
						airCallExamples[key] = make(map[string]Example)
					}
					airCallStreets[key][item.Street] = true
					airCallExamples[key][item.Street] = exampleFromEntry(item)
					if item.Street == "river" {
						r.RiverAirCalls++
					}
					if item.CallEV < -1e-9 || (item.Street == "river" && item.Equity-item.PotOdds < riverAirEdge) {
						r.QuestionableAirCalls++
						appendExample(&r.QuestionableCallExamples, item)
					}
					if item.HeroHandClass == "AKs" || item.HeroHandClass == "AKo" {
						r.AKHighCardCalls++
						if akCallStreets[key] == nil {
							akCallStreets[key] = make(map[string]bool)
						}
						akCallStreets[key][item.Street] = true
					}
				}
			}
			if item.Street == "river" && isCallEquivalent(item) && item.HandCategory == poker.OnePair {
				r.RiverOnePairCalls++
				edge := item.Equity - item.PotOdds
				updateRiverPairEdgeBuckets(r.RiverOnePairEdgeBuckets, edge)
				weak := edge < riverPairEdge
				if weak {
					r.RiverWeakOnePairCalls++
					appendExample(&r.RiverWeakPairExamples, item)
				}
				featuresAvailable := item.RiverCardFeaturesAvailable != nil && *item.RiverCardFeaturesAvailable
				boardPair := featuresAvailable && item.PairFromBoardOnly != nil && *item.PairFromBoardOnly
				missedFlush := item.MissedFlushDraw != nil && *item.MissedFlushDraw
				missedStraight := item.MissedStraightDraw != nil && *item.MissedStraightDraw
				if boardPair {
					r.RiverBoardPairCalls++
				}
				if boardPair && missedFlush {
					r.RiverMissedFlushBoardCalls++
					appendExample(&r.RiverMissedFlushExamples, item)
					if weak {
						r.RiverWeakMissedFlushCalls++
						appendExample(&r.RiverWeakMissedFlushExamples, item)
					}
				}
				if boardPair && missedStraight {
					r.RiverMissedStraightBoardCalls++
				}
				if boardPair && (missedFlush || missedStraight) {
					r.RiverMissedDrawBoardCalls++
					appendExample(&r.RiverMissedDrawExamples, item)
					if item.HeroPostflopCalls > 0 {
						r.RiverRepeatedMissedDrawCalls++
						appendExample(&r.RiverRepeatedMissedDrawExamples, item)
					}
				}
			}
			if intentConflict(item) {
				r.IntentConflicts++
				appendExample(&r.ConflictExamples, item)
			}
		case "gray_decision":
			r.GrayRoutes[item.Route]++
			if item.SameAction != nil {
				r.GrayCompared++
				if *item.SameAction {
					r.GraySameAction++
				}
			}
			if item.CandidateError != "" {
				r.GrayCandidateErrors++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return r, err
	}
	r.HTTPErrorRate = rate(r.HTTPErrors, r.HTTPRequests)
	r.PreflopFoldRate = rate(r.PreflopActions["fold"], preflop)
	r.PreflopAggressionRate = rate(r.PreflopActions["raise"]+r.PreflopActions["allin"], preflop)
	r.StrategyP95US, r.HTTPP95US = percentile(strategyLatency, .95), percentile(httpLatency, .95)
	r.DelayedDealP95US = percentile(delayedDealLatency, .95)
	r.DealAfterStartP95MS = percentile(dealAfterStartMS, .95)
	if len(dealAfterStartMS) > 0 {
		r.DealAfterStartMaxMS = percentile(dealAfterStartMS, 1)
	}
	r.GrayActionAgreement = rate(r.GraySameAction, r.GrayCompared)
	finalizeProfileMetrics(r.ProfileMetrics)
	finalizeSpecialProfileMetrics(r.SpecialProfileMetrics)
	airKeys := make([]string, 0, len(airCallStreets))
	for key := range airCallStreets {
		airKeys = append(airKeys, key)
	}
	sort.Strings(airKeys)
	for _, key := range airKeys {
		streets := airCallStreets[key]
		if len(streets) >= 2 {
			r.HighCardMultiStreetHands++
		}
		if len(streets) >= 3 {
			r.AirCallDownHands++
			r.HighCardThreeStreetHands++
			if len(r.AirCallDownExamples) < 20 {
				calls := make([]Example, 0, 3)
				for _, street := range []string{"flop", "turn", "river"} {
					if example, ok := airCallExamples[key][street]; ok {
						calls = append(calls, example)
					}
				}
				if len(calls) > 0 {
					first := calls[0]
					r.AirCallDownExamples = append(r.AirCallDownExamples, CallDownExample{TableID: first.TableID, HandID: first.HandID, PlayerID: first.PlayerID, AIProfile: first.AIProfile, Calls: calls})
				}
			}
		}
	}
	r.UniqueGameLogicErrors = len(logicErrorExamples)
	for _, example := range logicErrorExamples {
		r.GameLogicErrorExamples = append(r.GameLogicErrorExamples, *example)
	}
	sort.Slice(r.GameLogicErrorExamples, func(i, j int) bool {
		return r.GameLogicErrorExamples[i].Occurrences > r.GameLogicErrorExamples[j].Occurrences
	})
	if len(r.GameLogicErrorExamples) > 20 {
		r.GameLogicErrorExamples = r.GameLogicErrorExamples[:20]
	}
	for _, streets := range akCallStreets {
		if len(streets) >= 2 {
			r.AKHighCardCallDownHands++
		}
	}
	minimumProfileHands := expected.MinimumProfileHands
	if minimumProfileHands <= 0 {
		minimumProfileHands = 1000
	}
	maxVPIPDeviation, maxPFRDeviation := expected.MaxVPIPDeviation, expected.MaxPFRDeviation
	if maxVPIPDeviation <= 0 {
		maxVPIPDeviation = .05
	}
	if maxPFRDeviation <= 0 {
		maxPFRDeviation = .05
	}
	if expected.EnforceProfileRates {
		for key, metric := range r.ProfileMetrics {
			if metric.PreflopHands < minimumProfileHands {
				continue
			}
			if abs(metric.VPIPRate-metric.TargetVPIP) > maxVPIPDeviation {
				r.ProfileRateViolations++
				r.Passed = false
				r.Issues = append(r.Issues, Issue{Check: "profile_vpip:" + key, Expected: fmt.Sprintf("target±%.3f", maxVPIPDeviation), Actual: fmt.Sprintf("%.3f/%.3f", metric.VPIPRate, metric.TargetVPIP)})
			}
			if abs(metric.PFRRate-metric.TargetPFR) > maxPFRDeviation {
				r.ProfileRateViolations++
				r.Passed = false
				r.Issues = append(r.Issues, Issue{Check: "profile_pfr:" + key, Expected: fmt.Sprintf("target±%.3f", maxPFRDeviation), Actual: fmt.Sprintf("%.3f/%.3f", metric.PFRRate, metric.TargetPFR)})
			}
		}
	}
	r.evaluate(expected)
	return r, nil
}

func isActiveAllIn(item entry) bool {
	if strings.Contains(item.RuleID, "CALL") {
		return false
	}
	for _, tag := range item.Tags {
		// Humanization may deliberately convert a fold into a bounded call. If
		// the remaining stack equals ToCall, the legal guard represents that
		// call as all-in; it is not an主动 shove and belongs with call analysis.
		if tag == "bounded_loose_call" {
			return false
		}
	}
	return true
}

func isCallEquivalent(item entry) bool {
	return item.Action == "call" || item.Action == "allin" && !isActiveAllIn(item)
}

func updateRiverPairEdgeBuckets(buckets map[string]int, edge float64) {
	switch {
	case edge < 0:
		buckets["negative"]++
	case edge < .03:
		buckets["0_to_3pct"]++
	case edge < .05:
		buckets["3_to_5pct"]++
	case edge < .10:
		buckets["5_to_10pct"]++
	default:
		buckets["at_least_10pct"]++
	}
}

func isStartupBoundary(item entry, startedAt time.Time, grace time.Duration) bool {
	if startedAt.IsZero() || item.Time.IsZero() || item.Outcome == "applied" ||
		item.ErrorMessage != "start_hand_extended must be the first event for a hand" {
		return false
	}
	delta := item.Time.Sub(startedAt)
	return delta >= 0 && delta <= grace
}

func abs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func updateProfileMetric(metrics map[string]*ProfileMetric, item entry) {
	key := fmt.Sprintf("%s|%s|L%d", item.AIProfile, item.PersonalityID, item.StrategyLevel)
	metric := metrics[key]
	if metric == nil {
		metric = &ProfileMetric{
			AIProfile: item.AIProfile, PersonalityID: item.PersonalityID, StrategyLevel: item.StrategyLevel,
			ProfileSource: item.ProfileSource, TargetVPIP: item.TargetVPIP, TargetPFR: item.TargetPFR,
			hands: map[string]*profileHand{},
		}
		metrics[key] = metric
	}
	metric.Decisions++
	if item.Humanized {
		metric.humanized++
	}
	if item.Street == "preflop" {
		handKey := item.PlayerID + "|" + item.TableID + "|" + item.HandID
		if handKey == "||" {
			handKey = item.DecisionID
		}
		hand := metric.hands[handKey]
		if hand == nil {
			hand = &profileHand{}
			metric.hands[handKey] = hand
		}
		metric.PreflopDecisions++
		if item.RaisesFaced > 0 {
			metric.FacingPreflopRaise++
			if item.Action == "raise" || item.Action == "allin" && isActiveAllIn(item) {
				metric.preflopReraise++
			}
		}
		switch item.Action {
		case "fold":
			metric.preflopFold++
		case "raise", "allin":
			metric.preflopAggressive++
			hand.vpip, hand.pfr = true, true
		case "call":
			metric.preflopCall++
			hand.vpip = true
		}
	} else if item.ToCall > 0 {
		metric.FacingPostflopBet++
		switch item.Action {
		case "fold":
			metric.postflopFold++
		case "call", "allin":
			metric.postflopCallAllIn++
		case "raise":
			metric.postflopRaise++
		}
	}
}

func finalizeProfileMetrics(metrics map[string]*ProfileMetric) {
	for _, metric := range metrics {
		vpip, pfr := 0, 0
		for _, hand := range metric.hands {
			if hand.vpip {
				vpip++
			}
			if hand.pfr {
				pfr++
			}
		}
		metric.PreflopHands = len(metric.hands)
		metric.VPIPRate = rate(vpip, metric.PreflopHands)
		metric.PFRRate = rate(pfr, metric.PreflopHands)
		metric.PreflopFoldRate = rate(metric.preflopFold, metric.PreflopDecisions)
		metric.PreflopAggressionRate = rate(metric.preflopAggressive, metric.PreflopDecisions)
		metric.PreflopCallRate = rate(metric.preflopCall, metric.PreflopDecisions)
		metric.PreflopReraiseRate = rate(metric.preflopReraise, metric.FacingPreflopRaise)
		metric.PostflopFoldRate = rate(metric.postflopFold, metric.FacingPostflopBet)
		metric.PostflopCallAllInRate = rate(metric.postflopCallAllIn, metric.FacingPostflopBet)
		metric.PostflopRaiseRate = rate(metric.postflopRaise, metric.FacingPostflopBet)
		metric.HumanizedRate = rate(metric.humanized, metric.Decisions)
	}
}

func specialProfileMetric(metrics map[string]*SpecialProfileMetric, profile string) *SpecialProfileMetric {
	metric := metrics[profile]
	if metric == nil {
		metric = &SpecialProfileMetric{
			AIProfile: profile, StreetDecisions: map[string]int{}, StreetReached: map[string]int{},
			StreetReachRates: map[string]float64{}, ActionCounts: map[string]int{}, handKeys: map[string]bool{},
			handDecisions: map[string][]SpecialProfileDecision{},
		}
		metrics[profile] = metric
	}
	return metric
}

func updateSpecialDecision(metrics map[string]*SpecialProfileMetric, item entry) {
	metric := specialProfileMetric(metrics, item.AIProfile)
	metric.Decisions++
	metric.StreetDecisions[item.Street]++
	metric.ActionCounts[item.Action]++
	if item.Street == "preflop" && item.RaisesFaced > 0 {
		metric.FacingPreflopRaise++
		if item.Action == "raise" || (item.Action == "allin" && isActiveAllIn(item)) {
			metric.PreflopReraises++
		}
	} else if item.Street != "preflop" && item.RaisesFaced > 0 {
		metric.FacingPostflopRaise++
		if item.Action == "raise" || (item.Action == "allin" && isActiveAllIn(item)) {
			metric.PostflopReraises++
		}
	}
	if item.Action == "allin" && item.Equity < .35 {
		metric.LowEquityAllIns++
	}
	key := eventHandKey(item)
	metric.handDecisions[key] = append(metric.handDecisions[key], SpecialProfileDecision{
		Street: item.Street, Action: item.Action, Amount: item.Amount, RuleID: item.RuleID,
		HeroHandClass: item.HeroHandClass, HandClass: item.HandClass, Equity: item.Equity,
		Pot: item.Pot, ToCall: item.ToCall, PotOdds: item.PotOdds, CallEV: item.CallEV,
	})
	if !metric.handKeys[key] {
		metric.handKeys[key] = true
		metric.Hands++
	}
}

func updateSpecialHandResult(metrics map[string]*SpecialProfileMetric, profile string, item entry) {
	metric := specialProfileMetric(metrics, profile)
	metric.NetProfit += item.Profit
	switch {
	case item.Profit > 1e-9:
		metric.Wins++
	case item.Profit < -1e-9:
		metric.Losses++
		key := eventHandKey(item)
		metric.LossDetails = append(metric.LossDetails, SpecialProfileLoss{
			TableID: item.TableID, HandID: item.HandID, PlayerID: item.PlayerID,
			ReachedStreet: item.ReachedStreet, Profit: item.Profit,
			Decisions: append([]SpecialProfileDecision(nil), metric.handDecisions[key]...),
		})
	default:
		metric.Ties++
	}
	streets := []string{"preflop", "flop", "turn", "river"}
	reached := 0
	for i, street := range streets {
		if street == item.ReachedStreet {
			reached = i
			break
		}
	}
	for i := 0; i <= reached; i++ {
		metric.StreetReached[streets[i]]++
	}
	delete(metric.handDecisions, eventHandKey(item))
}

func finalizeSpecialProfileMetrics(metrics map[string]*SpecialProfileMetric) {
	for _, metric := range metrics {
		results := metric.Wins + metric.Losses + metric.Ties
		metric.WinRate = rate(metric.Wins, results)
		if results > 0 {
			metric.AverageProfit = metric.NetProfit / float64(results)
		}
		for _, street := range []string{"preflop", "flop", "turn", "river"} {
			metric.StreetReachRates[street] = rate(metric.StreetReached[street], results)
		}
		sort.SliceStable(metric.LossDetails, func(i, j int) bool {
			if metric.LossDetails[i].Profit != metric.LossDetails[j].Profit {
				return metric.LossDetails[i].Profit < metric.LossDetails[j].Profit
			}
			left, right := metric.LossDetails[i], metric.LossDetails[j]
			return left.TableID+"\x00"+left.HandID+"\x00"+left.PlayerID < right.TableID+"\x00"+right.HandID+"\x00"+right.PlayerID
		})
		metric.handKeys = nil
		metric.handDecisions = nil
	}
}

func intentConflict(item entry) bool {
	if item.Action != "fold" || item.Humanized {
		return false
	}
	// An explicit guard rule ending in _FOLD has already resolved its intent to
	// fold. Names such as PREFLOP_PROFILE_LARGE_CALL_FOLD contain CALL only to
	// describe the rejected alternative and must not be reported as conflicts.
	if strings.HasSuffix(item.RuleID, "_FOLD") {
		return false
	}
	return strings.Contains(item.RuleID, "CALL") || strings.Contains(item.RuleID, "OPEN") || strings.Contains(item.RuleID, "VALUE") || strings.Contains(item.RuleID, "SEMIBLUFF")
}

func isLegacyUnderpairCallCandidate(item entry) bool {
	if item.PocketPairUnderBoard != nil || (item.Street != "turn" && item.Street != "river") ||
		item.HandClass != "made" || !isCallEquivalent(item) {
		return false
	}
	class := item.HeroHandClass
	return len(class) == 2 && class[0] == class[1]
}

// Old logs did not record the configured BB threshold or the derived range
// feature. Two or more raises followed by a call is therefore an intentionally
// broad candidate set, not proof that the new large-call guard should fire.
func isLegacyReraisedPreflopCall(item entry) bool {
	return item.PreflopLargeCallOutsideRange == nil && item.Street == "preflop" && item.RaisesFaced >= 2 && isCallEquivalent(item)
}

func appendExample(target *[]Example, item entry) {
	for _, existing := range *target {
		if existing.DecisionID == item.DecisionID && item.DecisionID != "" {
			return
		}
	}
	if len(*target) >= 20 {
		return
	}
	*target = append(*target, exampleFromEntry(item))
}

func exampleFromEntry(item entry) Example {
	handClass := item.HandClass
	if handClass == "" {
		handClass = item.PreflopHandClass
	}
	example := Example{
		DecisionID: item.DecisionID, TableID: item.TableID, HandID: item.HandID, PlayerID: item.PlayerID,
		AIProfile: item.AIProfile, Street: item.Street, HeroClass: item.HeroHandClass, HandClass: handClass,
		Action: item.Action, RuleID: item.RuleID, Equity: item.Equity, Stack: item.EffectiveStack,
		ToCall: item.ToCall, Pot: item.Pot, PotOdds: item.PotOdds, CallEV: item.CallEV, HeroPostflopCalls: item.HeroPostflopCalls,
	}
	if item.RiverCardFeaturesAvailable != nil && *item.RiverCardFeaturesAvailable && item.PairFromBoardOnly != nil {
		example.FeatureAvailable = true
		example.PairFromBoardOnly = *item.PairFromBoardOnly
		if item.MissedFlushDraw != nil {
			example.MissedFlushDraw = *item.MissedFlushDraw
		}
		if item.MissedStraightDraw != nil {
			example.MissedStraightDraw = *item.MissedStraightDraw
		}
	}
	if item.PocketPairUnderBoard != nil {
		example.UnderpairFeatureAvailable = true
		example.PocketPairUnderBoard = *item.PocketPairUnderBoard
	}
	if item.PreflopLargeCallOutsideRange != nil {
		example.PreflopLargeCallFeatureAvailable = true
		example.PreflopLargeCallOutsideRange = *item.PreflopLargeCallOutsideRange
	}
	example.RaisesFaced = item.RaisesFaced
	return example
}

func eventHandKey(item entry) string {
	key := item.PlayerID + "|" + item.TableID + "|" + item.HandID
	if key == "||" {
		return item.DecisionID
	}
	return key
}

func eventTableHandKey(item entry) string {
	key := item.TableID + "|" + item.HandID
	if key == "|" {
		return item.DecisionID
	}
	return key
}

func eventSequenceKey(item entry) string {
	return eventHandKey(item) + "|" + strconv.FormatUint(item.SeqNum, 10) + "|" + item.Cmd
}

func aggressiveRule(ruleID string) bool {
	return strings.Contains(ruleID, "BET") || strings.Contains(ruleID, "RAISE") || strings.Contains(ruleID, "ALLIN") ||
		strings.Contains(ruleID, "SEMIBLUFF") || strings.Contains(ruleID, "THIN_VALUE")
}

func hasAdvice(raw json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed != "" && trimmed != "null"
}

func adviceType(raw json.RawMessage) string {
	var advice struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &advice) != nil {
		return ""
	}
	return advice.Type
}

func (r *Report) evaluate(e Expectations) {
	check := func(name string, ok bool, expected, actual string) {
		if !ok {
			r.Passed = false
			r.Issues = append(r.Issues, Issue{Check: name, Expected: expected, Actual: actual})
		}
	}
	check("minimum_strategy_decisions", r.StrategyDecisions >= e.MinimumStrategyDecisions, fmt.Sprintf(">=%d", e.MinimumStrategyDecisions), fmt.Sprint(r.StrategyDecisions))
	check("preflop_fold_rate", r.PreflopFoldRate >= e.MinPreflopFoldRate && r.PreflopFoldRate <= e.MaxPreflopFoldRate, fmt.Sprintf("%.3f..%.3f", e.MinPreflopFoldRate, e.MaxPreflopFoldRate), fmt.Sprintf("%.3f", r.PreflopFoldRate))
	check("preflop_aggression_rate", r.PreflopAggressionRate >= e.MinPreflopAggressionRate && r.PreflopAggressionRate <= e.MaxPreflopAggressionRate, fmt.Sprintf("%.3f..%.3f", e.MinPreflopAggressionRate, e.MaxPreflopAggressionRate), fmt.Sprintf("%.3f", r.PreflopAggressionRate))
	check("http_error_rate", r.HTTPErrorRate <= e.MaxHTTPErrorRate, fmt.Sprintf("<=%.3f", e.MaxHTTPErrorRate), fmt.Sprintf("%.3f", r.HTTPErrorRate))
	check("engine_failures", r.DecisionOutcomes["engine_failed"] <= e.MaxEngineFailures, fmt.Sprintf("<=%d", e.MaxEngineFailures), fmt.Sprint(r.DecisionOutcomes["engine_failed"]))
	check("intent_conflicts", r.IntentConflicts <= e.MaxIntentConflicts, fmt.Sprintf("<=%d", e.MaxIntentConflicts), fmt.Sprint(r.IntentConflicts))
	check("high_equity_preflop_folds", r.HighEquityPreflopFolds <= e.MaxHighEquityPreflopFolds, fmt.Sprintf("<=%d", e.MaxHighEquityPreflopFolds), fmt.Sprint(r.HighEquityPreflopFolds))
	check("strategy_p95_us", r.StrategyP95US <= e.MaxStrategyP95US, fmt.Sprintf("<=%d", e.MaxStrategyP95US), fmt.Sprint(r.StrategyP95US))
	check("http_p95_us", r.HTTPP95US <= e.MaxHTTPP95US, fmt.Sprintf("<=%d", e.MaxHTTPP95US), fmt.Sprint(r.HTTPP95US))
	check("gray_candidate_errors", r.GrayCandidateErrors <= e.MaxGrayCandidateErrors, fmt.Sprintf("<=%d", e.MaxGrayCandidateErrors), fmt.Sprint(r.GrayCandidateErrors))
	check("game_logic_errors", r.GameLogicErrors <= e.MaxGameLogicErrors, fmt.Sprintf("<=%d", e.MaxGameLogicErrors), fmt.Sprint(r.GameLogicErrors))
	check("incomplete_showdown_hands", r.IncompleteShowdownHands == 0, "<=0", fmt.Sprint(r.IncompleteShowdownHands))
	check("wrong_sequence_errors", r.WrongSequenceErrors <= e.MaxWrongSequenceErrors, fmt.Sprintf("<=%d", e.MaxWrongSequenceErrors), fmt.Sprint(r.WrongSequenceErrors))
	check("action_type_deviations", r.ActionTypeDeviations <= e.MaxActionTypeDeviations, fmt.Sprintf("<=%d", e.MaxActionTypeDeviations), fmt.Sprint(r.ActionTypeDeviations))
	check("default_profile_fallbacks", r.DefaultProfileFallbacks <= e.MaxDefaultProfileFallbacks, fmt.Sprintf("<=%d", e.MaxDefaultProfileFallbacks), fmt.Sprint(r.DefaultProfileFallbacks))
	if e.MaxDelayedDealP95US != nil && r.DelayedDealCards > 0 {
		check("delayed_deal_decision_p95_us", r.DelayedDealP95US <= *e.MaxDelayedDealP95US, fmt.Sprintf("<=%d", *e.MaxDelayedDealP95US), fmt.Sprint(r.DelayedDealP95US))
	}
	if e.MaxRejectedDealCards != nil {
		check("rejected_deal_cards", r.RejectedDealCards <= *e.MaxRejectedDealCards, fmt.Sprintf("<=%d", *e.MaxRejectedDealCards), fmt.Sprint(r.RejectedDealCards))
	}
	if e.MaxDelayedDealsNoAdvice != nil {
		check("delayed_deals_without_advice", r.DelayedDealsNoAdvice <= *e.MaxDelayedDealsNoAdvice, fmt.Sprintf("<=%d", *e.MaxDelayedDealsNoAdvice), fmt.Sprint(r.DelayedDealsNoAdvice))
	}
	if e.MaxNegativeEVCalls != nil {
		check("negative_ev_calls", r.NegativeEVCalls <= *e.MaxNegativeEVCalls, fmt.Sprintf("<=%d", *e.MaxNegativeEVCalls), fmt.Sprint(r.NegativeEVCalls))
	}
	if e.MaxQuestionableAirCalls != nil {
		check("questionable_air_calls", r.QuestionableAirCalls <= *e.MaxQuestionableAirCalls, fmt.Sprintf("<=%d", *e.MaxQuestionableAirCalls), fmt.Sprint(r.QuestionableAirCalls))
	}
	if e.MaxAirCallDownHands != nil {
		check("air_call_down_hands", r.AirCallDownHands <= *e.MaxAirCallDownHands, fmt.Sprintf("<=%d", *e.MaxAirCallDownHands), fmt.Sprint(r.AirCallDownHands))
	}
	if e.MaxFreeOptionFolds != nil {
		check("free_option_folds", r.FreeOptionFolds <= *e.MaxFreeOptionFolds, fmt.Sprintf("<=%d", *e.MaxFreeOptionFolds), fmt.Sprint(r.FreeOptionFolds))
	}
}

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
func percentile(values []int64, p float64) int64 {
	if len(values) == 0 {
		return 0
	}
	values = append([]int64(nil), values...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values[int(float64(len(values)-1)*p)]
}
