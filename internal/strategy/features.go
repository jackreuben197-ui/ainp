package strategy

import (
	"fmt"

	"gitlab.com/ubenbill/ainp/internal/equity"
	"gitlab.com/ubenbill/ainp/internal/poker"
)

func buildFeatures(req Request) (Features, error) {
	if req.Street == Preflop || len(req.Board) < 3 {
		return Features{Class: ClassAir}, nil
	}
	rules, omaha, minRank, err := strategyRules(req.Game)
	if err != nil {
		return Features{}, err
	}
	var rank poker.HandRank
	if omaha {
		rank, err = poker.EvaluateOmaha(req.Hero, req.Board, rules)
	} else {
		cards := append(append(make([]poker.Card, 0, len(req.Hero)+len(req.Board)), req.Board...), req.Hero...)
		rank, err = poker.EvaluateWithRules(cards, rules)
	}
	if err != nil {
		return Features{}, err
	}

	features := Features{Category: rank.Category}
	if !omaha && len(req.Hero) == 2 && req.Hero[0].Rank() == req.Hero[1].Rank() {
		for _, card := range req.Board {
			if card.Rank() > req.Hero[0].Rank() {
				features.PocketPairUnderBoard = true
				break
			}
		}
	}
	features.FlushDraw = hasFlushDraw(req.Hero, req.Board, omaha)
	features.StraightDraw, features.DrawOuts = hasStraightDraw(req, rank, rules, omaha, minRank)
	if !omaha && rank.Category == poker.OnePair {
		features.PairFromBoardOnly = boardHasPair(req.Board)
		features.OnePairBelowTopBoard = onePairBelowTopBoard(req.Hero, req.Board)
	}
	if req.Street == River && len(req.Board) == 5 && !omaha {
		features.RiverCardFeaturesAvailable = true
		features.MissedFlushDraw = rank.Category != poker.Flush && rank.Category != poker.StraightFlush && rank.Category != poker.RoyalFlush &&
			hasFlushDraw(req.Hero, req.Board[:4], false)
		turnReq := req
		turnReq.Street = Turn
		turnReq.Board = req.Board[:4]
		turnCards := append(append(make([]poker.Card, 0, len(req.Hero)+4), turnReq.Board...), req.Hero...)
		turnRank, turnErr := poker.EvaluateWithRules(turnCards, rules)
		if turnErr != nil {
			return Features{}, turnErr
		}
		hadStraightDraw, _ := hasStraightDraw(turnReq, turnRank, rules, false, minRank)
		features.MissedStraightDraw = rank.Category != poker.Straight && rank.Category != poker.StraightFlush && rank.Category != poker.RoyalFlush && hadStraightDraw
	}
	hasDraw := features.FlushDraw || features.StraightDraw
	switch {
	case rank.Category >= poker.TwoPair:
		features.Class = ClassMadeStrong
	case features.PairFromBoardOnly && hasDraw:
		features.Class = ClassDraw
	case features.PairFromBoardOnly:
		features.Class = ClassAir
	case rank.Category == poker.OnePair && hasDraw:
		features.Class = ClassMadeDraw
	case rank.Category == poker.OnePair:
		features.Class = ClassMade
	case hasDraw:
		features.Class = ClassDraw
	default:
		features.Class = ClassAir
	}
	return features, nil
}

func boardHasPair(board []poker.Card) bool {
	seen := make(map[int]bool, len(board))
	for _, card := range board {
		if seen[card.Rank()] {
			return true
		}
		seen[card.Rank()] = true
	}
	return false
}

func onePairBelowTopBoard(hero, board []poker.Card) bool {
	topBoardRank := -1
	for _, card := range board {
		if card.Rank() > topBoardRank {
			topBoardRank = card.Rank()
		}
	}
	for _, hole := range hero {
		for _, community := range board {
			if hole.Rank() == community.Rank() && hole.Rank() < topBoardRank {
				return true
			}
		}
	}
	return false
}

func hasFlushDraw(hero, board []poker.Card, omaha bool) bool {
	if len(board) >= 5 {
		return false
	}
	var heroSuits, boardSuits [4]int
	for _, card := range hero {
		heroSuits[card.Suit()]++
	}
	for _, card := range board {
		boardSuits[card.Suit()]++
	}
	for suit := 0; suit < 4; suit++ {
		if omaha {
			if heroSuits[suit] >= 2 && boardSuits[suit] == 2 {
				return true
			}
		} else if heroSuits[suit] > 0 && heroSuits[suit]+boardSuits[suit] == 4 {
			return true
		}
	}
	return false
}

func hasStraightDraw(req Request, current poker.HandRank, rules poker.Rules, omaha bool, minRank int) (bool, int) {
	if len(req.Board) >= 5 || current.Category == poker.Straight || current.Category == poker.StraightFlush || current.Category == poker.RoyalFlush {
		return false, 0
	}
	seen := uint64(0)
	for _, group := range [][]poker.Card{req.Hero, req.Board, req.Dead} {
		for _, card := range group {
			seen |= uint64(1) << card
		}
	}
	outs := 0
	for _, card := range poker.FullDeck() {
		if card.Rank() < minRank || seen&(uint64(1)<<card) != 0 {
			continue
		}
		board := append(append([]poker.Card(nil), req.Board...), card)
		var future poker.HandRank
		if omaha {
			future = poker.EvaluateOmahaUnchecked(req.Hero, board, rules)
		} else {
			cards := append(append(make([]poker.Card, 0, len(req.Hero)+len(board)), board...), req.Hero...)
			future = poker.EvaluateUncheckedWithRules(cards, rules)
		}
		if future.Category == poker.Straight || future.Category == poker.StraightFlush || future.Category == poker.RoyalFlush {
			outs++
		}
	}
	return outs >= 4, outs
}

func strategyRules(game equity.Game) (poker.Rules, bool, int, error) {
	switch game {
	case "", equity.GameNLH:
		return poker.StandardRules, false, 2, nil
	case equity.GameShortDeck:
		return poker.ShortDeckRules, false, 6, nil
	case equity.GameShortDeckFixed:
		return poker.ShortDeckFixedRules, false, 6, nil
	case equity.GamePLO4, equity.GamePLO5, equity.GamePLO6:
		return poker.StandardRules, true, 2, nil
	default:
		return poker.Rules{}, false, 0, fmt.Errorf("unsupported strategy game %q", game)
	}
}
