package service

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"gitlab.com/smoothsics/ainp/internal/game"
	"gitlab.com/smoothsics/ainp/internal/protocol"
)

func TestLogHandResultIncludesProfileStreetAndProfit(t *testing.T) {
	var output bytes.Buffer
	service := &EventService{logger: slog.New(slog.NewJSONHandler(&output, nil)), logEvents: true}
	state := &game.State{
		HeroID: "bot", RoomID: "room", TableID: "table", HandID: "hand",
		AIProfile: "FPCH_100_50", Street: game.Turn,
	}
	handID := "hand"
	service.logHandResult(protocol.EventRequest{
		PlayerID: "bot", RoomID: "room", TableID: "table", HandID: &handID,
		Cmd: protocol.CommandEndHand, Payload: []byte(`{"players":[{"player_id":"bot","profit":12.5},{"player_id":"villain","profit":-12.5}]}`),
	}, state)
	text := output.String()
	for _, expected := range []string{`"msg":"hand_result"`, `"ai_profile":"FPCH_100_50"`, `"reached_street":"turn"`, `"profit":12.5`} {
		if !strings.Contains(text, expected) {
			t.Fatalf("missing %s in %s", expected, text)
		}
	}
}
