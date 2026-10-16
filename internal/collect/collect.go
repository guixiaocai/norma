// SPDX-License-Identifier: MIT
package collect

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"time"
)

// Counters are cumulative since attachment. Collectors never clear live counters.
type Counters struct{ Read, Write, ReadOps, WriteOps uint64 }

func (a Counters) Add(b Counters) Counters {
	return Counters{a.Read + b.Read, a.Write + b.Write, a.ReadOps + b.ReadOps, a.WriteOps + b.WriteOps}
}
func (a Counters) Delta(b Counters) Counters {
	sub := func(x, y uint64) uint64 {
		if x < y {
			return x
		}
		return x - y
	}
	return Counters{sub(a.Read, b.Read), sub(a.Write, b.Write), sub(a.ReadOps, b.ReadOps), sub(a.WriteOps, b.WriteOps)}
}

type Record struct {
	Client, Server   netip.Addr
	UID, GID, Device uint32
	Inode            uint64
	Transport        uint32 // NFSD rq_prot: TCP=6, UDP=17, RDMA=256; unknown values retained.
	Name             string // Optional parent/basename hint, never an absolute path.
	Counters         Counters
}
type Snapshot struct {
	At          time.Time
	Records     []Record // Detailed file/UID/GID counters.
	Clients     []Record // Independently collected endpoint-pair counters.
	Diagnostics []Diagnostic
	Health      string
}

// OpDiagnostics describes completed NFSD VFS helper calls, including failures.
// These are cumulative helper durations, not client RPC or storage latencies.
// Bucket i contains durations <= 2^i microseconds (rounded up); bucket 31
// includes all larger durations as well. Quantiles are histogram upper bounds.
type OpDiagnostics struct {
	Calls, Errors, TotalNS, MaxNS uint64
	Buckets                       [32]uint64
}

func (d OpDiagnostics) Add(other OpDiagnostics) OpDiagnostics {
	d.Calls += other.Calls
	d.Errors += other.Errors
	d.TotalNS += other.TotalNS
	d.MaxNS = max(d.MaxNS, other.MaxNS)
	for i := range d.Buckets {
		d.Buckets[i] += other.Buckets[i]
	}
	return d
}

func (d OpDiagnostics) AverageNS() float64 {
	if d.Calls == 0 {
		return 0
	}
	return float64(d.TotalNS) / float64(d.Calls)
}

// HistogramComplete requires the histogram and call counter to describe the
// same observed population. A mismatch can mean live snapshot skew or map loss.
func (d OpDiagnostics) HistogramComplete() bool {
	if d.Calls == 0 {
		return false
	}
	var total uint64
	for _, n := range d.Buckets {
		if n > math.MaxUint64-total {
			return false
		}
		total += n
	}
	return total == d.Calls
}

// MaxAvailable excludes a missing maximum-map entry. A zero duration is not
// distinguishable from missing data and is conservatively marked unavailable.
func (d OpDiagnostics) MaxAvailable() bool { return d.Calls > 0 && d.MaxNS > 0 }

func (d OpDiagnostics) QuantileAvailable() bool {
	return d.HistogramComplete() && d.MaxAvailable()
}

// PercentileNS returns an inclusive histogram upper bound, limited by the
// observed maximum. q is in [0,1]; unavailable/incomplete data produces zero.
// Consumers must check QuantileAvailable before presenting the returned value.
func (d OpDiagnostics) PercentileNS(q float64) uint64 {
	if !d.QuantileAvailable() {
		return 0
	}
	if math.IsNaN(q) {
		return 0
	}
	q = min(1, max(0, q))
	rank := d.Calls
	if q < 1 {
		rank = max(uint64(1), uint64(math.Ceil(float64(d.Calls)*q)))
	}
	var seen uint64
	for i, n := range d.Buckets {
		seen += n
		if seen >= rank {
			bound := uint64(1000) << uint(i)
			if i == len(d.Buckets)-1 {
				return d.MaxNS
			}
			return min(bound, d.MaxNS)
		}
	}
	return d.MaxNS
}

type Diagnostic struct {
	Client, Server netip.Addr
	Transport      uint32
	Read, Write    OpDiagnostics
}

// CheckDiagnosticHealth marks incomplete directional observations in this
// snapshot. Warnings are per snapshot, not cumulative loss counters: a later
// coherent snapshot can clear them. Raw diagnostics are always preserved.
func (s *Snapshot) CheckDiagnosticHealth() {
	var incomplete, missingMax int
	for _, d := range s.Diagnostics {
		for _, op := range []OpDiagnostics{d.Read, d.Write} {
			active := op.Calls != 0 || op.Errors != 0 || op.TotalNS != 0 || op.MaxNS != 0
			for _, n := range op.Buckets {
				active = active || n != 0
			}
			if !active {
				continue
			}
			if !op.HistogramComplete() {
				incomplete++
			}
			if op.Calls > 0 && !op.MaxAvailable() {
				missingMax++
			}
		}
	}
	if incomplete == 0 && missingMax == 0 {
		return
	}
	if s.Health != "" {
		s.Health += "; "
	}
	s.Health += fmt.Sprintf("diagnostic_incomplete=%d max_unavailable=%d (snapshot skew or map loss)", incomplete, missingMax)
}

type Source interface {
	Read(context.Context) (Snapshot, error)
	Close() error
	Description() string
}
