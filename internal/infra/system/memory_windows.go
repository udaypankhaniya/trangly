//go:build windows

package system

import (
	"fmt"
	"syscall"
	"unsafe"
)

// memoryStatusEx mirrors the Win32 MEMORYSTATUSEX structure.
type memoryStatusEx struct {
	dwLength                uint32
	dwMemoryLoad            uint32
	ullTotalPhys            uint64
	ullAvailPhys            uint64
	ullTotalPageFile        uint64
	ullAvailPageFile        uint64
	ullTotalVirtual         uint64
	ullAvailVirtual         uint64
	ullAvailExtendedVirtual uint64
}

// ReadMemInfo returns total and available RAM on Windows via GlobalMemoryStatusEx.
func ReadMemInfo() (MemInfo, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GlobalMemoryStatusEx")

	var ms memoryStatusEx
	ms.dwLength = uint32(unsafe.Sizeof(ms))

	r1, _, err := proc.Call(uintptr(unsafe.Pointer(&ms)))
	if r1 == 0 {
		return MemInfo{}, fmt.Errorf("memory: GlobalMemoryStatusEx: %w", err)
	}
	return MemInfo{
		TotalMB:     int64(ms.ullTotalPhys) / (1024 * 1024),
		AvailableMB: int64(ms.ullAvailPhys) / (1024 * 1024),
	}, nil
}
