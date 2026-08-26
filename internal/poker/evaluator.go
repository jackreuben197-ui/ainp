package poker

import (
	"errors"
	"fmt"
)

var ErrInvalidHandSize = errors.New("a hand must contain between 5 and 7 cards")

type Rules struct {
	Name           string
	MinimumRank    int
	AceLowStraight int
	strength       [11]uint8
}

type Category uint8

const (
	HighCard Category = iota + 1
	OnePair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
	RoyalFlush
)

var (
	StandardRules = Rules{
		Name: "standard", MinimumRank: 2, AceLowStraight: 5,
		strength: [11]uint8{HighCard: 1, OnePair: 2, TwoPair: 3, ThreeOfAKind: 4, Straight: 5, Flush: 6, FullHouse: 7, FourOfAKind: 8, StraightFlush: 9, RoyalFlush: 10},
	}
	ShortDeckRules = Rules{
		Name: "short_deck", MinimumRank: 6, AceLowStraight: 9,
		strength: [11]uint8{HighCard: 1, OnePair: 2, TwoPair: 3, Straight: 4, ThreeOfAKind: 5, FullHouse: 6, Flush: 7, FourOfAKind: 8, StraightFlush: 9, RoyalFlush: 10},
	}
	ShortDeckFixedRules = Rules{
		Name: "short_deck_fixed", MinimumRank: 6, AceLowStraight: 9,
		strength: [11]uint8{HighCard: 1, OnePair: 2, TwoPair: 3, ThreeOfAKind: 4, Straight: 5, FullHouse: 6, Flush: 7, FourOfAKind: 8, StraightFlush: 9, RoyalFlush: 10},
	}
)

func (c Category) String() string {
	switch c {
	case HighCard:
		return "high_card"
	case OnePair:
		return "one_pair"
	case TwoPair:
		return "two_pair"
	case ThreeOfAKind:
		return "three_of_a_kind"
	case Straight:
		return "straight"
	case Flush:
		return "flush"
	case FullHouse:
		return "full_house"
	case FourOfAKind:
		return "four_of_a_kind"
	case StraightFlush:
		return "straight_flush"
	case RoyalFlush:
		return "royal_flush"
	default:
		return "unknown"
	}
}

type HandRank struct {
	Category Category
	Score    uint32
	BestFive [5]Card
}

func Evaluate(cards []Card) (HandRank, error) {
	return EvaluateWithRules(cards, StandardRules)
}

func EvaluateWithRules(cards []Card, rules Rules) (HandRank, error) {
	if len(cards) < 5 || len(cards) > 7 {
		return HandRank{}, ErrInvalidHandSize
	}
	if err := validateUnique(cards); err != nil {
		return HandRank{}, err
	}
	if err := validateCardsForRules(cards, rules); err != nil {
		return HandRank{}, err
	}
	return EvaluateUncheckedWithRules(cards, rules), nil
}

// EvaluateUnchecked skips size, validity and duplicate checks. It is intended
// for hot paths that deal cards from an already validated deck.
func EvaluateUnchecked(cards []Card) HandRank {
	return EvaluateUncheckedWithRules(cards, StandardRules)
}

func EvaluateUncheckedWithRules(cards []Card, rules Rules) HandRank {
	var best HandRank
	var combination [5]Card
	chooseFive(cards, 0, 0, &combination, func(candidate [5]Card) {
		rank := evaluateFiveWithRules(candidate, rules)
		if rank.Score > best.Score {
			best = rank
		}
	})
	return best
}

func Compare(left, right []Card) (int, error) {
	return CompareWithRules(left, right, StandardRules)
}

func CompareWithRules(left, right []Card, rules Rules) (int, error) {
	l, err := EvaluateWithRules(left, rules)
	if err != nil {
		return 0, err
	}
	r, err := EvaluateWithRules(right, rules)
	if err != nil {
		return 0, err
	}
	switch {
	case l.Score > r.Score:
		return 1, nil
	case l.Score < r.Score:
		return -1, nil
	default:
		return 0, nil
	}
}

// EvaluateOmaha enforces the Omaha rule of exactly two hole cards and exactly
// three board cards. PLO4, PLO5 and PLO6 are selected by the hole-card count.
func EvaluateOmaha(hole, board []Card, rules Rules) (HandRank, error) {
	if (len(hole) != 4 && len(hole) != 5 && len(hole) != 6) || len(board) < 3 || len(board) > 5 {
		return HandRank{}, fmt.Errorf("%w: Omaha requires 4, 5 or 6 hole cards and 3 to 5 board cards", ErrInvalidHandSize)
	}
	all := append(append(make([]Card, 0, len(hole)+len(board)), hole...), board...)
	if err := validateUnique(all); err != nil {
		return HandRank{}, err
	}
	if err := validateCardsForRules(all, rules); err != nil {
		return HandRank{}, err
	}
	return EvaluateOmahaUnchecked(hole, board, rules), nil
}

func EvaluateOmahaUnchecked(hole, board []Card, rules Rules) HandRank {
	var best HandRank
	for h1 := 0; h1 < len(hole)-1; h1++ {
		for h2 := h1 + 1; h2 < len(hole); h2++ {
			for b1 := 0; b1 < len(board)-2; b1++ {
				for b2 := b1 + 1; b2 < len(board)-1; b2++ {
					for b3 := b2 + 1; b3 < len(board); b3++ {
						candidate := [5]Card{hole[h1], hole[h2], board[b1], board[b2], board[b3]}
						rank := evaluateFiveWithRules(candidate, rules)
						if rank.Score > best.Score {
							best = rank
						}
					}
				}
			}
		}
	}
	return best
}

func chooseFive(cards []Card, start, depth int, current *[5]Card, visit func([5]Card)) {
	if depth == 5 {
		visit(*current)
		return
	}
	remaining := 5 - depth
	for i := start; i <= len(cards)-remaining; i++ {
		current[depth] = cards[i]
		chooseFive(cards, i+1, depth+1, current, visit)
	}
}

func evaluateFive(cards [5]Card) HandRank {
	return evaluateFiveWithRules(cards, StandardRules)
}

func evaluateFiveWithRules(cards [5]Card, rules Rules) HandRank {
	var rankCounts [15]int
	var suitCounts [4]int
	for _, card := range cards {
		rankCounts[card.Rank()]++
		suitCounts[card.Suit()]++
	}

	flush := false
	for _, count := range suitCounts {
		if count == 5 {
			flush = true
			break
		}
	}
	straightHigh := findStraightHigh(rankCounts, rules)
	if flush && straightHigh > 0 {
		category := StraightFlush
		if straightHigh == 14 {
			category = RoyalFlush
		}
		return makeRank(category, cards, [5]int{straightHigh}, rules)
	}

	var quads int
	var trips [2]int
	var pairs [2]int
	var singles [5]int
	tripCount, pairCount, singleCount := 0, 0, 0
	for rank := 14; rank >= 2; rank-- {
		switch rankCounts[rank] {
		case 4:
			quads = rank
		case 3:
			trips[tripCount] = rank
			tripCount++
		case 2:
			pairs[pairCount] = rank
			pairCount++
		case 1:
			singles[singleCount] = rank
			singleCount++
		}
	}
	if quads > 0 {
		return makeRank(FourOfAKind, cards, [5]int{quads, singles[0]}, rules)
	}
	if tripCount == 1 && pairCount == 1 {
		return makeRank(FullHouse, cards, [5]int{trips[0], pairs[0]}, rules)
	}
	if flush {
		return makeRank(Flush, cards, singles, rules)
	}
	if straightHigh > 0 {
		return makeRank(Straight, cards, [5]int{straightHigh}, rules)
	}
	if tripCount == 1 {
		return makeRank(ThreeOfAKind, cards, [5]int{trips[0], singles[0], singles[1]}, rules)
	}
	if pairCount == 2 {
		return makeRank(TwoPair, cards, [5]int{pairs[0], pairs[1], singles[0]}, rules)
	}
	if pairCount == 1 {
		return makeRank(OnePair, cards, [5]int{pairs[0], singles[0], singles[1], singles[2]}, rules)
	}
	return makeRank(HighCard, cards, singles, rules)
}

func findStraightHigh(counts [15]int, rules Rules) int {
	consecutive := 0
	for rank := 14; rank >= 2; rank-- {
		if counts[rank] > 0 {
			consecutive++
			if consecutive == 5 {
				return rank + 4
			}
		} else {
			consecutive = 0
		}
	}
	if rules.AceLowStraight == 5 && counts[14] > 0 && counts[2] > 0 && counts[3] > 0 && counts[4] > 0 && counts[5] > 0 {
		return rules.AceLowStraight
	}
	if rules.AceLowStraight == 9 && counts[14] > 0 && counts[6] > 0 && counts[7] > 0 && counts[8] > 0 && counts[9] > 0 {
		return rules.AceLowStraight
	}
	return 0
}

func makeRank(category Category, cards [5]Card, values [5]int, rules Rules) HandRank {
	score := uint32(rules.strength[category]) << 24
	for i, value := range values {
		score |= uint32(value) << (uint(4-i) * 4)
	}
	return HandRank{Category: category, Score: score, BestFive: cards}
}

func validateCardsForRules(cards []Card, rules Rules) error {
	for _, card := range cards {
		if card.Rank() < rules.MinimumRank {
			return fmt.Errorf("%w: %s is below minimum rank %d for %s", ErrInvalidCardString, card, rules.MinimumRank, rules.Name)
		}
	}
	return nil
}

func validateUnique(cards []Card) error {
	seen := uint64(0)
	for _, card := range cards {
		if !card.Valid() {
			return fmt.Errorf("%w: %s", ErrInvalidCardString, card)
		}
		mask := uint64(1) << card
		if seen&mask != 0 {
			return fmt.Errorf("%w: %s", ErrDuplicateCard, card)
		}
		seen |= mask
	}
	return nil
}
