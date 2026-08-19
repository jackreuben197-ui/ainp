package strategy

import (
	"errors"
	"math"
)

var ErrNoLegalActions = errors.New("no legal actions available")

const chipEpsilon = 1e-9

type actionGuard struct {
	actions map[Action]LegalAction
	req     Request
}

func newActionGuard(req Request) (*actionGuard, error) {
	if len(req.LegalActions) == 0 {
		return nil, ErrNoLegalActions
	}
	actions := make(map[Action]LegalAction, len(req.LegalActions))
	for _, action := range req.LegalActions {
		if action.Min < 0 || action.Max < 0 || (action.Max > 0 && action.Min > action.Max) {
			return nil, ErrNoLegalActions
		}
		actions[action.Action] = action
	}
	return &actionGuard{actions: actions, req: req}, nil
}

func (g *actionGuard) finalize(desired Action, amount float64, aggressive bool) (Action, float64, error) {
	// Folding when checking is available is never strategically useful. Protect
	// this invariant before accepting an otherwise legal Fold; upstream chip
	// arithmetic can leave a tiny positive ToCall residue such as 5.55e-17.
	if desired == Fold && g.req.ToCall <= chipEpsilon {
		if legal, ok := g.actions[Check]; ok {
			return Check, g.amount(legal, 0), nil
		}
	}
	if desired == Fold && g.req.NeverFold {
		for _, action := range []Action{Call, Check, AllIn} {
			if legal, ok := g.actions[action]; ok {
				return action, g.amount(legal, g.req.ToCall), nil
			}
		}
		return "", 0, ErrNoLegalActions
	}
	if legal, ok := g.actions[desired]; ok {
		amount = g.amount(legal, amount)
		if g.shouldCollapseNearAllIn(desired, amount) {
			allIn := g.actions[AllIn]
			return AllIn, g.amount(allIn, g.req.Stack), nil
		}
		return desired, amount, nil
	}
	fallbacks := []Action{Call, Check, Fold}
	if aggressive {
		switch desired {
		case AllIn:
			fallbacks = []Action{Raise, Call, Check, Fold}
		case Raise:
			fallbacks = []Action{Call, AllIn, Check, Fold}
		case Bet:
			fallbacks = []Action{Check, Fold}
			if g.shortAllIn(amount) {
				fallbacks = []Action{AllIn, Check, Fold}
			}
		}
	} else if desired == Call {
		// A call equal to the remaining stack is commonly exposed as all-in only.
		// Preserve the strategy intent instead of turning a continue into a fold.
		fallbacks = []Action{AllIn, Check, Fold}
	}
	for _, action := range fallbacks {
		if action == Fold && g.req.NeverFold {
			continue
		}
		if legal, ok := g.actions[action]; ok {
			return action, g.amount(legal, amount), nil
		}
	}
	return "", 0, ErrNoLegalActions
}

// shouldCollapseNearAllIn prevents a human-looking raise or bet from leaving
// only an unusable chip fragment behind. It is deliberately narrow: the
// feature must be enabled, the server must explicitly expose AllIn as legal,
// and the residual stack must be positive and within the configured threshold.
func (g *actionGuard) shouldCollapseNearAllIn(action Action, amount float64) bool {
	if !g.req.CollapseNearAllIn || (action != Bet && action != Raise) {
		return false
	}
	if _, ok := g.actions[AllIn]; !ok || g.req.Stack <= 0 || amount <= 0 || amount >= g.req.Stack {
		return false
	}
	threshold := g.req.NearAllInRemainingChips
	return threshold > 0 && g.req.Stack-amount <= threshold+1e-9
}

func (g *actionGuard) shortAllIn(desired float64) bool {
	legal, ok := g.actions[AllIn]
	if !ok {
		return false
	}
	return g.amount(legal, desired) <= desired+1e-9
}

func (g *actionGuard) amount(legal LegalAction, desired float64) float64 {
	switch legal.Action {
	case Fold, Check:
		return 0
	case Call:
		if legal.Min > 0 {
			return legal.Min
		}
		return math.Min(g.req.ToCall, g.req.Stack)
	case AllIn:
		if legal.Max > 0 {
			return legal.Max
		}
		return g.req.Stack
	default:
		if desired < legal.Min {
			desired = legal.Min
		}
		if legal.Max > 0 && desired > legal.Max {
			desired = legal.Max
		}
		if g.req.BigBlind > 0 {
			desired = math.Ceil(desired/g.req.BigBlind) * g.req.BigBlind
			if legal.Max > 0 && desired > legal.Max {
				desired = legal.Max
			}
		}
		return desired
	}
}
