// Package procmon exposes the running processes of the device by reading
// /proc directly, without spawning external tools such as ps or top.
package procmon

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// clockTicks is the kernel USER_HZ value used by /proc on Android.
const clockTicks = 100

// pageSize is the memory page size used to convert the RSS page count.
const pageSize = 4096

// Process describes a single running process.
type Process struct {
	PID        int     `json:"pid"`
	PPID       int     `json:"ppid"`
	Name       string  `json:"name"`
	Cmd        string  `json:"cmd"`
	CPU        float64 `json:"cpu"`
	MemPct     float64 `json:"mem_pct"`
	MemRSS     int64   `json:"mem_rss"`
	MemText    string  `json:"mem_text"`
	State      string  `json:"state"`
	User       string  `json:"user"`
	ExePath    string  `json:"exe_path"`
	Runtime    string  `json:"runtime"`
	Elapsed    string  `json:"elapsed"`
	ElapsedSec float64 `json:"elapsed_sec"`
	Threads    int     `json:"threads"`
}

// Monitor keeps the process list and the CPU accounting state between polls.
type Monitor struct {
	mu         sync.Mutex
	lastTicks  map[int]int64
	lastTotal  int64
	cpuCache   map[int]float64
	exeRuntime map[string]string
}

var (
	globalManager *Manager
	once          sync.Once
)

// Manager wraps the process monitor with a stable accessor.
type Manager struct {
	mon *Monitor
}

// GetManager returns the process monitor singleton.
func GetManager() *Manager {
	once.Do(func() {
		globalManager = &Manager{mon: NewMonitor()}
	})
	return globalManager
}

// NewMonitor creates a monitor and primes the CPU accounting baseline, so the
// first listing already carries usable CPU percentages.
func NewMonitor() *Monitor {
	m := &Monitor{
		lastTicks:  make(map[int]int64),
		cpuCache:   make(map[int]float64),
		exeRuntime: make(map[string]string),
	}
	m.refreshCPU(nil)
	return m
}

// List returns the running processes, optionally filtered by a search term.
// Results are sorted by the given key: "cpu", "mem", "pid", "name" or "elapsed".
func (m *Monitor) List(query string, sortBy string, limit int) []Process {
	procs := m.collect(query)

	sort.Slice(procs, func(i, j int) bool {
		switch sortBy {
		case "mem":
			if procs[i].MemPct != procs[j].MemPct {
				return procs[i].MemPct > procs[j].MemPct
			}
		case "pid":
			return procs[i].PID < procs[j].PID
		case "name":
			return strings.ToLower(procs[i].Name) < strings.ToLower(procs[j].Name)
		case "elapsed":
			if procs[i].ElapsedSec != procs[j].ElapsedSec {
				return procs[i].ElapsedSec > procs[j].ElapsedSec
			}
		default:
			if procs[i].CPU != procs[j].CPU {
				return procs[i].CPU > procs[j].CPU
			}
			if procs[i].MemPct != procs[j].MemPct {
				return procs[i].MemPct > procs[j].MemPct
			}
		}
		return procs[i].PID < procs[j].PID
	})

	if limit > 0 && len(procs) > limit {
		procs = procs[:limit]
	}
	return procs
}

// collect walks /proc and builds the process list.
func (m *Monitor) collect(query string) []Process {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}

	// A single pass over /proc collects the stat files. Reading them twice per
	// poll would double the I/O on a device that runs hundreds of processes.
	stats := make(map[int]*procStat, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		raw := readProc(fmt.Sprintf("/proc/%d/stat", pid))
		if raw == "" {
			continue
		}
		name, fields := parseProcStat(raw)
		if name == "" || len(fields) < 22 {
			continue
		}
		stats[pid] = &procStat{name: name, fields: fields}
	}

	ticks := make(map[int]int64, len(stats))
	for pid, st := range stats {
		ticks[pid] = parseInt64(st.fields[11]) + parseInt64(st.fields[12])
	}
	cpu := m.refreshCPU(ticks)

	totalMem := totalMemory()
	uptime := systemUptime()

	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]Process, 0, len(stats))

	for pid, st := range stats {
		name := st.name

		cmdline := strings.TrimSpace(strings.ReplaceAll(readProc(fmt.Sprintf("/proc/%d/cmdline", pid)), "\x00", " "))
		cmd := cmdline
		if cmd == "" {
			cmd = name
		}
		if len(cmd) > 240 {
			cmd = cmd[:240] + "…"
		}

		// The name of the stat file is limited to 15 characters, the command
		// line carries the full name of the process.
		if len(name) >= 15 {
			if argv0 := firstToken(cmd); argv0 != "" {
				if base := baseName(argv0); len(base) > len(name) {
					name = base
				}
			}
		}

		runtimeKind := m.detectRuntime(pid, name, cmdline)
		if q != "" &&
			!strings.Contains(strings.ToLower(name), q) &&
			!strings.Contains(strings.ToLower(cmd), q) &&
			!strings.Contains(strings.ToLower(runtimeKind), q) {
			continue
		}

		proc := Process{
			PID:     pid,
			Name:    name,
			Cmd:     cmd,
			State:   stateName(st.fields[0]),
			Threads: threadCount(pid),
			Runtime: runtimeKind,
			CPU:     cpu[pid],
		}

		if v, err := strconv.Atoi(st.fields[1]); err == nil {
			proc.PPID = v
		}

		// The stat file reports the RSS in pages, the status file is more
		// precise because it accounts for the shared pages.
		rss := parseInt64(st.fields[21]) * pageSize
		if statusRSS := statusRSS(pid); statusRSS > 0 {
			rss = statusRSS
		}
		proc.MemRSS = rss
		proc.MemText = formatBytes(rss)
		if totalMem > 0 {
			proc.MemPct = round1(float64(rss) / float64(totalMem) * 100)
		}

		proc.ExePath = exePath(pid)
		proc.User = userName(statusUID(pid))

		// The start time is counted in clock ticks since boot.
		startTicks := parseInt64(st.fields[19])
		if uptime > 0 && startTicks > 0 {
			elapsed := uptime - float64(startTicks)/clockTicks
			if elapsed > 0 {
				proc.ElapsedSec = round1(elapsed)
				proc.Elapsed = formatElapsed(elapsed)
			}
		}
		if proc.Elapsed == "" {
			proc.Elapsed = "—"
		}

		out = append(out, proc)
	}

	return out
}

// refreshCPU updates the CPU accounting cache and returns the percentages.
// The usage of a process is the share of the consumed clock ticks against the
// ticks of all cores, so a value below 100 means a single core is not fully
// saturated.
func (m *Monitor) refreshCPU(ticks map[int]int64) map[int]float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	curTotal := totalTicks()
	if m.lastTotal > 0 && curTotal > m.lastTotal {
		dtTotal := curTotal - m.lastTotal
		numCPU := float64(runtime.NumCPU())
		if numCPU < 1 {
			numCPU = 1
		}
		cache := make(map[int]float64, len(ticks))
		for pid, t := range ticks {
			prev, ok := m.lastTicks[pid]
			if !ok {
				continue
			}
			dt := t - prev
			if dt <= 0 {
				continue
			}
			cpu := float64(dt) / float64(dtTotal) * numCPU * 100
			if cpu > numCPU*100 {
				cpu = numCPU * 100
			}
			cache[pid] = round1(cpu)
		}
		m.cpuCache = cache
	}

	if ticks != nil {
		m.lastTicks = ticks
		m.lastTotal = curTotal
	}
	return m.cpuCache
}

// List returns the running processes through the manager.
func (m *Manager) List(query string, sortBy string, limit int) []Process {
	return m.mon.List(query, sortBy, limit)
}

// Kill terminates a process. A graceful SIGTERM is sent when force is false,
// SIGKILL is used when force is true.
func (m *Manager) Kill(pid int, force bool) error {
	return m.mon.Kill(pid, force)
}

// Terminate stops a process gracefully and force kills it when it does not
// exit within the grace period.
func (m *Manager) Terminate(pid int, grace time.Duration) error {
	if err := m.mon.Kill(pid, false); err != nil {
		return err
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err != nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return m.mon.Kill(pid, true)
}
