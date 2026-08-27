package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gitlab.com/smoothsics/ainp/internal/config"
	"gitlab.com/smoothsics/ainp/internal/equity"
	"gitlab.com/smoothsics/ainp/internal/poker"
	"gitlab.com/smoothsics/ainp/internal/strategy"
)

type profileResult struct {
	AIProfile   string  `json:"ai_profile"`
	Personality string  `json:"personality"`
	Level       int     `json:"level"`
	Hands       int     `json:"hands"`
	TargetVPIP  float64 `json:"target_vpip"`
	ActualVPIP  float64 `json:"actual_vpip"`
	VPIPError   float64 `json:"vpip_error"`
	TargetPFR   float64 `json:"target_pfr"`
	ActualPFR   float64 `json:"actual_pfr"`
	PFRError    float64 `json:"pfr_error"`
	Passed      bool    `json:"passed"`
}

type simulationReport struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Config      string          `json:"config"`
	HandsEach   int             `json:"hands_each"`
	TotalHands  int             `json:"total_hands"`
	Tolerance   float64         `json:"tolerance"`
	DurationMS  int64           `json:"duration_ms"`
	Passed      bool            `json:"passed"`
	Profiles    []profileResult `json:"profiles"`
}

func main() {
	configPath := flag.String("config", "conf/config.yaml", "configuration file")
	hands := flag.Int("hands", 1_000_000, "number of simulated hands per configured profile")
	tolerance := flag.Float64("tolerance", .002, "maximum absolute VPIP/PFR error")
	output := flag.String("output", "", "JSON report path")
	flag.Parse()
	if *hands < 1 || *tolerance < 0 {
		fatal(fmt.Errorf("hands must be positive and tolerance non-negative"))
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal(err)
	}
	if *output == "" {
		*output = filepath.Join("reports", "profile-sim-"+time.Now().Format("20060102T150405")+".json")
	}
	report, err := simulate(*configPath, cfg, *hands, *tolerance)
	if err != nil {
		fatal(err)
	}
	if err := writeReport(*output, report); err != nil {
		fatal(err)
	}
	for _, item := range report.Profiles {
		fmt.Printf("%s hands=%d VPIP %.2f%%/%.2f%% PFR %.2f%%/%.2f%% passed=%t\n", item.AIProfile, item.Hands, item.ActualVPIP*100, item.TargetVPIP*100, item.ActualPFR*100, item.TargetPFR*100, item.Passed)
	}
	fmt.Printf("profiles=%d total_hands=%d passed=%t duration=%dms report=%s\n", len(report.Profiles), report.TotalHands, report.Passed, report.DurationMS, *output)
	if !report.Passed {
		os.Exit(2)
	}
}

func simulate(configPath string, cfg config.Config, hands int, tolerance float64) (simulationReport, error) {
	started := time.Now()
	report := simulationReport{GeneratedAt: started, Config: configPath, HandsEach: hands, Tolerance: tolerance, Passed: true}
	names := make([]string, 0, len(cfg.Engine.Personality.Profiles))
	for name, profile := range cfg.Engine.Personality.Profiles {
		if profile.TargetVPIP > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	combos := startingHandCombos()
	engine := strategy.NewEngine(equity.NewCalculator())
	for profileIndex, name := range names {
		profile := cfg.Engine.Personality.Profiles[name]
		voluntary, raises := 0, 0
		for hand := 0; hand < hands; hand++ {
			cards := combos[hand%len(combos)]
			decision, err := engine.Decide(context.Background(), strategy.Request{
				PlayerID: "profile-sim-bot", TableID: name, HandID: fmt.Sprint(hand), AIProfile: name,
				Game: equity.GameNLH, Street: strategy.Preflop, Position: strategy.MP,
				Hero: cards[:], Opponents: [][]poker.Card{{}}, Pot: 3, ToCall: 1,
				Stack: 100, EffectiveStack: 100, BigBlind: 2, Level: profile.Level,
				TargetVPIP: profile.TargetVPIP, TargetPFR: profile.TargetPFR, PersonalityID: profile.Personality,
				BehaviorMode: profile.BehaviorMode, PreflopRaiseProbability: profile.PreflopRaiseProbability,
				PostflopAggressionChance: profile.PostflopAggressionChance, NeverFold: profile.NeverFold, AuditExempt: profile.AuditExempt,
				DisableOpponentModel: true, DisableThinkTime: true, Seed: int64(profileIndex*hands + hand + 1),
				LegalActions: []strategy.LegalAction{{Action: strategy.Fold}, {Action: strategy.Call, Min: 1, Max: 1}, {Action: strategy.Raise, Min: 5, Max: 100}, {Action: strategy.AllIn, Min: 100, Max: 100}},
			})
			if err != nil {
				return report, fmt.Errorf("profile %s hand %d: %w", name, hand, err)
			}
			switch decision.Action {
			case strategy.Call:
				voluntary++
			case strategy.Raise, strategy.AllIn:
				voluntary++
				raises++
			}
		}
		actualVPIP, actualPFR := float64(voluntary)/float64(hands), float64(raises)/float64(hands)
		item := profileResult{
			AIProfile: name, Personality: profile.Personality, Level: profile.Level, Hands: hands,
			TargetVPIP: profile.TargetVPIP, ActualVPIP: actualVPIP, VPIPError: actualVPIP - profile.TargetVPIP,
			TargetPFR: profile.TargetPFR, ActualPFR: actualPFR, PFRError: actualPFR - profile.TargetPFR,
		}
		item.Passed = math.Abs(item.VPIPError) <= tolerance && math.Abs(item.PFRError) <= tolerance
		report.Passed = report.Passed && item.Passed
		report.Profiles = append(report.Profiles, item)
		report.TotalHands += hands
	}
	report.DurationMS = time.Since(started).Milliseconds()
	return report, nil
}

func startingHandCombos() [][2]poker.Card {
	deck := poker.FullDeck()
	combos := make([][2]poker.Card, 0, 1326)
	for left := 0; left < len(deck); left++ {
		for right := left + 1; right < len(deck); right++ {
			combos = append(combos, [2]poker.Card{deck[left], deck[right]})
		}
	}
	return combos
}

func writeReport(path string, value simulationReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o640)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
