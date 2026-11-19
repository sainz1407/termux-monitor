package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"termux-monitor/internal/logger"
	"termux-monitor/internal/modem"
)

func HandleModemSignal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sig, err := modem.GetSignalInfo()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(sig)
}

func HandleModemSimStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	st := modem.GetSimStatus()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(st)
}

func HandleModemSimSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Slot int `json:"slot"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	if req.Slot < 0 || req.Slot > 1 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid slot, must be 0 or 1"})
		return
	}

	res, err := modem.SwitchDataSim(req.Slot)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		logger.Get().Errorf("Modem", "Failed to switch data SIM: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func HandleModemThermal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	st, err := modem.Get5GThermalStatus()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		logger.Get().Errorf("Modem", "Failed to get thermal status: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(st)
}

func HandleModemCellScan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	targetSlot := 0
	slotStr := r.URL.Query().Get("slot")
	if slotStr == "" {
		slotStr = r.URL.Query().Get("sim_slot")
	}
	if s, err := strconv.Atoi(slotStr); err == nil && (s == 0 || s == 1) {
		targetSlot = s
	} else {
		// Use currently active mobile data slot by default
		simSt := modem.GetSimStatus()
		targetSlot = simSt.ActiveSlot
	}

	res, err := modem.ScanNearbyCells(targetSlot)
	if err != nil {
		logger.Get().Warnf("Modem", "Cell scan failed on slot %d: %v", targetSlot, err)
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       false,
			"error_message": err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}
