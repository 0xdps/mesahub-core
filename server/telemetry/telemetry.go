// Package telemetry provides lightweight atomic counters for exec and query
// operations. A single *Counters instance is shared across the ExecHandler,
// QueryHandler and MetricsHandler via dependency injection in main.go.
package telemetry

import (
	"math"
	"sync/atomic"
)

// Counters tracks read/write/error counts and cumulative exec time.
// All fields are safe for concurrent use without a mutex.
type Counters struct {
	reads       atomic.Int64 // SELECT / read-path executions that succeeded
	writes      atomic.Int64 // INSERT / UPDATE / DELETE / write-path executions that succeeded
	errors      atomic.Int64 // any exec or query that returned an error to the caller
	execTimeSum atomic.Int64 // cumulative query duration in ms (successful ops only)
	execCount   atomic.Int64 // number of successful ops (denominator for avg)
}

// New returns a new zeroed Counters.
func New() *Counters { return &Counters{} }

// IncRead records one successful read-path operation with its duration.
func (c *Counters) IncRead(durationMs int64) {
	c.reads.Add(1)
	c.execTimeSum.Add(durationMs)
	c.execCount.Add(1)
}

// IncWrite records one successful write-path operation with its duration.
func (c *Counters) IncWrite(durationMs int64) {
	c.writes.Add(1)
	c.execTimeSum.Add(durationMs)
	c.execCount.Add(1)
}

// IncError records one failed exec or query operation.
func (c *Counters) IncError() {
	c.errors.Add(1)
}

// Snapshot is an immutable, JSON-serialisable view of the counters.
type Snapshot struct {
	Reads           int64   `json:"reads"`
	Writes          int64   `json:"writes"`
	Errors          int64   `json:"errors"`
	AvgExecMs       float64 `json:"avg_exec_ms"`
	ErrorRatePct    float64 `json:"error_rate_pct"`
	AvailabilityPct float64 `json:"availability_pct"`
}

// Snapshot returns a point-in-time copy of all counters.
func (c *Counters) Snapshot() Snapshot {
	reads := c.reads.Load()
	writes := c.writes.Load()
	errors := c.errors.Load()
	sum := c.execTimeSum.Load()
	count := c.execCount.Load()

	var avgMs float64
	if count > 0 {
		avgMs = round2(float64(sum) / float64(count))
	}

	total := reads + writes + errors // include errors in denominator
	var errRate float64
	if total > 0 {
		errRate = round2(float64(errors) / float64(total) * 100)
	}

	return Snapshot{
		Reads:           reads,
		Writes:          writes,
		Errors:          errors,
		AvgExecMs:       avgMs,
		ErrorRatePct:    errRate,
		AvailabilityPct: round2(100 - errRate),
	}
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
