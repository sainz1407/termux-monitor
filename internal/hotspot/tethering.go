package hotspot

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"termux-monitor/internal/config"
	"termux-monitor/internal/logger"
)

func notifyTetheringChange() {
	names := GetActiveInterfaceNames()
	if len(names) == 0 {
		// No tether interface is up: remove any Termux-owned gateway state.
		RemoveStaticGateway("")
	}
}

// ToggleTethering toggles specific tethering channel ("wifi", "usb", "ethernet").
func ToggleTethering(tetherType string, enable bool) error {
	tetherType = strings.ToLower(strings.TrimSpace(tetherType))

	switch tetherType {
	case "wifi", "softap":
		return ToggleHotspot(enable, "", "")
	case "usb", "rndis":
		return toggleUSBTethering(enable)
	case "ethernet", "eth":
		return toggleEthernetTethering(enable)
	default:
		return fmt.Errorf("unsupported tethering type: %s", tetherType)
	}
}

// tetheringShellSupport describes the shell surfaces the platform exposes.
type tetheringShellSupport struct {
	wifiSoftAP   bool // "cmd wifi start-softap", Android 12 and newer
	tetheringCmd bool // "cmd tethering start wifi", Android 11 and newer
}

var (
	shellSupportOnce sync.Once
	shellSupport     tetheringShellSupport
)

// detectTetheringShellSupport checks which tethering commands the platform
// provides. Android 8 to 10 expose neither of them.
func detectTetheringShellSupport() tetheringShellSupport {
	shellSupportOnce.Do(func() {
		if out, err := config.ExecSuTimeout(6*time.Second, "cmd wifi help 2>/dev/null"); err == nil {
			shellSupport.wifiSoftAP = strings.Contains(string(out), "start-softap")
		}
		if out, err := config.ExecSuTimeout(6*time.Second, "cmd tethering help 2>/dev/null"); err == nil {
			shellSupport.tetheringCmd = strings.Contains(string(out), "start")
		}
		logger.Get().Infof("hotspot", "tethering shell support: wifiSoftAP=%v tetheringCmd=%v",
			shellSupport.wifiSoftAP, shellSupport.tetheringCmd)
	})
	return shellSupport
}

// commandFailed reports whether the output of a shell command shows a failure.
func commandFailed(out string) bool {
	lower := strings.ToLower(out)
	for _, marker := range []string{"exception", "error", "unknown command", "usage:", "not found", "denied", "failed", "cannot"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// startSoftAP starts the Wi-Fi SoftAP with the best surface the platform offers.
func startSoftAP(ssid string, pass string, secType string, band string) error {
	support := detectTetheringShellSupport()

	if support.wifiSoftAP {
		if startSoftAPWithWifiShell(ssid, pass, secType, band) {
			return nil
		}
		logger.Get().Warnf("hotspot", "cmd wifi start-softap did not start the hotspot, falling back")
	}

	if support.tetheringCmd {
		out, err := config.ExecSuTimeout(10*time.Second, "cmd tethering start wifi 2>&1")
		if err == nil && !commandFailed(string(out)) {
			return nil
		}
		logger.Get().Warnf("hotspot", "cmd tethering start wifi failed: %s", strings.TrimSpace(string(out)))
	}

	// Android 8 to 10 only expose the connectivity service binder.
	return startTetheringViaBinder(tetherTypeWifi)
}

// startSoftAPWithWifiShell starts the SoftAP through the wifi shell command.
// The band option has to precede the positional arguments.
func startSoftAPWithWifiShell(ssid string, pass string, secType string, band string) bool {
	bandArg := ""
	if band != "" {
		bandArg = " -b " + band
	}

	candidates := []string{}
	if secType == "open" {
		candidates = append(candidates,
			"cmd wifi start-softap"+bandArg+" "+shellSingleQuote(ssid)+" open",
			"cmd wifi start-softap"+bandArg+" "+shellSingleQuote(ssid))
	} else {
		candidates = append(candidates,
			"cmd wifi start-softap"+bandArg+" "+shellSingleQuote(ssid)+" wpa2 "+shellSingleQuote(pass),
			"cmd wifi start-softap "+shellSingleQuote(ssid)+" wpa2 "+shellSingleQuote(pass))
	}

	for _, cmd := range candidates {
		out, err := config.ExecSuTimeout(10*time.Second, cmd+" 2>&1")
		if err == nil && !commandFailed(string(out)) {
			return true
		}
	}
	return false
}

// stopSoftAP stops the Wi-Fi SoftAP with the best surface the platform offers.
func stopSoftAP() error {
	support := detectTetheringShellSupport()

	if support.wifiSoftAP {
		out, err := config.ExecSuTimeout(10*time.Second, "cmd wifi stop-softap 2>&1")
		if err == nil && !commandFailed(string(out)) {
			return nil
		}
	}

	if support.tetheringCmd {
		out, err := config.ExecSuTimeout(10*time.Second, "cmd tethering stop wifi 2>&1")
		if err == nil && !commandFailed(string(out)) {
			return nil
		}
	}

	return stopTetheringViaBinder(tetherTypeWifi)
}

// toggleUSBTethering toggles USB RNDIS/NCM tethering mode.
func toggleUSBTethering(enable bool) error {
	support := detectTetheringShellSupport()

	if support.tetheringCmd {
		action := "stop"
		if enable {
			action = "start"
		}
		out, err := config.ExecSuTimeout(10*time.Second, "cmd tethering "+action+" usb 2>&1")
		if err == nil && !commandFailed(string(out)) {
			time.Sleep(1 * time.Second)
			notifyTetheringChange()
			return nil
		}
		logger.Get().Warnf("hotspot", "cmd tethering %s usb failed: %s", action, strings.TrimSpace(string(out)))
	}

	// Fall back to the USB gadget configuration plus the connectivity service.
	if enable {
		_, _ = config.ExecSuTimeout(3*time.Second, "svc usb setFunctions rndis 2>/dev/null || setprop sys.usb.config rndis,adb")
		if err := startTetheringViaBinder(tetherTypeUSB); err != nil {
			return err
		}
	} else {
		_, _ = config.ExecSuTimeout(3*time.Second, "svc usb setFunctions mtp 2>/dev/null || setprop sys.usb.config mtp,adb")
		if err := stopTetheringViaBinder(tetherTypeUSB); err != nil {
			return err
		}
	}

	time.Sleep(1 * time.Second)
	notifyTetheringChange()
	return nil
}

// toggleEthernetTethering toggles Ethernet OTG tethering.
func toggleEthernetTethering(enable bool) error {
	support := detectTetheringShellSupport()
	if !support.tetheringCmd {
		return fmt.Errorf("ethernet tethering is not supported by this Android version")
	}

	action := "stop"
	if enable {
		action = "start"
	}
	out, err := config.ExecSuTimeout(10*time.Second, "cmd tethering "+action+" ethernet 2>&1")
	if err != nil {
		return fmt.Errorf("failed to toggle Ethernet tethering: %w, out: %s", err, string(out))
	}
	if commandFailed(string(out)) {
		return fmt.Errorf("failed to toggle Ethernet tethering: %s", strings.TrimSpace(string(out)))
	}

	time.Sleep(1 * time.Second)
	notifyTetheringChange()
	return nil
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func escapeSedReplacement(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`&`, `\&`,
		`|`, `\|`,
		`'`, `'\''`,
	)
	return r.Replace(s)
}

// UpdateSavedSoftApCredentials persists new SSID, Passphrase and SecurityType to AOSP XML stores.
func UpdateSavedSoftApCredentials(newSSID, newPass, securityType string) error {
	if err := ValidateSoftApCredentials(newSSID, newPass, securityType); err != nil {
		return err
	}

	for _, p := range softApXMLCandidatePaths {
		if newSSID != "" {
			cleanSSID := escapeSedReplacement(strings.ReplaceAll(newSSID, "\"", ""))
			cmdSSID := fmt.Sprintf("sed -i 's|<string name=\"WifiSsid\">.*</string>|<string name=\"WifiSsid\">&quot;%s&quot;</string>|' %s 2>/dev/null", cleanSSID, shellSingleQuote(p))
			_, _ = config.ExecSuTimeout(2*time.Second, cmdSSID)
		}

		if securityType == "open" || newPass == "none" {
			cmdSec := fmt.Sprintf("sed -i 's|<int name=\"SecurityType\" value=\".*\" />|<int name=\"SecurityType\" value=\"0\" />|' %s 2>/dev/null", shellSingleQuote(p))
			cmdPass := fmt.Sprintf("sed -i 's|<string name=\"Passphrase\">.*</string>|<string name=\"Passphrase\"></string>|' %s 2>/dev/null", shellSingleQuote(p))
			_, _ = config.ExecSuTimeout(2*time.Second, cmdSec)
			_, _ = config.ExecSuTimeout(2*time.Second, cmdPass)
		} else if newPass != "" {
			cleanPass := escapeSedReplacement(newPass)
			cmdSec := fmt.Sprintf("sed -i 's|<int name=\"SecurityType\" value=\".*\" />|<int name=\"SecurityType\" value=\"1\" />|' %s 2>/dev/null", shellSingleQuote(p))
			cmdPass := fmt.Sprintf("sed -i 's|<string name=\"Passphrase\">.*</string>|<string name=\"Passphrase\">%s</string>|' %s 2>/dev/null", cleanPass, shellSingleQuote(p))
			_, _ = config.ExecSuTimeout(2*time.Second, cmdSec)
			_, _ = config.ExecSuTimeout(2*time.Second, cmdPass)
		}
	}

	if newSSID != "" {
		_, _ = config.ExecSuTimeout(2*time.Second, fmt.Sprintf("settings put global softap_ssid %s", shellSingleQuote(newSSID)))
	}
	if securityType == "open" || newPass == "none" {
		_, _ = config.ExecSuTimeout(2*time.Second, "settings put global softap_passphrase ''")
	} else if newPass != "" {
		_, _ = config.ExecSuTimeout(2*time.Second, fmt.Sprintf("settings put global softap_passphrase %s", shellSingleQuote(newPass)))
	}

	return nil
}

// ToggleHotspot enables or disables Wi-Fi Hotspot with persistent credential updates.
func ToggleHotspot(enable bool, ssid string, pass string, security ...string) error {
	// 1. If disabling, stop the radio and clean up Termux-owned gateway state
	if !enable {
		// Remove the Termux-owned address and rules first: once the radio stops,
		// the interface disappears and the objects can no longer be addressed.
		RemoveStaticGateway("")
		if err := stopSoftAP(); err != nil {
			logger.Get().Warnf("hotspot", "unable to stop the hotspot: %v", err)
		}
		go func() {
			time.Sleep(1 * time.Second)
			notifyTetheringChange()
		}()
		return nil
	}
	// 2. If enabling, resolve credentials & router config
	routerCfg, _ := LoadRouterConfig()
	var savedSSID, savedPass, savedSec string
	if routerCfg != nil && routerCfg.SSID != "" {
		savedSSID = routerCfg.SSID
		savedPass = routerCfg.Password
		savedSec = routerCfg.SecurityType
	} else {
		savedSSID, savedPass, savedSec = ReadSavedSoftApCredentials()
	}

	if ssid != "" {
		if !reHotspotSSID.MatchString(ssid) {
			return fmt.Errorf("invalid SSID: must be 1-32 alphanumeric characters, spaces, underscores, or hyphens")
		}
	} else {
		ssid = savedSSID
	}

	secType := savedSec
	if len(security) > 0 && security[0] != "" {
		secType = strings.ToLower(security[0])
	}

	if secType == "open" || pass == "none" {
		secType = "open"
		pass = "none"
	} else {
		secType = "wpa2"
		if pass != "" {
			if !reHotspotPassphrase.MatchString(pass) {
				return fmt.Errorf("invalid passphrase: must be 8-63 valid characters")
			}
		} else {
			pass = savedPass
			if pass == "" {
				pass = "12345678"
			}
		}
	}

	// Persist to Termux router config store
	if routerCfg != nil {
		routerCfg.SSID = ssid
		routerCfg.Password = pass
		routerCfg.SecurityType = secType
		_ = SaveRouterConfig(routerCfg)
	}

	// Persist to XML store so reboot / UI stays in sync
	_ = UpdateSavedSoftApCredentials(ssid, pass, secType)

	// 3. Resolve Band flag (-b 2, -b 5, or omitted for any)
	band := ""
	if routerCfg != nil && routerCfg.Band != "" && routerCfg.Band != "any" {
		band = routerCfg.Band
	}

	// 4. Start the SoftAP through the surface the platform provides
	if err := startSoftAP(ssid, pass, secType, band); err != nil {
		return fmt.Errorf("failed to start the hotspot: %w", err)
	}

	// 5. Post-startup: Apply static gateway only when in static mode.
	go func() {
		time.Sleep(2 * time.Second)
		if routerCfg != nil && routerCfg.GatewayMode == "static" {
			names := GetActiveInterfaceNames()
			if len(names) > 0 {
				_ = ApplyStaticGateway(names[0], routerCfg)
			}
		}
		notifyTetheringChange()
	}()

	return nil
}
