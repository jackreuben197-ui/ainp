package service

import (
	"io"
	"log/slog"
	"testing"

	"gitlab.com/smoothsics/ainp/internal/config"
	"gitlab.com/smoothsics/ainp/internal/protocol"
)

func TestGrayAssignmentIsStableForAHand(t *testing.T) {
	cfg := config.Default()
	cfg.Phase5.Gray.Enabled = true
	cfg.Phase5.Gray.Percentage = 50
	provider := NewGrayDecisionProvider(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	handID := "hand-1"
	input := DecisionInput{Event: protocol.EventRequest{PlayerID: "p1", TableID: "t1", HandID: &handID}}
	first := provider.selected(input)
	for i := 0; i < 10; i++ {
		if provider.selected(input) != first {
			t.Fatal("gray assignment changed within a hand")
		}
	}
	cfg.Phase5.Gray.Percentage = 0
	if NewGrayDecisionProvider(cfg, slog.New(slog.NewTextHandler(io.Discard, nil))).selected(input) {
		t.Fatal("zero percent selected")
	}
	cfg.Phase5.Gray.Percentage = 100
	if !NewGrayDecisionProvider(cfg, slog.New(slog.NewTextHandler(io.Discard, nil))).selected(input) {
		t.Fatal("100 percent not selected")
	}
}
