package main

import (
	"testing"

	"gitlab.com/ubenbill/ainp/internal/config"
)

func TestProfitSimulationIsDeterministic(t *testing.T) {
	profile := config.BotProfileConfig{Personality: "calling_station", Level: 1, TargetVPIP: .90, TargetPFR: .05, PostflopCallMargin: .10, LargePotThreshold: 12, LargePotMinEquity: .68}
	first, err := simulate("test", profile, 200, 4, .05)
	if err != nil {
		t.Fatal(err)
	}
	second, err := simulate("test", profile, 200, 4, .05)
	if err != nil {
		t.Fatal(err)
	}
	if first.NetProfitBB != second.NetProfitBB || first.WonHands != second.WonHands || first.EnteredHands == 0 || first.LargePotHands == 0 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestDealHasNineUniqueCards(t *testing.T) {
	cards := deal(42)
	seen := map[byte]bool{}
	for _, card := range cards {
		if seen[byte(card)] {
			t.Fatalf("duplicate card %s", card)
		}
		seen[byte(card)] = true
	}
}
