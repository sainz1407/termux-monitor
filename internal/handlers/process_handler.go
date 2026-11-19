package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"termux-monitor/internal/logger"
	"termux-monitor/internal/procmon"
)

// HandleProcesses returns the running processes of the device.
func HandleProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query().Get("q")
	sortBy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
	if sortBy == "" {
		sortBy = "cpu"
	}

	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	procs := procmon.GetManager().List(query, sortBy, limit)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"processes": procs,
		"count":     len(procs),
		"sort":      sortBy,
	})
}

// HandleProcessKill terminates a running process.
func HandleProcessKill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		PID   int  `json:"pid"`
		Force bool `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "Invalid process request payload"})
		return
	}

	if payload.PID <= 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "A valid PID is required"})
		return
	}

	mgr := procmon.GetManager()

	// A forced stop goes straight to SIGKILL, a graceful stop waits for the
	// process to exit before escalating.
	var err error
	if payload.Force {
		err = mgr.Kill(payload.PID, true)
	} else {
		err = mgr.Terminate(payload.PID, 3*time.Second)
	}
	if err != nil {
		logger.Get().Warnf("Processes", "Failed to stop PID %d: %v", payload.PID, err)
	} else {
		logger.Get().Infof("Processes", "Process %d stopped", payload.PID)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": err == nil,
		"error":   errString(err),
	})
}
