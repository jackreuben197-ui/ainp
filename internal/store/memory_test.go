package store

import (
	"testing"
	"time"

	"gitlab.com/smoothsics/ainp/internal/protocol"
)

func TestMemoryHandStoreSequenceAndIdempotency(t *testing.T) {
	state := NewMemoryHandStore(time.Hour)
	key := HandKey{PlayerID: "bot", RoomID: "fishcn", TableID: "1", HandID: "2"}
	now := time.Now()

	if got := state.Apply(key, 1, protocol.CommandStartHand, "one", now); got != Applied {
		t.Fatalf("first event = %v", got)
	}
	if got := state.Apply(key, 1, protocol.CommandStartHand, "one", now); got != AlreadyApplied {
		t.Fatalf("duplicate event = %v", got)
	}
	if got := state.Apply(key, 1, protocol.CommandStartHand, "changed", now); got != WrongSequence {
		t.Fatalf("changed duplicate = %v", got)
	}
	if got := state.Apply(key, 3, protocol.CommandDealCards, "three", now); got != WrongSequence {
		t.Fatalf("skipped sequence = %v", got)
	}
	if got := state.Apply(key, 2, protocol.CommandDealCards, "two", now); got != Applied {
		t.Fatalf("next event = %v", got)
	}
}

func TestMemoryHandStoreRequiresStart(t *testing.T) {
	state := NewMemoryHandStore(time.Hour)
	key := HandKey{PlayerID: "bot", RoomID: "fishcn", TableID: "1", HandID: "2"}
	if got := state.Apply(key, 1, protocol.CommandDealCards, "one", time.Now()); got != HandNotStarted {
		t.Fatalf("result = %v", got)
	}
}

func TestMemoryHandStoreCapacityEvictsOldest(t *testing.T) {
	state := NewMemoryHandStoreWithLimit(time.Hour, 2, time.Minute)
	now := time.Now()
	for index, handID := range []string{"one", "two", "three"} {
		key := HandKey{PlayerID: "bot", RoomID: "fishcn", TableID: "1", HandID: handID}
		if got := state.Apply(key, 1, protocol.CommandStartHand, handID, now.Add(time.Duration(index)*time.Second)); got != Applied {
			t.Fatalf("start %s=%v", handID, got)
		}
	}
	oldest := HandKey{PlayerID: "bot", RoomID: "fishcn", TableID: "1", HandID: "one"}
	if got := state.Apply(oldest, 2, protocol.CommandDealCards, "next", now.Add(4*time.Second)); got != HandNotStarted {
		t.Fatalf("oldest hand was not evicted: %v", got)
	}
}
