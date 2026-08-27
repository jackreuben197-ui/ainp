package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitlab.com/ubenbill/ainp/internal/config"
	"gitlab.com/ubenbill/ainp/internal/protocol"
	"gitlab.com/ubenbill/ainp/internal/replay"
	"gitlab.com/ubenbill/ainp/internal/service"
	"gitlab.com/ubenbill/ainp/internal/store"
)

func TestCollectResultSavesHeroProfit(t *testing.T) {
	report := report{ProfitByPlayer: map[string]float64{}}
	record := replay.Record{Outcome: "applied", Event: protocol.EventRequest{
		PlayerID: "bot", Cmd: protocol.CommandEndHand,
		Payload: []byte(`{"players":[{"player_id":"bot","profit":25},{"player_id":"human","profit":-25}]}`),
	}}
	collectResult(&report, record)
	if report.CompletedHands != 1 || report.WinningHands != 1 || report.HeroNetProfit != 25 || report.ProfitByPlayer["bot"] != 25 {
		t.Fatalf("report=%+v", report)
	}
}

func TestStateOnlyReplayRenumbersEventsAfterHistoricallyRejectedInput(t *testing.T) {
	handID := "hand"
	records := []replay.Record{
		{Fingerprint: "start", Outcome: "applied", Event: protocol.EventRequest{SeqNum: 1, PlayerID: "bot", RoomID: "fishcn", TableID: "table", HandID: &handID, Cmd: protocol.CommandStartHand, Payload: []byte(`{"game_type":"NLH","club_id":"1","ai_profile":"FPCH_default","time":1,"small_blind":1,"big_blind":2,"time_to_act":12000,"max_seat":2,"players":[{"player_id":"bot","nick":"bot","stack":100},{"player_id":"villain","nick":"villain","stack":100}],"blinds":[]}`)}},
		// The old build rejected this valid zero-value fold, so the historical
		// adapter reused seq_num=2 for the next distinct event.
		{Fingerprint: "fold", Outcome: "rejected", Event: protocol.EventRequest{SeqNum: 2, PlayerID: "bot", RoomID: "fishcn", TableID: "table", HandID: &handID, Cmd: protocol.CommandAction, Payload: []byte(`{"player_id":"villain","type":"fold","value":0,"value_mode":"street_total","stack_after":100,"next_player_id":"bot"}`)}},
		{Fingerprint: "deal", Outcome: "applied", Event: protocol.EventRequest{SeqNum: 2, PlayerID: "bot", RoomID: "fishcn", TableID: "table", HandID: &handID, Cmd: protocol.CommandDealCards, Payload: []byte(`{"cards":"AsKd","next_player_id":"bot"}`)}},
	}
	path := filepath.Join(t.TempDir(), "replay.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(file)
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Mock.Enabled = false
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	events := service.NewEventService(store.NewMemoryHandStoreWithLimit(time.Hour, 100, time.Minute), service.NewMockDecisionProvider(cfg.Mock), logger, false)
	result, err := run(path, "state-only", events, .001, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.NormalizedSequences || result.CandidateOutcomes["applied"] != 3 || result.ResolvedRejections != 1 || result.NewRejections != 0 || len(result.CandidateErrors) != 0 {
		t.Fatalf("result=%+v", result)
	}
}

func TestReplaySkipsOnlyTruncatedFinalRecord(t *testing.T) {
	handID := "hand"
	record := replay.Record{Fingerprint: "start", Outcome: "applied", Event: protocol.EventRequest{
		SeqNum: 1, PlayerID: "bot", RoomID: "fishcn", TableID: "table", HandID: &handID,
		Cmd: protocol.CommandStartHand, Payload: []byte(`{"game_type":"NLH","club_id":"1","ai_profile":"FPCH_default","time":1,"small_blind":1,"big_blind":2,"time_to_act":12000,"max_seat":2,"players":[{"player_id":"bot","nick":"bot","stack":100},{"player_id":"villain","nick":"villain","stack":100}],"blinds":[]}`),
	}}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "replay.jsonl")
	if err := os.WriteFile(path, append(append(data, '\n'), []byte(`{"schema_version":1,"event":`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	events := service.NewEventService(store.NewMemoryHandStoreWithLimit(time.Hour, 100, time.Minute), service.NewMockDecisionProvider(cfg.Mock), logger, false)
	result, err := run(path, "state-only", events, .001, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Records != 1 || result.TruncatedTailRecords != 1 || result.CandidateOutcomes["applied"] != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestReplayRejectsMalformedMiddleRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replay.jsonl")
	if err := os.WriteFile(path, []byte("{\"broken\":\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	events := service.NewEventService(store.NewMemoryHandStoreWithLimit(time.Hour, 100, time.Minute), service.NewMockDecisionProvider(cfg.Mock), logger, false)
	if _, err := run(path, "state-only", events, .001, false); err == nil {
		t.Fatal("expected malformed middle record error")
	}
}
