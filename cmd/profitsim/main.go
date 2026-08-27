package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/smoothsics/ainp/internal/config"
	"gitlab.com/smoothsics/ainp/internal/equity"
	"gitlab.com/smoothsics/ainp/internal/poker"
	"gitlab.com/smoothsics/ainp/internal/strategy"
)

type report struct {
	GeneratedAt      time.Time `json:"generated_at"`
	Profile          string    `json:"profile"`
	Opponent         string    `json:"opponent"`
	Hands            int       `json:"hands"`
	StartingStackBB  float64   `json:"starting_stack_bb"`
	RakePercent      float64   `json:"rake_percent"`
	EnteredHands     int       `json:"entered_hands"`
	EnteredRate      float64   `json:"entered_rate"`
	WonHands         int       `json:"won_hands"`
	WinRate          float64   `json:"win_rate"`
	Showdowns        int       `json:"showdowns"`
	ShowdownWins     int       `json:"showdown_wins"`
	ShowdownWinRate  float64   `json:"showdown_win_rate"`
	NetProfitBB      float64   `json:"net_profit_bb"`
	BBPer100         float64   `json:"bb_per_100"`
	StdErrorBBPer100 float64   `json:"std_error_bb_per_100"`
	CI95LowBBPer100  float64   `json:"ci95_low_bb_per_100"`
	CI95HighBBPer100 float64   `json:"ci95_high_bb_per_100"`
	LargePotHands    int       `json:"large_pot_hands"`
	LargePotWins     int       `json:"large_pot_wins"`
	LargePotWinRate  float64   `json:"large_pot_win_rate"`
	LargePotProfitBB float64   `json:"large_pot_profit_bb"`
	SmallPotHands    int       `json:"small_pot_hands"`
	SmallPotProfitBB float64   `json:"small_pot_profit_bb"`
	SmallPotBBHand   float64   `json:"small_pot_bb_per_hand"`
	Passed           bool      `json:"passed"`
	DurationMS       int64     `json:"duration_ms"`
}

type handResult struct {
	profit, finalPot float64
	entered          bool
	won, showdown    bool
}

func main() {
	configPath := flag.String("config", "conf/config.yaml", "configuration file")
	profileName := flag.String("profile", "FPCH_90_5", "AiProfile to simulate")
	hands := flag.Int("hands", 1_000_000, "number of complete hands")
	samples := flag.Int("equity-samples", 16, "Monte Carlo samples per postflop decision")
	rake := flag.Float64("rake", 0, "rake as a fraction of won pots")
	disableRisk := flag.Bool("disable-risk-control", false, "clear profile postflop risk controls for A/B comparison")
	callMargin := flag.Float64("postflop-call-margin", math.NaN(), "override profile postflop call margin")
	largeThreshold := flag.Float64("large-pot-threshold-bb", math.NaN(), "override profile large-pot threshold")
	largeMinEquity := flag.Float64("large-pot-min-equity", math.NaN(), "override profile large-pot minimum equity")
	output := flag.String("output", "", "JSON report path")
	flag.Parse()
	if *hands < 1 || *samples < 1 || *rake < 0 || *rake >= 1 {
		fatal(fmt.Errorf("invalid hands, samples or rake"))
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal(err)
	}
	profile, ok := cfg.Engine.Personality.Profiles[*profileName]
	if !ok {
		fatal(fmt.Errorf("unknown profile %q", *profileName))
	}
	if *disableRisk {
		profile.PostflopCallMargin, profile.LargePotThreshold, profile.LargePotMinEquity = 0, 0, 0
	}
	if !math.IsNaN(*callMargin) {
		profile.PostflopCallMargin = *callMargin
	}
	if !math.IsNaN(*largeThreshold) {
		profile.LargePotThreshold = *largeThreshold
	}
	if !math.IsNaN(*largeMinEquity) {
		profile.LargePotMinEquity = *largeMinEquity
	}
	if *output == "" {
		*output = filepath.Join("reports", "profit-sim-"+*profileName+"-"+time.Now().Format("20060102T150405")+".json")
	}
	result, err := simulate(*profileName, profile, *hands, *samples, *rake)
	if err != nil {
		fatal(err)
	}
	if err := writeReport(*output, result); err != nil {
		fatal(err)
	}
	fmt.Printf("profile=%s hands=%d entered=%.2f%% win=%.2f%% showdown_win=%.2f%% profit=%.2fBB BB/100=%.3f CI95=[%.3f,%.3f] large_win=%.2f%% large_profit=%.2fBB small_profit=%.2fBB duration=%dms report=%s\n", result.Profile, result.Hands, result.EnteredRate*100, result.WinRate*100, result.ShowdownWinRate*100, result.NetProfitBB, result.BBPer100, result.CI95LowBBPer100, result.CI95HighBBPer100, result.LargePotWinRate*100, result.LargePotProfitBB, result.SmallPotProfitBB, result.DurationMS, *output)
	if !result.Passed {
		os.Exit(2)
	}
}

func simulate(name string, profile config.BotProfileConfig, hands, samples int, rake float64) (report, error) {
	started := time.Now()
	result := report{GeneratedAt: started, Profile: name, Opponent: "heads-up loose-passive benchmark v1", Hands: hands, StartingStackBB: 100, RakePercent: rake}
	engine := strategy.NewEngine(equity.NewCalculator())
	sumSquares := 0.0
	for hand := 0; hand < hands; hand++ {
		cards := deal(uint64(hand + 1))
		outcome, err := playHand(engine, profile, cards, int64(hand+1), samples, rake)
		if err != nil {
			return result, fmt.Errorf("hand %d: %w", hand, err)
		}
		result.NetProfitBB += outcome.profit
		sumSquares += outcome.profit * outcome.profit
		if outcome.entered {
			result.EnteredHands++
		}
		if outcome.won {
			result.WonHands++
		}
		if outcome.showdown {
			result.Showdowns++
			if outcome.won {
				result.ShowdownWins++
			}
		}
		if outcome.finalPot >= 12 {
			result.LargePotHands++
			result.LargePotProfitBB += outcome.profit
			if outcome.won {
				result.LargePotWins++
			}
		} else {
			result.SmallPotHands++
			result.SmallPotProfitBB += outcome.profit
		}
	}
	result.WinRate = ratio(result.WonHands, hands)
	result.EnteredRate = ratio(result.EnteredHands, hands)
	result.ShowdownWinRate = ratio(result.ShowdownWins, result.Showdowns)
	result.LargePotWinRate = ratio(result.LargePotWins, result.LargePotHands)
	if result.SmallPotHands > 0 {
		result.SmallPotBBHand = result.SmallPotProfitBB / float64(result.SmallPotHands)
	}
	result.BBPer100 = result.NetProfitBB / float64(hands) * 100
	mean := result.NetProfitBB / float64(hands)
	variance := max(0, sumSquares/float64(hands)-mean*mean)
	result.StdErrorBBPer100 = 100 * math.Sqrt(variance/float64(hands))
	result.CI95LowBBPer100 = result.BBPer100 - 1.96*result.StdErrorBBPer100
	result.CI95HighBBPer100 = result.BBPer100 + 1.96*result.StdErrorBBPer100
	result.Passed = result.CI95LowBBPer100 > 0 && result.LargePotProfitBB > 0 && result.SmallPotBBHand >= -.5
	result.DurationMS = time.Since(started).Milliseconds()
	return result, nil
}

func playHand(engine *strategy.Engine, profile config.BotProfileConfig, dealt [9]poker.Card, seed int64, samples int, rake float64) (handResult, error) {
	hero, villain, board := dealt[:2], dealt[2:4], dealt[4:]
	heroInvested, villainInvested, pot := .5, 1.0, 1.5
	heroStack, villainStack := 99.5, 99.0
	preflop, err := engine.Decide(context.Background(), request(profile, strategy.Preflop, hero, nil, pot, .5, heroStack, seed, samples))
	if err != nil {
		return handResult{}, err
	}
	if preflop.Action == strategy.Fold {
		return handResult{profit: -.5, finalPot: pot}, nil
	}
	pay := preflop.Amount
	if preflop.Action == strategy.Call {
		pay = .5
	}
	pay = min(pay, heroStack)
	heroInvested, heroStack, pot = heroInvested+pay, heroStack-pay, pot+pay
	if preflop.Action == strategy.Raise || preflop.Action == strategy.AllIn {
		percentile, _ := equity.StartingHandPercentile(villain)
		if percentile > .65 {
			outcome := settleFold(heroInvested, pot, rake, true)
			outcome.entered = true
			return outcome, nil
		}
		call := min(heroInvested-villainInvested, villainStack)
		villainInvested, villainStack, pot = villainInvested+call, villainStack-call, pot+call
	}

	for index, street := range []strategy.Street{strategy.Flop, strategy.Turn, strategy.River} {
		visible := board[:index+3]
		villainBet := opponentBet(villain, visible, pot, villainStack, seed+int64(index)*17)
		villainInvested, villainStack, pot = villainInvested+villainBet, villainStack-villainBet, pot+villainBet
		decision, decideErr := engine.Decide(context.Background(), request(profile, street, hero, visible, pot-villainBet, villainBet, heroStack, seed+int64(index+1)*101, samples))
		if decideErr != nil {
			return handResult{}, decideErr
		}
		if decision.Action == strategy.Fold {
			return handResult{profit: -heroInvested, finalPot: pot, entered: true}, nil
		}
		heroPay := min(decision.Amount, heroStack)
		if decision.Action == strategy.Check {
			heroPay = 0
		} else if decision.Action == strategy.Call {
			heroPay = min(villainBet, heroStack)
		}
		heroInvested, heroStack, pot = heroInvested+heroPay, heroStack-heroPay, pot+heroPay
		if decision.Action == strategy.Bet || decision.Action == strategy.Raise || decision.Action == strategy.AllIn {
			toCall := max(0, heroPay-villainBet)
			if !opponentCalls(villain, visible, toCall, pot, seed+int64(index)*31) {
				outcome := settleFold(heroInvested, pot, rake, true)
				outcome.entered = true
				return outcome, nil
			}
			call := min(toCall, villainStack)
			villainInvested, villainStack, pot = villainInvested+call, villainStack-call, pot+call
		}
		if heroStack <= 0 || villainStack <= 0 {
			break
		}
	}
	comparison, err := poker.Compare(append(append([]poker.Card{}, hero...), board...), append(append([]poker.Card{}, villain...), board...))
	if err != nil {
		return handResult{}, err
	}
	payout, won := 0.0, comparison > 0
	if comparison > 0 {
		payout = pot * (1 - rake)
	} else if comparison == 0 {
		payout = pot * (1 - rake) / 2
	}
	return handResult{profit: payout - heroInvested, finalPot: pot, entered: true, won: won, showdown: true}, nil
}

func request(profile config.BotProfileConfig, street strategy.Street, hero, board []poker.Card, pot, toCall, stack float64, seed int64, samples int) strategy.Request {
	actions := []strategy.LegalAction{{Action: strategy.Fold}, {Action: strategy.Call, Min: toCall, Max: toCall}, {Action: strategy.Raise, Min: min(stack, toCall+max(2, toCall)), Max: stack}, {Action: strategy.AllIn, Min: stack, Max: stack}}
	if toCall == 0 {
		actions = []strategy.LegalAction{{Action: strategy.Check}, {Action: strategy.Bet, Min: min(1, stack), Max: stack}, {Action: strategy.AllIn, Min: stack, Max: stack}}
	}
	return strategy.Request{Game: equity.GameNLH, Street: street, Position: strategy.BTN, Hero: hero, Board: board, Opponents: [][]poker.Card{{}}, Pot: pot, ToCall: min(toCall, stack), Stack: stack, EffectiveStack: stack, BigBlind: 1, Level: profile.Level, TargetVPIP: profile.TargetVPIP, TargetPFR: profile.TargetPFR, PersonalityID: profile.Personality, PostflopCallMargin: profile.PostflopCallMargin, LargePotThresholdBB: profile.LargePotThreshold, LargePotMinEquity: profile.LargePotMinEquity, DisableOpponentModel: true, DisableThinkTime: true, Seed: seed, EquitySamples: samples, MaxExactOutcomes: 1, LegalActions: actions}
}

func opponentBet(hole, board []poker.Card, pot, stack float64, seed int64) float64 {
	rank, _ := poker.Evaluate(append(append([]poker.Card{}, hole...), board...))
	fraction := 0.0
	if rank.Category >= poker.TwoPair {
		fraction = .75
	} else if rank.Category == poker.OnePair {
		fraction = .5
	} else if uint64(seed*6364136223846793005+1)%20 == 0 {
		fraction = .34
	}
	return min(stack, fraction*pot)
}

func opponentCalls(hole, board []poker.Card, toCall, pot float64, seed int64) bool {
	if toCall <= 0 {
		return true
	}
	rank, _ := poker.Evaluate(append(append([]poker.Card{}, hole...), board...))
	if rank.Category >= poker.TwoPair || rank.Category == poker.OnePair && toCall <= .75*pot {
		return true
	}
	return uint64(seed*2862933555777941757+3037000493)%100 < 15
}

func settleFold(heroInvested, pot, rake float64, heroWon bool) handResult {
	if heroWon {
		return handResult{profit: pot*(1-rake) - heroInvested, finalPot: pot, won: true}
	}
	return handResult{profit: -heroInvested, finalPot: pot}
}

func deal(seed uint64) [9]poker.Card {
	deck := poker.FullDeck()
	for index := 0; index < 9; index++ {
		seed += 0x9e3779b97f4a7c15
		value := seed
		value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
		value = (value ^ (value >> 27)) * 0x94d049bb133111eb
		value ^= value >> 31
		target := index + int(value%uint64(52-index))
		deck[index], deck[target] = deck[target], deck[index]
	}
	var result [9]poker.Card
	copy(result[:], deck[:9])
	return result
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
func writeReport(path string, value report) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o640)
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
