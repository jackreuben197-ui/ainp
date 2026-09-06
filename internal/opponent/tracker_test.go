package opponent

import (
	"fmt"
	"testing"
)

func TestTrackerIdempotencyAndClassification(t *testing.T) {
	tracker := NewTracker()
	for hand := 0; hand < 30; hand++ {
		observation := Observation{
			ObservationID: fmt.Sprintf("%d-pre", hand), PlayerID: "loose", HandID: fmt.Sprintf("h%d", hand),
			Street: "preflop", Action: "call", Voluntary: true, PreflopOpportunity: true, PFROpportunity: true,
		}
		if err := tracker.Observe(observation); err != nil {
			t.Fatal(err)
		}
		if err := tracker.Observe(observation); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := tracker.Snapshot("loose")
	if snapshot.Hands != 30 || snapshot.VPIP < .65 || snapshot.Archetype != "calling_station" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	unknown := tracker.Snapshot("missing")
	if unknown.Archetype != "unknown" || unknown.VPIP != .20 {
		t.Fatalf("unknown=%+v", unknown)
	}
}

func TestTrackerAggressiveProfile(t *testing.T) {
	tracker := NewTracker()
	for hand := 0; hand < 30; hand++ {
		action := "fold"
		voluntary := false
		if hand%3 != 0 {
			action = "raise"
			voluntary = true
		}
		_ = tracker.Observe(Observation{
			ObservationID: fmt.Sprintf("%d", hand), PlayerID: "aggressive", HandID: fmt.Sprintf("h%d", hand),
			Street: "preflop", Action: action, Voluntary: voluntary, PreflopOpportunity: true, PFROpportunity: true,
		})
	}
	snapshot := tracker.Snapshot("aggressive")
	if snapshot.Archetype != "lag" || snapshot.Aggression <= 1.5 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestTrackerEvictsOldPlayersAndBoundsDedupeHistory(t *testing.T) {
	tracker := NewTrackerWithLimits(2, 2)
	for _, player := range []string{"old", "active"} {
		if err := tracker.Observe(Observation{ObservationID: player + "-1", PlayerID: player, HandID: "h1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tracker.Observe(Observation{ObservationID: "new-1", PlayerID: "new", HandID: "h1"}); err != nil {
		t.Fatal(err)
	}
	if snapshot := tracker.Snapshot("old"); snapshot.Hands != 0 || snapshot.Archetype != "unknown" {
		t.Fatalf("old player was not evicted: %+v", snapshot)
	}
	for i := 2; i <= 4; i++ {
		_ = tracker.Observe(Observation{ObservationID: fmt.Sprintf("new-%d", i), PlayerID: "new", HandID: fmt.Sprintf("h%d", i)})
	}
	if stats := tracker.players["new"]; len(stats.seen) != 2 || len(stats.handsSeen) != 2 || stats.hands != 4 {
		t.Fatalf("bounded stats=%+v", stats)
	}
}
