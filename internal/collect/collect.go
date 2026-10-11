// SPDX-License-Identifier: MIT
package collect

import (
	"context"
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
	Name             string // Optional parent/basename hint, never an absolute path.
	Counters         Counters
}
type Snapshot struct {
	At      time.Time
	Records []Record
	Health  string
}
type Source interface {
	Read(context.Context) (Snapshot, error)
	Close() error
	Description() string
}
