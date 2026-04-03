//go:build !windows

package system

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadMemInfo reads /proc/meminfo and returns total and available RAM in MB.
func ReadMemInfo() (MemInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemInfo{}, fmt.Errorf("memory: open /proc/meminfo: %w", err)
	}
	defer f.Close()

	info := MemInfo{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			info.TotalMB = kb / 1024
		case "MemAvailable":
			info.AvailableMB = kb / 1024
		}
	}
	if err := sc.Err(); err != nil {
		return MemInfo{}, fmt.Errorf("memory: scanning /proc/meminfo: %w", err)
	}
	return info, nil
}
