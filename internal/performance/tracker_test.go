package performance

import (
	"testing"
	"time"
)

func TestTrackerAccumulatesPerTableAndDeduplicatesShadowObservation(t *testing.T) {
	tracker := NewTracker(time.Hour, 10)
	key := Key{TableID: "table", PlayerID: "bot"}
	tracker.ObserveStart(key, 100)
	tracker.ObserveStart(key, 140)
	tracker.RecordAction(key, "action-1", "call")
	tracker.RecordAction(key, "action-1", "call")
	tracker.RecordAction(key, "action-2", "raise")
	tracker.RecordResult(key, "hand-1", -20)
	tracker.RecordResult(key, "hand-1", -20)
	tracker.RecordResult(key, "hand-2", 5)

	got := tracker.Snapshot(key)
	if got.InitialStack != 100 || got.NetProfit != -15 || got.Hands != 2 || got.Actions != 2 || got.Calls != 1 || got.Aggressive != 1 {
		t.Fatalf("snapshot=%+v", got)
	}
	if got.CallRate() != .5 || got.AggressionRate() != .5 {
		t.Fatalf("rates call=%v aggression=%v", got.CallRate(), got.AggressionRate())
	}
	if other := tracker.Snapshot(Key{TableID: "other", PlayerID: "bot"}); other != (Snapshot{}) {
		t.Fatalf("other table leaked snapshot=%+v", other)
	}
}
