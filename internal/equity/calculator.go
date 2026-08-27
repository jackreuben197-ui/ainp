package equity

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"gitlab.com/ubenbill/ainp/internal/poker"
)

var (
	ErrInvalidInput  = errors.New("invalid equity input")
	ErrExactTooLarge = errors.New("exact enumeration exceeds configured limit")
)

type Algorithm string
type Game string

const (
	AlgorithmAuto       Algorithm = "auto"
	AlgorithmExact      Algorithm = "exact"
	AlgorithmMonteCarlo Algorithm = "monte_carlo"
	AlgorithmPreflop    Algorithm = "preflop_lookup"
)

const (
	GameNLH            Game = "NLH"
	GameShortDeck      Game = "SHORT_DECK"
	GameShortDeckFixed Game = "SHORT_DECK_FIXED"
	GamePLO4           Game = "PLO4"
	GamePLO5           Game = "PLO5"
	GamePLO6           Game = "PLO6"
)

type Request struct {
	Game             Game
	Hero             []poker.Card
	Board            []poker.Card
	Opponents        [][]poker.Card
	Dead             []poker.Card
	Algorithm        Algorithm
	Samples          int
	Seed             int64
	MaxExactOutcomes uint64
}

type Result struct {
	WinProbability  float64
	TieProbability  float64
	LossProbability float64
	Equity          float64
	Wins            uint64
	Ties            uint64
	Losses          uint64
	Trials          uint64
	Method          Algorithm
	Seed            int64
	Confidence95    float64
	EstimatedExact  uint64
	Completed       bool
	Cached          bool
	Duration        time.Duration
	equitySquared   float64
}

type Calculator struct {
	DefaultSamples       int
	DefaultPLO4Samples   int
	DefaultPLO5Samples   int
	DefaultPLO6Samples   int
	MaxExactOutcomes     uint64
	PreflopLookupEnabled bool
	AutoExactEnabled     bool
	exactCache           *resultCache
}

func NewCalculator() *Calculator {
	return &Calculator{DefaultSamples: 5_000, DefaultPLO4Samples: 3_000, DefaultPLO5Samples: 2_000, DefaultPLO6Samples: 1_500, MaxExactOutcomes: 5_000, PreflopLookupEnabled: true, AutoExactEnabled: true, exactCache: newResultCache(4_096)}
}

func (c *Calculator) SetCacheCapacity(capacity int) {
	if capacity <= 0 {
		c.exactCache = nil
		return
	}
	c.exactCache = newResultCache(capacity)
}

func (c *Calculator) Calculate(ctx context.Context, req Request) (Result, error) {
	started := time.Now()
	prepared, err := prepare(req)
	if err != nil {
		return Result{Duration: time.Since(started)}, err
	}
	limit := req.MaxExactOutcomes
	if limit == 0 {
		limit = c.MaxExactOutcomes
	}
	estimated := estimateOutcomes(len(prepared.deck), len(prepared.unknownOpponents), prepared.holeCards, prepared.boardNeeded, math.MaxUint64)
	method := req.Algorithm
	if method == AlgorithmPreflop && !eligibleForPreflopLookup(req, prepared) {
		return Result{Method: AlgorithmPreflop, EstimatedExact: estimated, Duration: time.Since(started)}, fmt.Errorf("%w: preflop lookup only supports one random opponent without board or dead cards", ErrInvalidInput)
	}
	if method == AlgorithmPreflop && !c.PreflopLookupEnabled {
		return Result{Method: AlgorithmPreflop, EstimatedExact: estimated, Duration: time.Since(started)}, fmt.Errorf("%w: preflop lookup is disabled", ErrInvalidInput)
	}
	if method == AlgorithmPreflop || (c.PreflopLookupEnabled && (method == "" || method == AlgorithmAuto) && eligibleForPreflopLookup(req, prepared)) {
		result, lookupErr := lookupPreflop(req.Hero)
		result.EstimatedExact = estimated
		result.Duration = time.Since(started)
		return result, lookupErr
	}
	if method == "" || method == AlgorithmAuto {
		if c.AutoExactEnabled && estimated <= limit {
			method = AlgorithmExact
		} else {
			method = AlgorithmMonteCarlo
		}
	}

	result := Result{Method: method, Seed: req.Seed, EstimatedExact: estimated}
	switch method {
	case AlgorithmExact:
		if estimated > limit {
			result.Duration = time.Since(started)
			return result, fmt.Errorf("%w: estimated=%d limit=%d", ErrExactTooLarge, estimated, limit)
		}
		key := exactCacheKey(req)
		if c.exactCache != nil {
			if cached, ok := c.exactCache.Get(key); ok {
				cached.Cached = true
				cached.Duration = time.Since(started)
				return cached, nil
			}
		}
		err = enumerateExact(ctx, prepared, &result)
		if err == nil && c.exactCache != nil {
			finalize(&result)
			result.Completed = true
			result.Duration = time.Since(started)
			c.exactCache.Set(key, result)
			return result, nil
		}
	case AlgorithmMonteCarlo:
		samples := req.Samples
		if samples <= 0 {
			switch prepared.game {
			case GamePLO4:
				samples = c.DefaultPLO4Samples
			case GamePLO5:
				samples = c.DefaultPLO5Samples
			case GamePLO6:
				samples = c.DefaultPLO6Samples
			default:
				samples = c.DefaultSamples
			}
		}
		if samples <= 0 {
			return result, fmt.Errorf("%w: samples must be greater than zero", ErrInvalidInput)
		}
		if result.Seed == 0 {
			result.Seed = time.Now().UnixNano()
		}
		err = simulate(ctx, prepared, samples, result.Seed, &result)
	default:
		return result, fmt.Errorf("%w: unsupported algorithm %q", ErrInvalidInput, method)
	}
	finalize(&result)
	result.Completed = err == nil
	result.Duration = time.Since(started)
	return result, err
}

func eligibleForPreflopLookup(req Request, prepared preparedRequest) bool {
	return prepared.game == GameNLH && len(req.Board) == 0 && len(req.Dead) == 0 && len(req.Opponents) == 1 && len(prepared.unknownOpponents) == 1
}

func lookupPreflop(hero []poker.Card) (Result, error) {
	key, err := StartingHandClass(hero)
	if err != nil {
		return Result{Method: AlgorithmPreflop}, err
	}
	entry, ok := preflopHeadsUpRandom[key]
	if !ok {
		return Result{Method: AlgorithmPreflop}, fmt.Errorf("%w: no preflop entry for %s", ErrInvalidInput, key)
	}
	wins := uint64(math.Round(entry.WinProbability * float64(entry.Trials)))
	ties := uint64(math.Round(entry.TieProbability * float64(entry.Trials)))
	confidence95 := 1.96 * math.Sqrt(entry.Equity*(1-entry.Equity)/float64(entry.Trials))
	return Result{
		WinProbability:  entry.WinProbability,
		TieProbability:  entry.TieProbability,
		LossProbability: 1 - entry.WinProbability - entry.TieProbability,
		Equity:          entry.Equity,
		Wins:            wins,
		Ties:            ties,
		Losses:          entry.Trials - wins - ties,
		Trials:          entry.Trials,
		Method:          AlgorithmPreflop,
		Confidence95:    confidence95,
		Completed:       true,
	}, nil
}

type preparedRequest struct {
	game             Game
	hero             []poker.Card
	board            []poker.Card
	opponents        [][]poker.Card
	unknownOpponents []int
	deck             []poker.Card
	holeCards        int
	boardNeeded      int
	rules            poker.Rules
	omaha            bool
}

func prepare(req Request) (preparedRequest, error) {
	spec, err := gameSpecification(req.Game)
	if err != nil {
		return preparedRequest{}, err
	}
	if len(req.Hero) != spec.holeCards {
		return preparedRequest{}, fmt.Errorf("%w: %s hero must have exactly %d cards", ErrInvalidInput, spec.game, spec.holeCards)
	}
	if len(req.Board) != 0 && len(req.Board) != 3 && len(req.Board) != 4 && len(req.Board) != 5 {
		return preparedRequest{}, fmt.Errorf("%w: board must contain 0, 3, 4 or 5 cards", ErrInvalidInput)
	}
	if len(req.Opponents) < 1 || len(req.Opponents) > 8 {
		return preparedRequest{}, fmt.Errorf("%w: opponents must contain between 1 and 8 seats", ErrInvalidInput)
	}

	seen := uint64(0)
	addKnown := func(cards []poker.Card, label string) error {
		for _, card := range cards {
			if !card.Valid() {
				return fmt.Errorf("%w: invalid %s card", ErrInvalidInput, label)
			}
			mask := uint64(1) << card
			if seen&mask != 0 {
				return fmt.Errorf("%w: duplicate card %s", ErrInvalidInput, card)
			}
			if card.Rank() < spec.rules.MinimumRank {
				return fmt.Errorf("%w: card %s is not in the %s deck", ErrInvalidInput, card, spec.game)
			}
			seen |= mask
		}
		return nil
	}
	if err := addKnown(req.Hero, "hero"); err != nil {
		return preparedRequest{}, err
	}
	if err := addKnown(req.Board, "board"); err != nil {
		return preparedRequest{}, err
	}
	if err := addKnown(req.Dead, "dead"); err != nil {
		return preparedRequest{}, err
	}

	opponents := make([][]poker.Card, len(req.Opponents))
	unknown := make([]int, 0, len(req.Opponents))
	for i, hole := range req.Opponents {
		if len(hole) != 0 && len(hole) != spec.holeCards {
			return preparedRequest{}, fmt.Errorf("%w: %s opponent %d must have zero or %d cards", ErrInvalidInput, spec.game, i, spec.holeCards)
		}
		if err := addKnown(hole, fmt.Sprintf("opponent %d", i)); err != nil {
			return preparedRequest{}, err
		}
		if len(hole) == 0 {
			opponents[i] = make([]poker.Card, spec.holeCards)
			unknown = append(unknown, i)
		} else {
			opponents[i] = append([]poker.Card(nil), hole...)
		}
	}

	deck := make([]poker.Card, 0, 52)
	for _, card := range poker.FullDeck() {
		if card.Rank() < spec.rules.MinimumRank {
			continue
		}
		if seen&(uint64(1)<<card) == 0 {
			deck = append(deck, card)
		}
	}
	boardNeeded := 5 - len(req.Board)
	if len(unknown)*spec.holeCards+boardNeeded > len(deck) {
		return preparedRequest{}, fmt.Errorf("%w: not enough unknown cards", ErrInvalidInput)
	}
	return preparedRequest{
		game:             spec.game,
		hero:             append([]poker.Card(nil), req.Hero...),
		board:            append([]poker.Card(nil), req.Board...),
		opponents:        opponents,
		unknownOpponents: unknown,
		deck:             deck,
		holeCards:        spec.holeCards,
		boardNeeded:      boardNeeded,
		rules:            spec.rules,
		omaha:            spec.omaha,
	}, nil
}

type gameSpec struct {
	game      Game
	holeCards int
	rules     poker.Rules
	omaha     bool
}

func gameSpecification(game Game) (gameSpec, error) {
	if game == "" {
		game = GameNLH
	}
	switch game {
	case GameNLH:
		return gameSpec{game: game, holeCards: 2, rules: poker.StandardRules}, nil
	case GameShortDeck:
		return gameSpec{game: game, holeCards: 2, rules: poker.ShortDeckRules}, nil
	case GameShortDeckFixed:
		return gameSpec{game: game, holeCards: 2, rules: poker.ShortDeckFixedRules}, nil
	case GamePLO4:
		return gameSpec{game: game, holeCards: 4, rules: poker.StandardRules, omaha: true}, nil
	case GamePLO5:
		return gameSpec{game: game, holeCards: 5, rules: poker.StandardRules, omaha: true}, nil
	case GamePLO6:
		return gameSpec{game: game, holeCards: 6, rules: poker.StandardRules, omaha: true}, nil
	default:
		return gameSpec{}, fmt.Errorf("%w: unsupported game %q", ErrInvalidInput, game)
	}
}

func estimateOutcomes(deckSize, unknownOpponents, holeCards, boardNeeded int, cap uint64) uint64 {
	total := uint64(1)
	remaining := deckSize
	for i := 0; i < unknownOpponents; i++ {
		total = saturatingMultiply(total, combinations(remaining, holeCards), cap)
		remaining -= holeCards
	}
	total = saturatingMultiply(total, combinations(remaining, boardNeeded), cap)
	return total
}

func combinations(n, k int) uint64 {
	if k < 0 || k > n {
		return 0
	}
	if k > n-k {
		k = n - k
	}
	result := uint64(1)
	for i := 1; i <= k; i++ {
		result = result * uint64(n-k+i) / uint64(i)
	}
	return result
}

func saturatingMultiply(left, right, cap uint64) uint64 {
	if left == 0 || right == 0 {
		return 0
	}
	if left > cap/right {
		return cap
	}
	return left * right
}

func finalize(result *Result) {
	if result.Trials == 0 {
		return
	}
	trials := float64(result.Trials)
	result.WinProbability = float64(result.Wins) / trials
	result.TieProbability = float64(result.Ties) / trials
	result.LossProbability = float64(result.Losses) / trials
	result.Equity /= trials
	if result.Method == AlgorithmMonteCarlo {
		variance := result.equitySquared/trials - result.Equity*result.Equity
		if variance < 0 {
			variance = 0
		}
		result.Confidence95 = 1.96 * math.Sqrt(variance/trials)
	}
}
