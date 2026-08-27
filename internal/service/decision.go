package service

import (
	"context"

	"gitlab.com/ubenbill/ainp/internal/config"
	"gitlab.com/ubenbill/ainp/internal/game"
	"gitlab.com/ubenbill/ainp/internal/protocol"
)

type DecisionInput struct {
	Event      protocol.EventRequest
	State      *game.State
	RequestID  string
	DecisionID string
}

type DecisionProvider interface {
	Name() string
	Decide(context.Context, DecisionInput) (*protocol.AdviseResponse, error)
}

type MockDecisionProvider struct {
	enabled  bool
	action   protocol.ActionType
	value    float64
	adviseOn map[protocol.Command]struct{}
}

func NewMockDecisionProvider(cfg config.MockConfig) *MockDecisionProvider {
	commands := make(map[protocol.Command]struct{}, len(cfg.AdviseOn))
	for _, cmd := range cfg.AdviseOn {
		commands[protocol.Command(cmd)] = struct{}{}
	}
	return &MockDecisionProvider{enabled: cfg.Enabled, action: protocol.ActionType(cfg.Action), value: cfg.Value, adviseOn: commands}
}

func (p *MockDecisionProvider) Name() string { return "mock" }

func (p *MockDecisionProvider) Decide(_ context.Context, input DecisionInput) (*protocol.AdviseResponse, error) {
	if !p.enabled {
		return nil, nil
	}
	if _, ok := p.adviseOn[input.Event.Cmd]; !ok {
		return nil, nil
	}
	advise := &protocol.AdviseResponse{Type: p.action}
	if p.action == protocol.ActionBet || p.action == protocol.ActionRaise || p.action == protocol.ActionCall || p.action == protocol.ActionAllIn {
		value := p.value
		advise.Value = &value
	}
	return advise, nil
}
