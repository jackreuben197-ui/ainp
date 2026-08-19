package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/smoothsics/ainp/internal/audit"
	"gopkg.in/yaml.v3"
)

func main() {
	input := flag.String("input", "build/nohup.out", "ainp JSON log")
	expectPath := flag.String("expect", "conf/audit.yaml", "expectation YAML")
	output := flag.String("output", "", "JSON report path")
	flag.Parse()
	data, err := os.ReadFile(*expectPath)
	if err != nil {
		fatal(err)
	}
	var expected audit.Expectations
	if err := yaml.Unmarshal(data, &expected); err != nil {
		fatal(err)
	}
	report, err := audit.Analyze(*input, expected)
	if err != nil {
		fatal(err)
	}
	if *output == "" {
		*output = filepath.Join("reports", "audit-"+time.Now().Format("20060102T150405")+".json")
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o750); err != nil {
		fatal(err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, append(encoded, '\n'), 0o640); err != nil {
		fatal(err)
	}
	fmt.Printf("audit passed=%t decisions=%d preflop_fold=%.2f%% preflop_aggression=%.2f%% http_errors=%.2f%% game_logic=%d(unique=%d) action_deviations=%d late_streams=%d(late_deviations=%d) rejected_deals=%d startup_boundary=%d(startup_deals=%d) delayed_deals=%d delayed_no_advice=%d negative_ev_calls=%d questionable_high_card_calls=%d free_option_folds=%d high_card_three_street_hands=%d profile_rate_violations=%d report=%s\n", report.Passed, report.StrategyDecisions, report.PreflopFoldRate*100, report.PreflopAggressionRate*100, report.HTTPErrorRate*100, report.GameLogicErrors, report.UniqueGameLogicErrors, report.ActionTypeDeviations, report.LatePlayerStreams, report.LateStreamTypeDeviations, report.RejectedDealCards, report.StartupBoundaryErrors, report.StartupRejectedDealCards, report.DelayedDealCards, report.DelayedDealsNoAdvice, report.NegativeEVCalls, report.QuestionableAirCalls, report.FreeOptionFolds, report.HighCardThreeStreetHands, report.ProfileRateViolations, *output)
	if !report.Passed {
		for _, issue := range report.Issues {
			fmt.Printf("FAIL %s expected=%s actual=%s\n", issue.Check, issue.Expected, issue.Actual)
		}
		os.Exit(2)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
