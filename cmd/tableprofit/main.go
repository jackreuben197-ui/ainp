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
	AIProfile string `json:"ai_profile"`
}

type endPayload struct {
	Players []struct {
		PlayerID string  `json:"player_id"`
		Profit   float64 `json:"profit"`
	} `json:"players"`
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
}

func main() {
	input := flag.String("input", "", "directory containing replay JSONL files")
	output := flag.String("output", "", "output CSV path")
	exclude := flag.String("exclude-table", "", "comma-separated table IDs to exclude")
	flag.Parse()
	if *input == "" || *output == "" {
		fatal(fmt.Errorf("-input and -output are required"))
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
		return nil
	}); err != nil {
		fatal(err)
	}

	tableHands := make(map[string]map[string]bool)
	tables := make(map[string]*metric)
	profileMetrics := make(map[[2]string]*metric)
	playerMetrics := make(map[[3]string]*metric)
	unprofiled := make(map[string]*metric)
	seenHands := make(map[[2]string]bool)
	if err := scan(files, func(record journalRecord) error {
		if record.Event.Command != "end_hand" || excluded[record.Event.TableID] {
			return nil
		}
		handKey := [2]string{record.Event.TableID, record.Event.HandID}
		if seenHands[handKey] {
			return nil
		}
		seenHands[handKey] = true
		if tableHands[record.Event.TableID] == nil {
			tableHands[record.Event.TableID] = make(map[string]bool)
		}
		tableHands[record.Event.TableID][record.Event.HandID] = true
		var payload endPayload
		if err := json.Unmarshal(record.Event.Payload, &payload); err != nil {
			return err
		}
		for _, player := range payload.Players {
			profile := profiles[key{record.Event.TableID, record.Event.HandID, player.PlayerID}]
			if profile == "" {
				update(metricFor(unprofiled, record.Event.TableID), player.Profit)
				continue
			}
			update(metricFor(tables, record.Event.TableID), player.Profit)
			update(metricFor(profileMetrics, [2]string{record.Event.TableID, profile}), player.Profit)
			update(metricFor(playerMetrics, [3]string{record.Event.TableID, player.PlayerID, profile}), player.Profit)
		}
		return nil
	}); err != nil {
		fatal(err)
	}
	if err := writeCSV(*output, tableHands, tables, profileMetrics, playerMetrics, unprofiled); err != nil {
		fatal(err)
	}
	fmt.Printf("tables=%d hands=%d bot_assignments=%d bot_profit=%.2f report=%s\n", len(tables), len(seenHands), sumAssignments(tables), sumProfit(tables), *output)
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

func update(metric *metric, profit float64) {
	metric.Assignments++
	metric.Profit += profit
	switch {
	case profit > 0:
		metric.Wins++
	case profit < 0:
		metric.Losses++
	default:
		metric.Ties++
	}
}

func writeCSV(path string, tableHands map[string]map[string]bool, tables map[string]*metric, profiles map[[2]string]*metric, players map[[3]string]*metric, unprofiled map[string]*metric) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	if err := writer.Write([]string{"scope", "table_id", "hands", "player_id", "ai_profile", "assignments", "wins", "losses", "ties", "net_profit"}); err != nil {
		return err
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
		playerKeys := make([][3]string, 0)
		for playerKey := range players {
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
			if err := writeMetric(writer, "table_player_profile", tableID, len(tableHands[tableID]), playerKey[1], playerKey[2], players[playerKey]); err != nil {
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

func writeMetric(writer *csv.Writer, scope, tableID string, hands int, playerID, profile string, metric *metric) error {
	return writer.Write([]string{scope, tableID, strconv.Itoa(hands), playerID, profile, strconv.Itoa(metric.Assignments), strconv.Itoa(metric.Wins), strconv.Itoa(metric.Losses), strconv.Itoa(metric.Ties), strconv.FormatFloat(metric.Profit, 'f', 2, 64)})
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
