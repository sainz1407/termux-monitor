package procmon

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
)

// procStat holds the parsed content of a /proc/<pid>/stat file.
type procStat struct {
	name   string
	fields []string
}

// readProc reads a file below /proc and returns its content as a string.
func readProc(path string) string {
	buf, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(buf)
}

// parseProcStat splits the stat file into the process name and the fields
// that follow it. The name is wrapped in parentheses and may contain spaces,
// so the fields are only split after the closing parenthesis.
func parseProcStat(raw string) (string, []string) {
	open := strings.Index(raw, "(")
	closeIdx := strings.LastIndex(raw, ")")
	if open < 0 || closeIdx < open {
		return "", nil
	}

	name := raw[open+1 : closeIdx]
	rest := strings.TrimSpace(raw[closeIdx+1:])
	if rest == "" {
		return name, nil
	}
	return name, strings.Fields(rest)
}

// stateName maps the state field of the stat file to a readable label.
func stateName(state string) string {
	switch state {
	case "R":
		return "running"
	case "S":
		return "sleeping"
	case "D":
		return "disk-sleep"
	case "Z":
		return "zombie"
	case "T":
		return "stopped"
	case "t":
		return "tracing"
	case "X", "x":
		return "dead"
	case "K":
		return "wakekill"
	case "W":
		return "waking"
	case "I":
		return "idle"
	default:
		return state
	}
}

// totalMemory returns the physical memory of the device in bytes.
func totalMemory() int64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb := parseInt64(fields[1])
		return kb * 1024
	}
	return 0
}

// totalTicks returns the accumulated clock ticks of all CPU cores.
func totalTicks() int64 {
	raw := readProc("/proc/stat")
	if raw == "" {
		return 0
	}
	for line := range strings.SplitSeq(raw, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		var total int64
		for _, field := range strings.Fields(line)[1:] {
			total += parseInt64(field)
		}
		return total
	}
	return 0
}

// systemUptime returns the uptime of the device in seconds.
func systemUptime() float64 {
	raw := readProc("/proc/uptime")
	if raw == "" {
		return 0
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return 0
	}
	uptime, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return uptime
}

// statusValue returns the first value of a status field of a process.
func statusValue(pid int, key string) string {
	raw := readProc("/proc/" + strconv.Itoa(pid) + "/status")
	if raw == "" {
		return ""
	}
	for line := range strings.SplitSeq(raw, "\n") {
		if !strings.HasPrefix(line, key+":") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, key+":"))
		if len(fields) == 0 {
			return ""
		}
		return fields[0]
	}
	return ""
}

// statusUID returns the real user id of a process.
func statusUID(pid int) int {
	raw := readProc("/proc/" + strconv.Itoa(pid) + "/status")
	if raw == "" {
		return -1
	}
	for line := range strings.SplitSeq(raw, "\n") {
		if !strings.HasPrefix(line, "Uid:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "Uid:"))
		if len(fields) == 0 {
			return -1
		}
		uid, err := strconv.Atoi(fields[0])
		if err != nil {
			return -1
		}
		return uid
	}
	return -1
}

// statusRSS returns the resident set size of a process in bytes.
func statusRSS(pid int) int64 {
	value := statusValue(pid, "VmRSS")
	if value == "" {
		return 0
	}
	return parseInt64(value) * 1024
}

// threadCount returns the number of threads of a process.
func threadCount(pid int) int {
	value := statusValue(pid, "Threads")
	if value == "" {
		return 0
	}
	count, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return count
}

// exePath returns the absolute path of the executable of a process.
func exePath(pid int) string {
	path, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(path, " (deleted)")
}

// androidSystemUsers maps the well known Android system user ids to their
// names. They are not listed in the passwd database of the device.
var androidSystemUsers = map[int]string{
	1000: "system",
	1001: "radio",
	1002: "bluetooth",
	1003: "graphics",
	1004: "input",
	1005: "audio",
	1006: "camera",
	1007: "log",
	1008: "compass",
	1009: "mount",
	1010: "wifi",
	1011: "adb",
	1012: "install",
	1013: "media",
	1014: "dhcp",
	1015: "sdcard_rw",
	1016: "vpn",
	1017: "keystore",
	1018: "usb",
	1019: "drm",
	1020: "mdnsr",
	1021: "gps",
	2000: "shell",
	2001: "cache",
	2002: "diag",
	9997: "everybody",
}

// userName maps a user id to a readable user name. Android application
// processes are named after their user and application id.
func userName(uid int) string {
	if uid < 0 {
		return "unknown"
	}
	if uid == 0 {
		return "root"
	}
	if name, ok := passwdNames()[uid]; ok {
		return name
	}
	if name, ok := androidSystemUsers[uid]; ok {
		return name
	}

	// Android assigns the application ids 10000-19999 to the first user, the
	// user id is the leading part of the user id.
	appID := uid % 100000
	if appID >= 10000 {
		return "u" + strconv.Itoa(uid/100000) + "_a" + strconv.Itoa(appID-10000)
	}
	return "uid" + strconv.Itoa(uid)
}

var (
	passwdOnce  sync.Once
	passwdCache map[int]string
)

// passwdNames reads the user database of the device.
func passwdNames() map[int]string {
	passwdOnce.Do(func() {
		passwdCache = make(map[int]string)
		for _, path := range []string{"/etc/passwd", "/system/etc/passwd"} {
			file, err := os.Open(path)
			if err != nil {
				continue
			}
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				parts := strings.Split(scanner.Text(), ":")
				if len(parts) < 3 {
					continue
				}
				uid, err := strconv.Atoi(parts[2])
				if err != nil {
					continue
				}
				if _, ok := passwdCache[uid]; !ok && parts[0] != "" {
					passwdCache[uid] = parts[0]
				}
			}
			file.Close()
		}
	})
	return passwdCache
}

// firstToken returns the first word of a command line.
func firstToken(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// baseName returns the last element of a Unix path.
func baseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// parseInt64 parses an integer and returns 0 for invalid input.
func parseInt64(value string) int64 {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

// round1 rounds a float to a single decimal place.
func round1(value float64) float64 {
	return float64(int64(value*10+0.5)) / 10
}

// formatBytes renders a byte count in a human readable form.
func formatBytes(value int64) string {
	const unit = 1024
	if value < unit {
		return strconv.FormatInt(value, 10) + " B"
	}
	div, exp := int64(unit), 0
	for n := value / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return strconv.FormatFloat(float64(value)/float64(div), 'f', 1, 64) + " " + []string{"KB", "MB", "GB"}[exp]
}

// formatElapsed renders a duration in seconds as a compact label.
func formatElapsed(seconds float64) string {
	total := int64(seconds)
	days := total / 86400
	hours := (total % 86400) / 3600
	minutes := (total % 3600) / 60
	secs := total % 60

	switch {
	case days > 0:
		return strconv.FormatInt(days, 10) + "d " + strconv.FormatInt(hours, 10) + "h"
	case hours > 0:
		return strconv.FormatInt(hours, 10) + "h " + strconv.FormatInt(minutes, 10) + "m"
	case minutes > 0:
		return strconv.FormatInt(minutes, 10) + "m " + strconv.FormatInt(secs, 10) + "s"
	default:
		return strconv.FormatInt(secs, 10) + "s"
	}
}
