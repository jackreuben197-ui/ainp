package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"time"

	"gitlab.com/ubenbill/ainp/internal/config"
	"gitlab.com/ubenbill/ainp/internal/protocol"
	"gitlab.com/ubenbill/ainp/internal/replay"
	"gitlab.com/ubenbill/ainp/internal/service"
	"gitlab.com/ubenbill/ainp/internal/store"
)

type comparison struct {
	DecisionID string                   `json:"decision_id"`
	HandID     string                   `json:"hand_id"`
	SeqNum     uint64                   `json:"seq_num"`
	Command    protocol.Command         `json:"command"`
	Baseline   *protocol.AdviseResponse `json:"baseline,omitempty"`
	Candidate  *protocol.AdviseResponse `json:"candidate,omitempty"`
	Kind       string                   `json:"kind"`
}

type candidateError struct {
	DecisionID string             `json:"decision_id"`
	HandID     string             `json:"hand_id"`
	SeqNum     uint64             `json:"seq_num"`
	Command    protocol.Command   `json:"command"`
	Code       protocol.ErrorCode `json:"code"`
	Message    string             `json:"message"`
}

type sequenceState struct {
	next     uint64
	assigned map[string]uint64
}

type report struct {
	GeneratedAt           time.Time          `json:"generated_at"`
	Input                 string             `json:"input"`
	CandidateVersion      string             `json:"candidate_version"`
	NormalizedSequences   bool               `json:"normalized_sequences"`
	Records               int                `json:"records"`
	TruncatedTailRecords  int                `json:"truncated_tail_records"`
	Comparable            int                `json:"comparable"`
	Same                  int                `json:"same"`
	ActionChanged         int                `json:"action_changed"`
	ValueChanged          int                `json:"value_changed"`
	AdviceAdded           int                `json:"advice_added"`
	AdviceRemoved         int                `json:"advice_removed"`
	OutcomeMismatch       int                `json:"outcome_mismatch"`
	ResolvedRejections    int                `json:"resolved_rejections"`
	NewRejections         int                `json:"new_rejections"`
	CandidateOutcomes     map[string]int     `json:"candidate_outcomes"`
	CandidateErrors       map[string]int     `json:"candidate_errors"`
	CandidateErrorDetails []candidateError   `json:"candidate_error_details,omitempty"`
	CompletedHands        int                `json:"completed_hands"`
	WinningHands          int                `json:"winning_hands"`
	LosingHands           int                `json:"losing_hands"`
	BreakEvenHands        int                `json:"break_even_hands"`
	HeroNetProfit         float64            `json:"hero_net_profit"`
	ProfitByPlayer        map[string]float64 `json:"profit_by_player"`
	AgreementRate         float64            `json:"agreement_rate"`
	Transitions           map[string]int     `json:"transitions"`
	Changes               []comparison       `json:"changes"`
}

func main() {
	configPath := flag.String("config", "conf/config.yaml", "candidate configuration file")
	inputPath := flag.String("input", "", "replay JSONL journal")
	outputPath := flag.String("output", "", "JSON comparison report")
	tolerance := flag.Float64("value-tolerance", .001, "absolute advice value tolerance")
	stateOnly := flag.Bool("state-only", false, "validate protocol and game-state transitions without running strategy/equity")
	flag.Parse()
	if *inputPath == "" {
		fatal(fmt.Errorf("-input is required"))
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal(err)
	}
	// Offline replay must be fast and deterministic; generated think time remains in decisions but is not slept.
	cfg.Engine.Personality.ApplyThinkTime = false
	cfg.Log.Events, cfg.Log.Strategy, cfg.Log.Access = false, false, false
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	var provider service.DecisionProvider
	version := "state-only"
	if *stateOnly {
		cfg.Mock.Enabled = false
		provider = service.NewMockDecisionProvider(cfg.Mock)
	} else {
		engineProvider := service.NewEngineDecisionProvider(cfg, logger)
		provider, version = engineProvider, engineProvider.PolicyVersion()
	}
	events := service.NewEventService(store.NewMemoryHandStoreWithLimit(cfg.State.TTL, cfg.State.MaxHands, cfg.State.PruneInterval), provider, logger, false)
	report, err := run(*inputPath, version, events, *tolerance, !*stateOnly)
	if err != nil {
		fatal(err)
	}
	if *outputPath == "" {
		*outputPath = filepath.Join("reports", "replay-"+time.Now().Format("20060102T150405")+".json")
	}
	if err := writeReport(*outputPath, report); err != nil {
		fatal(err)
	}
	if *stateOnly {
		fmt.Printf("state replay records=%d applied=%d rejected=%d resolved_rejections=%d new_rejections=%d truncated_tail=%d report=%s\n", report.Records, report.CandidateOutcomes["applied"], report.CandidateOutcomes["rejected"], report.ResolvedRejections, report.NewRejections, report.TruncatedTailRecords, *outputPath)
	} else {
		fmt.Printf("replay records=%d comparable=%d agreement=%.2f%% changes=%d outcome_mismatch=%d report=%s\n", report.Records, report.Comparable, report.AgreementRate*100, len(report.Changes), report.OutcomeMismatch, *outputPath)
	}
}

func run(path, version string, events *service.EventService, tolerance float64, compareStrategy bool) (report, error) {
	file, err := os.Open(path)
	if err != nil {
		return report{}, err
	}
	defer file.Close()
	result := report{GeneratedAt: time.Now(), Input: path, CandidateVersion: version, NormalizedSequences: !compareStrategy, Transitions: map[string]int{}, ProfitByPlayer: map[string]float64{}, CandidateOutcomes: map[string]int{}, CandidateErrors: map[string]int{}}
	sequenceStreams := make(map[string]*sequenceState)
	reader := bufio.NewReaderSize(file, 64*1024)
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) == 0 {
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return result, readErr
			}
		}
		lineNumber++
		var baseline replay.Record
		if err := json.Unmarshal(line, &baseline); err != nil {
			if readErr == io.EOF && err.Error() == "unexpected end of JSON input" {
				result.TruncatedTailRecords++
				break
			}
			return result, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		result.Records++
		collectResult(&result, baseline)
		newSequenceAssignment := false
		streamKey := ""
		if !compareStrategy {
			streamKey = replayStreamKey(baseline)
			stream := sequenceStreams[streamKey]
			if stream == nil {
				stream = &sequenceState{next: 1, assigned: make(map[string]uint64)}
				sequenceStreams[streamKey] = stream
			}
			sequenceIdentity := fmt.Sprintf("%d|%s", baseline.Event.SeqNum, baseline.Fingerprint)
			sequence, exists := stream.assigned[sequenceIdentity]
			if !exists {
				sequence = stream.next
				stream.assigned[sequenceIdentity] = sequence
				newSequenceAssignment = true
			}
			baseline.Event.SeqNum = sequence
		}
		actual := events.Apply(context.Background(), baseline.Event, "replay-"+baseline.RequestID)
		actualOutcome := "applied"
		if actual.AlreadyApplied {
			actualOutcome = "duplicate"
		} else if actual.ErrorCode != "" {
			actualOutcome = "rejected"
		}
		result.CandidateOutcomes[actualOutcome]++
		if !compareStrategy && newSequenceAssignment && actualOutcome == "applied" {
			sequenceStreams[streamKey].next = baseline.Event.SeqNum + 1
		}
		if !compareStrategy && baseline.Event.Cmd == protocol.CommandEndHand && actualOutcome == "applied" {
			delete(sequenceStreams, streamKey)
		}
		if actual.ErrorCode != "" {
			result.CandidateErrors[string(actual.ErrorCode)]++
			if len(result.CandidateErrorDetails) < 100 {
				handID := ""
				if baseline.Event.HandID != nil {
					handID = *baseline.Event.HandID
				}
				result.CandidateErrorDetails = append(result.CandidateErrorDetails, candidateError{
					DecisionID: actual.DecisionID,
					HandID:     handID,
					SeqNum:     baseline.Event.SeqNum,
					Command:    baseline.Event.Cmd,
					Code:       actual.ErrorCode,
					Message:    actual.ErrorMessage,
				})
			}
		}
		if actualOutcome != baseline.Outcome && !(baseline.Outcome == "failed" && actual.ErrorCode != "") {
			result.OutcomeMismatch++
		}
		if baseline.Outcome == "rejected" && actualOutcome == "applied" {
			result.ResolvedRejections++
		}
		if baseline.Outcome == "applied" && actualOutcome == "rejected" {
			result.NewRejections++
		}
		if compareStrategy {
			compareAdvice(&result, baseline, actual.Response.Advise, tolerance)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, readErr
		}
	}
	if result.Comparable > 0 {
		result.AgreementRate = float64(result.Same) / float64(result.Comparable)
	}
	return result, nil
}

func replayStreamKey(record replay.Record) string {
	handID := ""
	if record.Event.HandID != nil {
		handID = *record.Event.HandID
	}
	return record.Event.PlayerID + "|" + record.Event.RoomID + "|" + record.Event.TableID + "|" + handID
}

func collectResult(result *report, record replay.Record) {
	if record.Outcome != "applied" || record.Event.Cmd != protocol.CommandEndHand {
		return
	}
	var payload protocol.EndHandPayload
	if json.Unmarshal(record.Event.Payload, &payload) != nil {
		return
	}
	for _, player := range payload.Players {
		if player.PlayerID != record.Event.PlayerID {
			continue
		}
		result.CompletedHands++
		result.HeroNetProfit += player.Profit
		result.ProfitByPlayer[player.PlayerID] += player.Profit
		switch {
		case player.Profit > 0:
			result.WinningHands++
		case player.Profit < 0:
			result.LosingHands++
		default:
			result.BreakEvenHands++
		}
		return
	}
}

func compareAdvice(result *report, baseline replay.Record, candidate *protocol.AdviseResponse, tolerance float64) {
	expected := baseline.Response.Advise
	if expected == nil && candidate == nil {
		return
	}
	handID := ""
	if baseline.Event.HandID != nil {
		handID = *baseline.Event.HandID
	}
	item := comparison{DecisionID: baseline.DecisionID, HandID: handID, SeqNum: baseline.Event.SeqNum, Command: baseline.Event.Cmd, Baseline: expected, Candidate: candidate}
	if expected == nil {
		result.AdviceAdded++
		item.Kind = "advice_added"
		result.Changes = append(result.Changes, item)
		return
	}
	if candidate == nil {
		result.AdviceRemoved++
		item.Kind = "advice_removed"
		result.Changes = append(result.Changes, item)
		return
	}
	result.Comparable++
	transition := string(expected.Type) + "->" + string(candidate.Type)
	result.Transitions[transition]++
	if expected.Type != candidate.Type {
		result.ActionChanged++
		item.Kind = "action_changed"
		result.Changes = append(result.Changes, item)
		return
	}
	if math.Abs(value(expected)-value(candidate)) > tolerance {
		result.ValueChanged++
		item.Kind = "value_changed"
		result.Changes = append(result.Changes, item)
		return
	}
	result.Same++
}

func value(advice *protocol.AdviseResponse) float64 {
	if advice == nil || advice.Value == nil {
		return 0
	}
	return *advice.Value
}

func writeReport(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o640)
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
