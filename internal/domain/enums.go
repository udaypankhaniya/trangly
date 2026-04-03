// Package domain contains pure business models with zero external dependencies.
// Nothing in this package may import from infra, app, or any external library.
package domain

// Job status constants. These are the ONLY place status string values are defined.
// All other packages must import these; never declare status strings locally.
const (
	StatusPending     = "pending"
	StatusHeld        = "held"
	StatusRunning     = "running"
	StatusBuilding    = "building"
	StatusHealthCheck = "health_check"
	StatusSwapping    = "swapping"
	StatusSuccess     = "success"
	StatusFailed      = "failed"
)

// EstimateSource constants indicate how a job's RAM estimate was derived.
const (
	EstimateSourceBootstrap = "bootstrap" // first-deploy fallback: memLimit+150MB or 300MB
	EstimateSourceAdaptive  = "adaptive"  // avg of last 3 runtime steady-state RSS × 1.20
)

// HoldReason constants describe why a job was placed in the held state.
const (
	HoldReasonMemory = "insufficient_memory"
)

// HealthMode constants describe the auto-detected health check strategy.
const (
	HealthModeHTTP = "http"
	HealthModeTCP  = "tcp"
	HealthModeNone = "none"
)

// CheckStatus constants for preflight check results.
const (
	CheckStatusOK   = "ok"
	CheckStatusFail = "fail"
	CheckStatusWarn = "warn"
	CheckStatusSkip = "skip"
)

// DeployMode constants define how a project is deployed.
const (
	DeployModeRebuild = "rebuild"  // full pipeline: fetch → build → healthcheck → swap → cleanup
	DeployModeHotSwap = "hot_swap" // file copy: fetch → hot_swap → cleanup
)

// StatusHotSwapping is used during the hot-swap deploy pipeline.
const StatusHotSwapping = "hot_swapping"

// CommitStatus constants for GitHub commit status API.
const (
	CommitStatusPending = "pending"
	CommitStatusSuccess = "success"
	CommitStatusFailure = "failure"
	CommitStatusError   = "error"
)
