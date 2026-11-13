package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const nextActionMarker = "\tNextAction Success\t"
const callbackMarker = "\tcallBenapiHoldemCallback\t"
const holdemMarker = "\tcallBenapiHoldem\t"
const aofMarker = "\tcallBenapiAof\t"
const aofCallbackMarker = "\tcallBenapiAofCallback\t"

var logDatePattern = regexp.MustCompile(`(?:^|_)(20\d{6})(?:_|$)`)

var requestFields = []string{
	"n_players", "sb", "bb", "ante", "straddle", "stack", "private_hand", "beginner_bb_position",
	"beginner_blind_amount", "community_cards", "style_name", "anti_cheat_mode", "current_player_id",
	"button_player_id", "bb_player_id", "game_id", "uuids", "game_type", "spin_go_buyin",
	"spin_go_player_number", "spin_go_rewards", "squid_value", "squid_total_amount",
	"squid_player_distribution", "action_sequance", "use_diff_raise_logic", "meta_data",
}

type options struct {
	Input         []string
	ConfigPath    string
	Profiles      []string
	MaxCompare    int
	EquitySamples int
	MaxExamples   int
}

type benAction struct {
	PlayerID    int     `json:"player_id"`
	BetType     string  `json:"bet_type"`
	Amount      float64 `json:"amount"`
	ActionProb  float64 `json:"action_prob,omitempty"`
	RaiseAction string  `json:"raise_action,omitempty"`
}

type benRequest struct {
	NPlayers           int         `json:"n_players"`
	SB                 float64     `json:"sb"`
	BB                 float64     `json:"bb"`
	Ante               float64     `json:"ante"`
	Straddle           float64     `json:"straddle"`
	Stack              []float64   `json:"stack"`
	PrivateHand        []string    `json:"private_hand"`
	BeginnerBBPosition []int       `json:"beginner_bb_position"`
	CommunityCards     []string    `json:"community_cards"`
	StyleName          string      `json:"style_name"`
	AntiCheatMode      bool        `json:"anti_cheat_mode"`
	CurrentPlayerID    int         `json:"current_player_id"`
	ButtonPlayerID     *int        `json:"button_player_id"`
	BBPlayerID         *int        `json:"bb_player_id"`
	GameID             string      `json:"game_id"`
	UUIDs              []string    `json:"uuids"`
	GameType           string      `json:"game_type"`
	UseDiffRaiseLogic  bool        `json:"use_diff_raise_logic"`
	ActionSequence     []benAction `json:"action_sequance"`
}

type benExtraInfo struct {
	ActionProbs       []benAction `json:"action_probs"`
	OriginActionProbs []benAction `json:"origin_action_probs"`
	ActionProbsSource string      `json:"action_probs_source"`
	DecisionTime      float64     `json:"decision_time"`
	CurrentPot        float64     `json:"current_pot"`
	CurrentStack      float64     `json:"current_player_stack"`
	CurrentToCall     float64     `json:"current_player_tocall"`
}

type benResponse struct {
	PlayerID int          `json:"player_id"`
	BetType  string       `json:"bet_type"`
	Amount   float64      `json:"amount"`
	Extra    benExtraInfo `json:"extra_info"`
}

type nextActionEnvelope struct {
	UID      string          `json:"uid"`
	RoomID   json.RawMessage `json:"room_id"`
	HandNum  json.RawMessage `json:"hand_num"`
	Action   string          `json:"action"`
	BetType  string          `json:"bet_type"`
	Request  json.RawMessage `json:"req"`
	Response json.RawMessage `json:"resp.Data"`
}

type callbackEnvelope struct {
	UID          string          `json:"uid"`
	RoomID       json.RawMessage `json:"room_id"`
	Body         string          `json:"body"`
	ResponseBody string          `json:"response_body"`
}

type serviceResponseEnvelope struct {
	Code int         `json:"code"`
	Data benResponse `json:"data"`
}

type decisionRecord struct {
	File        string
	Line        int
	UID         string
	RoomID      string
	HandNum     string
	Request     benRequest
	RequestMap  map[string]json.RawMessage
	Response    benResponse
	BenAction   string
	Street      string
	Position    string
	ChosenProb  float64
	Argmax      string
	Fingerprint [32]byte
}

type callbackRecord struct {
	UID         string
	RoomID      string
	Request     benRequest
	RequestMap  map[string]json.RawMessage
	Payoff      []float64
	Rake        []float64
	Showdown    [][]string
	Fingerprint [32]byte
}

type fileStat struct {
	Path               string            `json:"path"`
	Source             string            `json:"source"`
	Partial            bool              `json:"partial"`
	SizeBytes          int64             `json:"size_bytes"`
	Lines              int               `json:"lines"`
	DecisionRecords    int               `json:"decision_records"`
	CallbackRecords    int               `json:"callback_records"`
	APIRequestRecords  int               `json:"api_request_records"`
	APIResponseRecords int               `json:"api_response_records"`
	APIResponseCodes   map[string]int    `json:"api_response_codes,omitempty"`
	APIResponseErrors  map[string]int    `json:"api_response_errors,omitempty"`
	CallbackResponses  int               `json:"callback_response_records"`
	CallbackCodes      map[string]int    `json:"callback_response_codes,omitempty"`
	CallbackErrors     map[string]int    `json:"callback_response_errors,omitempty"`
	AOF                aofStat           `json:"aof"`
	Malformed          int               `json:"malformed_records"`
	MalformedExamples  []malformedRecord `json:"malformed_examples,omitempty"`
}

type datasetStat struct {
	Files                    int                    `json:"files"`
	PartialFiles             int                    `json:"partial_files"`
	SizeBytes                int64                  `json:"size_bytes"`
	RawDecisionRecords       int                    `json:"raw_decision_records"`
	DuplicateDecisionRecords int                    `json:"duplicate_decision_records"`
	RawCallbackRecords       int                    `json:"raw_callback_records"`
	DuplicateCallbackRecords int                    `json:"duplicate_callback_records"`
	APIRequestRecords        int                    `json:"api_request_records"`
	APIResponseRecords       int                    `json:"api_response_records"`
	APIResponseCodes         map[string]int         `json:"api_response_codes"`
	APIResponseErrors        map[string]int         `json:"api_response_errors"`
	CallbackResponseRecords  int                    `json:"callback_response_records"`
	CallbackResponseCodes    map[string]int         `json:"callback_response_codes"`
	CallbackResponseErrors   map[string]int         `json:"callback_response_errors"`
	Sources                  map[string]*sourceStat `json:"sources"`
}

type aofStat struct {
	RequestRecords          int              `json:"request_records"`
	ResponseRecords         int              `json:"response_records"`
	ResponseCodes           map[string]int   `json:"response_codes"`
	ResponseErrors          map[string]int   `json:"response_errors"`
	Actions                 map[string]int   `json:"actions"`
	AIVersions              map[string]int   `json:"ai_versions"`
	CallbackRequestRecords  int              `json:"callback_request_records"`
	CallbackResponseRecords int              `json:"callback_response_records"`
	CallbackResponseCodes   map[string]int   `json:"callback_response_codes"`
	CallbackResponseErrors  map[string]int   `json:"callback_response_errors"`
	PushEV                  distributionStat `json:"push_ev"`
	FoldEV                  distributionStat `json:"fold_ev"`
	pushEVValues            []float64
	foldEVValues            []float64
}

type sourceStat struct {
	Files           int   `json:"files"`
	PartialFiles    int   `json:"partial_files"`
	SizeBytes       int64 `json:"size_bytes"`
	DecisionRecords int   `json:"decision_records"`
	CallbackRecords int   `json:"callback_records"`
	Malformed       int   `json:"malformed_records"`
}

type malformedRecord struct {
	Line   int    `json:"line"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type fieldStat struct {
	Present int     `json:"present"`
	Rate    float64 `json:"rate"`
}

type probabilityStat struct {
	RecordsWithProbabilities   int                          `json:"records_with_probabilities"`
	MixedStrategyRecords       int                          `json:"mixed_strategy_records"`
	ChosenIsArgmax             int                          `json:"chosen_is_argmax"`
	ChosenIsNotArgmax          int                          `json:"chosen_is_not_argmax"`
	ChosenArgmaxRate           float64                      `json:"chosen_argmax_rate"`
	MeanChosenProbability      float64                      `json:"mean_chosen_probability"`
	ActionProbSource           map[string]int               `json:"action_prob_source"`
	OriginProbabilityRecords   int                          `json:"origin_probability_records"`
	AdjustedProbabilityRecords int                          `json:"adjusted_probability_records"`
	UseX1ActionTrue            int                          `json:"use_x1_action_true"`
	ByStreet                   map[string]*probabilityGroup `json:"by_street"`
	ByStyle                    map[string]*probabilityGroup `json:"by_style"`
}

type probabilityGroup struct {
	Records               int     `json:"records"`
	MixedStrategyRecords  int     `json:"mixed_strategy_records"`
	ChosenIsArgmax        int     `json:"chosen_is_argmax"`
	ChosenArgmaxRate      float64 `json:"chosen_argmax_rate"`
	MeanChosenProbability float64 `json:"mean_chosen_probability"`
	chosenProbabilitySum  float64
}

type distributionStat struct {
	Count int     `json:"count"`
	Mean  float64 `json:"mean"`
	P50   float64 `json:"p50"`
	P90   float64 `json:"p90"`
	P95   float64 `json:"p95"`
	P99   float64 `json:"p99"`
	Max   float64 `json:"max"`
}

type sizingStat struct {
	AggressiveDecisions  int                       `json:"aggressive_decisions"`
	AllInDecisions       int                       `json:"all_in_decisions"`
	AllInRate            float64                   `json:"all_in_rate"`
	RaiseActions         map[string]int            `json:"raise_actions"`
	RaiseActionsByStreet map[string]map[string]int `json:"raise_actions_by_street"`
}

type callbackPerformance struct {
	Hands            int                          `json:"hands"`
	SkippedRecords   int                          `json:"skipped_records"`
	Wins             int                          `json:"wins"`
	Losses           int                          `json:"losses"`
	Ties             int                          `json:"ties"`
	Showdowns        int                          `json:"showdowns"`
	NonZeroRakeHands int                          `json:"non_zero_rake_hands"`
	PayoffBB         float64                      `json:"payoff_bb"`
	RakeBB           float64                      `json:"rake_bb"`
	MeanPayoffBB     float64                      `json:"mean_payoff_bb"`
	StdDevPayoffBB   float64                      `json:"stddev_payoff_bb"`
	StandardErrorBB  float64                      `json:"standard_error_bb"`
	CI95LowBB        float64                      `json:"ci95_low_bb"`
	CI95HighBB       float64                      `json:"ci95_high_bb"`
	ByStyle          map[string]*stylePerformance `json:"by_style"`
	BySource         map[string]*stylePerformance `json:"by_source"`
	payoffSquareSum  float64
}

type stylePerformance struct {
	Hands           int     `json:"hands"`
	Wins            int     `json:"wins"`
	Losses          int     `json:"losses"`
	Ties            int     `json:"ties"`
	PayoffBB        float64 `json:"payoff_bb"`
	MeanPayoffBB    float64 `json:"mean_payoff_bb"`
	StdDevPayoffBB  float64 `json:"stddev_payoff_bb"`
	StandardErrorBB float64 `json:"standard_error_bb"`
	CI95LowBB       float64 `json:"ci95_low_bb"`
	CI95HighBB      float64 `json:"ci95_high_bb"`
	payoffSquareSum float64
}

type inputFeatureStat struct {
	PlayerCounts             map[string]int `json:"player_counts"`
	GameTypes                map[string]int `json:"game_types"`
	AntiCheatModes           map[string]int `json:"anti_cheat_modes"`
	UseDiffRaiseLogic        map[string]int `json:"use_diff_raise_logic"`
	StraddleNonZero          int            `json:"straddle_non_zero"`
	BeginnerPositionNonEmpty int            `json:"beginner_bb_position_non_empty"`
	BeginnerAmountNonZero    int            `json:"beginner_blind_amount_non_zero"`
	CustomButtonPosition     int            `json:"custom_button_player_id"`
	CustomBBPosition         int            `json:"custom_bb_player_id"`
	CompleteUUIDLists        int            `json:"complete_uuid_lists"`
}

type preflopMetric struct {
	Hands     int     `json:"hands"`
	VPIPHands int     `json:"vpip_hands"`
	PFRHands  int     `json:"pfr_hands"`
	VPIPRate  float64 `json:"vpip_rate"`
	PFRRate   float64 `json:"pfr_rate"`
}

type preflopHand struct {
	style string
	vpip  bool
	pfr   bool
}

type periodStat struct {
	Files                  int                      `json:"files"`
	PartialFiles           int                      `json:"partial_files"`
	SizeBytes              int64                    `json:"size_bytes"`
	Decisions              int                      `json:"decisions"`
	Callbacks              int                      `json:"callbacks"`
	MalformedRecords       int                      `json:"malformed_records"`
	Streets                map[string]int           `json:"streets"`
	Styles                 map[string]int           `json:"styles"`
	Actions                map[string]int           `json:"actions"`
	StraddleNonZero        int                      `json:"straddle_non_zero"`
	StraddleRate           float64                  `json:"straddle_rate"`
	BeginnerBlindNonZero   int                      `json:"beginner_blind_non_zero"`
	BeginnerBlindRate      float64                  `json:"beginner_blind_rate"`
	Probability            probabilityGroup         `json:"probability_analysis"`
	PreflopMetricsByStyle  map[string]preflopMetric `json:"preflop_metrics_by_style"`
	CallbackPerformance    callbackPerformance      `json:"callback_performance"`
	APIResponseCodes       map[string]int           `json:"api_response_codes"`
	APIResponseErrors      map[string]int           `json:"api_response_errors"`
	CallbackResponseCodes  map[string]int           `json:"callback_response_codes"`
	CallbackResponseErrors map[string]int           `json:"callback_response_errors"`
	AOF                    aofStat                  `json:"aof_analysis"`
	preflopHands           map[string]*preflopHand
}

type report struct {
	SchemaVersion            int                          `json:"schema_version"`
	GeneratedAt              time.Time                    `json:"generated_at"`
	Dataset                  datasetStat                  `json:"dataset"`
	Inputs                   []fileStat                   `json:"inputs"`
	Decisions                int                          `json:"decisions"`
	Callbacks                int                          `json:"callbacks"`
	MalformedRecords         int                          `json:"malformed_records"`
	RequestFieldCoverage     map[string]fieldStat         `json:"request_field_coverage"`
	CallbackFieldCoverage    map[string]fieldStat         `json:"callback_field_coverage"`
	Styles                   map[string]int               `json:"styles"`
	Streets                  map[string]int               `json:"streets"`
	Positions                map[string]int               `json:"positions"`
	BenActions               map[string]int               `json:"ben_actions"`
	ActionsByStreet          map[string]map[string]int    `json:"actions_by_street"`
	ActionsByPosition        map[string]map[string]int    `json:"actions_by_position"`
	PreflopActionsByPosition map[string]map[string]int    `json:"preflop_actions_by_position"`
	ActionsByStyle           map[string]map[string]int    `json:"actions_by_style"`
	PreflopMetricsByStyle    map[string]preflopMetric     `json:"preflop_metrics_by_style"`
	InputFeatures            inputFeatureStat             `json:"input_feature_summary"`
	Probability              probabilityStat              `json:"probability_analysis"`
	DecisionTime             distributionStat             `json:"decision_time"`
	Sizing                   sizingStat                   `json:"sizing_analysis"`
	CallbackPerformance      callbackPerformance          `json:"callback_performance"`
	ByDate                   map[string]*periodStat       `json:"by_date"`
	AOF                      aofStat                      `json:"aof_analysis"`
	RetainedForComparison    int                          `json:"retained_for_comparison"`
	ComparisonSampleStreet   map[string]int               `json:"comparison_sample_by_street"`
	ComparisonAssumptions    []string                     `json:"comparison_assumptions"`
	Comparisons              map[string]profileComparison `json:"ainp_comparisons"`
}

type reservoir struct {
	cap  int
	seen int
	rng  *rand.Rand
	rows []decisionRecord
}

func (r *reservoir) add(item decisionRecord) {
	r.seen++
	if len(r.rows) < r.cap {
		r.rows = append(r.rows, item)
		return
	}
	if index := r.rng.Intn(r.seen); index < r.cap {
		r.rows[index] = item
	}
}

func analyze(opts options) (report, error) {
	paths, err := expandInputs(opts.Input)
	if err != nil {
		return report{}, err
	}
	if len(paths) == 0 {
		return report{}, fmt.Errorf("no input files matched")
	}
	if opts.MaxCompare <= 0 {
		opts.MaxCompare = 4000
	}
	if opts.EquitySamples <= 0 {
		opts.EquitySamples = 1000
	}
	if opts.MaxExamples <= 0 {
		opts.MaxExamples = 100
	}
	r := report{
		SchemaVersion: 4, GeneratedAt: time.Now(), RequestFieldCoverage: map[string]fieldStat{}, CallbackFieldCoverage: map[string]fieldStat{},
		Dataset: datasetStat{
			Sources: map[string]*sourceStat{}, APIResponseCodes: map[string]int{}, APIResponseErrors: map[string]int{},
			CallbackResponseCodes: map[string]int{}, CallbackResponseErrors: map[string]int{},
		},
		Styles: map[string]int{}, Streets: map[string]int{}, Positions: map[string]int{}, BenActions: map[string]int{},
		ActionsByStreet: map[string]map[string]int{}, ActionsByPosition: map[string]map[string]int{}, ActionsByStyle: map[string]map[string]int{},
		PreflopActionsByPosition: map[string]map[string]int{}, PreflopMetricsByStyle: map[string]preflopMetric{},
		InputFeatures:          inputFeatureStat{PlayerCounts: map[string]int{}, GameTypes: map[string]int{}, AntiCheatModes: map[string]int{}, UseDiffRaiseLogic: map[string]int{}},
		Probability:            probabilityStat{ActionProbSource: map[string]int{}, ByStreet: map[string]*probabilityGroup{}, ByStyle: map[string]*probabilityGroup{}},
		Sizing:                 sizingStat{RaiseActions: map[string]int{}, RaiseActionsByStreet: map[string]map[string]int{}},
		CallbackPerformance:    newCallbackPerformance(),
		ByDate:                 map[string]*periodStat{},
		AOF:                    newAOFStat(),
		ComparisonSampleStreet: map[string]int{}, Comparisons: map[string]profileComparison{},
		ComparisonAssumptions: []string{
			"Primary agreement compares action type; chip amount does not affect the agreement rate.",
			"Sizing is reported separately only when both engines choose the same aggressive action; BenAPI and AinP amounts are treated as action increments and normalized by the pre-action pot.",
			"Preflop snapshots contain enough state for high-confidence comparison.",
			"Postflop is limited-confidence because action_sequance has no explicit street separators; current-street raises and prior postflop hero calls are conservatively set to zero.",
			"BenAPI current_pot/current_player_tocall/current_player_stack from extra_info are used when present.",
			"AinP opponent hole cards remain unknown, matching normal visible-information rules; BenAPI uuids-based exploitation and AinP's persisted opponent snapshots cannot be reproduced from one decision record.",
			"BenAPI logs do not contain the server legal-action list or last full raise; legal actions are approximated with the configured minimum raise size.",
		},
	}
	perStreetCap := int(math.Ceil(float64(opts.MaxCompare) / 4))
	reservoirs := map[string]*reservoir{}
	for index, street := range []string{"preflop", "flop", "turn", "river"} {
		reservoirs[street] = &reservoir{cap: perStreetCap, rng: rand.New(rand.NewSource(int64(1040 + index)))}
	}
	requestPresence := map[string]int{}
	callbackPresence := map[string]int{}
	preflopHands := map[string]*preflopHand{}
	seenDecisions := map[[32]byte]struct{}{}
	seenCallbacks := map[[32]byte]struct{}{}
	chosenProbSum := 0.0
	var decisionTimes []float64
	for _, path := range paths {
		date := dateOf(path)
		period := r.ByDate[date]
		if period == nil {
			period = newPeriodStat()
			r.ByDate[date] = period
		}
		sourceName := sourceOf(path)
		stat, scanErr := scanFile(path, func(item decisionRecord) {
			r.Dataset.RawDecisionRecords++
			if _, duplicate := seenDecisions[item.Fingerprint]; duplicate {
				r.Dataset.DuplicateDecisionRecords++
				return
			}
			seenDecisions[item.Fingerprint] = struct{}{}
			r.Decisions++
			period.Decisions++
			for field := range item.RequestMap {
				requestPresence[field]++
			}
			increment(r.Styles, fallback(item.Request.StyleName, "raw"))
			increment(r.Streets, item.Street)
			increment(r.Positions, item.Position)
			increment(r.BenActions, item.BenAction)
			increment(period.Streets, item.Street)
			increment(period.Styles, fallback(item.Request.StyleName, "raw"))
			increment(period.Actions, item.BenAction)
			incrementNested(r.ActionsByStreet, item.Street, item.BenAction)
			incrementNested(r.ActionsByPosition, item.Position, item.BenAction)
			incrementNested(r.ActionsByStyle, fallback(item.Request.StyleName, "raw"), item.BenAction)
			increment(r.InputFeatures.PlayerCounts, fmt.Sprint(item.Request.NPlayers))
			increment(r.InputFeatures.GameTypes, fallback(item.Request.GameType, "holdem"))
			increment(r.InputFeatures.AntiCheatModes, fmt.Sprint(item.Request.AntiCheatMode))
			increment(r.InputFeatures.UseDiffRaiseLogic, fmt.Sprint(item.Request.UseDiffRaiseLogic))
			if item.Request.Straddle > 0 {
				r.InputFeatures.StraddleNonZero++
				period.StraddleNonZero++
			}
			if len(item.Request.BeginnerBBPosition) > 0 {
				r.InputFeatures.BeginnerPositionNonEmpty++
			}
			if raw := item.RequestMap["beginner_blind_amount"]; len(raw) > 0 && string(raw) != "0" && string(raw) != "null" {
				r.InputFeatures.BeginnerAmountNonZero++
				period.BeginnerBlindNonZero++
			}
			if item.Request.ButtonPlayerID != nil && *item.Request.ButtonPlayerID != item.Request.NPlayers-1 {
				r.InputFeatures.CustomButtonPosition++
			}
			if item.Request.BBPlayerID != nil && *item.Request.BBPlayerID != 1 {
				r.InputFeatures.CustomBBPosition++
			}
			if len(item.Request.UUIDs) == item.Request.NPlayers {
				r.InputFeatures.CompleteUUIDLists++
			}
			if item.Street == "preflop" {
				incrementNested(r.PreflopActionsByPosition, item.Position, item.BenAction)
				key := item.UID + "|" + item.Request.GameID
				hand := preflopHands[key]
				if hand == nil {
					hand = &preflopHand{style: fallback(item.Request.StyleName, "raw")}
					preflopHands[key] = hand
				}
				for _, prior := range item.Request.ActionSequence {
					if prior.PlayerID == item.Request.CurrentPlayerID {
						markPreflopAction(hand, normalizeAction("", prior.BetType, prior.BetType, 0))
					}
				}
				markPreflopAction(hand, item.BenAction)
				periodHand := period.preflopHands[key]
				if periodHand == nil {
					periodHand = &preflopHand{style: fallback(item.Request.StyleName, "raw")}
					period.preflopHands[key] = periodHand
				}
				for _, prior := range item.Request.ActionSequence {
					if prior.PlayerID == item.Request.CurrentPlayerID {
						markPreflopAction(periodHand, normalizeAction("", prior.BetType, prior.BetType, 0))
					}
				}
				markPreflopAction(periodHand, item.BenAction)
			}
			if len(item.Response.Extra.ActionProbs) > 0 {
				r.Probability.RecordsWithProbabilities++
				positive := 0
				for _, candidate := range item.Response.Extra.ActionProbs {
					if candidate.ActionProb > 0.000001 {
						positive++
					}
				}
				if positive > 1 {
					r.Probability.MixedStrategyRecords++
				}
				if item.BenAction == item.Argmax {
					r.Probability.ChosenIsArgmax++
				} else {
					r.Probability.ChosenIsNotArgmax++
				}
				chosenProbSum += item.ChosenProb
				updateProbabilityGroup(r.Probability.ByStreet, item.Street, item)
				updateProbabilityGroup(r.Probability.ByStyle, fallback(item.Request.StyleName, "raw"), item)
				updateProbabilityValue(&period.Probability, item)
			}
			if item.Response.Extra.DecisionTime >= 0 {
				decisionTimes = append(decisionTimes, item.Response.Extra.DecisionTime)
			}
			updateSizing(&r.Sizing, item)
			reservoirs[item.Street].add(item)
		}, func(item callbackRecord) {
			r.Dataset.RawCallbackRecords++
			if _, duplicate := seenCallbacks[item.Fingerprint]; duplicate {
				r.Dataset.DuplicateCallbackRecords++
				return
			}
			seenCallbacks[item.Fingerprint] = struct{}{}
			r.Callbacks++
			period.Callbacks++
			for field := range item.RequestMap {
				callbackPresence[field]++
			}
			updateCallbackPerformance(&r.CallbackPerformance, item, sourceName)
			updateCallbackPerformance(&period.CallbackPerformance, item, sourceName)
		}, func(response benResponse, useX1 bool) {
			increment(r.Probability.ActionProbSource, fallback(response.Extra.ActionProbsSource, "unspecified"))
			if len(response.Extra.OriginActionProbs) > 0 {
				r.Probability.OriginProbabilityRecords++
				if !sameProbabilities(response.Extra.ActionProbs, response.Extra.OriginActionProbs) {
					r.Probability.AdjustedProbabilityRecords++
				}
			}
			if useX1 {
				r.Probability.UseX1ActionTrue++
			}
		})
		if scanErr != nil {
			return report{}, scanErr
		}
		r.Inputs = append(r.Inputs, stat)
		r.MalformedRecords += stat.Malformed
		r.Dataset.Files++
		period.Files++
		r.Dataset.SizeBytes += stat.SizeBytes
		period.SizeBytes += stat.SizeBytes
		period.MalformedRecords += stat.Malformed
		r.Dataset.APIRequestRecords += stat.APIRequestRecords
		r.Dataset.APIResponseRecords += stat.APIResponseRecords
		r.Dataset.CallbackResponseRecords += stat.CallbackResponses
		mergeCounts(r.Dataset.APIResponseCodes, stat.APIResponseCodes)
		mergeCounts(r.Dataset.APIResponseErrors, stat.APIResponseErrors)
		mergeCounts(r.Dataset.CallbackResponseCodes, stat.CallbackCodes)
		mergeCounts(r.Dataset.CallbackResponseErrors, stat.CallbackErrors)
		mergeCounts(period.APIResponseCodes, stat.APIResponseCodes)
		mergeCounts(period.APIResponseErrors, stat.APIResponseErrors)
		mergeCounts(period.CallbackResponseCodes, stat.CallbackCodes)
		mergeCounts(period.CallbackResponseErrors, stat.CallbackErrors)
		mergeAOF(&r.AOF, stat.AOF)
		mergeAOF(&period.AOF, stat.AOF)
		if stat.Partial {
			r.Dataset.PartialFiles++
			period.PartialFiles++
		}
		source := r.Dataset.Sources[stat.Source]
		if source == nil {
			source = &sourceStat{}
			r.Dataset.Sources[stat.Source] = source
		}
		source.Files++
		source.SizeBytes += stat.SizeBytes
		source.DecisionRecords += stat.DecisionRecords
		source.CallbackRecords += stat.CallbackRecords
		source.Malformed += stat.Malformed
		if stat.Partial {
			source.PartialFiles++
		}
	}
	for _, field := range requestFields {
		r.RequestFieldCoverage[field] = coverage(requestPresence[field], r.Decisions)
	}
	for _, field := range []string{"n_players", "sb", "bb", "ante", "straddle", "stack", "private_hand", "community_cards", "style_name", "current_player_id", "game_id", "uuids", "payoff", "rake", "showdown_cards", "action_sequance", "use_diff_raise_logic"} {
		r.CallbackFieldCoverage[field] = coverage(callbackPresence[field], r.Callbacks)
	}
	if r.Probability.RecordsWithProbabilities > 0 {
		r.Probability.ChosenArgmaxRate = float64(r.Probability.ChosenIsArgmax) / float64(r.Probability.RecordsWithProbabilities)
		r.Probability.MeanChosenProbability = chosenProbSum / float64(r.Probability.RecordsWithProbabilities)
	}
	finalizeProbabilityGroups(r.Probability.ByStreet)
	finalizeProbabilityGroups(r.Probability.ByStyle)
	r.DecisionTime = summarizeDistribution(decisionTimes)
	if r.Sizing.AggressiveDecisions > 0 {
		r.Sizing.AllInRate = float64(r.Sizing.AllInDecisions) / float64(r.Sizing.AggressiveDecisions)
	}
	finalizeCallbackPerformance(&r.CallbackPerformance)
	finalizeAOF(&r.AOF)
	finalizePreflopMetrics(r.PreflopMetricsByStyle, preflopHands)
	for _, period := range r.ByDate {
		if period.Decisions > 0 {
			period.StraddleRate = float64(period.StraddleNonZero) / float64(period.Decisions)
			period.BeginnerBlindRate = float64(period.BeginnerBlindNonZero) / float64(period.Decisions)
		}
		finalizeProbabilityValue(&period.Probability)
		finalizeCallbackPerformance(&period.CallbackPerformance)
		finalizePreflopMetrics(period.PreflopMetricsByStyle, period.preflopHands)
		finalizeAOF(&period.AOF)
	}
	var retained []decisionRecord
	for _, street := range []string{"preflop", "flop", "turn", "river"} {
		rows := reservoirs[street].rows
		retained = append(retained, rows...)
		r.ComparisonSampleStreet[street] = len(rows)
	}
	r.RetainedForComparison = len(retained)
	comparisons, err := compareWithAinP(opts, retained)
	if err != nil {
		return report{}, err
	}
	r.Comparisons = comparisons
	return r, nil
}

func scanFile(path string, decision func(decisionRecord), callback func(callbackRecord), apiResponse func(benResponse, bool)) (fileStat, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileStat{}, err
	}
	stat := fileStat{
		Path: path, Source: sourceOf(path), Partial: strings.HasSuffix(path, ".part"), SizeBytes: info.Size(),
		APIResponseCodes: map[string]int{}, APIResponseErrors: map[string]int{},
		CallbackCodes: map[string]int{}, CallbackErrors: map[string]int{}, AOF: newAOFStat(),
	}
	file, err := os.Open(path)
	if err != nil {
		return stat, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		stat.Lines++
		line := scanner.Text()
		if index := strings.Index(line, nextActionMarker); index >= 0 {
			item, parseErr := parseNextAction(line[index+len(nextActionMarker):], path, stat.Lines)
			if parseErr != nil {
				stat.Malformed++
				appendMalformed(&stat, "decision", parseErr)
				continue
			}
			stat.DecisionRecords++
			decision(item)
			continue
		}
		if index := strings.Index(line, aofCallbackMarker); index >= 0 {
			if err := scanAOFLine(line[index+len(aofCallbackMarker):], true, &stat.AOF); err != nil {
				stat.Malformed++
				appendMalformed(&stat, "aof_callback", err)
			}
			continue
		}
		if index := strings.Index(line, aofMarker); index >= 0 {
			if err := scanAOFLine(line[index+len(aofMarker):], false, &stat.AOF); err != nil {
				stat.Malformed++
				appendMalformed(&stat, "aof", err)
			}
			continue
		}
		if index := strings.Index(line, callbackMarker); index >= 0 {
			var envelope callbackEnvelope
			payload := line[index+len(callbackMarker):]
			if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
				stat.Malformed++
				appendMalformed(&stat, "callback_envelope", err)
				continue
			}
			if envelope.Body == "" {
				if envelope.ResponseBody != "" {
					var response struct {
						Code int    `json:"code"`
						Msg  string `json:"msg"`
					}
					if err := json.Unmarshal([]byte(envelope.ResponseBody), &response); err != nil {
						stat.Malformed++
						appendMalformed(&stat, "callback_response", err)
					} else {
						stat.CallbackResponses++
						increment(stat.CallbackCodes, fmt.Sprint(response.Code))
						if response.Code != 0 {
							increment(stat.CallbackErrors, normalizeAPIError(response.Msg))
						}
					}
				}
				continue
			}
			item, err := parseCallback(envelope, payload)
			if err != nil {
				stat.Malformed++
				appendMalformed(&stat, "callback", err)
				continue
			}
			stat.CallbackRecords++
			callback(item)
			continue
		}
		if index := strings.Index(line, holdemMarker); index >= 0 {
			var envelope callbackEnvelope
			if err := json.Unmarshal([]byte(line[index+len(holdemMarker):]), &envelope); err != nil {
				stat.Malformed++
				appendMalformed(&stat, "api_response_envelope", err)
				continue
			}
			if envelope.Body != "" {
				stat.APIRequestRecords++
				continue
			}
			if envelope.ResponseBody == "" {
				continue
			}
			var raw struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
				Data struct {
					PlayerID    int          `json:"player_id"`
					BetType     string       `json:"bet_type"`
					Amount      float64      `json:"amount"`
					UseX1Action bool         `json:"use_x1_action"`
					Extra       benExtraInfo `json:"extra_info"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(envelope.ResponseBody), &raw); err != nil {
				stat.Malformed++
				appendMalformed(&stat, "api_response", err)
				continue
			}
			stat.APIResponseRecords++
			increment(stat.APIResponseCodes, fmt.Sprint(raw.Code))
			if raw.Code != 0 {
				increment(stat.APIResponseErrors, normalizeAPIError(raw.Msg))
				continue
			}
			apiResponse(benResponse{PlayerID: raw.Data.PlayerID, BetType: raw.Data.BetType, Amount: raw.Data.Amount, Extra: raw.Data.Extra}, raw.Data.UseX1Action)
		}
	}
	return stat, scanner.Err()
}

func parseNextAction(data, path string, line int) (decisionRecord, error) {
	var envelope nextActionEnvelope
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return decisionRecord{}, err
	}
	var req benRequest
	var reqMap map[string]json.RawMessage
	var resp benResponse
	if err := json.Unmarshal(envelope.Request, &req); err != nil {
		return decisionRecord{}, err
	}
	if err := json.Unmarshal(envelope.Request, &reqMap); err != nil {
		return decisionRecord{}, err
	}
	if err := json.Unmarshal(envelope.Response, &resp); err != nil {
		return decisionRecord{}, err
	}
	chosen := normalizeAction(envelope.Action, envelope.BetType, resp.BetType, resp.Extra.CurrentToCall)
	argmax, chosenProb := probabilitySummary(resp.Extra.ActionProbs, chosen, resp.Amount, resp.Extra.CurrentToCall)
	return decisionRecord{
		File: filepath.Base(path), Line: line, UID: envelope.UID, RoomID: rawString(envelope.RoomID), HandNum: rawString(envelope.HandNum),
		Request: req, RequestMap: reqMap, Response: resp, BenAction: chosen, Street: streetOf(req.CommunityCards),
		Position: positionOf(req), ChosenProb: chosenProb, Argmax: argmax, Fingerprint: sha256.Sum256([]byte(data)),
	}, nil
}

func parseCallback(envelope callbackEnvelope, payload string) (callbackRecord, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(envelope.Body), &fields); err != nil {
		return callbackRecord{}, fmt.Errorf("invalid callback body JSON: %w", err)
	}
	var request benRequest
	if err := json.Unmarshal([]byte(envelope.Body), &request); err != nil {
		return callbackRecord{}, err
	}
	var result struct {
		Payoff   []float64  `json:"payoff"`
		Rake     []float64  `json:"rake"`
		Showdown [][]string `json:"showdown_cards"`
	}
	if err := json.Unmarshal([]byte(envelope.Body), &result); err != nil {
		return callbackRecord{}, err
	}
	return callbackRecord{
		UID: envelope.UID, RoomID: rawString(envelope.RoomID), Request: request, RequestMap: fields,
		Payoff: result.Payoff, Rake: result.Rake, Showdown: result.Showdown, Fingerprint: sha256.Sum256([]byte(payload)),
	}, nil
}

func scanAOFLine(payload string, callback bool, stat *aofStat) error {
	var envelope callbackEnvelope
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return err
	}
	if envelope.Body != "" {
		if callback {
			stat.CallbackRequestRecords++
		} else {
			stat.RequestRecords++
		}
		return nil
	}
	if envelope.ResponseBody == "" {
		return nil
	}
	var response struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Action    *string  `json:"action"`
			AIVersion string   `json:"ai_version"`
			PushEV    *float64 `json:"push_ev"`
			FoldEV    *float64 `json:"fold_ev"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(envelope.ResponseBody), &response); err != nil {
		return err
	}
	if callback {
		stat.CallbackResponseRecords++
		increment(stat.CallbackResponseCodes, fmt.Sprint(response.Code))
		if response.Code != 0 {
			increment(stat.CallbackResponseErrors, normalizeAPIError(response.Msg))
		}
		return nil
	}
	stat.ResponseRecords++
	increment(stat.ResponseCodes, fmt.Sprint(response.Code))
	if response.Code != 0 {
		increment(stat.ResponseErrors, normalizeAPIError(response.Msg))
		return nil
	}
	if response.Data.Action != nil {
		increment(stat.Actions, *response.Data.Action)
	}
	if response.Data.AIVersion != "" {
		increment(stat.AIVersions, response.Data.AIVersion)
	}
	if response.Data.PushEV != nil {
		stat.pushEVValues = append(stat.pushEVValues, *response.Data.PushEV)
	}
	if response.Data.FoldEV != nil {
		stat.foldEVValues = append(stat.foldEVValues, *response.Data.FoldEV)
	}
	return nil
}

func normalizeAPIError(message string) string {
	message = strings.TrimSpace(message)
	switch {
	case message == "'0'":
		return "missing_player_zero"
	case strings.HasPrefix(message, "Raise size of player id"):
		return "raise_size_exceeds_max"
	case strings.HasPrefix(message, "valid action must be"):
		return "invalid_legal_action_state"
	case strings.HasPrefix(message, "hero_index must be"):
		return "hero_index_mismatch"
	case message == "":
		return "unspecified"
	default:
		return message
	}
}

func probabilitySummary(candidates []benAction, chosen string, amount, toCall float64) (string, float64) {
	bestAction, bestProbability, chosenProbability := "", -1.0, 0.0
	for _, item := range candidates {
		action := normalizeAction("", item.BetType, item.BetType, toCall)
		if item.ActionProb > bestProbability {
			bestAction, bestProbability = action, item.ActionProb
		}
		if action == chosen && (chosen != "raise" && chosen != "bet" || math.Abs(item.Amount-amount) < 1e-6) {
			chosenProbability = item.ActionProb
		}
	}
	return bestAction, chosenProbability
}

func streetOf(board []string) string {
	switch len(board) {
	case 0:
		return "preflop"
	case 3:
		return "flop"
	case 4:
		return "turn"
	case 5:
		return "river"
	default:
		return "invalid"
	}
}

func positionOf(req benRequest) string {
	button := req.NPlayers - 1
	if req.ButtonPlayerID != nil {
		button = *req.ButtonPlayerID
	}
	bb := 1
	if req.BBPlayerID != nil {
		bb = *req.BBPlayerID
	}
	player := req.CurrentPlayerID
	sb := (bb - 1 + req.NPlayers) % req.NPlayers
	switch player {
	case sb:
		return "SB"
	case bb:
		return "BB"
	case button:
		return "BTN"
	}
	straddle := -1
	if req.Straddle > 0 {
		straddle = (bb + 1) % req.NPlayers
		if player == straddle {
			return "STR"
		}
	}
	var normals []int
	for index := 0; index < req.NPlayers; index++ {
		if index != 0 && index != bb && index != button && index != straddle {
			normals = append(normals, index)
		}
	}
	for index, value := range normals {
		if value != player {
			continue
		}
		if index == 0 {
			return "UTG"
		}
		if index == len(normals)-1 {
			return "CO"
		}
		return "MP"
	}
	return "MP"
}

func updateProbabilityGroup(groups map[string]*probabilityGroup, key string, item decisionRecord) {
	group := groups[key]
	if group == nil {
		group = &probabilityGroup{}
		groups[key] = group
	}
	updateProbabilityValue(group, item)
}

func updateProbabilityValue(group *probabilityGroup, item decisionRecord) {
	group.Records++
	positive := 0
	for _, candidate := range item.Response.Extra.ActionProbs {
		if candidate.ActionProb > 0.000001 {
			positive++
		}
	}
	if positive > 1 {
		group.MixedStrategyRecords++
	}
	if item.BenAction == item.Argmax {
		group.ChosenIsArgmax++
	}
	group.chosenProbabilitySum += item.ChosenProb
}

func finalizeProbabilityGroups(groups map[string]*probabilityGroup) {
	for _, group := range groups {
		finalizeProbabilityValue(group)
	}
}

func finalizeProbabilityValue(group *probabilityGroup) {
	if group.Records == 0 {
		return
	}
	group.ChosenArgmaxRate = float64(group.ChosenIsArgmax) / float64(group.Records)
	group.MeanChosenProbability = group.chosenProbabilitySum / float64(group.Records)
}

func updateSizing(stat *sizingStat, item decisionRecord) {
	if item.BenAction != "bet" && item.BenAction != "raise" {
		return
	}
	stat.AggressiveDecisions++
	if item.Response.Extra.CurrentStack > 0 && item.Response.Amount >= item.Response.Extra.CurrentStack-1e-6 {
		stat.AllInDecisions++
	}
	label := "unspecified"
	for _, candidate := range item.Response.Extra.ActionProbs {
		if normalizeAction("", candidate.BetType, candidate.BetType, item.Response.Extra.CurrentToCall) == item.BenAction &&
			math.Abs(candidate.Amount-item.Response.Amount) < 1e-6 {
			label = fallback(candidate.RaiseAction, "unspecified")
			break
		}
	}
	increment(stat.RaiseActions, label)
	incrementNested(stat.RaiseActionsByStreet, item.Street, label)
}

func updateCallbackPerformance(stat *callbackPerformance, item callbackRecord, source string) {
	index := item.Request.CurrentPlayerID
	if index < 0 || index >= len(item.Payoff) {
		stat.SkippedRecords++
		return
	}
	bb := item.Request.BB
	if bb <= 0 {
		stat.SkippedRecords++
		return
	}
	payoff := item.Payoff[index] / bb
	style := fallback(item.Request.StyleName, "raw")
	byStyle := stat.ByStyle[style]
	if byStyle == nil {
		byStyle = &stylePerformance{}
		stat.ByStyle[style] = byStyle
	}
	bySource := stat.BySource[source]
	if bySource == nil {
		bySource = &stylePerformance{}
		stat.BySource[source] = bySource
	}
	stat.Hands++
	stat.PayoffBB += payoff
	stat.payoffSquareSum += payoff * payoff
	byStyle.Hands++
	byStyle.PayoffBB += payoff
	byStyle.payoffSquareSum += payoff * payoff
	bySource.Hands++
	bySource.PayoffBB += payoff
	bySource.payoffSquareSum += payoff * payoff
	switch {
	case payoff > 1e-9:
		stat.Wins++
		byStyle.Wins++
		bySource.Wins++
	case payoff < -1e-9:
		stat.Losses++
		byStyle.Losses++
		bySource.Losses++
	default:
		stat.Ties++
		byStyle.Ties++
		bySource.Ties++
	}
	if index < len(item.Rake) && item.Rake[index] > 0 {
		stat.NonZeroRakeHands++
		stat.RakeBB += item.Rake[index] / bb
	}
	if index < len(item.Showdown) && len(item.Showdown[index]) > 0 {
		stat.Showdowns++
	}
}

func newCallbackPerformance() callbackPerformance {
	return callbackPerformance{ByStyle: map[string]*stylePerformance{}, BySource: map[string]*stylePerformance{}}
}

func finalizeCallbackPerformance(stat *callbackPerformance) {
	if stat.Hands > 0 {
		stat.MeanPayoffBB = stat.PayoffBB / float64(stat.Hands)
		stat.StdDevPayoffBB, stat.StandardErrorBB, stat.CI95LowBB, stat.CI95HighBB = payoffUncertainty(stat.Hands, stat.PayoffBB, stat.payoffSquareSum)
	}
	for _, groups := range []map[string]*stylePerformance{stat.ByStyle, stat.BySource} {
		for _, item := range groups {
			if item.Hands > 0 {
				item.MeanPayoffBB = item.PayoffBB / float64(item.Hands)
				item.StdDevPayoffBB, item.StandardErrorBB, item.CI95LowBB, item.CI95HighBB = payoffUncertainty(item.Hands, item.PayoffBB, item.payoffSquareSum)
			}
		}
	}
}

func payoffUncertainty(count int, sum, squareSum float64) (stddev, standardError, low, high float64) {
	if count < 2 {
		mean := sum / math.Max(1, float64(count))
		return 0, 0, mean, mean
	}
	mean := sum / float64(count)
	variance := (squareSum - float64(count)*mean*mean) / float64(count-1)
	stddev = math.Sqrt(math.Max(0, variance))
	standardError = stddev / math.Sqrt(float64(count))
	return stddev, standardError, mean - 1.96*standardError, mean + 1.96*standardError
}

func finalizePreflopMetrics(metrics map[string]preflopMetric, hands map[string]*preflopHand) {
	for _, hand := range hands {
		metric := metrics[hand.style]
		metric.Hands++
		if hand.vpip {
			metric.VPIPHands++
		}
		if hand.pfr {
			metric.PFRHands++
		}
		metrics[hand.style] = metric
	}
	for style, metric := range metrics {
		if metric.Hands > 0 {
			metric.VPIPRate = float64(metric.VPIPHands) / float64(metric.Hands)
			metric.PFRRate = float64(metric.PFRHands) / float64(metric.Hands)
			metrics[style] = metric
		}
	}
}

func summarizeDistribution(values []float64) distributionStat {
	result := distributionStat{Count: len(values)}
	if len(values) == 0 {
		return result
	}
	sort.Float64s(values)
	for _, value := range values {
		result.Mean += value
	}
	result.Mean /= float64(len(values))
	result.P50 = percentile(values, .50)
	result.P90 = percentile(values, .90)
	result.P95 = percentile(values, .95)
	result.P99 = percentile(values, .99)
	result.Max = values[len(values)-1]
	return result
}

func percentile(values []float64, fraction float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(fraction*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func normalizeAction(action, betType, responseBetType string, toCall float64) string {
	value := strings.ToLower(strings.TrimSpace(action))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(responseBetType))
	}
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(betType))
	}
	value = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(value)
	switch strings.TrimSuffix(value, "s") {
	case "fold":
		return "fold"
	case "check":
		return "check"
	case "call":
		return "call"
	case "bet":
		return "bet"
	case "raise", "allin":
		if value == "allin" && toCall <= 0 {
			return "bet"
		}
		return "raise"
	default:
		return value
	}
}

func appendMalformed(stat *fileStat, kind string, err error) {
	if len(stat.MalformedExamples) >= 20 {
		return
	}
	stat.MalformedExamples = append(stat.MalformedExamples, malformedRecord{Line: stat.Lines, Kind: kind, Reason: err.Error()})
}

func markPreflopAction(hand *preflopHand, action string) {
	if action == "call" || action == "raise" || action == "bet" {
		hand.vpip = true
	}
	if action == "raise" || action == "bet" {
		hand.pfr = true
	}
}

func sameProbabilities(left, right []benAction) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if normalizeAction("", left[index].BetType, left[index].BetType, 0) != normalizeAction("", right[index].BetType, right[index].BetType, 0) ||
			math.Abs(left[index].Amount-right[index].Amount) > 1e-6 || math.Abs(left[index].ActionProb-right[index].ActionProb) > 1e-9 {
			return false
		}
	}
	return true
}

func expandInputs(patterns []string) ([]string, error) {
	set := map[string]bool{}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			matches = []string{pattern}
		}
		for _, match := range matches {
			info, statErr := os.Stat(match)
			if statErr != nil {
				continue
			}
			if !info.IsDir() {
				set[match] = true
				continue
			}
			if err := filepath.WalkDir(match, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.Type().IsRegular() {
					set[path] = true
				}
				return nil
			}); err != nil {
				return nil, err
			}
		}
	}
	result := make([]string, 0, len(set))
	for path := range set {
		result = append(result, path)
	}
	sort.Strings(result)
	return result, nil
}

func sourceOf(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	marker := "/benapi/"
	index := strings.Index(clean, marker)
	if index < 0 {
		return "unknown"
	}
	relative := strings.TrimPrefix(clean[index+len(marker):], "/")
	parts := strings.Split(relative, "/")
	if len(parts) >= 2 && strings.HasPrefix(parts[1], "pokerbot") {
		return parts[0] + "/" + parts[1]
	}
	return "legacy-root"
}

func dateOf(path string) string {
	match := logDatePattern.FindStringSubmatch(filepath.Base(path))
	if len(match) == 2 {
		return match[1]
	}
	return "unknown"
}

func newPeriodStat() *periodStat {
	return &periodStat{
		Streets: map[string]int{}, Styles: map[string]int{}, Actions: map[string]int{},
		PreflopMetricsByStyle: map[string]preflopMetric{}, CallbackPerformance: newCallbackPerformance(),
		APIResponseCodes: map[string]int{}, APIResponseErrors: map[string]int{},
		CallbackResponseCodes: map[string]int{}, CallbackResponseErrors: map[string]int{},
		AOF: newAOFStat(), preflopHands: map[string]*preflopHand{},
	}
}

func writeReport(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(data, '\n'), 0o640); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func increment(values map[string]int, key string) { values[key]++ }

func incrementNested(values map[string]map[string]int, outer, inner string) {
	if values[outer] == nil {
		values[outer] = map[string]int{}
	}
	values[outer][inner]++
}

func mergeCounts(target, source map[string]int) {
	for key, value := range source {
		target[key] += value
	}
}

func newAOFStat() aofStat {
	return aofStat{
		ResponseCodes: map[string]int{}, ResponseErrors: map[string]int{}, Actions: map[string]int{}, AIVersions: map[string]int{},
		CallbackResponseCodes: map[string]int{}, CallbackResponseErrors: map[string]int{},
	}
}

func mergeAOF(target *aofStat, source aofStat) {
	target.RequestRecords += source.RequestRecords
	target.ResponseRecords += source.ResponseRecords
	target.CallbackRequestRecords += source.CallbackRequestRecords
	target.CallbackResponseRecords += source.CallbackResponseRecords
	mergeCounts(target.ResponseCodes, source.ResponseCodes)
	mergeCounts(target.ResponseErrors, source.ResponseErrors)
	mergeCounts(target.Actions, source.Actions)
	mergeCounts(target.AIVersions, source.AIVersions)
	mergeCounts(target.CallbackResponseCodes, source.CallbackResponseCodes)
	mergeCounts(target.CallbackResponseErrors, source.CallbackResponseErrors)
	target.pushEVValues = append(target.pushEVValues, source.pushEVValues...)
	target.foldEVValues = append(target.foldEVValues, source.foldEVValues...)
}

func finalizeAOF(stat *aofStat) {
	stat.PushEV = summarizeDistribution(stat.pushEVValues)
	stat.FoldEV = summarizeDistribution(stat.foldEVValues)
}

func coverage(present, total int) fieldStat {
	result := fieldStat{Present: present}
	if total > 0 {
		result.Rate = float64(present) / float64(total)
	}
	return result
}

func rawString(value json.RawMessage) string {
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	return strings.Trim(string(value), `"`)
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}
