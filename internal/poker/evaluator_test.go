package poker

import "testing"

func TestEvaluateCategories(t *testing.T) {
	tests := []struct {
		name     string
		cards    string
		category Category
	}{
		{"high card", "AsKd9c7h3s", HighCard},
		{"one pair", "AsAd9c7h3s", OnePair},
		{"two pair", "AsAd9c9h3s", TwoPair},
		{"trips", "AsAdAc7h3s", ThreeOfAKind},
		{"wheel", "As2d3c4h5s", Straight},
		{"flush", "AsJs8s4s2s", Flush},
		{"full house", "AsAdAc7h7s", FullHouse},
		{"quads", "AsAdAcAh7s", FourOfAKind},
		{"straight flush", "9s8s7s6s5s", StraightFlush},
		{"royal flush", "AsKsQsJsTs", RoyalFlush},
		{"best of seven", "AsKsQsJsTs2d2c", RoyalFlush},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rank, err := Evaluate(MustParseCards(test.cards))
			if err != nil {
				t.Fatal(err)
			}
			if rank.Category != test.category {
				t.Fatalf("category = %s, want %s", rank.Category, test.category)
			}
		})
	}
}

func TestEvaluateKickersAndBestFive(t *testing.T) {
	aceKicker, _ := Evaluate(MustParseCards("AhAdKcQs9d2h3c"))
	queenKicker, _ := Evaluate(MustParseCards("AsAcQdJs9c2d3h"))
	if aceKicker.Score <= queenKicker.Score {
		t.Fatalf("ace kicker score %d <= queen kicker score %d", aceKicker.Score, queenKicker.Score)
	}
	if len(aceKicker.BestFive) != 5 {
		t.Fatal("best five was not returned")
	}
}

func TestEvaluateDuplicateAndInvalidCards(t *testing.T) {
	if _, err := ParseCards("AsAs"); err == nil {
		t.Fatal("expected duplicate card error")
	}
	if _, err := ParseCard("1s"); err == nil {
		t.Fatal("expected invalid rank error")
	}
	if _, err := Evaluate(MustParseCards("AsKd9c7h")); err == nil {
		t.Fatal("expected hand size error")
	}
}

func TestCompareTieAndWheelOrdering(t *testing.T) {
	board := MustParseCards("2s3d4c5h9s")
	left := append(append([]Card{}, board...), MustParseCards("AsKd")...)
	right := append(append([]Card{}, board...), MustParseCards("AhQd")...)
	comparison, err := Compare(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if comparison != 0 {
		t.Fatalf("wheel should tie, comparison = %d", comparison)
	}
}

func TestAllFiveCardCategoryCounts(t *testing.T) {
	want := map[Category]int{
		HighCard: 1_302_540, OnePair: 1_098_240, TwoPair: 123_552,
		ThreeOfAKind: 54_912, Straight: 10_200, Flush: 5_108,
		FullHouse: 3_744, FourOfAKind: 624, StraightFlush: 36, RoyalFlush: 4,
	}
	got := make(map[Category]int, len(want))
	deck := FullDeck()
	for a := 0; a < 48; a++ {
		for b := a + 1; b < 49; b++ {
			for c := b + 1; c < 50; c++ {
				for d := c + 1; d < 51; d++ {
					for e := d + 1; e < 52; e++ {
						got[evaluateFive([5]Card{deck[a], deck[b], deck[c], deck[d], deck[e]}).Category]++
					}
				}
			}
		}
	}
	for category, count := range want {
		if got[category] != count {
			t.Errorf("%s count = %d, want %d", category, got[category], count)
		}
	}
}

func TestShortDeckRules(t *testing.T) {
	wheel, err := EvaluateWithRules(MustParseCards("As9d8c7h6s"), ShortDeckRules)
	if err != nil || wheel.Category != Straight {
		t.Fatalf("short-deck wheel=%+v error=%v", wheel, err)
	}
	trips := MustParseCards("AsAhAcKdQd")
	straight := MustParseCards("9s8h7c6dAs")
	comparison, err := CompareWithRules(trips, straight, ShortDeckRules)
	if err != nil || comparison != 1 {
		t.Fatalf("short-deck trips vs straight = %d, error=%v", comparison, err)
	}
	comparison, err = CompareWithRules(trips, straight, ShortDeckFixedRules)
	if err != nil || comparison != -1 {
		t.Fatalf("fixed short-deck trips vs straight = %d, error=%v", comparison, err)
	}
	flush := MustParseCards("AsKsQs8s6s")
	fullHouse := MustParseCards("AhAdAcKhKd")
	comparison, err = CompareWithRules(flush, fullHouse, ShortDeckRules)
	if err != nil || comparison != 1 {
		t.Fatalf("short-deck flush vs full house = %d, error=%v", comparison, err)
	}
	if _, err := EvaluateWithRules(MustParseCards("AsKsQsJs2s"), ShortDeckRules); err == nil {
		t.Fatal("expected rank below six to be rejected")
	}
}

func TestAllShortDeckFiveCardCategoryCounts(t *testing.T) {
	want := map[Category]int{
		HighCard: 122_400, OnePair: 193_536, TwoPair: 36_288,
		ThreeOfAKind: 16_128, Straight: 6_120, Flush: 480,
		FullHouse: 1_728, FourOfAKind: 288, StraightFlush: 20, RoyalFlush: 4,
	}
	deck := make([]Card, 0, 36)
	for _, card := range FullDeck() {
		if card.Rank() >= 6 {
			deck = append(deck, card)
		}
	}
	got := make(map[Category]int, len(want))
	for a := 0; a < len(deck)-4; a++ {
		for b := a + 1; b < len(deck)-3; b++ {
			for c := b + 1; c < len(deck)-2; c++ {
				for d := c + 1; d < len(deck)-1; d++ {
					for e := d + 1; e < len(deck); e++ {
						got[evaluateFiveWithRules([5]Card{deck[a], deck[b], deck[c], deck[d], deck[e]}, ShortDeckRules).Category]++
					}
				}
			}
		}
	}
	for category, count := range want {
		if got[category] != count {
			t.Errorf("short-deck %s count = %d, want %d", category, got[category], count)
		}
	}
}

func TestOmahaUsesExactlyTwoHoleAndThreeBoardCards(t *testing.T) {
	rank, err := EvaluateOmaha(MustParseCards("AsKhQdJc"), MustParseCards("Ts9s8s7s6s"), StandardRules)
	if err != nil {
		t.Fatal(err)
	}
	if rank.Category != Straight {
		t.Fatalf("Omaha rank = %s, want straight", rank.Category)
	}
	flush, err := EvaluateOmaha(MustParseCards("AsKsQdJc5h"), MustParseCards("Ts9s8s2d3c"), StandardRules)
	if err != nil || flush.Category != Flush {
		t.Fatalf("PLO5 flush=%+v error=%v", flush, err)
	}
	plo6, err := EvaluateOmaha(MustParseCards("AsKsQdJc5h4h"), MustParseCards("Ts9s8s2d3c"), StandardRules)
	if err != nil || plo6.Category != Flush {
		t.Fatalf("PLO6 flush=%+v error=%v", plo6, err)
	}
}
