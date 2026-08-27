package store

import (
	"errors"
	"sync"
	"time"

	"gitlab.com/smoothsics/ainp/internal/game"
	"gitlab.com/smoothsics/ainp/internal/protocol"
)

type ApplyResult int

const (
	Applied ApplyResult = iota
	AlreadyApplied
	WrongSequence
	HandNotStarted
)

type HandKey struct{ PlayerID, RoomID, TableID, HandID string }

type HandState struct {
	LastSequence    uint64
	LastCommand     protocol.Command
	LastFingerprint string
	UpdatedAt       time.Time
	Game            *game.State
}

func (s *MemoryHandStore) ApplyEvent(key HandKey, req protocol.EventRequest, fingerprint string, now time.Time) (ApplyResult, *game.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	state, exists := s.hands[key]
	if !exists {
		if req.Cmd != protocol.CommandStartHand {
			return HandNotStarted, nil, nil
		}
		if req.SeqNum != 1 {
			return WrongSequence, nil, nil
		}
		gameState, err := game.New(req)
		if err != nil {
			return Applied, nil, err
		}
		s.ensureCapacity()
		s.hands[key] = HandState{LastSequence: req.SeqNum, LastCommand: req.Cmd, LastFingerprint: fingerprint, UpdatedAt: now, Game: gameState}
		return Applied, gameState.Clone(), nil
	}
	if req.SeqNum == state.LastSequence {
		if fingerprint == state.LastFingerprint {
			return AlreadyApplied, state.Game.Clone(), nil
		}
		return WrongSequence, nil, nil
	}
	if req.SeqNum != state.LastSequence+1 {
		return WrongSequence, nil, nil
	}
	if state.Game == nil {
		return Applied, nil, errors.New("hand has no normalized game state")
	}
	next := state.Game.Clone()
	if err := next.Apply(req); err != nil {
		return Applied, nil, err
	}
	state.LastSequence, state.LastCommand, state.LastFingerprint, state.UpdatedAt, state.Game = req.SeqNum, req.Cmd, fingerprint, now, next
	s.hands[key] = state
	return Applied, next.Clone(), nil
}

func (s *MemoryHandStore) SetAdvice(key HandKey, sequence uint64, advice *protocol.AdviseResponse) {
	if advice == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, exists := s.hands[key]
	if !exists || state.Game == nil || state.LastSequence != sequence {
		return
	}
	copyAdvice := *advice
	if advice.Value != nil {
		value := *advice.Value
		copyAdvice.Value = &value
	}
	state.Game.PendingAdvice = &copyAdvice
	s.hands[key] = state
}

type MemoryHandStore struct {
	mu            sync.Mutex
	ttl           time.Duration
	maxHands      int
	pruneInterval time.Duration
	lastPrune     time.Time
	hands         map[HandKey]HandState
}

func NewMemoryHandStore(ttl time.Duration) *MemoryHandStore {
	return NewMemoryHandStoreWithLimit(ttl, 50_000, time.Minute)
}

func NewMemoryHandStoreWithLimit(ttl time.Duration, maxHands int, pruneInterval time.Duration) *MemoryHandStore {
	return &MemoryHandStore{ttl: ttl, maxHands: maxHands, pruneInterval: pruneInterval, hands: make(map[HandKey]HandState)}
}

func (s *MemoryHandStore) Apply(key HandKey, sequence uint64, command protocol.Command, fingerprint string, now time.Time) ApplyResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	state, exists := s.hands[key]
	if !exists {
		if command != protocol.CommandStartHand {
			return HandNotStarted
		}
		if sequence != 1 {
			return WrongSequence
		}
		s.ensureCapacity()
		s.hands[key] = HandState{LastSequence: sequence, LastCommand: command, LastFingerprint: fingerprint, UpdatedAt: now}
		return Applied
	}
	if sequence == state.LastSequence {
		if fingerprint == state.LastFingerprint {
			return AlreadyApplied
		}
		return WrongSequence
	}
	if sequence != state.LastSequence+1 {
		return WrongSequence
	}
	state.LastSequence = sequence
	state.LastCommand = command
	state.LastFingerprint = fingerprint
	state.UpdatedAt = now
	s.hands[key] = state
	return Applied
}

func (s *MemoryHandStore) prune(now time.Time) {
	if !s.lastPrune.IsZero() && now.Sub(s.lastPrune) < s.pruneInterval {
		return
	}
	s.lastPrune = now
	for key, state := range s.hands {
		if now.Sub(state.UpdatedAt) > s.ttl {
			delete(s.hands, key)
		}
	}
}

func (s *MemoryHandStore) ensureCapacity() {
	if s.maxHands < 1 || len(s.hands) < s.maxHands {
		return
	}
	var oldestKey HandKey
	var oldest time.Time
	for key, state := range s.hands {
		if oldest.IsZero() || state.UpdatedAt.Before(oldest) {
			oldestKey, oldest = key, state.UpdatedAt
		}
	}
	delete(s.hands, oldestKey)
}
