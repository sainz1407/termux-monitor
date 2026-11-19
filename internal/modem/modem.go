// Package modem provides non-proprietary cellular & radio telemetry for the
// Termux Monitor control panel. It reads the Android telephony registry through
// the ported NetMonster parser and never talks to vendor baseband transports
// (Qualcomm QRTR/PDC, Samsung SecRIL, Tensor Shannon) directly.
package modem

// SignalInfo describes the currently serving cellular radio cell along with
// derived quality metrics. Fields are populated by QueryNetMonsterSignal and
// decorated by CalculateSignalMetrics.
type SignalInfo struct {
	RSRP          int    `json:"rsrp"`
	RSRQ          int    `json:"rsrq"`
	SINR          int    `json:"sinr"`
	RSSI          int    `json:"rssi"`
	Operator      string `json:"operator"`
	NetworkType   string `json:"network_type"` // "LTE", "NR_5G", "HSPA", "GSM"
	Band          string `json:"band"`
	CellID        string `json:"cell_id"`
	TAC           string `json:"tac"`
	PCI           string `json:"pci"`
	EARFCN        int    `json:"earfcn"`
	Bandwidth     string `json:"bandwidth"`
	QualityRSRP   string `json:"quality_rsrp"`
	QualityRSRQ   string `json:"quality_rsrq"`
	QualitySINR   string `json:"quality_sinr"`
	RSRPPct       int    `json:"rsrp_pct"`
	RSRQPct       int    `json:"rsrq_pct"`
	SINRPct       int    `json:"sinr_pct"`
	TimingAdvance int    `json:"timing_advance,omitempty"`
	DistanceMeter int    `json:"distance_meters,omitempty"`
	DistanceStr   string `json:"distance_str,omitempty"`
}

// CalculateSignalMetrics populates signal percentages and quality rating strings.
func CalculateSignalMetrics(sig *SignalInfo) {
	if sig.RSRP != -999 {
		if sig.RSRP >= -85 {
			sig.QualityRSRP = "Excellent"
			sig.RSRPPct = 100
		} else if sig.RSRP >= -95 {
			sig.QualityRSRP = "Good"
			sig.RSRPPct = 75
		} else if sig.RSRP >= -105 {
			sig.QualityRSRP = "Fair"
			sig.RSRPPct = 50
		} else {
			sig.QualityRSRP = "Poor"
			sig.RSRPPct = 25
		}
	} else {
		sig.QualityRSRP = "Unknown"
		sig.RSRPPct = 0
	}

	if sig.RSRQ != -999 {
		if sig.RSRQ >= -10 {
			sig.QualityRSRQ = "Excellent"
			sig.RSRQPct = 100
		} else if sig.RSRQ >= -15 {
			sig.QualityRSRQ = "Good"
			sig.RSRQPct = 75
		} else if sig.RSRQ >= -19 {
			sig.QualityRSRQ = "Fair"
			sig.RSRQPct = 50
		} else {
			sig.QualityRSRQ = "Poor"
			sig.RSRQPct = 25
		}
	} else {
		sig.QualityRSRQ = "Unknown"
		sig.RSRQPct = 0
	}

	if sig.SINR != -999 {
		if sig.SINR >= 20 {
			sig.QualitySINR = "High Speed"
			sig.SINRPct = 100
		} else if sig.SINR >= 13 {
			sig.QualitySINR = "Good"
			sig.SINRPct = 75
		} else if sig.SINR >= 0 {
			sig.QualitySINR = "Fair"
			sig.SINRPct = 50
		} else {
			sig.QualitySINR = "Poor"
			sig.SINRPct = 25
		}
	} else {
		sig.QualitySINR = "Unknown"
		sig.SINRPct = 0
	}
}

// GetSignalInfo reads the serving cell from the Android telephony registry and
// returns a fully decorated SignalInfo.
func GetSignalInfo() (SignalInfo, error) {
	simSt := GetSimStatus()
	activeSlot := 0
	if simSt != nil {
		activeSlot = simSt.ActiveSlot
	}

	sig := QueryNetMonsterSignal(activeSlot)
	CalculateSignalMetrics(&sig)
	return sig, nil
}
