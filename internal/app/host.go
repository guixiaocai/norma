// SPDX-License-Identifier: MIT
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HostStats are host-wide correlation hints, separate from NFSD accounting.
// Missing /proc files remain absent, rather than being reported as zero load.
type HostStats struct {
	CPUPercent  *float64    `json:"cpu_percent,omitempty"`
	NFSDThreads *int        `json:"nfsd_threads,omitempty"`
	Disk        *DeviceRate `json:"busiest_disk,omitempty"`
	Network     *DeviceRate `json:"busiest_interface,omitempty"`
}

type DeviceRate struct {
	Name     string  `json:"name"`
	ReadBPS  float64 `json:"read_Bps"`  // Network: receive bytes/s.
	WriteBPS float64 `json:"write_Bps"` // Network: transmit bytes/s.
}

func (s HostStats) Summary() string {
	var parts []string
	if s.CPUPercent != nil {
		parts = append(parts, fmt.Sprintf("CPU %.1f%%", *s.CPUPercent))
	}
	if s.NFSDThreads != nil {
		parts = append(parts, fmt.Sprintf("NFSD %d threads", *s.NFSDThreads))
	}
	if s.Disk != nil {
		parts = append(parts, fmt.Sprintf("disk %s R %s W %s", clean(s.Disk.Name), human(s.Disk.ReadBPS, false, true), human(s.Disk.WriteBPS, false, true)))
	}
	if s.Network != nil {
		parts = append(parts, fmt.Sprintf("net %s RX %s TX %s", clean(s.Network.Name), human(s.Network.ReadBPS, false, true), human(s.Network.WriteBPS, false, true)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "HOST · " + strings.Join(parts, " · ")
}

type deviceCounters struct{ read, write uint64 }
type hostSampler struct {
	readFile          func(string) ([]byte, error)
	wholeDisk         func(string) bool
	last              time.Time
	cpuTotal, cpuIdle uint64
	cpuValid          bool
	disks, networks   map[string]deviceCounters
}

func newHostSampler() *hostSampler {
	return &hostSampler{
		readFile: os.ReadFile,
		wholeDisk: func(name string) bool {
			if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
				return false
			}
			_, exists := os.Stat(filepath.Join("/sys/class/block", name))
			_, partition := os.Stat(filepath.Join("/sys/class/block", name, "partition"))
			return exists == nil && os.IsNotExist(partition)
		},
	}
}

func (s *hostSampler) Read(at time.Time) HostStats {
	var stats HostStats
	if s == nil {
		return stats
	}
	seconds := at.Sub(s.last).Seconds()
	if b, err := s.readFile("/proc/fs/nfsd/threads"); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && n >= 0 {
			stats.NFSDThreads = &n
		}
	}
	if b, err := s.readFile("/proc/stat"); err == nil {
		if total, idle, ok := cpuCounters(string(b)); ok {
			if s.cpuValid && total > s.cpuTotal && idle >= s.cpuIdle {
				percent := 100 * (1 - float64(idle-s.cpuIdle)/float64(total-s.cpuTotal))
				percent = max(0, min(100, percent))
				stats.CPUPercent = &percent
			}
			s.cpuTotal, s.cpuIdle, s.cpuValid = total, idle, true
		} else {
			s.cpuValid = false
		}
	} else {
		s.cpuValid = false
	}
	if b, err := s.readFile("/proc/diskstats"); err == nil {
		current := parseDiskCounters(string(b), s.wholeDisk)
		if !s.last.IsZero() && seconds > 0 {
			stats.Disk = busiest(current, s.disks, seconds)
		}
		s.disks = current
	} else {
		s.disks = nil
	}
	if b, err := s.readFile("/proc/net/dev"); err == nil {
		current := parseNetworkCounters(string(b))
		if !s.last.IsZero() && seconds > 0 {
			stats.Network = busiest(current, s.networks, seconds)
		}
		s.networks = current
	} else {
		s.networks = nil
	}
	s.last = at
	return stats
}

func cpuCounters(input string) (total, idle uint64, ok bool) {
	for _, line := range strings.Split(input, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		// Guest time is already included in user/nice. Do not count it twice.
		for i := 1; i < min(len(fields), 9); i++ {
			n, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return 0, 0, false
			}
			total += n
			if i == 4 || i == 5 {
				idle += n
			} // idle + iowait
		}
		return total, idle, true
	}
	return 0, 0, false
}

func parseDiskCounters(input string, whole func(string) bool) map[string]deviceCounters {
	result := map[string]deviceCounters{}
	for _, line := range strings.Split(input, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || !whole(f[2]) {
			continue
		}
		r, er := strconv.ParseUint(f[5], 10, 64)
		w, ew := strconv.ParseUint(f[9], 10, 64)
		if er == nil && ew == nil {
			result[f[2]] = deviceCounters{r * 512, w * 512}
		}
	}
	return result
}

func parseNetworkCounters(input string) map[string]deviceCounters {
	result := map[string]deviceCounters{}
	for _, line := range strings.Split(input, "\n") {
		name, counters, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		f := strings.Fields(counters)
		if !ok || name == "lo" || len(f) < 16 {
			continue
		}
		r, er := strconv.ParseUint(f[0], 10, 64)
		w, ew := strconv.ParseUint(f[8], 10, 64)
		if er == nil && ew == nil {
			result[name] = deviceCounters{r, w}
		}
	}
	return result
}

func busiest(current, previous map[string]deviceCounters, seconds float64) *DeviceRate {
	var best *DeviceRate
	for name, c := range current {
		p, ok := previous[name]
		if !ok || c.read < p.read || c.write < p.write {
			continue
		}
		rate := &DeviceRate{Name: name, ReadBPS: float64(c.read-p.read) / seconds, WriteBPS: float64(c.write-p.write) / seconds}
		if best == nil || rate.ReadBPS+rate.WriteBPS > best.ReadBPS+best.WriteBPS || (rate.ReadBPS+rate.WriteBPS == best.ReadBPS+best.WriteBPS && name < best.Name) {
			best = rate
		}
	}
	return best
}
