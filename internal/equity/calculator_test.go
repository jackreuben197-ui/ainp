package equity

import (
	"context"
	"errors"
	"math"
	"testing"

	"gitlab.com/smoothsics/ainp/internal/poker"
)

func TestExactKnownHandsWinAndTie(t *testing.T) {
	calculator := NewCalculator()
	win, err := calculator.Calculate(context.Background(), Request{
		Hero: poker.MustParseCards("AsAh"), Board: poker.MustParseCards("AcAd2s3h4c"),
		Opponents: [][]poker.Card{poker.MustParseCards("KsKh")}, Algorithm: AlgorithmExact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if win.Trials != 1 || win.Equity != 1 || win.Wins != 1 || !win.Completed {
		t.Fatalf("win result = %+v", win)
	}

	tie, err := calculator.Calculate(context.Background(), Request{
		Hero: poker.MustParseCards("2c3c"), Board: poker.MustParseCards("AsKsQsJsTs"),
		Opponents: [][]poker.Card{poker.MustParseCards("4d5d"), poker.MustParseCards("6h7h")}, Algorithm: AlgorithmExact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tie.Ties != 1 || math.Abs(tie.Equity-1.0/3.0) > 1e-12 {
		t.Fatalf("three-way tie = %+v", tie)
	}
}

func TestExactRiverAgainstRandomOpponent(t *testing.T) {
	calculator := NewCalculator()
	request := Request{
		Hero: poker.MustParseCards("AsAh"), Board: poker.MustParseCards("2c3d7h9sJc"),
		Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmExact,
	}
	result, err := calculator.Calculate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Trials != 990 || result.Method != AlgorithmExact {
		t.Fatalf("result = %+v", result)
	}
	if math.Abs(result.WinProbability+result.TieProbability+result.LossProbability-1) > 1e-12 {
		t.Fatalf("probabilities do not sum to one: %+v", result)
	}
	cached, err := calculator.Calculate(context.Background(), request)
	if err != nil || !cached.Cached || cached.Equity != result.Equity {
		t.Fatalf("cached result=%+v error=%v", cached, err)
	}
}

func TestAutoAndMonteCarloAreDeterministic(t *testing.T) {
	request := Request{
		Hero: poker.MustParseCards("AsKs"), Board: poker.MustParseCards("QsJs2d"),
		Opponents: [][]poker.Card{{}}, Samples: 2_000, Seed: 42,
	}
	calculator := NewCalculator()
	first, err := calculator.Calculate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := calculator.Calculate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Method != AlgorithmMonteCarlo || first.Wins != second.Wins || first.Ties != second.Ties || first.Equity != second.Equity {
		t.Fatalf("results differ: first=%+v second=%+v", first, second)
	}
	if first.Confidence95 <= 0 || first.Trials != 2_000 {
		t.Fatalf("unexpected monte carlo metadata: %+v", first)
	}
}

func TestAutoTurnAndMultiwayMonteCarlo(t *testing.T) {
	calculator := NewCalculator()
	turn, err := calculator.Calculate(context.Background(), Request{
		Hero: poker.MustParseCards("AsKs"), Board: poker.MustParseCards("QsJs2d3c"),
		Opponents: [][]poker.Card{{}}, Samples: 1_000, Seed: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if turn.EstimatedExact != 45_540 || turn.Method != AlgorithmMonteCarlo {
		t.Fatalf("turn result = %+v", turn)
	}

	multiway, err := calculator.Calculate(context.Background(), Request{
		Hero: poker.MustParseCards("AsKs"), Board: poker.MustParseCards("QsJs2d"),
		Opponents: [][]poker.Card{{}, {}, {}}, Samples: 1_000, Seed: 9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if multiway.Trials != 1_000 || multiway.Wins+multiway.Ties+multiway.Losses != 1_000 || multiway.Equity < 0 || multiway.Equity > 1 {
		t.Fatalf("multiway result = %+v", multiway)
	}
}

func TestPreflopLookup(t *testing.T) {
	calculator := NewCalculator()
	aces, err := calculator.Calculate(context.Background(), Request{Hero: poker.MustParseCards("AsAh"), Opponents: [][]poker.Card{{}}})
	if err != nil {
		t.Fatal(err)
	}
	sevenDeuce, err := calculator.Calculate(context.Background(), Request{Hero: poker.MustParseCards("7s2h"), Opponents: [][]poker.Card{{}}})
	if err != nil {
		t.Fatal(err)
	}
	if aces.Method != AlgorithmPreflop || aces.Trials != 20_000 || aces.Equity <= sevenDeuce.Equity {
		t.Fatalf("aces=%+v seven-deuce=%+v", aces, sevenDeuce)
	}
	if len(preflopHeadsUpRandom) != 169 {
		t.Fatalf("preflop table entries = %d", len(preflopHeadsUpRandom))
	}
	if class, _ := StartingHandClass(poker.MustParseCards("KsAs")); class != "AKs" {
		t.Fatalf("class = %s", class)
	}
}

func TestStartingHandPercentileRanksAllConcreteCombos(t *testing.T) {
	aces, err := StartingHandPercentile(poker.MustParseCards("AsAh"))
	if err != nil {
		t.Fatal(err)
	}
	sevenDeuce, err := StartingHandPercentile(poker.MustParseCards("7s2h"))
	if err != nil {
		t.Fatal(err)
	}
	if aces >= sevenDeuce {
		t.Fatalf("aces percentile=%f seven-deuce=%f", aces, sevenDeuce)
	}
	seen := make(map[float64]bool, 1326)
	deck := poker.FullDeck()
	for left := 0; left < len(deck); left++ {
		for right := left + 1; right < len(deck); right++ {
			percentile, err := StartingHandPercentile([]poker.Card{deck[left], deck[right]})
			if err != nil || percentile <= 0 || percentile >= 1 || seen[percentile] {
				t.Fatalf("combo=%s%s percentile=%f duplicate=%t err=%v", deck[left], deck[right], percentile, seen[percentile], err)
			}
			seen[percentile] = true
		}
	}
	if len(seen) != 1326 {
		t.Fatalf("percentiles=%d", len(seen))
	}
}

func TestAutoAlgorithmFeatureSwitches(t *testing.T) {
	calculator := NewCalculator()
	calculator.PreflopLookupEnabled = false
	preflop, err := calculator.Calculate(context.Background(), Request{Hero: poker.MustParseCards("AsAh"), Opponents: [][]poker.Card{{}}, Samples: 100, Seed: 1})
	if err != nil || preflop.Method != AlgorithmMonteCarlo {
		t.Fatalf("disabled preflop lookup result=%+v error=%v", preflop, err)
	}
	calculator.AutoExactEnabled = false
	river, err := calculator.Calculate(context.Background(), Request{Hero: poker.MustParseCards("AsAh"), Board: poker.MustParseCards("2c3d7h9sJc"), Opponents: [][]poker.Card{{}}, Samples: 100, Seed: 2})
	if err != nil || river.Method != AlgorithmMonteCarlo {
		t.Fatalf("disabled auto exact result=%+v error=%v", river, err)
	}
}

func TestExactLimitAndCancellation(t *testing.T) {
	calculator := NewCalculator()
	_, err := calculator.Calculate(context.Background(), Request{
		Hero: poker.MustParseCards("AsKs"), Opponents: [][]poker.Card{{}},
		Algorithm: AlgorithmExact, MaxExactOutcomes: 10,
	})
	if !errors.Is(err, ErrExactTooLarge) {
		t.Fatalf("error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := calculator.Calculate(ctx, Request{
		Hero: poker.MustParseCards("AsKs"), Opponents: [][]poker.Card{{}},
		Algorithm: AlgorithmMonteCarlo, Samples: 10_000, Seed: 1,
	})
	if !errors.Is(err, context.Canceled) || result.Completed {
		t.Fatalf("result=%+v error=%v", result, err)
	}

	result, err = calculator.Calculate(ctx, Request{
		Hero: poker.MustParseCards("AsAh"), Board: poker.MustParseCards("AcAd2s3h4c"),
		Opponents: [][]poker.Card{poker.MustParseCards("KsKh")}, Algorithm: AlgorithmExact,
	})
	if !errors.Is(err, context.Canceled) || result.Trials != 0 {
		t.Fatalf("canceled exact result=%+v error=%v", result, err)
	}
}

func TestInputValidation(t *testing.T) {
	_, err := NewCalculator().Calculate(context.Background(), Request{
		Hero: poker.MustParseCards("AsKs"), Board: poker.MustParseCards("As2d3c"), Opponents: [][]poker.Card{{}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v", err)
	}
}

func TestShortDeckAndOmahaEquity(t *testing.T) {
	calculator := NewCalculator()
	short, err := calculator.Calculate(context.Background(), Request{
		Game: GameShortDeck, Hero: poker.MustParseCards("AsAh"), Board: poker.MustParseCards("6c7dThQsKc"),
		Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmExact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if short.Trials != 406 || short.Method != AlgorithmExact {
		t.Fatalf("short-deck result=%+v", short)
	}

	plo4, err := calculator.Calculate(context.Background(), Request{
		Game: GamePLO4, Hero: poker.MustParseCards("AsKsQdJc"), Board: poker.MustParseCards("Ts9s8s2d3c"),
		Opponents: [][]poker.Card{poker.MustParseCards("AhAdKhKd")}, Algorithm: AlgorithmExact,
	})
	if err != nil || plo4.Equity != 1 || plo4.Trials != 1 {
		t.Fatalf("PLO4 result=%+v error=%v", plo4, err)
	}
	hero := poker.MustParseCards("AsKsQdJc")
	board := poker.MustParseCards("Ts9s8s2d3c")
	used := make(map[poker.Card]bool)
	for _, card := range append(append([]poker.Card{}, hero...), board...) {
		used[card] = true
	}
	dead := make([]poker.Card, 0, 35)
	for _, card := range poker.FullDeck() {
		if !used[card] && len(dead) < 35 {
			dead = append(dead, card)
		}
	}
	ploExact, err := calculator.Calculate(context.Background(), Request{
		Game: GamePLO4, Hero: hero, Board: board, Dead: dead,
		Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmExact,
	})
	if err != nil || ploExact.Trials != 70 {
		t.Fatalf("PLO4 exact result=%+v error=%v", ploExact, err)
	}

	plo5, err := calculator.Calculate(context.Background(), Request{
		Game: GamePLO5, Hero: poker.MustParseCards("AsKsQdJc5h"), Board: poker.MustParseCards("Ts9s8s2d3c"),
		Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmMonteCarlo, Samples: 500, Seed: 11,
	})
	if err != nil || plo5.Trials != 500 || plo5.Method != AlgorithmMonteCarlo {
		t.Fatalf("PLO5 result=%+v error=%v", plo5, err)
	}
	plo6, err := calculator.Calculate(context.Background(), Request{
		Game: GamePLO6, Hero: poker.MustParseCards("AsKsQdJc5h4h"), Board: poker.MustParseCards("Ts9s8s2d3c"),
		Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmMonteCarlo, Samples: 300, Seed: 12,
	})
	if err != nil || plo6.Trials != 300 || plo6.Method != AlgorithmMonteCarlo {
		t.Fatalf("PLO6 result=%+v error=%v", plo6, err)
	}
	plo6Default, err := calculator.Calculate(context.Background(), Request{
		Game: GamePLO6, Hero: poker.MustParseCards("AsKsQdJc5h4h"), Board: poker.MustParseCards("Ts9s8s2d3c"),
		Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmMonteCarlo, Seed: 12,
	})
	if err != nil || plo6Default.Trials != uint64(calculator.DefaultPLO6Samples) {
		t.Fatalf("PLO6 default result=%+v error=%v", plo6Default, err)
	}

	_, err = calculator.Calculate(context.Background(), Request{
		Game: GameShortDeck, Hero: poker.MustParseCards("As2h"), Opponents: [][]poker.Card{{}},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("short-deck invalid-card error=%v", err)
	}
}

func BenchmarkMonteCarloHeadsUpFlop(b *testing.B) {
	calculator := NewCalculator()
	request := Request{Hero: poker.MustParseCards("AsKs"), Board: poker.MustParseCards("QsJs2d"), Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmMonteCarlo, Samples: 5_000, Seed: 42}
	for i := 0; i < b.N; i++ {
		if _, err := calculator.Calculate(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPreflopLookup(b *testing.B) {
	calculator := NewCalculator()
	request := Request{Hero: poker.MustParseCards("AsKs"), Opponents: [][]poker.Card{{}}}
	for i := 0; i < b.N; i++ {
		if _, err := calculator.Calculate(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExactRiverHeadsUp(b *testing.B) {
	request := Request{Hero: poker.MustParseCards("AsAh"), Board: poker.MustParseCards("2c3d7h9sJc"), Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmExact}
	for i := 0; i < b.N; i++ {
		calculator := NewCalculator()
		if _, err := calculator.Calculate(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMonteCarloPLO4Flop(b *testing.B) {
	calculator := NewCalculator()
	request := Request{Game: GamePLO4, Hero: poker.MustParseCards("AsKsQdJc"), Board: poker.MustParseCards("Ts9s2d"), Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmMonteCarlo, Samples: 1_000, Seed: 42}
	for i := 0; i < b.N; i++ {
		if _, err := calculator.Calculate(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMonteCarloPLO5Flop(b *testing.B) {
	calculator := NewCalculator()
	request := Request{Game: GamePLO5, Hero: poker.MustParseCards("AsKsQdJc5h"), Board: poker.MustParseCards("Ts9s2d"), Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmMonteCarlo, Samples: 1_000, Seed: 42}
	for i := 0; i < b.N; i++ {
		if _, err := calculator.Calculate(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMonteCarloPLO6Flop(b *testing.B) {
	calculator := NewCalculator()
	request := Request{Game: GamePLO6, Hero: poker.MustParseCards("AsKsQdJc5h4h"), Board: poker.MustParseCards("Ts9s2d"), Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmMonteCarlo, Samples: 1_000, Seed: 42}
	for i := 0; i < b.N; i++ {
		if _, err := calculator.Calculate(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMonteCarloShortDeckFlop(b *testing.B) {
	calculator := NewCalculator()
	request := Request{Game: GameShortDeck, Hero: poker.MustParseCards("AsKs"), Board: poker.MustParseCards("QsJs6d"), Opponents: [][]poker.Card{{}}, Algorithm: AlgorithmMonteCarlo, Samples: 5_000, Seed: 42}
	for i := 0; i < b.N; i++ {
		if _, err := calculator.Calculate(context.Background(), request); err != nil {
			b.Fatal(err)
		}
	}
}
