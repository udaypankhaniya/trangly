package system

// MemInfo holds total and available RAM in megabytes.
// Populated by ReadMemInfo(), which is implemented per-platform.
type MemInfo struct {
	TotalMB     int64
	AvailableMB int64
}
