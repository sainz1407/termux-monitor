package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"termux-monitor/internal/handlers"
)

func TestHandleProcesses(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/processes", nil)
	rr := httptest.NewRecorder()
	handlers.HandleProcesses(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var payload struct {
		Processes []map[string]interface{} `json:"processes"`
		Count     int                      `json:"count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed decoding the response: %v", err)
	}
	if payload.Count != len(payload.Processes) {
		t.Errorf("expected the count to match the list, got %d and %d", payload.Count, len(payload.Processes))
	}

	// Wrong method
	reqPost := httptest.NewRequest("POST", "/api/processes", nil)
	rrPost := httptest.NewRecorder()
	handlers.HandleProcesses(rrPost, reqPost)
	if rrPost.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rrPost.Code)
	}
}

func TestHandleProcessKill_Validation(t *testing.T) {
	// Wrong method
	reqGet := httptest.NewRequest("GET", "/api/processes/kill", nil)
	rrGet := httptest.NewRecorder()
	handlers.HandleProcessKill(rrGet, reqGet)
	if rrGet.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rrGet.Code)
	}

	// Invalid payload
	reqInvalid := httptest.NewRequest("POST", "/api/processes/kill", bytes.NewBufferString("not json"))
	rrInvalid := httptest.NewRecorder()
	handlers.HandleProcessKill(rrInvalid, reqInvalid)
	if rrInvalid.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rrInvalid.Code)
	}

	// Missing PID
	reqMissing := httptest.NewRequest("POST", "/api/processes/kill", bytes.NewBufferString(`{"pid":0}`))
	rrMissing := httptest.NewRecorder()
	handlers.HandleProcessKill(rrMissing, reqMissing)
	if rrMissing.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rrMissing.Code)
	}
}

// TestHandleProcessKill_ForceRefusesOwnProcess makes sure the forced stop
// goes through the immediate kill path and still refuses the daemon itself.
func TestHandleProcessKill_ForceRefusesOwnProcess(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("the proc filesystem is not available on this platform")
	}

	payload, _ := json.Marshal(map[string]any{"pid": os.Getpid(), "force": true})
	req := httptest.NewRequest("POST", "/api/processes/kill", bytes.NewBuffer(payload))
	rr := httptest.NewRecorder()
	handlers.HandleProcessKill(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var response struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed decoding the response: %v", err)
	}
	if response.Success {
		t.Error("expected the forced request to be refused")
	}
	if response.Error == "" {
		t.Error("expected an error message")
	}
}

// TestHandleProcessKill_RefusesOwnProcess makes sure the API never terminates
// the daemon itself.
func TestHandleProcessKill_RefusesOwnProcess(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("the proc filesystem is not available on this platform")
	}

	payload, _ := json.Marshal(map[string]int{"pid": os.Getpid()})
	req := httptest.NewRequest("POST", "/api/processes/kill", bytes.NewBuffer(payload))
	rr := httptest.NewRecorder()
	handlers.HandleProcessKill(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var response struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed decoding the response: %v", err)
	}
	if response.Success {
		t.Error("expected the request to be refused")
	}
	if response.Error == "" {
		t.Error("expected an error message")
	}
}
