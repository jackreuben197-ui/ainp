package performance

import (
	"sync"
	"time"
)

type Key struct {
	TableID  string
	PlayerID string
}

type Snapshot struct {
	InitialStack float64
	NetProfit    float64
	Hands        uint64
	Actions      uint64
	Calls        uint64
	Aggressive   uint64
}

func (s Snapshot) CallRate() float64 {
	if s.Actions == 0 {
		return 0
	}
	return float64(s.Calls) / float64(s.Actions)
}

func (s Snapshot) AggressionRate() float64 {
	if s.Actions == 0 {
		return 0
	}
	return float64(s.Aggressive) / float64(s.Actions)
}

type entry struct {
	Snapshot
	updatedAt    time.Time
	lastHandID   string
	lastActionID string
}

// Tracker is an in-process, bounded table/player ledger. Dedupe keys make it
// safe to share between control and shadow engines.
type Tracker struct {
	mu      sync.Mutex
	ttl     time.Duration
	max     int
	now     func() time.Time
	entries map[Key]*entry
}

func NewTracker(ttl time.Duration, maxPlayers int) *Tracker {
	return &Tracker{ttl: ttl, max: maxPlayers, now: time.Now, entries: make(map[Key]*entry)}
}

func (t *Tracker) ObserveStart(key Key, initialStack float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	item := t.getOrCreate(key)
	if item.InitialStack <= 0 && initialStack > 0 {
		item.InitialStack = initialStack
	}
}

func (t *Tracker) RecordResult(key Key, handID string, profit float64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	item := t.getOrCreate(key)
	if item.lastHandID == handID {
		return
	}
	item.lastHandID = handID
	item.NetProfit += profit
	item.Hands++
}

func (t *Tracker) RecordAction(key Key, actionID, action string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	item := t.getOrCreate(key)
	if item.lastActionID == actionID {
		return
	}
	item.lastActionID = actionID
	item.Actions++
	switch action {
	case "call":
		item.Calls++
	case "bet", "raise", "allin":
		item.Aggressive++
	}
}

func (t *Tracker) Snapshot(key Key) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune()
	if item := t.entries[key]; item != nil {
		item.updatedAt = t.now()
		return item.Snapshot
	}
	return Snapshot{}
}

func (t *Tracker) getOrCreate(key Key) *entry {
	t.prune()
	if item := t.entries[key]; item != nil {
		item.updatedAt = t.now()
		return item
	}
	if t.max > 0 && len(t.entries) >= t.max {
		var oldestKey Key
		var oldest time.Time
		for candidate, item := range t.entries {
			if oldest.IsZero() || item.updatedAt.Before(oldest) {
				oldestKey, oldest = candidate, item.updatedAt
			}
		}
		delete(t.entries, oldestKey)
	}
	item := &entry{updatedAt: t.now()}
	t.entries[key] = item
	return item
}

func (t *Tracker) prune() {
	if t.ttl <= 0 {
		return
	}
	now := t.now()
	for key, item := range t.entries {
		if now.Sub(item.updatedAt) > t.ttl {
			delete(t.entries, key)
		}
	}
}
