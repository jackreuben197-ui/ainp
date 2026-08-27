package main

import (
	"testing"

	"gitlab.com/ubenbill/ainp/internal/config"
)

func TestSimulateUsesRealStrategyAndMeetsTargets(t *testing.T) {
	cfg := config.Default()
	cfg.Engine.Personality.Profiles["TEST_30_15"] = config.BotProfileConfig{Personality: "tag", Level: 4, TargetVPIP: .30, TargetPFR: .15}
	report, err := simulate("test", cfg, 1326, 1.0/1326)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.TotalHands != 1326 || len(report.Profiles) != 1 {
		t.Fatalf("report=%+v", report)
	}
}
