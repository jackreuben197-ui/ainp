package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type journalRecord struct {
	Event struct {
		PlayerID string          `json:"player_id"`
		TableID  string          `json:"table_id"`
		HandID   string          `json:"hand_id"`
		Command  string          `json:"cmd"`
		Payload  json.RawMessage `json:"payload"`
	} `json:"event"`
}

type startPayload struct {
	AIProfile string  `json:"ai_profile"`
	BigBlind  float64 `json:"big_blind"`
}

type endPayload struct {
	Players []struct {
		PlayerID string  `json:"player_id"`
		Profit   float64 `json:"profit"`
	} `json:"players"`
}

type settlementVote struct {
	Payload endPayload
	Count   int
}

type settledRobot struct {
	PlayerID string
	Profile  string
	Profit   float64
}

type nonRobotHand struct {
	TableID         string
	HandID          string
	RobotPlayers    int
	NonRobotPlayers int
	RobotDetails    string
	NonRobotDetails string
	RobotProfit     float64
	NonRobotProfit  float64
	BigBlind        float64
}

type key struct {
	TableID  string
	HandID   string
	PlayerID string
}

type metric struct {
	Assignments int
	Wins        int
	Losses      int
	Ties        int
	Profit      float64
	ProfitBB    float64
	BBSamples   int
}

func main() {
	input := flag.String("input", "", "directory containing replay JSONL files")
	output := flag.String("output", "", "output CSV path")
	nonRobotOutput := flag.String("non-robot-output", "", "robot versus non-robot hand CSV path (defaults next to -output)")
	exclude := flag.String("exclude-table", "", "comma-separated table IDs to exclude")
	flag.Parse()
	if *input == "" || *output == "" {
		fatal(fmt.Errorf("-input and -output are required"))
	}
	if *nonRobotOutput == "" {
		extension := filepath.Ext(*output)
		*nonRobotOutput = strings.TrimSuffix(*output, extension) + "-vs-non-robot" + extension
	}
	files, err := replayFiles(*input)
	if err != nil {
		fatal(err)
	}
	excluded := make(map[string]bool)
	for _, tableID := range strings.Split(*exclude, ",") {
		if tableID = strings.TrimSpace(tableID); tableID != "" {
			excluded[tableID] = true
		}
	}
	profiles := make(map[key]string)
	bigBlinds := make(map[[2]string]float64)
	if err := scan(files, func(record journalRecord) error {
		if record.Event.Command != "start_hand_extended" || excluded[record.Event.TableID] {
			return nil
		}
		var payload startPayload
		if err := json.Unmarshal(record.Event.Payload, &payload); err != nil {
			return err
		}
		if payload.AIProfile != "" {
			profiles[key{record.Event.TableID, record.Event.HandID, record.Event.PlayerID}] = payload.AIProfile
		}
		if payload.BigBlind > 0 {
			bigBlinds[[2]string{record.Event.TableID, record.Event.HandID}] = payload.BigBlind
		}
		return nil
	}); err != nil {
		fatal(err)
	}

	tableHands := make(map[string]map[string]bool)
	tables := make(map[string]*metric)
	profileMetrics := make(map[[2]string]*metric)
	tablePlayerMetrics := make(map[[2]string]*metric)
	playerMetrics := make(map[[3]string]*metric)
	overall := &metric{}
	overallProfiles := make(map[string]*metric)
	overallPlayers := make(map[string]*metric)
	overallPlayerProfiles := make(map[[2]string]*metric)
	unprofiled := make(map[string]*metric)
	vsNonRobotOverall := &metric{}
	vsNonRobotProfiles := make(map[string]*metric)
	vsNonRobotProfileHands := make(map[string]map[[2]string]bool)
	var vsNonRobotHands []nonRobotHand
	settlements := make(map[[2]string]map[string]*settlementVote)
	if err := scan(files, func(record journalRecord) error {
		if record.Event.Command != "end_hand" || excluded[record.Event.TableID] {
			return nil
		}
		handKey := [2]string{record.Event.TableID, record.Event.HandID}
		var payload endPayload
		if err := json.Unmarshal(record.Event.Payload, &payload); err != nil {
			return err
		}
		canonical, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if settlements[handKey] == nil {
			settlements[handKey] = make(map[string]*settlementVote)
		}
		voteKey := string(canonical)
		if settlements[handKey][voteKey] == nil {
			settlements[handKey][voteKey] = &settlementVote{Payload: payload}
		}
		settlements[handKey][voteKey].Count++
		return nil
	}); err != nil {
		fatal(err)
	}
	conflictingSettlements := 0
	for handKey, votes := range settlements {
		payload, conflict := selectSettlement(votes)
		if conflict {
			conflictingSettlements++
		}
		tableID, handID := handKey[0], handKey[1]
		bigBlind := bigBlinds[handKey]
		if tableHands[tableID] == nil {
			tableHands[tableID] = make(map[string]bool)
		}
		tableHands[tableID][handID] = true
		robots := make([]settledRobot, 0, len(payload.Players))
		nonRobotPlayers := 0
		nonRobotProfit := 0.0
		nonRobotDetails := make([]string, 0)
		for _, player := range payload.Players {
			profile := profiles[key{tableID, handID, player.PlayerID}]
			if profile == "" {
				update(metricFor(unprofiled, tableID), player.Profit, bigBlind)
				nonRobotPlayers++
				nonRobotProfit += player.Profit
				nonRobotDetails = append(nonRobotDetails, fmt.Sprintf("%s:%s", player.PlayerID, formatProfit(player.Profit)))
				continue
			}
			robots = append(robots, settledRobot{PlayerID: player.PlayerID, Profile: profile, Profit: player.Profit})
			update(metricFor(tables, tableID), player.Profit, bigBlind)
			update(metricFor(profileMetrics, [2]string{tableID, profile}), player.Profit, bigBlind)
			update(metricFor(tablePlayerMetrics, [2]string{tableID, player.PlayerID}), player.Profit, bigBlind)
			update(metricFor(playerMetrics, [3]string{tableID, player.PlayerID, profile}), player.Profit, bigBlind)
			update(overall, player.Profit, bigBlind)
			update(metricFor(overallProfiles, profile), player.Profit, bigBlind)
			update(metricFor(overallPlayers, player.PlayerID), player.Profit, bigBlind)
			update(metricFor(overallPlayerProfiles, [2]string{player.PlayerID, profile}), player.Profit, bigBlind)
		}
		if nonRobotPlayers > 0 && len(robots) > 0 {
			robotProfit := 0.0
			robotDetails := make([]string, 0, len(robots))
			for _, robot := range robots {
				robotProfit += robot.Profit
				robotDetails = append(robotDetails, fmt.Sprintf("%s:%s:%s", robot.PlayerID, robot.Profile, formatProfit(robot.Profit)))
				update(metricFor(vsNonRobotProfiles, robot.Profile), robot.Profit, bigBlind)
				if vsNonRobotProfileHands[robot.Profile] == nil {
					vsNonRobotProfileHands[robot.Profile] = make(map[[2]string]bool)
				}
				vsNonRobotProfileHands[robot.Profile][handKey] = true
			}
			update(vsNonRobotOverall, robotProfit, bigBlind)
			vsNonRobotHands = append(vsNonRobotHands, nonRobotHand{
				TableID: tableID, HandID: handID, RobotPlayers: len(robots), NonRobotPlayers: nonRobotPlayers,
				RobotDetails: strings.Join(robotDetails, ";"), NonRobotDetails: strings.Join(nonRobotDetails, ";"),
				RobotProfit: robotProfit, NonRobotProfit: nonRobotProfit, BigBlind: bigBlind,
			})
		}
	}
	if err := writeCSV(*output, len(settlements), overall, overallProfiles, overallPlayers, overallPlayerProfiles, tableHands, tables, profileMetrics, tablePlayerMetrics, playerMetrics, unprofiled); err != nil {
		fatal(err)
	}
	if err := writeNonRobotCSV(*nonRobotOutput, vsNonRobotOverall, vsNonRobotProfiles, vsNonRobotProfileHands, vsNonRobotHands); err != nil {
		fatal(err)
	}
	fmt.Printf("tables=%d hands=%d bot_assignments=%d bot_profit=%.2f vs_non_robot_hands=%d vs_non_robot_profit=%.2f conflicting_end_hands=%d report=%s non_robot_report=%s\n", len(tables), len(settlements), sumAssignments(tables), sumProfit(tables), vsNonRobotOverall.Assignments, vsNonRobotOverall.Profit, conflictingSettlements, *output, *nonRobotOutput)
}

func selectSettlement(votes map[string]*settlementVote) (endPayload, bool) {
	keys := sortedKeys(votes)
	selected := endPayload{}
	selectedCount := -1
	for _, voteKey := range keys {
		if votes[voteKey].Count > selectedCount {
			selected = votes[voteKey].Payload
			selectedCount = votes[voteKey].Count
		}
	}
	return selected, len(votes) > 1
}

func replayFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func scan(files []string, visit func(journalRecord) error) error {
	for _, path := range files {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			var record journalRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				continue
			}
			if err := visit(record); err != nil {
				file.Close()
				return fmt.Errorf("%s: %w", path, err)
			}
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func metricFor[K comparable](metrics map[K]*metric, key K) *metric {
	if metrics[key] == nil {
		metrics[key] = &metric{}
	}
	return metrics[key]
}

func update(metric *metric, profit, bigBlind float64) {
	metric.Assignments++
	metric.Profit += profit
	if bigBlind > 0 {
		metric.ProfitBB += profit / bigBlind
		metric.BBSamples++
	}
	switch {
	case profit > 0:
		metric.Wins++
	case profit < 0:
		metric.Losses++
	default:
		metric.Ties++
	}
}

func writeCSV(path string, totalHands int, overall *metric, overallProfiles map[string]*metric, overallPlayers map[string]*metric, overallPlayerProfiles map[[2]string]*metric, tableHands map[string]map[string]bool, tables map[string]*metric, profiles map[[2]string]*metric, tablePlayers map[[2]string]*metric, playerProfiles map[[3]string]*metric, unprofiled map[string]*metric) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"scope", "table_id", "hands", "player_id", "ai_profile", "assignments", "wins", "losses", "ties", "net_profit", "bb_samples", "net_profit_bb", "bb_per_100"}); err != nil {
		return err
	}
	if err := writeMetric(writer, "overall", "", totalHands, "", "", overall); err != nil {
		return err
	}
	for _, profile := range sortedKeys(overallProfiles) {
		if err := writeMetric(writer, "overall_profile", "", totalHands, "", profile, overallProfiles[profile]); err != nil {
			return err
		}
	}
	for _, playerID := range sortedKeys(overallPlayers) {
		if err := writeMetric(writer, "overall_player", "", totalHands, playerID, "", overallPlayers[playerID]); err != nil {
			return err
		}
	}
	overallPlayerKeys := make([][2]string, 0, len(overallPlayerProfiles))
	for playerKey := range overallPlayerProfiles {
		overallPlayerKeys = append(overallPlayerKeys, playerKey)
	}
	sort.Slice(overallPlayerKeys, func(i, j int) bool {
		if overallPlayerKeys[i][0] != overallPlayerKeys[j][0] {
			return overallPlayerKeys[i][0] < overallPlayerKeys[j][0]
		}
		return overallPlayerKeys[i][1] < overallPlayerKeys[j][1]
	})
	for _, playerKey := range overallPlayerKeys {
		if err := writeMetric(writer, "overall_player_profile", "", totalHands, playerKey[0], playerKey[1], overallPlayerProfiles[playerKey]); err != nil {
			return err
		}
	}
	tableIDs := sortedKeys(tables)
	for _, tableID := range tableIDs {
		if err := writeMetric(writer, "table", tableID, len(tableHands[tableID]), "", "", tables[tableID]); err != nil {
			return err
		}
		profileKeys := make([][2]string, 0)
		for profileKey := range profiles {
			if profileKey[0] == tableID {
				profileKeys = append(profileKeys, profileKey)
			}
		}
		sort.Slice(profileKeys, func(i, j int) bool { return profileKeys[i][1] < profileKeys[j][1] })
		for _, profileKey := range profileKeys {
			if err := writeMetric(writer, "table_profile", tableID, len(tableHands[tableID]), "", profileKey[1], profiles[profileKey]); err != nil {
				return err
			}
		}
		tablePlayerKeys := make([][2]string, 0)
		for playerKey := range tablePlayers {
			if playerKey[0] == tableID {
				tablePlayerKeys = append(tablePlayerKeys, playerKey)
			}
		}
		sort.Slice(tablePlayerKeys, func(i, j int) bool { return tablePlayerKeys[i][1] < tablePlayerKeys[j][1] })
		for _, playerKey := range tablePlayerKeys {
			if err := writeMetric(writer, "table_player", tableID, len(tableHands[tableID]), playerKey[1], "", tablePlayers[playerKey]); err != nil {
				return err
			}
		}
		playerKeys := make([][3]string, 0)
		for playerKey := range playerProfiles {
			if playerKey[0] == tableID {
				playerKeys = append(playerKeys, playerKey)
			}
		}
		sort.Slice(playerKeys, func(i, j int) bool {
			if playerKeys[i][1] != playerKeys[j][1] {
				return playerKeys[i][1] < playerKeys[j][1]
			}
			return playerKeys[i][2] < playerKeys[j][2]
		})
		for _, playerKey := range playerKeys {
			if err := writeMetric(writer, "table_player_profile", tableID, len(tableHands[tableID]), playerKey[1], playerKey[2], playerProfiles[playerKey]); err != nil {
				return err
			}
		}
		if unprofiled[tableID] != nil {
			if err := writeMetric(writer, "unprofiled", tableID, len(tableHands[tableID]), "", "", unprofiled[tableID]); err != nil {
				return err
			}
		}
	}
	return writer.Error()
}

func writeNonRobotCSV(path string, overall *metric, profiles map[string]*metric, profileHands map[string]map[[2]string]bool, hands []nonRobotHand) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	header := []string{"scope", "table_id", "hand_id", "ai_profile", "hands", "assignments", "robot_players", "non_robot_players", "robot_details", "non_robot_details", "wins", "losses", "ties", "robot_net_profit", "non_robot_net_profit", "settlement_gap", "non_robot_net_profit_bb", "settlement_gap_bb", "bb_samples", "robot_net_profit_bb", "bb_per_100"}
	if err := writer.Write(header); err != nil {
		return err
	}
	nonRobotProfit := 0.0
	nonRobotProfitBB := 0.0
	settlementGapBB := 0.0
	for _, hand := range hands {
		nonRobotProfit += hand.NonRobotProfit
		if hand.BigBlind > 0 {
			nonRobotProfitBB += hand.NonRobotProfit / hand.BigBlind
			settlementGapBB += (hand.RobotProfit + hand.NonRobotProfit) / hand.BigBlind
		}
	}
	if err := writeNonRobotMetric(writer, "overall", "", "", "", overall.Assignments, overall, "", "", "", "", formatProfit(nonRobotProfit), formatProfit(overall.Profit+nonRobotProfit), formatBB(nonRobotProfitBB), formatBB(settlementGapBB)); err != nil {
		return err
	}
	for _, profile := range sortedKeys(profiles) {
		if err := writeNonRobotMetric(writer, "profile", "", "", profile, len(profileHands[profile]), profiles[profile], "", "", "", "", "", "", "", ""); err != nil {
			return err
		}
	}
	sort.Slice(hands, func(i, j int) bool {
		if hands[i].TableID != hands[j].TableID {
			return hands[i].TableID < hands[j].TableID
		}
		left, leftErr := strconv.Atoi(hands[i].HandID)
		right, rightErr := strconv.Atoi(hands[j].HandID)
		if leftErr == nil && rightErr == nil {
			return left < right
		}
		return hands[i].HandID < hands[j].HandID
	})
	for _, hand := range hands {
		item := &metric{}
		update(item, hand.RobotProfit, hand.BigBlind)
		nonRobotProfitBB, settlementGapBB := "", ""
		if hand.BigBlind > 0 {
			nonRobotProfitBB = formatBB(hand.NonRobotProfit / hand.BigBlind)
			settlementGapBB = formatBB((hand.RobotProfit + hand.NonRobotProfit) / hand.BigBlind)
		}
		if err := writeNonRobotMetric(writer, "hand", hand.TableID, hand.HandID, "", 1, item, strconv.Itoa(hand.RobotPlayers), strconv.Itoa(hand.NonRobotPlayers), hand.RobotDetails, hand.NonRobotDetails, formatProfit(hand.NonRobotProfit), formatProfit(hand.RobotProfit+hand.NonRobotProfit), nonRobotProfitBB, settlementGapBB); err != nil {
			return err
		}
	}
	return writer.Error()
}

func writeNonRobotMetric(writer *csv.Writer, scope, tableID, handID, profile string, hands int, item *metric, robotPlayers, nonRobotPlayers, robotDetails, nonRobotDetails, nonRobotProfit, settlementGap, nonRobotProfitBB, settlementGapBB string) error {
	bbPer100 := 0.0
	if item.BBSamples > 0 {
		bbPer100 = item.ProfitBB / float64(item.BBSamples) * 100
	}
	return writer.Write([]string{
		scope, tableID, handID, profile, strconv.Itoa(hands), strconv.Itoa(item.Assignments), robotPlayers, nonRobotPlayers, robotDetails, nonRobotDetails,
		strconv.Itoa(item.Wins), strconv.Itoa(item.Losses), strconv.Itoa(item.Ties),
		formatProfit(item.Profit), nonRobotProfit, settlementGap, nonRobotProfitBB, settlementGapBB,
		strconv.Itoa(item.BBSamples), strconv.FormatFloat(item.ProfitBB, 'f', 4, 64), strconv.FormatFloat(bbPer100, 'f', 4, 64),
	})
}

func formatProfit(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func formatBB(value float64) string {
	return strconv.FormatFloat(value, 'f', 4, 64)
}

func writeMetric(writer *csv.Writer, scope, tableID string, hands int, playerID, profile string, metric *metric) error {
	bbPer100 := 0.0
	if metric.BBSamples > 0 {
		bbPer100 = metric.ProfitBB / float64(metric.BBSamples) * 100
	}
	return writer.Write([]string{
		scope, tableID, strconv.Itoa(hands), playerID, profile,
		strconv.Itoa(metric.Assignments), strconv.Itoa(metric.Wins), strconv.Itoa(metric.Losses), strconv.Itoa(metric.Ties),
		strconv.FormatFloat(metric.Profit, 'f', 2, 64), strconv.Itoa(metric.BBSamples), strconv.FormatFloat(metric.ProfitBB, 'f', 4, 64), strconv.FormatFloat(bbPer100, 'f', 4, 64),
	})
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sumAssignments(metrics map[string]*metric) int {
	total := 0
	for _, metric := range metrics {
		total += metric.Assignments
	}
	return total
}

func sumProfit(metrics map[string]*metric) float64 {
	total := 0.0
	for _, metric := range metrics {
		total += metric.Profit
	}
	return total
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
