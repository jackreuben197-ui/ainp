package equity

import (
	"context"
	"math/rand"

	"gitlab.com/ubenbill/ainp/internal/poker"
)

func enumerateExact(ctx context.Context, req preparedRequest, result *Result) error {
	used := make([]bool, len(req.deck))
	runout := make([]poker.Card, req.boardNeeded)
	var visitBoard func(start, depth int) error
	visitBoard = func(start, depth int) error {
		if depth == req.boardNeeded {
			if result.Trials%256 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			recordOutcome(req, runout, result)
			return nil
		}
		remainingNeeded := req.boardNeeded - depth
		for i := start; i < len(req.deck); i++ {
			if used[i] {
				continue
			}
			availableAfter := 0
			for j := i; j < len(req.deck); j++ {
				if !used[j] {
					availableAfter++
				}
			}
			if availableAfter < remainingNeeded {
				break
			}
			used[i] = true
			runout[depth] = req.deck[i]
			if err := visitBoard(i+1, depth+1); err != nil {
				used[i] = false
				return err
			}
			used[i] = false
		}
		return nil
	}

	var visitOpponent func(depth int) error
	visitOpponent = func(depth int) error {
		if depth == len(req.unknownOpponents) {
			return visitBoard(0, 0)
		}
		seat := req.unknownOpponents[depth]
		var chooseHole func(start, cardIndex int) error
		chooseHole = func(start, cardIndex int) error {
			if cardIndex == req.holeCards {
				return visitOpponent(depth + 1)
			}
			needed := req.holeCards - cardIndex
			for i := start; i < len(req.deck); i++ {
				if used[i] {
					continue
				}
				available := 0
				for j := i; j < len(req.deck); j++ {
					if !used[j] {
						available++
					}
				}
				if available < needed {
					break
				}
				used[i] = true
				req.opponents[seat][cardIndex] = req.deck[i]
				if err := chooseHole(i+1, cardIndex+1); err != nil {
					used[i] = false
					return err
				}
				used[i] = false
			}
			return nil
		}
		return chooseHole(0, 0)
	}
	return visitOpponent(0)
}

func simulate(ctx context.Context, req preparedRequest, samples int, seed int64, result *Result) error {
	rng := rand.New(rand.NewSource(seed))
	deck := append([]poker.Card(nil), req.deck...)
	runout := make([]poker.Card, req.boardNeeded)
	cardsNeeded := len(req.unknownOpponents)*req.holeCards + req.boardNeeded
	for sample := 0; sample < samples; sample++ {
		if sample%256 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		for i := 0; i < cardsNeeded; i++ {
			j := i + rng.Intn(len(deck)-i)
			deck[i], deck[j] = deck[j], deck[i]
		}
		offset := 0
		for _, seat := range req.unknownOpponents {
			copy(req.opponents[seat], deck[offset:offset+req.holeCards])
			offset += req.holeCards
		}
		copy(runout, deck[offset:offset+req.boardNeeded])
		recordOutcome(req, runout, result)
	}
	return nil
}

func recordOutcome(req preparedRequest, runout []poker.Card, result *Result) {
	var community [5]poker.Card
	communityLength := copy(community[:], req.board)
	communityLength += copy(community[communityLength:], runout)
	var heroRank poker.HandRank
	if req.omaha {
		heroRank = poker.EvaluateOmahaUnchecked(req.hero, community[:communityLength], req.rules)
	} else {
		var heroCards [7]poker.Card
		copy(heroCards[:], community[:communityLength])
		copy(heroCards[communityLength:], req.hero)
		heroRank = poker.EvaluateUncheckedWithRules(heroCards[:communityLength+len(req.hero)], req.rules)
	}

	tiedPlayers := 1
	lost := false
	for _, hole := range req.opponents {
		var rank poker.HandRank
		if req.omaha {
			rank = poker.EvaluateOmahaUnchecked(hole, community[:communityLength], req.rules)
		} else {
			var cards [7]poker.Card
			copy(cards[:], community[:communityLength])
			copy(cards[communityLength:], hole)
			rank = poker.EvaluateUncheckedWithRules(cards[:communityLength+len(hole)], req.rules)
		}
		if rank.Score > heroRank.Score {
			lost = true
			break
		}
		if rank.Score == heroRank.Score {
			tiedPlayers++
		}
	}

	result.Trials++
	if lost {
		result.Losses++
		return
	}
	if tiedPlayers > 1 {
		result.Ties++
		share := 1 / float64(tiedPlayers)
		result.Equity += share
		result.equitySquared += share * share
		return
	}
	result.Wins++
	result.Equity++
	result.equitySquared++
}
