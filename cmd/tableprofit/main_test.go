package main

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetricNormalizesProfitByBigBlind(t *testing.T) {
	metric := &metric{}
	update(metric, 20, 10)
	update(metric, -2, 1)
	update(metric, 5, 0)

	if metric.Assignments != 3 || metric.BBSamples != 2 || metric.Profit != 23 || metric.ProfitBB != 0 {
		t.Fatalf("metric=%+v", metric)
	}

	var output bytes.Buffer
	writer := csv.NewWriter(&output)
	if err := writeMetric(writer, "profile", "", 0, "", "FPCH_default", metric); err != nil {
		t.Fatal(err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	fields, err := csv.NewReader(strings.NewReader(output.String())).Read()
	if err != nil {
		t.Fatal(err)
	}
	if fields[10] != "2" || fields[11] != "0.0000" || fields[12] != "0.0000" {
		t.Fatalf("fields=%v", fields)
	}
}

func TestWriteCSVIncludesOverallSummaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profit.csv")
	overall := &metric{}
	update(overall, 6, 2)
	profiles := map[string]*metric{"FPCH_default": {Assignments: 1, Wins: 1, Profit: 6, ProfitBB: 3, BBSamples: 1}}
	players := map[string]*metric{"bot-1": {Assignments: 1, Wins: 1, Profit: 6, ProfitBB: 3, BBSamples: 1}}
	playerProfiles := map[[2]string]*metric{{"bot-1", "FPCH_default"}: {Assignments: 1, Wins: 1, Profit: 6, ProfitBB: 3, BBSamples: 1}}
	if err := writeCSV(path, 1, overall, profiles, players, playerProfiles, map[string]map[string]bool{}, map[string]*metric{}, map[[2]string]*metric{}, map[[2]string]*metric{}, map[[3]string]*metric{}, map[string]*metric{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 || rows[1][0] != "overall" || rows[2][0] != "overall_profile" || rows[3][0] != "overall_player" || rows[4][0] != "overall_player_profile" {
		t.Fatalf("rows=%v", rows)
	}
}

func TestSelectSettlementUsesMajorityAndReportsConflict(t *testing.T) {
	winner := endPayload{}
	winner.Players = append(winner.Players, struct {
		PlayerID string  `json:"player_id"`
		Profit   float64 `json:"profit"`
	}{PlayerID: "bot", Profit: 10})
	stale := endPayload{}
	selected, conflict := selectSettlement(map[string]*settlementVote{
		"winner": {Payload: winner, Count: 3},
		"stale":  {Payload: stale, Count: 1},
	})
	if !conflict || len(selected.Players) != 1 || selected.Players[0].PlayerID != "bot" {
		t.Fatalf("selected=%+v conflict=%v", selected, conflict)
	}
}

func TestWriteNonRobotCSVSeparatesExactCounterpartyFromProfileBreakdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "non-robot.csv")
	overall := &metric{}
	update(overall, 6, 2)
	profiles := map[string]*metric{"FPCH_default": {Assignments: 1, Wins: 1, Profit: 6, ProfitBB: 3, BBSamples: 1}}
	profileHands := map[string]map[[2]string]bool{"FPCH_default": {{"table", "1"}: true}}
	hands := []nonRobotHand{{TableID: "table", HandID: "1", RobotPlayers: 2, NonRobotPlayers: 1, RobotProfit: 6, BigBlind: 2}}
	if err := writeNonRobotCSV(path, overall, profiles, profileHands, hands); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[1][14] != "-6.00" || rows[2][14] != "" || rows[3][14] != "-6.00" {
		t.Fatalf("rows=%v", rows)
	}
}
