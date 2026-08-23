package service

import (
	"context"
	"hash/fnv"
	"log/slog"
	"math"

	"gitlab.com/smoothsics/ainp/internal/config"
	"gitlab.com/smoothsics/ainp/internal/opponent"
	"gitlab.com/smoothsics/ainp/internal/protocol"
)

type GrayDecisionProvider struct {
	control   *EngineDecisionProvider
	candidate *EngineDecisionProvider
	cfg       config.GrayConfig
	logger    *slog.Logger
}

func NewGrayDecisionProvider(cfg config.Config, logger *slog.Logger) *GrayDecisionProvider {
	tracker := opponent.NewTrackerWithLimits(cfg.Engine.OpponentModel.MaxPlayers, cfg.Engine.OpponentModel.DedupeWindow)
	control := newEngineDecisionProvider(cfg, cfg.Engine, tracker, logger)
	candidateCfg := cfg.GrayCandidateEngine()
	if cfg.Phase5.Gray.Mode == "shadow" {
		candidateCfg.Personality.ApplyThinkTime = false
	}
	candidate := newEngineDecisionProvider(cfg, candidateCfg, tracker, logger)
	return &GrayDecisionProvider{control: control, candidate: candidate, cfg: cfg.Phase5.Gray, logger: logger}
}

func (p *GrayDecisionProvider) Name() string { return "gray" }
func (p *GrayDecisionProvider) PolicyVersion() string {
	return p.control.PolicyVersion() + "|" + p.candidate.PolicyVersion()
}

func (p *GrayDecisionProvider) Decide(ctx context.Context, input DecisionInput) (*protocol.AdviseResponse, error) {
	selected := p.selected(input)
	if p.cfg.Mode == "canary" && selected {
		advice, err := p.candidate.Decide(ctx, input)
		p.logSelection(input, "candidate", nil, advice, err)
		return advice, err
	}
	control, err := p.control.Decide(ctx, input)
	if err != nil || p.cfg.Mode != "shadow" || !selected {
		p.logSelection(input, "control", control, nil, err)
		return control, err
	}
	candidate, candidateErr := p.candidate.Decide(ctx, input)
	p.logSelection(input, "shadow", control, candidate, candidateErr)
	return control, nil
}

func (p *GrayDecisionProvider) selected(input DecisionInput) bool {
	if p.cfg.Percentage <= 0 {
		return false
	}
	if p.cfg.Percentage >= 100 {
		return true
	}
	handID := ""
	if input.Event.HandID != nil {
		handID = *input.Event.HandID
	}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(p.cfg.Salt + "|" + input.Event.PlayerID + "|" + input.Event.TableID + "|" + handID))
	return int(hash.Sum64()%100) < p.cfg.Percentage
}

func (p *GrayDecisionProvider) logSelection(input DecisionInput, route string, control, candidate *protocol.AdviseResponse, err error) {
	attrs := []any{
		"request_id", input.RequestID, "decision_id", input.DecisionID, "mode", p.cfg.Mode, "route", route,
		"control_version", p.control.PolicyVersion(), "candidate_version", p.candidate.PolicyVersion(),
		"control", control, "candidate", candidate,
	}
	if control != nil && candidate != nil {
		attrs = append(attrs, "same_action", control.Type == candidate.Type, "value_delta", adviceValue(candidate)-adviceValue(control))
	}
	if err != nil {
		attrs = append(attrs, "candidate_error", err.Error())
	}
	p.logger.Info("gray_decision", attrs...)
}

func adviceValue(advice *protocol.AdviseResponse) float64 {
	if advice == nil || advice.Value == nil {
		return 0
	}
	if math.IsNaN(*advice.Value) || math.IsInf(*advice.Value, 0) {
		return 0
	}
	return *advice.Value
}
