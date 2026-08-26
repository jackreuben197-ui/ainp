package audit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeDetectsIntentConflict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"http_access","status":200,"latency_us":100}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"d1","street":"preflop","action":"fold","rule_id":"PREFLOP_CALL","equity":0.7,"latency_us":20}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	expected := Expectations{MinimumStrategyDecisions: 1, MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1, HighEquityThreshold: .65, MaxStrategyP95US: 100, MaxHTTPP95US: 1000}
	report, err := Analyze(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.IntentConflicts != 1 || report.HighEquityPreflopFolds != 1 {
		t.Fatalf("report=%+v", report)
	}
}

func TestAnalyzeCountsNearAllInCollapses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := `{"msg":"near_allin_collapsed","original_action":"call"}` + "\n" +
		`{"msg":"near_allin_collapsed","original_action":"raise"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{})
	if err != nil {
		t.Fatal(err)
	}
	if report.NearAllInCollapses != 2 || report.NearAllInCallCollapses != 1 || report.NearAllInRaiseCollapses != 1 {
		t.Fatalf("report=%+v", report)
	}
}

func TestAnalyzeRejectsStateErrorsAndActionDeviation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"decision_event","decision_id":"logic-1","event_fingerprint":"event-1","table_id":"t","hand_id":"h","seq_num":2,"cmd":"action","error_code":"Game logic error","error_message":"bad amount","outcome":"rejected"}` + "\n" +
		`{"msg":"decision_event","decision_id":"logic-2","event_fingerprint":"event-1","table_id":"t","hand_id":"h","seq_num":2,"cmd":"action","error_code":"Game logic error","error_message":"bad amount","outcome":"rejected"}` + "\n" +
		`{"msg":"decision_event","error_code":"Wrong seq_num","outcome":"rejected"}` + "\n" +
		`{"time":"2026-08-04T00:00:00Z","msg":"decision_event","decision_id":"advice","table_id":"t","hand_id":"deviation","player_id":"bot","cmd":"deal_cards","outcome":"applied","advise":{"type":"raise"}}` + "\n" +
		`{"time":"2026-08-04T00:00:01Z","msg":"decision_event","decision_id":"actual","table_id":"t","hand_id":"deviation","player_id":"bot","cmd":"action","outcome":"applied","deviation":{"by_type":{"actual":"fold","expected":"raise"},"by_value":{"actual":0,"expected":100}}}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"d1","hand_id":"h1","player_id":"p1","ai_profile":"TAG_L2","profile_source":"profiles","personality_id":"tag","strategy_level":2,"target_vpip":0.3,"target_pfr":0.15,"street":"preflop","raises_faced":1,"action":"raise","latency_us":10}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"d2","hand_id":"h2","player_id":"p1","ai_profile":"TAG_L2","profile_source":"profiles","personality_id":"tag","strategy_level":2,"target_vpip":0.3,"target_pfr":0.15,"street":"preflop","action":"fold","latency_us":10}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	expected := Expectations{MinimumStrategyDecisions: 1, MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1, MaxStrategyP95US: 100, MaxHTTPP95US: 100}
	report, err := Analyze(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.GameLogicErrors != 2 || report.UniqueGameLogicErrors != 1 || len(report.GameLogicErrorExamples) != 1 || report.GameLogicErrorExamples[0].Occurrences != 2 || report.WrongSequenceErrors != 1 || report.ActionTypeDeviations != 1 || report.ActionValueDeviations != 1 || len(report.ActionDeviationExamples) != 1 {
		t.Fatalf("report=%+v", report)
	}
	if example := report.ActionDeviationExamples[0]; example.AdviceDecisionID != "advice" || example.AdviceCommand != "deal_cards" || example.AdviceAgeMS != 1000 {
		t.Fatalf("deviation example=%+v", example)
	}
	metric := report.ProfileMetrics["TAG_L2|tag|L2"]
	if metric == nil || metric.StrategyLevel != 2 || metric.TargetVPIP != .3 || metric.TargetPFR != .15 || metric.PreflopHands != 2 || metric.VPIPRate != .5 || metric.PFRRate != .5 || metric.PreflopAggressionRate != .5 || metric.FacingPreflopRaise != 1 || metric.PreflopReraiseRate != 1 {
		t.Fatalf("metric=%+v", metric)
	}
}

func TestAnalyzeKeepsSameNumberedHandsOnDifferentTablesSeparate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"strategy_decision","decision_id":"d1","table_id":"table-a","hand_id":"1","player_id":"bot","ai_profile":"TAG_L2","profile_source":"profiles","personality_id":"tag","strategy_level":2,"target_vpip":0.3,"target_pfr":0.15,"street":"preflop","action":"raise"}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"d2","table_id":"table-b","hand_id":"1","player_id":"bot","ai_profile":"TAG_L2","profile_source":"profiles","personality_id":"tag","strategy_level":2,"target_vpip":0.3,"target_pfr":0.15,"street":"preflop","action":"fold"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	expected := Expectations{MinimumStrategyDecisions: 1, MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1}
	report, err := Analyze(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	metric := report.ProfileMetrics["TAG_L2|tag|L2"]
	if metric == nil || metric.PreflopHands != 2 || metric.VPIPRate != .5 || metric.PFRRate != .5 {
		t.Fatalf("same-numbered hands from different tables were merged: %+v", metric)
	}
}

func TestAnalyzeAcceptsLegacyStrategyLogWithoutTableID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := `{"msg":"strategy_decision","decision_id":"legacy","hand_id":"1","player_id":"bot","ai_profile":"TAG_L2","profile_source":"profiles","personality_id":"tag","strategy_level":2,"street":"preflop","action":"call"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{MinimumStrategyDecisions: 1, MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	metric := report.ProfileMetrics["TAG_L2|tag|L2"]
	if metric == nil || metric.PreflopHands != 1 || metric.VPIPRate != 1 {
		t.Fatalf("legacy metric=%+v", metric)
	}
}

func TestAnalyzeDetectsDelayedDealAndQuestionableAirCallDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"time":"2026-08-04T00:00:00Z","msg":"decision_event","outcome":"applied","cmd":"start_hand_extended","seq_num":1,"player_id":"bot","table_id":"t1","hand_id":"h1"}` + "\n" +
		`{"time":"2026-08-04T00:00:02Z","msg":"decision_event","outcome":"applied","cmd":"deal_cards","seq_num":3,"player_id":"bot","table_id":"t1","hand_id":"h1","advise":{"type":"check"},"latency_us":50}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"f","table_id":"t1","hand_id":"h1","player_id":"bot","ai_profile":"FPCH_default","personality_id":"balanced","strategy_level":5,"street":"flop","action":"call","hero_hand_class":"AKo","hand_class":"air","equity":0.40,"pot_odds":0.30,"call_ev":1}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"t","table_id":"t1","hand_id":"h1","player_id":"bot","ai_profile":"FPCH_default","personality_id":"balanced","strategy_level":5,"street":"turn","action":"call","hero_hand_class":"AKo","hand_class":"air","equity":0.35,"pot_odds":0.30,"call_ev":0.2}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"r","table_id":"t1","hand_id":"h1","player_id":"bot","ai_profile":"FPCH_default","personality_id":"balanced","strategy_level":5,"street":"river","action":"call","hero_hand_class":"AKo","hand_class":"air","equity":0.31,"pot_odds":0.30,"call_ev":-0.1}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	zero, maxLatency := 0, int64(100)
	report, err := Analyze(path, Expectations{
		MinimumStrategyDecisions: 3, MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1,
		DelayedDealThresholdMS: 1000, MinRiverAirCallEquityEdge: .10,
		MaxDelayedDealP95US: &maxLatency, MaxRejectedDealCards: &zero, MaxDelayedDealsNoAdvice: &zero,
		MaxNegativeEVCalls: &zero, MaxQuestionableAirCalls: &zero, MaxAirCallDownHands: &zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.DelayedDealCards != 1 || report.DelayedDealsWithAdvice != 1 || report.DelayedDealP95US != 50 || report.PostflopAirCalls != 3 || report.HighCardCalls != 3 || report.HighCardMultiStreetHands != 1 || report.HighCardThreeStreetHands != 1 || report.NegativeEVCalls != 1 || report.QuestionableAirCalls != 1 || report.AirCallDownHands != 1 || report.AKHighCardCalls != 3 || report.AKHighCardCallDownHands != 1 || len(report.QuestionableCallExamples) == 0 || len(report.AirCallDownExamples) != 1 || len(report.AirCallDownExamples[0].Calls) != 3 {
		t.Fatalf("report=%+v", report)
	}
}

func TestAnalyzeDetectsGenericStrategyAnomalies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"strategy_decision","decision_id":"free-fold","table_id":"t","hand_id":"h1","player_id":"bot","street":"flop","action":"fold","to_call":0,"equity":0.4}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"low-allin","table_id":"t","hand_id":"h2","player_id":"bot","street":"turn","action":"allin","to_call":10,"equity":0.2}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"allin-call","table_id":"t","hand_id":"h2b","player_id":"bot","street":"turn","action":"allin","to_call":10,"equity":0.2,"rule_id":"POSTFLOP_CALL"}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"humanized-allin-call","table_id":"t","hand_id":"h2c","player_id":"bot","street":"turn","action":"allin","to_call":10,"equity":0.2,"rule_id":"POSTFLOP_FOLD","tags":["bounded_loose_call","legal_guard_passed"]}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"missed-value","table_id":"t","hand_id":"h3","player_id":"bot","street":"river","action":"check","to_call":0,"equity":0.9}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"guarded-value","table_id":"t","hand_id":"h3b","player_id":"bot","street":"river","action":"check","to_call":0,"equity":0.9,"rule_id":"POSTFLOP_VALUE_BET"}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"q-flop","table_id":"t","hand_id":"h4","player_id":"bot","street":"flop","action":"call","hero_hand_class":"QJo","hand_class":"air","equity":0.4,"pot_odds":0.3,"call_ev":1}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"q-turn","table_id":"t","hand_id":"h4","player_id":"bot","street":"turn","action":"call","hero_hand_class":"QJo","hand_class":"air","equity":0.4,"pot_odds":0.3,"call_ev":1}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	zero := 0
	report, err := Analyze(path, Expectations{MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1, MaxFreeOptionFolds: &zero})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.FreeOptionFolds != 1 || len(report.FreeOptionFoldExamples) != 1 || report.FreeOptionFoldExamples[0].DecisionID != "free-fold" || report.LowEquityAllIns != 1 || report.RiverHighEquityChecks != 1 || report.HighCardCalls != 2 || report.HighCardMultiStreetHands != 1 {
		t.Fatalf("report=%+v", report)
	}
	foundGate := false
	for _, issue := range report.Issues {
		foundGate = foundGate || issue.Check == "free_option_folds"
	}
	if !foundGate {
		t.Fatalf("missing free_option_folds gate: %+v", report.Issues)
	}
}

func TestAnalyzeDetectsRiverWeakBoardPairAfterMissedFlushDraw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"strategy_decision","decision_id":"exact-weak","table_id":"t","hand_id":"h1","player_id":"bot","street":"river","action":"call","hand_category":2,"hand_class":"made","equity":0.27,"pot_odds":0.25,"call_ev":1,"river_card_features_available":true,"pair_from_board_only":true,"missed_flush_draw":true}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"legacy-weak","table_id":"t","hand_id":"h2","player_id":"bot","street":"river","action":"call","hand_category":2,"hand_class":"made","equity":0.29,"pot_odds":0.25,"call_ev":1}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"exact-priced","table_id":"t","hand_id":"h3","player_id":"bot","street":"river","action":"call","hand_category":2,"hand_class":"made","equity":0.45,"pot_odds":0.25,"call_ev":2,"river_card_features_available":true,"pair_from_board_only":true,"missed_flush_draw":true}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"not-target","table_id":"t","hand_id":"h4","player_id":"bot","street":"river","action":"fold","hand_category":2,"hand_class":"made","equity":0.20,"pot_odds":0.25,"river_card_features_available":true,"pair_from_board_only":true,"missed_flush_draw":true}` + "\n"
	content += `{"msg":"strategy_decision","decision_id":"missed-straight","table_id":"t","hand_id":"h5","player_id":"bot","street":"river","action":"call","hand_category":2,"hand_class":"made","equity":0.449,"pot_odds":0.172,"call_ev":261,"hero_postflop_calls":1,"river_card_features_available":true,"pair_from_board_only":true,"missed_flush_draw":false,"missed_straight_draw":true}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{
		MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1,
		MinRiverPairCallEquityEdge: .10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.RiverCardFeatureDecisions != 4 || report.RiverOnePairCalls != 4 || report.RiverWeakOnePairCalls != 2 ||
		report.RiverMissedFlushBoardCalls != 2 || report.RiverWeakMissedFlushCalls != 1 ||
		report.RiverBoardPairCalls != 3 || report.RiverMissedStraightBoardCalls != 1 || report.RiverMissedDrawBoardCalls != 3 || report.RiverRepeatedMissedDrawCalls != 1 ||
		len(report.RiverWeakPairExamples) != 2 || len(report.RiverMissedFlushExamples) != 2 || len(report.RiverWeakMissedFlushExamples) != 1 || len(report.RiverMissedDrawExamples) != 3 || len(report.RiverRepeatedMissedDrawExamples) != 1 {
		t.Fatalf("report=%+v", report)
	}
	if report.RiverOnePairEdgeBuckets["0_to_3pct"] != 1 || report.RiverOnePairEdgeBuckets["3_to_5pct"] != 1 || report.RiverOnePairEdgeBuckets["at_least_10pct"] != 2 {
		t.Fatalf("edge buckets=%+v", report.RiverOnePairEdgeBuckets)
	}
	if !report.RiverMissedFlushExamples[0].FeatureAvailable || !report.RiverMissedFlushExamples[0].PairFromBoardOnly || !report.RiverMissedFlushExamples[0].MissedFlushDraw {
		t.Fatalf("exact example=%+v", report.RiverMissedFlushExamples[0])
	}
	if !report.RiverRepeatedMissedDrawExamples[0].MissedStraightDraw || report.RiverRepeatedMissedDrawExamples[0].HeroPostflopCalls != 1 {
		t.Fatalf("missed straight example=%+v", report.RiverRepeatedMissedDrawExamples[0])
	}
}

func TestAnalyzeTracksUnderpairCallsGuardsAndLegacyCandidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"strategy_decision","decision_id":"exact-call","table_id":"t","hand_id":"h1","player_id":"bot","ai_profile":"FPCH_default","street":"turn","action":"call","hero_hand_class":"44","hand_class":"made","pocket_pair_under_board":true,"equity":0.53,"pot_odds":0.30,"to_call":140}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"guard-fold","table_id":"t","hand_id":"h2","player_id":"bot","ai_profile":"FPCH_default","street":"river","action":"fold","rule_id":"POSTFLOP_UNDERPAIR_FOLD","hero_hand_class":"55","hand_class":"made","pocket_pair_under_board":true,"equity":0.40,"pot_odds":0.30,"to_call":200}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"legacy","table_id":"t","hand_id":"h3","player_id":"bot","ai_profile":"FPCH_default","street":"river","action":"call","hero_hand_class":"66","hand_class":"made","equity":0.45,"pot_odds":0.30,"to_call":300}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"legacy-not-pair","table_id":"t","hand_id":"h4","player_id":"bot","street":"river","action":"call","hero_hand_class":"A6s","hand_class":"made"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.UnderpairFeatureDecisions != 2 || report.UnderpairCalls != 1 || report.UnderpairCallHands != 1 ||
		report.UnderpairGuardFolds != 1 || report.UnderpairGuardFoldHands != 1 || report.LegacyUnderpairCallCandidates != 1 || report.LegacyUnderpairCandidateHands != 1 ||
		len(report.UnderpairCallExamples) != 1 || len(report.UnderpairGuardFoldExamples) != 1 || len(report.LegacyUnderpairExamples) != 1 {
		t.Fatalf("report=%+v", report)
	}
	if !report.UnderpairCallExamples[0].UnderpairFeatureAvailable || !report.UnderpairCallExamples[0].PocketPairUnderBoard {
		t.Fatalf("exact example=%+v", report.UnderpairCallExamples[0])
	}
}

func TestAnalyzeTracksPreflopLargeCallGuardAndLegacyCandidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"strategy_decision","decision_id":"miss","table_id":"t","hand_id":"h1","player_id":"bot","street":"preflop","action":"call","hero_hand_class":"72s","raises_faced":2,"preflop_large_call_outside_range":true,"to_call":1000}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"guard","table_id":"t","hand_id":"h2","player_id":"bot","street":"preflop","action":"fold","rule_id":"PREFLOP_PROFILE_LARGE_CALL_FOLD","hero_hand_class":"Q6s","raises_faced":2,"preflop_large_call_outside_range":true,"to_call":200}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"legacy","table_id":"t","hand_id":"h3","player_id":"bot","street":"preflop","action":"allin","rule_id":"PREFLOP_CALL","hero_hand_class":"33","raises_faced":3,"to_call":860}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"legacy-open","table_id":"t","hand_id":"h4","player_id":"bot","street":"preflop","action":"call","hero_hand_class":"A6s","raises_faced":0}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.PreflopLargeCallFeatureDecisions != 2 || report.PreflopLargeCallMisses != 1 || report.PreflopLargeCallMissHands != 1 ||
		report.PreflopLargeCallGuardFolds != 1 || report.PreflopLargeCallGuardFoldHands != 1 || report.LegacyReraisedPreflopCalls != 1 || report.LegacyReraisedPreflopCallHands != 1 ||
		len(report.PreflopLargeCallMissExamples) != 1 || len(report.PreflopLargeCallGuardExamples) != 1 || len(report.LegacyReraisedPreflopExamples) != 1 {
		t.Fatalf("report=%+v", report)
	}
	if !report.PreflopLargeCallMissExamples[0].PreflopLargeCallFeatureAvailable || !report.PreflopLargeCallMissExamples[0].PreflopLargeCallOutsideRange || report.PreflopLargeCallMissExamples[0].RaisesFaced != 2 {
		t.Fatalf("exact example=%+v", report.PreflopLargeCallMissExamples[0])
	}
}

func TestObservedProfileRateGateIsOptionalForRankBiasedDeals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := `{"msg":"strategy_decision","decision_id":"d","table_id":"t","hand_id":"h","player_id":"bot","ai_profile":"FPCH_default","personality_id":"balanced","strategy_level":5,"profile_source":"profiles","target_vpip":0.32,"target_pfr":0.16,"street":"preflop","action":"call","to_call":1,"pot":3,"equity":0.5,"call_ev":1}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	base := Expectations{MinimumProfileHands: 1, MaxVPIPDeviation: .05, MaxPFRDeviation: .05, MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1}
	report, err := Analyze(path, base)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.ProfileRateViolations != 0 {
		t.Fatalf("disabled profile gate report=%+v", report)
	}
	base.EnforceProfileRates = true
	report, err = Analyze(path, base)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.ProfileRateViolations != 2 {
		t.Fatalf("enabled profile gate report=%+v", report)
	}
}

func TestRejectedDelayedDealIsNotAlsoReportedAsMissingAdvice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := `{"time":"2026-08-04T00:00:02Z","msg":"decision_event","outcome":"rejected","cmd":"deal_cards","seq_num":3,"player_id":"bot","table_id":"t1","hand_id":"h1"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	if report.RejectedDealCards != 1 || report.DelayedDealCards != 1 || report.DelayedDealsNoAdvice != 0 {
		t.Fatalf("report=%+v", report)
	}
}

func TestSpecialProfileIsExcludedFromAnomaliesAndReportedSeparately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"strategy_decision","decision_id":"s-pre","table_id":"t","hand_id":"h","player_id":"bot","ai_profile":"FPCH_100_50","audit_exempt":true,"street":"preflop","action":"raise","amount":20,"rule_id":"SPECIAL_RAISE","hero_hand_class":"AKo","equity":0.1,"pot":3}` + "\n" +
		`{"msg":"decision_event","decision_id":"s-dev","table_id":"t","hand_id":"h","player_id":"bot","cmd":"action","outcome":"applied","deviation":{"by_type":{"actual":"fold","expected":"raise"}}}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"s-flop","table_id":"t","hand_id":"h","player_id":"bot","ai_profile":"FPCH_100_50","audit_exempt":true,"street":"flop","action":"call","amount":10,"hand_class":"air","equity":0.01,"pot":30,"to_call":10,"pot_odds":0.5,"call_ev":-10}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"s-turn","table_id":"t","hand_id":"h","player_id":"bot","ai_profile":"FPCH_100_50","audit_exempt":true,"street":"turn","action":"allin","equity":0.01}` + "\n" +
		`{"msg":"hand_result","table_id":"t","hand_id":"h","player_id":"bot","ai_profile":"FPCH_100_50","reached_street":"turn","profit":-25}` + "\n" +
		`{"msg":"strategy_decision","decision_id":"s2-pre","table_id":"t","hand_id":"h2","player_id":"bot","ai_profile":"FPCH_100_50","audit_exempt":true,"street":"preflop","action":"call"}` + "\n" +
		`{"msg":"hand_result","table_id":"t","hand_id":"h2","player_id":"bot","ai_profile":"FPCH_100_50","reached_street":"river","profit":40}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	metric := report.SpecialProfileMetrics["FPCH_100_50"]
	if !report.Passed || report.StrategyDecisions != 0 || report.ActionTypeDeviations != 0 || report.NegativeEVCalls != 0 || report.LowEquityAllIns != 0 || metric == nil || metric.Hands != 2 || metric.Decisions != 4 || metric.Wins != 1 || metric.Losses != 1 || metric.NetProfit != 15 || metric.StreetReached["flop"] != 2 || metric.StreetReached["turn"] != 2 || metric.StreetReached["river"] != 1 || metric.StreetReachRates["river"] != .5 {
		t.Fatalf("report=%+v special=%+v", report, metric)
	}
	if len(metric.LossDetails) != 1 || metric.LossDetails[0].TableID != "t" || metric.LossDetails[0].HandID != "h" || metric.LossDetails[0].PlayerID != "bot" || metric.LossDetails[0].ReachedStreet != "turn" || metric.LossDetails[0].Profit != -25 {
		t.Fatalf("loss details=%+v", metric.LossDetails)
	}
	decisions := metric.LossDetails[0].Decisions
	if len(decisions) != 3 || decisions[0].Street != "preflop" || decisions[0].Action != "raise" || decisions[0].Amount != 20 || decisions[0].RuleID != "SPECIAL_RAISE" || decisions[1].Street != "flop" || decisions[1].ToCall != 10 {
		t.Fatalf("loss decisions=%+v", decisions)
	}
}

func TestMalformedLineIsSkippedWithoutLosingSpecialProfileMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"strategy_decision","table_id":"t","hand_id":"h","player_id":"bot","ai_profile":"FPCH_100_50","audit_exempt":true,"street":"preflop","action":"raise"}` + "\n" +
		`{"msg":"decision_event","error_code"":"broken"}` + "\n" +
		`{"msg":"hand_result","table_id":"t","hand_id":"h","player_id":"bot","ai_profile":"FPCH_100_50","reached_street":"river","profit":10}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	metric := report.SpecialProfileMetrics["FPCH_100_50"]
	if report.Lines != 3 || report.MalformedLines != 1 || len(report.MalformedLineExamples) != 1 || report.MalformedLineExamples[0] != 2 || metric == nil || metric.Hands != 1 || metric.Wins != 1 {
		t.Fatalf("report=%+v special=%+v", report, metric)
	}
}

func TestNormalHandResultDoesNotPolluteSpecialProfileMetrics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"msg":"strategy_decision","table_id":"t","hand_id":"normal","player_id":"bot","ai_profile":"FPCH_default","street":"preflop","action":"fold"}` + "\n" +
		`{"msg":"hand_result","table_id":"t","hand_id":"normal","player_id":"bot","ai_profile":"FPCH_default","reached_street":"preflop","profit":-1}` + "\n" +
		`{"msg":"strategy_decision","table_id":"t","hand_id":"special","player_id":"bot","ai_profile":"FPCH_100_50","audit_exempt":true,"street":"preflop","action":"raise"}` + "\n" +
		`{"msg":"hand_result","table_id":"t","hand_id":"special","player_id":"bot","ai_profile":"FPCH_100_50","reached_street":"preflop","profit":1}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := report.SpecialProfileMetrics["FPCH_default"]; exists {
		t.Fatalf("normal profile polluted special metrics: %+v", report.SpecialProfileMetrics)
	}
	metric := report.SpecialProfileMetrics["FPCH_100_50"]
	if metric == nil || metric.Hands != 1 || metric.Wins != 1 {
		t.Fatalf("special=%+v", metric)
	}
}

func TestStartupBoundaryDealIsReportedButDoesNotFailRuntimeDealGate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"time":"2026-08-04T00:00:00Z","msg":"ainp started"}` + "\n" +
		`{"time":"2026-08-04T00:00:05Z","msg":"decision_event","outcome":"rejected","error_code":"Broken hand","error_message":"start_hand_extended must be the first event for a hand","cmd":"deal_cards","seq_num":1,"player_id":"bot","table_id":"t1","hand_id":"h1"}` + "\n" +
		`{"time":"2026-08-04T00:02:00Z","msg":"decision_event","outcome":"rejected","error_code":"Broken hand","error_message":"start_hand_extended must be the first event for a hand","cmd":"deal_cards","seq_num":1,"player_id":"bot","table_id":"t2","hand_id":"h2"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	zero := 0
	report, err := Analyze(path, Expectations{
		MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1,
		StartupBoundaryGraceMS: 60_000, MaxRejectedDealCards: &zero,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || report.StartupBoundaryErrors != 1 || report.StartupRejectedDealCards != 1 || report.RejectedDealCards != 1 || len(report.StartupBoundaryExamples) != 1 {
		t.Fatalf("report=%+v", report)
	}
}

func TestLatePlayerStreamDeviationIsSeparatedFromLiveDeviation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"time":"2026-08-04T00:00:00Z","msg":"decision_event","decision_id":"a-start","outcome":"applied","cmd":"start_hand_extended","player_id":"a","table_id":"table","hand_id":"hand"}` + "\n" +
		`{"time":"2026-08-04T00:00:10Z","msg":"decision_event","decision_id":"a-end","outcome":"applied","cmd":"end_hand","player_id":"a","table_id":"table","hand_id":"hand"}` + "\n" +
		`{"time":"2026-08-04T00:00:15Z","msg":"decision_event","decision_id":"b-start","outcome":"applied","cmd":"start_hand_extended","player_id":"b","table_id":"table","hand_id":"hand"}` + "\n" +
		`{"time":"2026-08-04T00:00:15.100Z","msg":"decision_event","decision_id":"b-advice","outcome":"applied","cmd":"deal_cards","player_id":"b","table_id":"table","hand_id":"hand","advise":{"type":"call","value":0.2}}` + "\n" +
		`{"time":"2026-08-04T00:00:16Z","msg":"decision_event","decision_id":"b-fold","outcome":"applied","cmd":"action","player_id":"b","table_id":"table","hand_id":"hand","deviation":{"by_type":{"actual":"fold","expected":"call"},"by_value":{"actual":0,"expected":0.2}}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{
		MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1,
		LateStreamAfterEndMS: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.ActionTypeDeviations != 0 || report.ActionValueDeviations != 0 ||
		report.LatePlayerStreams != 1 || report.LateStreamsAfterEnd != 1 || report.LateStreamsAfterTableProgress != 0 || report.LateStreamTypeDeviations != 1 || report.LateStreamValueDeviations != 1 ||
		len(report.ActionDeviationExamples) != 0 || len(report.LateStreamExamples) != 1 || len(report.LateStreamDeviationExamples) != 1 ||
		report.LateStreamDeviationExamples[0].StartAfterEndMS != 5000 {
		t.Fatalf("report=%+v", report)
	}
}

func TestPlayerStreamStartingAfterTableProgressIsSeparatedFromLiveDeviation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ainp.jsonl")
	content := "" +
		`{"time":"2026-08-04T00:00:00Z","msg":"decision_event","decision_id":"a-start","outcome":"applied","cmd":"start_hand_extended","seq_num":1,"player_id":"a","table_id":"table","hand_id":"hand"}` + "\n" +
		`{"time":"2026-08-04T00:00:00.100Z","msg":"decision_event","decision_id":"a-deal","outcome":"applied","cmd":"deal_cards","seq_num":2,"player_id":"a","table_id":"table","hand_id":"hand"}` + "\n" +
		`{"time":"2026-08-04T00:00:04Z","msg":"decision_event","decision_id":"a-action","outcome":"applied","cmd":"action","seq_num":6,"player_id":"a","table_id":"table","hand_id":"hand"}` + "\n" +
		`{"time":"2026-08-04T00:00:05Z","msg":"decision_event","decision_id":"b-start","outcome":"applied","cmd":"start_hand_extended","seq_num":1,"player_id":"b","table_id":"table","hand_id":"hand"}` + "\n" +
		`{"time":"2026-08-04T00:00:05.100Z","msg":"decision_event","decision_id":"b-advice","outcome":"applied","cmd":"deal_cards","seq_num":2,"player_id":"b","table_id":"table","hand_id":"hand","advise":{"type":"call","value":0.2}}` + "\n" +
		`{"time":"2026-08-04T00:00:05.200Z","msg":"decision_event","decision_id":"b-fold","outcome":"applied","cmd":"action","seq_num":3,"player_id":"b","table_id":"table","hand_id":"hand","deviation":{"by_type":{"actual":"fold","expected":"call"},"by_value":{"actual":0,"expected":0.2}}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(path, Expectations{
		MaxPreflopFoldRate: 1, MaxPreflopAggressionRate: 1, MaxHTTPErrorRate: 1,
		LateStreamAfterEndMS: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.ActionTypeDeviations != 0 || report.ActionValueDeviations != 0 ||
		report.LatePlayerStreams != 1 || report.LateStreamsAfterEnd != 0 || report.LateStreamsAfterTableProgress != 1 || report.LateStreamTypeDeviations != 1 || report.LateStreamValueDeviations != 1 ||
		len(report.LateStreamExamples) != 1 || len(report.LateStreamDeviationExamples) != 1 {
		t.Fatalf("report=%+v", report)
	}
	stream := report.LateStreamExamples[0]
	if stream.Reason != "after_table_progress" || stream.StartDelayMS != 5000 || stream.StartAfterEndMS != 0 || stream.TableSeqAtStart != 6 {
		t.Fatalf("late stream=%+v", stream)
	}
	deviation := report.LateStreamDeviationExamples[0]
	if deviation.LateStreamReason != "after_table_progress" || deviation.StartDelayMS != 5000 || deviation.TableSeqAtStart != 6 {
		t.Fatalf("late deviation=%+v", deviation)
	}
}
