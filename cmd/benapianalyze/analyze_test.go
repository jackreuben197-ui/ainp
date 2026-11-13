package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestParseNextActionRecord(t *testing.T) {
	line := `{"engine":"benapi","uid":"bot","room_id":123,"hand_num":9,"bet_type":"raises","action":"raise","req":{"n_players":3,"sb":100,"bb":200,"ante":100,"straddle":0,"stack":[10000,9000,8000],"private_hand":["As","Ts"],"community_cards":[],"current_player_id":1,"style_name":"shark","game_id":"123_9","action_sequance":[{"player_id":2,"bet_type":"folds","amount":0}]},"resp.Data":{"player_id":1,"bet_type":"raises","amount":700,"extra_info":{"action_probs":[{"player_id":1,"bet_type":"checks","amount":0,"action_prob":0.2},{"player_id":1,"bet_type":"raises","amount":700,"action_prob":0.8}],"current_pot":700,"current_player_stack":8800,"current_player_tocall":0}}}`
	record, err := parseNextAction(line, "sample.out", 42)
	if err != nil {
		t.Fatal(err)
	}
	if record.UID != "bot" || record.RoomID != "123" || record.HandNum != "9" || record.Street != "preflop" || record.Position != "BB" || record.BenAction != "raise" || record.Argmax != "raise" || record.ChosenProb != .8 {
		t.Fatalf("record=%+v", record)
	}
}

func TestNormalizeAllInUsesActionContext(t *testing.T) {
	if got := normalizeAction("all in", "allins", "allins", 0); got != "bet" {
		t.Fatalf("no-call all-in=%q", got)
	}
	if got := normalizeAction("ALL_IN", "allins", "allins", 100); got != "raise" {
		t.Fatalf("facing-call all-in=%q", got)
	}
}

func TestPositionMapping(t *testing.T) {
	button, bb := 5, 1
	req := benRequest{NPlayers: 6, CurrentPlayerID: 2, ButtonPlayerID: &button, BBPlayerID: &bb}
	if got := positionOf(req); got != "UTG" {
		t.Fatalf("position=%q", got)
	}
	req.CurrentPlayerID = 4
	if got := positionOf(req); got != "CO" {
		t.Fatalf("position=%q", got)
	}
	req.Straddle = 400
	req.CurrentPlayerID = 2
	if got := positionOf(req); got != "STR" {
		t.Fatalf("straddle position=%q", got)
	}
	req.CurrentPlayerID = 3
	if got := positionOf(req); got != "UTG" {
		t.Fatalf("straddled UTG position=%q", got)
	}
	customButton, customBB := 2, 5
	req = benRequest{NPlayers: 6, CurrentPlayerID: 4, ButtonPlayerID: &customButton, BBPlayerID: &customBB}
	if got := positionOf(req); got != "SB" {
		t.Fatalf("custom small blind position=%q", got)
	}
}

func TestScanFileExtractsOnlyStructuredBenAPIRecords(t *testing.T) {
	next := `{"uid":"bot","room_id":123,"hand_num":9,"action":"check","req":{"n_players":2,"sb":100,"bb":200,"stack":[10000,10000],"private_hand":["As","Ts"],"community_cards":["2c","3d","4h"],"current_player_id":1,"game_id":"123_9","action_sequance":[]},"resp.Data":{"player_id":1,"bet_type":"checks","amount":0,"extra_info":{"action_probs":[{"player_id":1,"bet_type":"checks","action_prob":1}]}}}`
	callbackBody := `{"n_players":2,"payoff":[100,-100],"rake":[0,0],"showdown_cards":[["As","Ts"],[]]}`
	callbackOuter, _ := json.Marshal(map[string]string{"body": callbackBody})
	responseBody := `{"code":0,"data":{"player_id":1,"bet_type":"checks","amount":0,"extra_info":{"action_probs_source":"normal","action_probs":[{"player_id":1,"bet_type":"checks","action_prob":1}],"origin_action_probs":[{"player_id":1,"bet_type":"checks","action_prob":1}]}}}`
	responseOuter, _ := json.Marshal(map[string]string{"response_body": responseBody})
	content := "noise\n" + "prefix" + nextActionMarker + next + "\n" + "prefix" + callbackMarker + string(callbackOuter) + "\n" + "prefix" + holdemMarker + string(responseOuter) + "\n"
	path := filepath.Join(t.TempDir(), "benapi.out")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	decisions, callbacks, responses := 0, 0, 0
	stat, err := scanFile(path, func(decisionRecord) { decisions++ }, func(callbackRecord) { callbacks++ }, func(benResponse, bool) { responses++ })
	if err != nil {
		t.Fatal(err)
	}
	if decisions != 1 || callbacks != 1 || responses != 1 || stat.Lines != 4 || stat.Malformed != 0 || stat.DecisionRecords != 1 || stat.CallbackRecords != 1 || stat.APIResponseRecords != 1 {
		t.Fatalf("decisions=%d callbacks=%d responses=%d stat=%+v", decisions, callbacks, responses, stat)
	}
}

func TestExpandInputsRecursively(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "host", "pokerbot01", "logs")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "top.out"), filepath.Join(nested, "nested.out.part")} {
		if err := os.WriteFile(path, []byte("log\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := expandInputs([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths=%v", paths)
	}
}

func TestDateOf(t *testing.T) {
	if got := dateOf("build/benapi/host/pokerbot01/logs/history/nohup.out_20260825_0003"); got != "20260825" {
		t.Fatalf("date=%q", got)
	}
	if got := dateOf("build/benapi/no-date.out"); got != "unknown" {
		t.Fatalf("unknown date=%q", got)
	}
}

func TestPayoffUncertainty(t *testing.T) {
	stddev, standardError, low, high := payoffUncertainty(2, 0, 2)
	if math.Abs(stddev-math.Sqrt(2)) > 1e-9 || math.Abs(standardError-1) > 1e-9 || math.Abs(low+1.96) > 1e-9 || math.Abs(high-1.96) > 1e-9 {
		t.Fatalf("stddev=%v standard_error=%v ci=[%v,%v]", stddev, standardError, low, high)
	}
}

func TestScanAOFLine(t *testing.T) {
	stat := newAOFStat()
	request := `{"body":"{\"game_id\":\"1_2\"}"}`
	response := `{"response_body":"{\"code\":0,\"msg\":\"success\",\"data\":{\"action\":\"Push\",\"ai_version\":\"3.0.3\",\"push_ev\":1.25,\"fold_ev\":-1}}"}`
	if err := scanAOFLine(request, false, &stat); err != nil {
		t.Fatal(err)
	}
	if err := scanAOFLine(response, false, &stat); err != nil {
		t.Fatal(err)
	}
	finalizeAOF(&stat)
	if stat.RequestRecords != 1 || stat.ResponseRecords != 1 || stat.Actions["Push"] != 1 || stat.AIVersions["3.0.3"] != 1 || stat.PushEV.Mean != 1.25 {
		t.Fatalf("stat=%+v", stat)
	}
}
