package cron

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// RunRecord is one recorded job execution. It mirrors the OpenLocalPanel
// record so exported histories stay comparable.
type RunRecord struct {
	ID         string `json:"id"`
	JobID      string `json:"jobId"`
	Name       string `json:"name"`
	Command    string `json:"command"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	ExitCode   *int   `json:"exitCode"`
	TimedOut   bool   `json:"timedOut"`
	LatencyMs  int64  `json:"latencyMs"`
	Output     string `json:"output"`
}

// RunStoreCap bounds the retained history. The file is rewritten in full on
// every append, so a small cap keeps the cost predictable.
const RunStoreCap = 500

// RunStore is a bounded, newest-first history of executions.
type RunStore struct {
	mu   sync.Mutex
	path string
	runs []RunRecord
	// loaded guards the lazy first read so a missing or corrupt file is read
	// once and then answered from memory.
	loaded bool
}

// NewRunStore opens (or lazily reads) the history at path. A missing, empty or
// corrupt file simply means an empty history: run bookkeeping must never break
// the cron endpoints.
func NewRunStore(path string) *RunStore {
	return &RunStore{path: path}
}

// load reads the backing file once. Callers must hold the mutex.
func (s *RunStore) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil || len(data) == 0 {
		return
	}
	var runs []RunRecord
	if err := json.Unmarshal(data, &runs); err != nil {
		// Corrupt history is treated as no history rather than an error.
		return
	}
	if len(runs) > RunStoreCap {
		runs = runs[:RunStoreCap]
	}
	s.runs = runs
}

// List returns up to limit records, newest first.
func (s *RunStore) List(limit int) []RunRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	if limit < 1 {
		limit = 1
	}
	if limit > RunStoreCap {
		limit = RunStoreCap
	}
	if limit > len(s.runs) {
		limit = len(s.runs)
	}
	out := make([]RunRecord, limit)
	copy(out, s.runs[:limit])
	return out
}

// Append prepends a record and persists the trimmed history. Write failures are
// swallowed: the run already happened and the API answer must not depend on a
// full data partition.
func (s *RunStore) Append(run RunRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	s.runs = append([]RunRecord{run}, s.runs...)
	if len(s.runs) > RunStoreCap {
		s.runs = s.runs[:RunStoreCap]
	}
	if s.path == "" {
		return
	}
	data, err := json.Marshal(s.runs)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(s.path, data, 0o600)
}
