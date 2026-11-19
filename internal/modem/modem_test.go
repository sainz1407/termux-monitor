package modem_test

import (
	"testing"

	"termux-monitor/internal/modem"
)

func TestGetSignalInfo(t *testing.T) {
	sig, err := modem.GetSignalInfo()
	if err != nil {
		t.Fatalf("unexpected error getting signal info: %v", err)
	}
	_ = sig
}

func TestCalculateSignalMetrics(t *testing.T) {
	sig := modem.SignalInfo{
		RSRP: -80,
		RSRQ: -8,
		SINR: 25,
	}

	modem.CalculateSignalMetrics(&sig)

	if sig.QualityRSRP != "Excellent" || sig.RSRPPct != 100 {
		t.Errorf("expected Excellent RSRP metrics, got quality=%s pct=%d", sig.QualityRSRP, sig.RSRPPct)
	}
	if sig.QualityRSRQ != "Excellent" || sig.RSRQPct != 100 {
		t.Errorf("expected Excellent RSRQ metrics, got quality=%s pct=%d", sig.QualityRSRQ, sig.RSRQPct)
	}
	if sig.QualitySINR != "High Speed" || sig.SINRPct != 100 {
		t.Errorf("expected High Speed SINR metrics, got quality=%s pct=%d", sig.QualitySINR, sig.SINRPct)
	}
}

func TestCalculateSignalMetricsUnknown(t *testing.T) {
	sig := modem.SignalInfo{RSRP: -999, RSRQ: -999, SINR: -999}
	modem.CalculateSignalMetrics(&sig)

	if sig.QualityRSRP != "Unknown" || sig.RSRPPct != 0 {
		t.Errorf("expected Unknown RSRP metrics, got quality=%s pct=%d", sig.QualityRSRP, sig.RSRPPct)
	}
	if sig.QualitySINR != "Unknown" || sig.SINRPct != 0 {
		t.Errorf("expected Unknown SINR metrics, got quality=%s pct=%d", sig.QualitySINR, sig.SINRPct)
	}
}
