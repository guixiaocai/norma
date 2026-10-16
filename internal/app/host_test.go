// SPDX-License-Identifier: MIT
package app

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHostRatesAndMissingData(t *testing.T) {
	i := 0
	s := &hostSampler{
		wholeDisk: func(name string) bool { return name == "sda" },
		readFile: func(path string) ([]byte, error) {
			switch path {
			case "/proc/stat":
				return []byte(fmt.Sprintf("cpu %d 0 0 %d 0 0 0 0 9999 0\n", 100+i*25, 100+i*75)), nil
			case "/proc/fs/nfsd/threads":
				return []byte("16\n"), nil
			case "/proc/diskstats":
				return []byte(fmt.Sprintf("8 0 sda 1 0 %d 0 1 0 %d 0\n8 1 sda1 1 0 90000 0 1 0 90000 0\n", 100+i*8, 100+i*4)), nil
			case "/proc/net/dev":
				return []byte(fmt.Sprintf("eth0: %d 0 0 0 0 0 0 0 %d 0 0 0 0 0 0 0\nlo: 999999 0 0 0 0 0 0 0 999999 0 0 0 0 0 0 0\n", 1000+i*1000, 1000+i*2000)), nil
			}
			return nil, os.ErrNotExist
		},
	}
	at := time.Now()
	first := s.Read(at)
	if first.CPUPercent != nil || first.Disk != nil || first.Network != nil {
		t.Fatal("first sample must not infer a rate")
	}
	i++
	second := s.Read(at.Add(2 * time.Second))
	if second.CPUPercent == nil || *second.CPUPercent != 25 || second.Disk.ReadBPS != 2048 || second.Network.WriteBPS != 1000 {
		t.Fatalf("wrong host rates: %+v, %+v, %+v", second.CPUPercent, second.Disk, second.Network)
	}
	if !strings.Contains(second.Summary(), "NFSD 16 threads") {
		t.Fatal("missing NFSD correlation")
	}
	s.readFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	if got := s.Read(at.Add(3 * time.Second)); got.Summary() != "" {
		t.Fatalf("missing proc data invented readings: %+v", got)
	}
}
