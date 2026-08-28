package opponent

import (
	"errors"
	"sync"
	"time"
)

var ErrInvalidObservation = errors.New("observation_id and player_id are required")

type Observation struct {
	ObservationID       string
	PlayerID            string
	HandID              string
	Street              string
	Action              string
	Voluntary           bool
	PreflopOpportunity  bool
	PFROpportunity      bool
	ThreeBetOpportunity bool
	CBetOpportunity     bool
	FacingCBet          bool
}

type Snapshot struct {
	PlayerID   string
	Hands      uint64
	VPIP       float64
	PFR        float64
	ThreeBet   float64
	Aggression float64
	CBet       float64
	FoldToCBet float64
	Archetype  string
}

type counters struct {
	updatedAt              time.Time
	hands                  uint64
	vpipOpp, vpip          uint64
	pfrOpp, pfr            uint64
	threeBetOpp            uint64
	threeBet               uint64
	aggressive, calls      uint64
	cBetOpp, cBet          uint64
	facingCBet, foldToCBet uint64
	seen                   map[string]struct{}
	seenOrder              []string
	handsSeen              map[string]struct{}
	handOrder              []string
}

type Tracker struct {
	mu               sync.RWMutex
	players          map[string]*counters
	maxPlayers       int
	maxSeenPerPlayer int
}

func NewTracker() *Tracker {
	return NewTrackerWithLimits(10_000, 256)
}

func NewTrackerWithLimits(maxPlayers, maxSeenPerPlayer int) *Tracker {
	if maxPlayers < 1 {
		maxPlayers = 1
	}
	if maxSeenPerPlayer < 1 {
		maxSeenPerPlayer = 1
	}
	return &Tracker{players: make(map[string]*counters), maxPlayers: maxPlayers, maxSeenPerPlayer: maxSeenPerPlayer}
}

func (t *Tracker) Observe(observation Observation) error {
	if observation.ObservationID == "" || observation.PlayerID == "" {
		return ErrInvalidObservation
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	stats := t.players[observation.PlayerID]
	if stats == nil {
		if len(t.players) >= t.maxPlayers {
			t.evictOldest()
		}
		stats = &counters{seen: make(map[string]struct{}), handsSeen: make(map[string]struct{})}
		t.players[observation.PlayerID] = stats
	}
	if _, duplicate := stats.seen[observation.ObservationID]; duplicate {
		return nil
	}
	stats.updatedAt = time.Now()
	stats.seen[observation.ObservationID] = struct{}{}
	stats.seenOrder = append(stats.seenOrder, observation.ObservationID)
	if len(stats.seenOrder) > t.maxSeenPerPlayer {
		delete(stats.seen, stats.seenOrder[0])
		stats.seenOrder = stats.seenOrder[1:]
	}
	if observation.HandID != "" {
		if _, exists := stats.handsSeen[observation.HandID]; !exists {
			stats.handsSeen[observation.HandID] = struct{}{}
			stats.handOrder = append(stats.handOrder, observation.HandID)
			if len(stats.handOrder) > t.maxSeenPerPlayer {
				delete(stats.handsSeen, stats.handOrder[0])
				stats.handOrder = stats.handOrder[1:]
			}
			stats.hands++
		}
	}

	action := observation.Action
	aggressive := action == "bet" || action == "raise" || action == "allin"
	voluntaryMoney := observation.Voluntary && (aggressive || action == "call")
	if observation.PreflopOpportunity {
		stats.vpipOpp++
		if voluntaryMoney {
			stats.vpip++
		}
	}
	if observation.PFROpportunity {
		stats.pfrOpp++
		if aggressive {
			stats.pfr++
		}
	}
	if observation.ThreeBetOpportunity {
		stats.threeBetOpp++
		if aggressive {
			stats.threeBet++
		}
	}
	if aggressive {
		stats.aggressive++
	} else if action == "call" {
		stats.calls++
	}
	if observation.CBetOpportunity {
		stats.cBetOpp++
		if aggressive {
			stats.cBet++
		}
	}
	if observation.FacingCBet {
		stats.facingCBet++
		if action == "fold" {
			stats.foldToCBet++
		}
	}
	return nil
}

func (t *Tracker) evictOldest() {
	var oldestID string
	var oldest time.Time
	for playerID, stats := range t.players {
		if oldestID == "" || stats.updatedAt.Before(oldest) {
			oldestID, oldest = playerID, stats.updatedAt
		}
	}
	if oldestID != "" {
		delete(t.players, oldestID)
	}
}

func (t *Tracker) Snapshot(playerID string) Snapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	stats := t.players[playerID]
	if stats == nil {
		return defaultSnapshot(playerID)
	}
	snapshot := Snapshot{
		PlayerID:   playerID,
		Hands:      stats.hands,
		VPIP:       smoothed(stats.vpip, stats.vpipOpp, 4, 20),
		PFR:        smoothed(stats.pfr, stats.pfrOpp, 2, 20),
		ThreeBet:   smoothed(stats.threeBet, stats.threeBetOpp, 1, 25),
		Aggression: float64(stats.aggressive+1) / float64(stats.calls+1),
		CBet:       smoothed(stats.cBet, stats.cBetOpp, 2, 4),
		FoldToCBet: smoothed(stats.foldToCBet, stats.facingCBet, 2, 4),
	}
	snapshot.Archetype = classify(snapshot)
	return snapshot
}

func defaultSnapshot(playerID string) Snapshot {
	return Snapshot{PlayerID: playerID, VPIP: .20, PFR: .10, ThreeBet: .04, Aggression: 1, CBet: .5, FoldToCBet: .5, Archetype: "unknown"}
}

func smoothed(success, opportunities uint64, priorSuccess, priorTotal float64) float64 {
	return (float64(success) + priorSuccess) / (float64(opportunities) + priorTotal)
}

func classify(snapshot Snapshot) string {
	if snapshot.Hands < 20 {
		return "unknown"
	}
	switch {
	case snapshot.VPIP < .18:
		return "nit"
	case snapshot.VPIP > .32 && snapshot.Aggression < 1:
		return "calling_station"
	case snapshot.VPIP > .30 && snapshot.Aggression >= 1.5:
		return "lag"
	case snapshot.PFR >= .16 && snapshot.Aggression >= 1.3:
		return "tag"
	case snapshot.VPIP > .30:
		return "loose_passive"
	default:
		return "balanced"
	}
}
