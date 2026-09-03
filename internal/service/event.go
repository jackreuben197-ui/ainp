package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gitlab.com/smoothsics/ainp/internal/game"
	"gitlab.com/smoothsics/ainp/internal/protocol"
	"gitlab.com/smoothsics/ainp/internal/replay"
	"gitlab.com/smoothsics/ainp/internal/store"
)

type EventService struct {
	store     *store.MemoryHandStore
	decision  DecisionProvider
	logger    *slog.Logger
	now       func() time.Time
	logEvents bool
	recorder  replay.Recorder
}

func (s *EventService) SetRecorder(recorder replay.Recorder) { s.recorder = recorder }

type ApplyOutcome struct {
	Response       protocol.EventResponse
	AlreadyApplied bool
	ErrorCode      protocol.ErrorCode
	ErrorMessage   string
	DecisionID     string
	Fingerprint    string
}

func NewEventService(handStore *store.MemoryHandStore, decision DecisionProvider, logger *slog.Logger, logEvents ...bool) *EventService {
	enabled := true
	if len(logEvents) > 0 {
		enabled = logEvents[0]
	}
	return &EventService{store: handStore, decision: decision, logger: logger, now: time.Now, logEvents: enabled}
}

func (s *EventService) Apply(ctx context.Context, req protocol.EventRequest, requestID string) ApplyOutcome {
	started := time.Now()
	fingerprint := eventFingerprint(req)
	decisionID := stableDecisionID(req, fingerprint)
	if code, message := validateRequest(req); code != "" {
		s.persistOutcome(req, requestID, decisionID, fingerprint, "rejected", code, message, protocol.EventResponse{}, started)
		return ApplyOutcome{ErrorCode: code, ErrorMessage: message, DecisionID: decisionID, Fingerprint: fingerprint}
	}
	key := store.HandKey{PlayerID: req.PlayerID, RoomID: req.RoomID, TableID: req.TableID, HandID: *req.HandID}
	result, gameState, stateErr := s.store.ApplyEvent(key, req, fingerprint, s.now())
	if stateErr != nil {
		s.persistOutcome(req, requestID, decisionID, fingerprint, "rejected", protocol.ErrorGameLogic, stateErr.Error(), protocol.EventResponse{}, started)
		return ApplyOutcome{ErrorCode: protocol.ErrorGameLogic, ErrorMessage: stateErr.Error(), DecisionID: decisionID, Fingerprint: fingerprint}
	}
	switch result {
	case store.AlreadyApplied:
		s.persistOutcome(req, requestID, decisionID, fingerprint, "duplicate", "", "", protocol.EventResponse{}, started)
		return ApplyOutcome{AlreadyApplied: true, DecisionID: decisionID, Fingerprint: fingerprint}
	case store.WrongSequence:
		s.persistOutcome(req, requestID, decisionID, fingerprint, "rejected", protocol.ErrorWrongSeq, "seq_num must be the next sequence number for this hand", protocol.EventResponse{}, started)
		return ApplyOutcome{ErrorCode: protocol.ErrorWrongSeq, ErrorMessage: "seq_num must be the next sequence number for this hand", DecisionID: decisionID, Fingerprint: fingerprint}
	case store.HandNotStarted:
		s.persistOutcome(req, requestID, decisionID, fingerprint, "rejected", protocol.ErrorBrokenHand, "start_hand_extended must be the first event for a hand", protocol.EventResponse{}, started)
		return ApplyOutcome{ErrorCode: protocol.ErrorBrokenHand, ErrorMessage: "start_hand_extended must be the first event for a hand", DecisionID: decisionID, Fingerprint: fingerprint}
	}
	if gameState.LastNormalization != "" && s.logEvents {
		s.logger.Info("game_state_normalized",
			"normalization", gameState.LastNormalization,
			"request_id", requestID,
			"decision_id", decisionID,
			"player_id", req.PlayerID,
			"room_id", req.RoomID,
			"table_id", req.TableID,
			"hand_id", *req.HandID,
			"seq_num", req.SeqNum,
			"cmd", req.Cmd,
		)
	}
	advise, err := s.decision.Decide(ctx, DecisionInput{Event: req, State: gameState, RequestID: requestID, DecisionID: decisionID})
	if err != nil {
		s.persistOutcome(req, requestID, decisionID, fingerprint, "failed", protocol.ErrorServer, err.Error(), protocol.EventResponse{}, started)
		return ApplyOutcome{ErrorCode: protocol.ErrorServer, ErrorMessage: err.Error(), DecisionID: decisionID, Fingerprint: fingerprint}
	}
	if advise != nil {
		s.store.SetAdvice(key, req.SeqNum, advise)
	}
	response := protocol.EventResponse{SeqNum: req.SeqNum, Advise: advise, Deviation: gameState.Deviation}
	s.persistOutcome(req, requestID, decisionID, fingerprint, "applied", "", "", response, started)
	if req.Cmd == protocol.CommandEndHand {
		s.logHandResult(req, gameState)
	}
	return ApplyOutcome{Response: response, DecisionID: decisionID, Fingerprint: fingerprint}
}

func (s *EventService) logHandResult(req protocol.EventRequest, state *game.State) {
	if !s.logEvents || state == nil {
		return
	}
	var payload protocol.EndHandPayload
	if err := json.Unmarshal(req.Payload, &payload); err != nil {
		return
	}
	showdownPlayers := 0
	for _, result := range payload.Players {
		if result.Cards != nil && strings.TrimSpace(*result.Cards) != "" {
			showdownPlayers++
		}
	}
	for _, player := range payload.Players {
		if player.PlayerID != state.HeroID {
			continue
		}
		s.logger.Info("hand_result",
			"player_id", state.HeroID,
			"room_id", state.RoomID,
			"table_id", state.TableID,
			"hand_id", state.HandID,
			"ai_profile", state.AIProfile,
			"reached_street", state.Street,
			"board_card_count", len(state.Board),
			"showdown_players", showdownPlayers,
			"profit", player.Profit,
		)
		return
	}
}

func eventFingerprint(req protocol.EventRequest) string {
	sum := sha256.Sum256(append([]byte(req.Cmd+":"), req.Payload...))
	return fmt.Sprintf("%x", sum)
}

func stableDecisionID(req protocol.EventRequest, fingerprint string) string {
	handID := ""
	if req.HandID != nil {
		handID = *req.HandID
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%s|%d|%s", req.PlayerID, req.RoomID, req.TableID, handID, req.SeqNum, fingerprint)))
	return fmt.Sprintf("%x", sum[:16])
}

func (s *EventService) persistOutcome(req protocol.EventRequest, requestID, decisionID, fingerprint, outcome string, code protocol.ErrorCode, message string, response protocol.EventResponse, started time.Time) {
	s.logOutcome(req, requestID, decisionID, fingerprint, outcome, code, message, response, started)
	if s.recorder == nil {
		return
	}
	version := ""
	if provider, ok := s.decision.(interface{ PolicyVersion() string }); ok {
		version = provider.PolicyVersion()
	}
	err := s.recorder.Record(replay.Record{
		RecordedAt: s.now(), RequestID: requestID, DecisionID: decisionID, Fingerprint: fingerprint,
		Provider: s.decision.Name(), PolicyVersion: version, Outcome: outcome, ErrorCode: code, ErrorMessage: message,
		Event: req, Response: response,
	})
	if err != nil {
		s.logger.Error("replay_record_failed", "request_id", requestID, "decision_id", decisionID, "error", err)
	}
}

func (s *EventService) logOutcome(req protocol.EventRequest, requestID, decisionID, fingerprint, outcome string, code protocol.ErrorCode, message string, response protocol.EventResponse, started time.Time) {
	if !s.logEvents {
		return
	}
	handID := ""
	if req.HandID != nil {
		handID = *req.HandID
	}
	s.logger.Info("decision_event",
		"request_id", requestID,
		"decision_id", decisionID,
		"event_fingerprint", fingerprint,
		"outcome", outcome,
		"error_code", code,
		"error_message", message,
		"player_id", req.PlayerID,
		"room_id", req.RoomID,
		"table_id", req.TableID,
		"hand_id", handID,
		"seq_num", req.SeqNum,
		"cmd", req.Cmd,
		"provider", s.decision.Name(),
		"advise", response.Advise,
		"deviation", response.Deviation,
		"latency_us", time.Since(started).Microseconds(),
	)
}

func validateRequest(req protocol.EventRequest) (protocol.ErrorCode, string) {
	if req.SeqNum == 0 {
		return protocol.ErrorMissedField, "seq_num must be greater than zero"
	}
	if strings.TrimSpace(req.PlayerID) == "" || strings.TrimSpace(req.RoomID) == "" || strings.TrimSpace(req.TableID) == "" {
		return protocol.ErrorMissedField, "player_id, room_id and table_id are required"
	}
	if req.HandID == nil || strings.TrimSpace(*req.HandID) == "" {
		return protocol.ErrorMissedField, "hand_id is required by ainp"
	}
	if !protocol.ValidCommand(req.Cmd) {
		return protocol.ErrorWrongValue, fmt.Sprintf("unsupported cmd %q", req.Cmd)
	}
	if len(req.Payload) == 0 || string(req.Payload) == "null" {
		return protocol.ErrorMissedField, "payload is required"
	}
	if !json.Valid(req.Payload) {
		return protocol.ErrorDecodeRequest, "payload must be valid JSON"
	}
	return validatePayload(req)
}

func validatePayload(req protocol.EventRequest) (protocol.ErrorCode, string) {
	switch req.Cmd {
	case protocol.CommandStartHand:
		var payload protocol.StartHandPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return protocol.ErrorWrongValue, "invalid start_hand_extended payload"
		}
		if payload.GameType == "" || payload.ClubID == "" || payload.AIProfile == "" || payload.Time == 0 || payload.TimeToAct == 0 || payload.MaxSeat < 2 || len(payload.Players) < 2 {
			return protocol.ErrorMissedField, "start hand required fields are missing or invalid"
		}
		if payload.NearAllInCallPercent < 0 || payload.NearAllInCallPercent > 100 || payload.NearAllInRaisePercent < 0 || payload.NearAllInRaisePercent > 100 {
			return protocol.ErrorWrongValue, "near-all-in percentages must be between 0 and 100"
		}
	case protocol.CommandDealCards, protocol.CommandFlop, protocol.CommandTurn, protocol.CommandRiver:
		var payload protocol.CardsPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil || payload.Cards == "" {
			return protocol.ErrorMissedField, "cards is required"
		}
	case protocol.CommandAction:
		var payload protocol.ActionPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil || payload.PlayerID == "" || !protocol.ValidAction(payload.Type) {
			return protocol.ErrorWrongValue, "action payload requires a valid player_id and type"
		}
	case protocol.CommandShowCards:
		var payload protocol.ShowCardsPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil || payload.PlayerID == "" || payload.Cards == "" {
			return protocol.ErrorMissedField, "show_cards requires player_id and cards"
		}
	case protocol.CommandEndHand:
		var payload protocol.EndHandPayload
		if err := json.Unmarshal(req.Payload, &payload); err != nil || len(payload.Players) == 0 {
			return protocol.ErrorMissedField, "end_hand requires players"
		}
	}
	return "", ""
}
