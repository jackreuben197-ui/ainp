package equity

import (
	"fmt"
	"sort"
	"sync"

	"gitlab.com/ubenbill/ainp/internal/poker"
)

var (
	startingHandPercentilesOnce sync.Once
	startingHandPercentiles     map[uint16]float64
)

type PreflopEntry struct {
	WinProbability float64
	TieProbability float64
	Equity         float64
	Trials         uint64
}

func StartingHandClass(cards []poker.Card) (string, error) {
	if len(cards) != 2 || !cards[0].Valid() || !cards[1].Valid() || cards[0] == cards[1] {
		return "", fmt.Errorf("%w: starting hand requires two unique cards", ErrInvalidInput)
	}
	left, right := cards[0], cards[1]
	if left.Rank() < right.Rank() {
		left, right = right, left
	}
	key := rankSymbol(left.Rank()) + rankSymbol(right.Rank())
	if left.Rank() == right.Rank() {
		return key, nil
	}
	if left.Suit() == right.Suit() {
		return key + "s", nil
	}
	return key + "o", nil
}

// StartingHandPercentile returns the strength rank of one concrete NLH combo.
// Zero is strongest and one is weakest. All 1,326 two-card combinations are
// ordered by the generated heads-up preflop equity table; suit combinations
// break ties deterministically so percentage ranges have sub-class precision.
func StartingHandPercentile(cards []poker.Card) (float64, error) {
	if _, err := StartingHandClass(cards); err != nil {
		return 0, err
	}
	startingHandPercentilesOnce.Do(buildStartingHandPercentiles)
	key := startingHandComboKey(cards[0], cards[1])
	percentile, ok := startingHandPercentiles[key]
	if !ok {
		return 0, fmt.Errorf("%w: missing starting-hand percentile", ErrInvalidInput)
	}
	return percentile, nil
}

func buildStartingHandPercentiles() {
	type rankedCombo struct {
		left, right poker.Card
		class       string
		equity      float64
	}
	deck := poker.FullDeck()
	combos := make([]rankedCombo, 0, 1326)
	for left := 0; left < len(deck); left++ {
		for right := left + 1; right < len(deck); right++ {
			cards := []poker.Card{deck[left], deck[right]}
			class, _ := StartingHandClass(cards)
			combos = append(combos, rankedCombo{left: cards[0], right: cards[1], class: class, equity: preflopHeadsUpRandom[class].Equity})
		}
	}
	sort.Slice(combos, func(left, right int) bool {
		if combos[left].equity != combos[right].equity {
			return combos[left].equity > combos[right].equity
		}
		if combos[left].class != combos[right].class {
			return combos[left].class < combos[right].class
		}
		return startingHandComboKey(combos[left].left, combos[left].right) < startingHandComboKey(combos[right].left, combos[right].right)
	})
	startingHandPercentiles = make(map[uint16]float64, len(combos))
	for rank, combo := range combos {
		startingHandPercentiles[startingHandComboKey(combo.left, combo.right)] = (float64(rank) + .5) / float64(len(combos))
	}
}

func startingHandComboKey(left, right poker.Card) uint16 {
	if left > right {
		left, right = right, left
	}
	return uint16(left)<<8 | uint16(right)
}

func rankSymbol(rank int) string {
	return string("23456789TJQKA"[rank-2])
}
