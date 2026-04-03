//go:build windows

package system

import (
	"fmt"
	"syscall"
	"unsafe"
)

// CPUInfo holds host CPU usage as a percentage (0-100).
type CPUInfo struct {
	UsagePercent float64
}

// ReadCPUInfo returns an approximate CPU usage on Windows via GetSystemTimes.
func ReadCPUInfo() (CPUInfo, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetSystemTimes")

	var idleTime, kernelTime, userTime syscall.Filetime
	r1, _, err := proc.Call(
		uintptr(unsafe.Pointer(&idleTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	)
	if r1 == 0 {
		return CPUInfo{}, fmt.Errorf("cpu: GetSystemTimes: %w", err)
	}

	idle := filetimeToUint64(idleTime)
	kernel := filetimeToUint64(kernelTime)
	user := filetimeToUint64(userTime)

	total := kernel + user
	if total == 0 {
		return CPUInfo{UsagePercent: 0}, nil
	}
	usage := float64(total-idle) / float64(total) * 100
	if usage < 0 {
		usage = 0
	}
	return CPUInfo{UsagePercent: usage}, nil
}

func filetimeToUint64(ft syscall.Filetime) uint64 {
	return uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
}
