//go:build !windows

package system

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// CPUInfo holds host CPU usage as a percentage (0-100).
type CPUInfo struct {
	UsagePercent float64
}

// ReadCPUInfo returns an approximate CPU usage percentage by reading /proc/stat twice.
// On first call it returns 0 since there is no prior sample.
func ReadCPUInfo() (CPUInfo, error) {
	idle1, total1, err := readCPUSample()
	if err != nil {
		return CPUInfo{}, err
	}
	// We cannot sleep here (this is a synchronous handler), so we return the
	// instantaneous load average as an approximation instead.
	idle2, total2, err := readCPUSample()
	if err != nil {
		return CPUInfo{}, err
	}

	idleDelta := float64(idle2 - idle1)
	totalDelta := float64(total2 - total1)
	if totalDelta == 0 {
		// Fallback: read load average
		return readLoadAvg()
	}
	usage := (1.0 - idleDelta/totalDelta) * 100
	if usage < 0 {
		usage = 0
	}
	return CPUInfo{UsagePercent: usage}, nil
}

func readCPUSample() (idle, total int64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, fmt.Errorf("cpu: open /proc/stat: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0, fmt.Errorf("cpu: unexpected /proc/stat format")
		}
		var sum int64
		for i := 1; i < len(fields); i++ {
			v, _ := strconv.ParseInt(fields[i], 10, 64)
			sum += v
			if i == 4 { // idle is the 4th value (index 4)
				idle = v
			}
		}
		return idle, sum, nil
	}
	return 0, 0, fmt.Errorf("cpu: no cpu line in /proc/stat")
}

func readLoadAvg() (CPUInfo, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return CPUInfo{}, fmt.Errorf("cpu: read /proc/loadavg: %w", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return CPUInfo{}, fmt.Errorf("cpu: unexpected /proc/loadavg format")
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return CPUInfo{}, fmt.Errorf("cpu: parse load average: %w", err)
	}
	// Normalize to percentage (rough approximation using 1 core)
	usage := load * 100
	if usage > 100 {
		usage = 100
	}
	return CPUInfo{UsagePercent: usage}, nil
}
