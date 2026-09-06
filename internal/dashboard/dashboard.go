package dashboard

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gitlab.com/smoothsics/ainp/internal/audit"
	"gitlab.com/smoothsics/ainp/internal/config"
	"gopkg.in/yaml.v3"
)

//go:embed index.html
var assets embed.FS

type FileStatus struct {
	Path    string    `json:"path"`
	Exists  bool      `json:"exists"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time,omitempty"`
}

type Status struct {
	StartedAt        time.Time  `json:"started_at"`
	UptimeSeconds    int64      `json:"uptime_seconds"`
	Running          bool       `json:"running"`
	LastStartedAt    time.Time  `json:"last_started_at,omitempty"`
	LastFinishedAt   time.Time  `json:"last_finished_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	ReportAvailable  bool       `json:"report_available"`
	ReportGenerated  time.Time  `json:"report_generated_at,omitempty"`
	ReportPassed     bool       `json:"report_passed"`
	RefreshInterval  string     `json:"refresh_interval"`
	Log              FileStatus `json:"log"`
	ExpectationsPath string     `json:"expectations_path"`
	ReportPath       string     `json:"report_path"`
}

type Manager struct {
	cfg       config.AdminConfig
	logger    *slog.Logger
	startedAt time.Time

	mu             sync.RWMutex
	running        bool
	lastStartedAt  time.Time
	lastFinishedAt time.Time
	lastError      string
	report         *audit.Report
	stop           chan struct{}
	closeOnce      sync.Once
}

func NewManager(cfg config.AdminConfig, logger *slog.Logger) *Manager {
	manager := &Manager{cfg: cfg, logger: logger, startedAt: time.Now(), stop: make(chan struct{})}
	if report, err := readReport(cfg.ReportPath); err == nil {
		manager.report = &report
	} else if !errors.Is(err, os.ErrNotExist) {
		manager.lastError = fmt.Sprintf("load existing report: %v", err)
	}
	if cfg.RefreshInterval > 0 {
		go manager.periodicRefresh()
	}
	return manager
}

func HTML() ([]byte, error) { return assets.ReadFile("index.html") }

func (m *Manager) Close() { m.closeOnce.Do(func() { close(m.stop) }) }

func (m *Manager) Trigger() bool {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return false
	}
	m.running = true
	m.lastStartedAt = time.Now()
	m.lastError = ""
	m.mu.Unlock()
	go m.analyze()
	return true
}

func (m *Manager) Report() (audit.Report, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.report == nil {
		return audit.Report{}, false
	}
	return *m.report, true
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	status := Status{
		StartedAt: m.startedAt, UptimeSeconds: int64(time.Since(m.startedAt).Seconds()),
		Running: m.running, LastStartedAt: m.lastStartedAt, LastFinishedAt: m.lastFinishedAt,
		LastError: m.lastError, RefreshInterval: m.cfg.RefreshInterval.String(),
		ExpectationsPath: m.cfg.ExpectationsPath, ReportPath: m.cfg.ReportPath,
	}
	if m.report != nil {
		status.ReportAvailable = true
		status.ReportGenerated = m.report.GeneratedAt
		status.ReportPassed = m.report.Passed
	}
	m.mu.RUnlock()
	status.Log = statFile(m.cfg.LogPath)
	return status
}

func (m *Manager) periodicRefresh() {
	ticker := time.NewTicker(m.cfg.RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.Trigger()
		case <-m.stop:
			return
		}
	}
}

func (m *Manager) analyze() {
	report, err := analyze(m.cfg)
	finished := time.Now()
	m.mu.Lock()
	m.running = false
	m.lastFinishedAt = finished
	if err != nil {
		m.lastError = err.Error()
	} else {
		m.report = &report
		m.lastError = ""
	}
	m.mu.Unlock()
	if err != nil {
		m.logger.Error("admin_audit_failed", "error", err)
		return
	}
	m.logger.Info("admin_audit_completed", "passed", report.Passed, "lines", report.Lines, "report", m.cfg.ReportPath)
}

func analyze(cfg config.AdminConfig) (audit.Report, error) {
	data, err := os.ReadFile(cfg.ExpectationsPath)
	if err != nil {
		return audit.Report{}, fmt.Errorf("read expectations: %w", err)
	}
	var expected audit.Expectations
	if err := yaml.Unmarshal(data, &expected); err != nil {
		return audit.Report{}, fmt.Errorf("decode expectations: %w", err)
	}
	report, err := audit.Analyze(cfg.LogPath, expected)
	if err != nil {
		return audit.Report{}, err
	}
	if err := writeReport(cfg.ReportPath, report); err != nil {
		return audit.Report{}, err
	}
	return report, nil
}

func writeReport(path string, report audit.Report) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".audit-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func readReport(path string) (audit.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return audit.Report{}, err
	}
	var report audit.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return audit.Report{}, err
	}
	return report, nil
}

func statFile(path string) FileStatus {
	status := FileStatus{Path: path}
	info, err := os.Stat(path)
	if err != nil {
		return status
	}
	status.Exists = true
	status.Size = info.Size()
	status.ModTime = info.ModTime()
	return status
}
