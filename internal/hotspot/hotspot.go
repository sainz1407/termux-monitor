package hotspot

import (
	"strings"
	"sync"
	"time"

	"termux-monitor/internal/config"
)

var (
	activeSSIDMu      sync.Mutex
	activeSSIDValue   string
	activeSSIDExpires time.Time
)

// GetHotspotStatus inspects network interfaces and system status to return tethering state.
func GetHotspotStatus() HotspotStatus {
	activeIfaces := GetActiveTetherInterfaces()

	var status HotspotStatus
	status.ActiveInterfaces = make([]string, 0, len(activeIfaces))
	ssid, _, secType := ReadSavedSoftApCredentials()
	status.SSID = ssid
	status.SecurityType = secType

	for _, ifc := range activeIfaces {
		status.ActiveInterfaces = append(status.ActiveInterfaces, ifc.Name)
		switch ifc.Type {
		case "wifi":
			status.WifiEnabled = true
			if status.GatewayIP == "" {
				status.GatewayIP = ifc.IP
				status.Subnet = ifc.CIDR
			}
		case "usb":
			status.UsbEnabled = true
			if status.GatewayIP == "" {
				status.GatewayIP = ifc.IP
				status.Subnet = ifc.CIDR
			}
		case "ethernet":
			status.EthernetEnabled = true
			if status.GatewayIP == "" {
				status.GatewayIP = ifc.IP
				status.Subnet = ifc.CIDR
			}
		}
	}

	// Hotspot is considered enabled if any Wi-Fi tethering interface is up
	// or if cmd wifi reports enabled
	if status.WifiEnabled {
		status.Enabled = true
	} else {
		out, err := config.ExecSuTimeout(2*time.Second, "cmd wifi status 2>/dev/null | grep -i 'Wifi AP'")
		if err == nil && strings.Contains(strings.ToLower(string(out)), "enabled") {
			status.Enabled = true
			status.WifiEnabled = true
		}
	}

	// The running hotspot may broadcast another name than the stored one, so
	// the live value of the wifi service replaces it while the hotspot is up.
	if status.WifiEnabled {
		if live := activeSoftApSSID(); live != "" {
			status.SSID = live
		}
	}

	return status
}

// activeSoftApSSID reads the SSID the running hotspot really broadcasts from
// the wifi service. The value is cached shortly to keep the status cheap.
func activeSoftApSSID() string {
	activeSSIDMu.Lock()
	defer activeSSIDMu.Unlock()

	if time.Now().Before(activeSSIDExpires) {
		return activeSSIDValue
	}

	activeSSIDValue = ""
	activeSSIDExpires = time.Now().Add(10 * time.Second)

	out, err := config.ExecSuTimeout(3*time.Second, "dumpsys wifi 2>/dev/null | grep -m1 'mApConfig.SSID'")
	if err != nil {
		return ""
	}

	line := strings.TrimSpace(string(out))
	_, value, found := strings.Cut(line, ":")
	if !found {
		return ""
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	activeSSIDValue = value
	activeSSIDExpires = time.Now().Add(10 * time.Second)
	return value
}
