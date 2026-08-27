package replay

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gitlab.com/ubenbill/ainp/internal/protocol"
)

const SchemaVersion = 1

type Record struct {
	SchemaVersion int                    `json:"schema_version"`
	RecordedAt    time.Time              `json:"recorded_at"`
	RequestID     string                 `json:"request_id"`
	DecisionID    string                 `json:"decision_id"`
	Fingerprint   string                 `json:"fingerprint"`
	Provider      string                 `json:"provider"`
	PolicyVersion string                 `json:"policy_version,omitempty"`
	Outcome       string                 `json:"outcome"`
	ErrorCode     protocol.ErrorCode     `json:"error_code,omitempty"`
	ErrorMessage  string                 `json:"error_message,omitempty"`
	Event         protocol.EventRequest  `json:"event"`
	Response      protocol.EventResponse `json:"response"`
}

type Recorder interface {
	Record(Record) error
	Close() error
	Path() string
}

type JSONLRecorder struct {
	mu        sync.Mutex
	file      *os.File
	writer    *bufio.Writer
	path      string
	flushEach bool
}

func NewJSONLRecorder(directory, prefix string, flushEach bool, now time.Time) (*JSONLRecorder, error) {
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, fmt.Errorf("create replay directory: %w", err)
	}
	path := filepath.Join(directory, fmt.Sprintf("%s-%s.jsonl", prefix, now.Format("20060102T150405.000000000")))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return nil, fmt.Errorf("create replay journal: %w", err)
	}
	return &JSONLRecorder{file: file, writer: bufio.NewWriterSize(file, 64*1024), path: path, flushEach: flushEach}, nil
}

func (r *JSONLRecorder) Record(record Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return fmt.Errorf("replay recorder is closed")
	}
	if record.SchemaVersion == 0 {
		record.SchemaVersion = SchemaVersion
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode replay record: %w", err)
	}
	if _, err = r.writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write replay record: %w", err)
	}
	if r.flushEach {
		return r.writer.Flush()
	}
	return nil
}

func (r *JSONLRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return nil
	}
	flushErr := r.writer.Flush()
	closeErr := r.file.Close()
	r.file = nil
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

func (r *JSONLRecorder) Path() string { return r.path }
