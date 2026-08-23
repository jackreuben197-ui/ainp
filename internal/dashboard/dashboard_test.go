package dashboard

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitlab.com/smoothsics/ainp/internal/config"
)

func TestManagerRunsAuditAndPublishesReport(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "ainp.log")
	expectPath := filepath.Join(directory, "audit.yaml")
	reportPath := filepath.Join(directory, "report.json")
	logLine := `{"msg":"strategy_decision","decision_id":"d1","table_id":"t1","hand_id":"h1","player_id":"bot","ai_profile":"FPCH_default","personality_id":"balanced","strategy_level":5,"street":"preflop","action":"raise","latency_us":10}` + "\n"
	if err := os.WriteFile(logPath, []byte(logLine), 0o600); err != nil {
		t.Fatal(err)
	}
	expectations := "minimum_strategy_decisions: 1\nmax_preflop_fold_rate: 1\nmax_preflop_aggression_rate: 1\nmax_http_error_rate: 1\nmax_strategy_p95_us: 100\nmax_http_p95_us: 100\n"
	if err := os.WriteFile(expectPath, []byte(expectations), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(config.AdminConfig{Enabled: true, LogPath: logPath, ExpectationsPath: expectPath, ReportPath: reportPath}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer manager.Close()
	if !manager.Trigger() || manager.Trigger() {
		t.Fatal("manager must start once and reject a concurrent refresh")
	}
	deadline := time.Now().Add(2 * time.Second)
	for manager.Status().Running && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	report, ok := manager.Report()
	if !ok || !report.Passed || report.StrategyDecisions != 1 {
		t.Fatalf("report=%+v available=%v status=%+v", report, ok, manager.Status())
	}
	if info, err := os.Stat(reportPath); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("report file info=%v error=%v", info, err)
	}
}

func TestEmbeddedDashboardIsAvailable(t *testing.T) {
	page, err := HTML()
	if err != nil || len(page) < 1000 {
		t.Fatalf("dashboard bytes=%d error=%v", len(page), err)
	}
	html := string(page)
	for _, want := range []string{`id="special-losses"`, `id="decision-modal"`, `id="special-loss-decisions"`, `id="river-pair-examples"`, `data-help="river_missed_draw_calls"`, `river_repeated_missed_draw_board_pair_calls`, `missed_straight_draw`, `迟到补发玩家事件流`, `id="late-stream-summary"`, `late_streams_after_table_progress`, `after_table_progress`, `table_seq_at_start`, `data-special-profile`, `class="stack"`, `overflow-x:hidden`} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
}
