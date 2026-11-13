package poker

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidCardString = errors.New("invalid card string")
	ErrDuplicateCard     = errors.New("duplicate card")
)

// Card uses a compact 0..51 encoding: suit*13 + rankIndex.
// Ranks are 2..14 and suits use the AiCon wire format s/h/c/d.
type Card uint8

const InvalidCard Card = 0xff

func NewCard(rank int, suit byte) (Card, error) {
	rankIndex := rank - 2
	if rankIndex < 0 || rankIndex >= 13 {
		return InvalidCard, fmt.Errorf("%w: rank %d", ErrInvalidCardString, rank)
	}
	suitIndex, ok := parseSuit(suit)
	if !ok {
		return InvalidCard, fmt.Errorf("%w: suit %q", ErrInvalidCardString, suit)
	}
	return Card(suitIndex*13 + rankIndex), nil
}

func ParseCard(value string) (Card, error) {
	if len(value) != 2 {
		return InvalidCard, fmt.Errorf("%w: %q", ErrInvalidCardString, value)
	}
	rank, ok := parseRank(value[0])
	if !ok {
		return InvalidCard, fmt.Errorf("%w: rank in %q", ErrInvalidCardString, value)
	}
	return NewCard(rank, value[1])
}

func ParseCards(value string) ([]Card, error) {
	if len(value)%2 != 0 {
		return nil, fmt.Errorf("%w: card list has odd length", ErrInvalidCardString)
	}
	cards := make([]Card, 0, len(value)/2)
	seen := uint64(0)
	for i := 0; i < len(value); i += 2 {
		card, err := ParseCard(value[i : i+2])
		if err != nil {
			return nil, err
		}
		mask := uint64(1) << card
		if seen&mask != 0 {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateCard, card)
		}
		seen |= mask
		cards = append(cards, card)
	}
	return cards, nil
}

func MustParseCards(value string) []Card {
	cards, err := ParseCards(value)
	if err != nil {
		panic(err)
	}
	return cards
}

func (c Card) Valid() bool { return c < 52 }
func (c Card) Rank() int   { return int(c%13) + 2 }
func (c Card) Suit() int   { return int(c / 13) }

func (c Card) String() string {
	if !c.Valid() {
		return "??"
	}
	ranks := "23456789TJQKA"
	suits := "shcd"
	return string([]byte{ranks[c.Rank()-2], suits[c.Suit()]})
}

func CardsString(cards []Card) string {
	var builder strings.Builder
	builder.Grow(len(cards) * 2)
	for _, card := range cards {
		builder.WriteString(card.String())
	}
	return builder.String()
}

func FullDeck() []Card {
	deck := make([]Card, 52)
	for i := range deck {
		deck[i] = Card(i)
	}
	return deck
}

func parseRank(value byte) (int, bool) {
	switch value {
	case '2', '3', '4', '5', '6', '7', '8', '9':
		return int(value - '0'), true
	case 'T', 't':
		return 10, true
	case 'J', 'j':
		return 11, true
	case 'Q', 'q':
		return 12, true
	case 'K', 'k':
		return 13, true
	case 'A', 'a':
		return 14, true
	default:
		return 0, false
	}
}

func parseSuit(value byte) (int, bool) {
	switch value {
	case 's', 'S':
		return 0, true
	case 'h', 'H':
		return 1, true
	case 'c', 'C':
		return 2, true
	case 'd', 'D':
		return 3, true
	default:
		return 0, false
	}
}
