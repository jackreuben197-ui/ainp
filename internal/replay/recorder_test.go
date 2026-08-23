package replay

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
	"time"

	"gitlab.com/smoothsics/ainp/internal/protocol"
)

func TestJSONLRecorderPersistsReplayableRecord(t *testing.T) {
	directory := t.TempDir()
	recorder, err := NewJSONLRecorder(directory, "events", true, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	handID := "h1"
	record := Record{Outcome: "applied", Event: protocol.EventRequest{SeqNum: 1, PlayerID: "p1", RoomID: "r", TableID: "t", HandID: &handID, Cmd: protocol.CommandStartHand, Payload: []byte(`{"game_type":"NLH"}`)}, Response: protocol.EventResponse{SeqNum: 1}}
	if err := recorder.Record(record); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(recorder.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("missing record")
	}
	var decoded Record
	if err := json.Unmarshal(scanner.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != SchemaVersion || decoded.Event.PlayerID != "p1" || decoded.Outcome != "applied" {
		t.Fatalf("decoded=%+v", decoded)
	}
}
