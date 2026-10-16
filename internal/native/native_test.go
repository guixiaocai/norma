// SPDX-License-Identifier: MIT
package native

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/cilium/ebpf"
	"nfsmon/internal/collect"
)

func TestNativeDiagnosticWarnings(t *testing.T) {
	for _, tc := range []struct {
		name    string
		buckets uint64
		peaks   []uint64
		warning string
	}{
		{"non-atomic histogram", 1, []uint64{2000, 4000}, "diagnostic_incomplete=1"},
		{"maximum map capacity", 2, nil, "max_unavailable=1"},
		{"coherent", 2, []uint64{2000, 4000}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op := diagnosticOperation(collectorDiagnosticT{Calls: 2, TotalNs: 6000, Buckets: [32]uint64{0, 0, tc.buckets}}, tc.peaks)
			s := collect.Snapshot{Diagnostics: []collect.Diagnostic{{Read: op}}}
			s.CheckDiagnosticHealth()
			if tc.warning == "" {
				if s.Health != "" {
					t.Fatal(s.Health)
				}
			} else if !strings.Contains(s.Health, tc.warning) {
				t.Fatalf("missing %q in %q", tc.warning, s.Health)
			}
			if op.Calls != 2 || op.Buckets[2] != tc.buckets {
				t.Fatal("raw diagnostic lost")
			}
			if op.QuantileAvailable() != (tc.warning == "") {
				t.Fatal("incorrect quantile availability")
			}
		})
	}
}

func TestNativeCompletionPaths(t *testing.T) {
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("C compiler unavailable; generated-object checks still run")
	}
	// Track the compiler inputs in Go's test cache as well as the child process.
	for _, input := range []string{"bpf/collector.c", "testdata/collector_harness.c", "testdata/bpf/bpf_helpers.h"} {
		if _, err := os.ReadFile(input); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "collector-harness")
	cmd := exec.Command(cc, "-std=gnu11", "-O2", "-Itestdata", "testdata/collector_harness.c", "-o", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile accounting harness: %v\n%s", err, out)
	}
	if out, err := exec.Command(path).CombinedOutput(); err != nil {
		t.Fatalf("accounting control flow: %v\n%s", err, out)
	}
}

func TestEmbeddedMapLayoutsMatchUserspace(t *testing.T) {
	spec, err := loadCollector()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                string
		key, value, entries uint32
		kind                ebpf.MapType
	}{
		{"counts", uint32(binary.Size(collectorKeyT{})), uint32(binary.Size(collectorValT{})), 16384, ebpf.PerCPUHash},
		{"clients", uint32(binary.Size(collectorKeyT{})), uint32(binary.Size(collectorValT{})), 4096, ebpf.PerCPUHash},
		{"diagnostics", uint32(binary.Size(collectorDiagnosticKey{})), uint32(binary.Size(collectorDiagnosticT{})), 8192, ebpf.Hash},
		{"diagnostic_max", uint32(binary.Size(collectorDiagnosticKey{})), 8, 8192, ebpf.PerCPUHash},
		{"health", 4, uint32(binary.Size(collectorHealthT{})), 1, ebpf.PerCPUArray},
	} {
		m := spec.Maps[tc.name]
		if m == nil || m.KeySize != tc.key || m.ValueSize != tc.value || m.MaxEntries != tc.entries || m.Type != tc.kind {
			t.Errorf("%s map layout: %+v", tc.name, m)
		}
	}
	if got := unsafe.Offsetof(collectorKeyT{}.Transport); got != 24 {
		t.Fatalf("transport ABI offset %d", got)
	}
	if got := binary.Size(collectorDiagnosticT{}); got != 280 {
		t.Fatalf("histogram value size %d", got)
	}
	if len(spec.Programs) != 6 {
		t.Fatalf("unexpected probe coverage %d", len(spec.Programs))
	}
}

func TestNativeKeyTransportAndPerCPUTotals(t *testing.T) {
	wire := make([]byte, 64)
	binary.LittleEndian.PutUint64(wire, 1234)
	binary.LittleEndian.PutUint32(wire[8:], 1000)
	binary.LittleEndian.PutUint32(wire[12:], 1001)
	binary.LittleEndian.PutUint32(wire[16:], 259)
	binary.LittleEndian.PutUint32(wire[20:], 2)
	binary.LittleEndian.PutUint32(wire[24:], 256)
	copy(wire[32:], []byte{192, 0, 2, 1})
	copy(wire[48:], []byte{192, 0, 2, 2})
	var key collectorKeyT
	if err := binary.Read(bytes.NewReader(wire), binary.LittleEndian, &key); err != nil {
		t.Fatal(err)
	}
	r := recordFromKey(key, []collectorValT{{R_bytes: 10, R_ops: 1}, {R_bytes: 20, W_bytes: 8, R_ops: 2, W_ops: 1}}, "dir/file")
	if r.Transport != 256 || r.Client != netip.MustParseAddr("192.0.2.1") || r.Server != netip.MustParseAddr("192.0.2.2") || r.Inode != 1234 || r.UID != 1000 || r.GID != 1001 || r.Device != 259 || r.Name != "dir/file" || r.Counters.Read != 30 || r.Counters.ReadOps != 3 || r.Counters.Write != 8 || r.Counters.WriteOps != 1 {
		t.Fatalf("decoded %+v", r)
	}
	key.Family, key.Transport = 10, 999
	key.Client = netip.MustParseAddr("2001:db8::1").As16()
	key.Server = netip.MustParseAddr("2001:db8::2").As16()
	r = recordFromKey(key, nil, "")
	if r.Transport != 999 || r.Client != netip.MustParseAddr("2001:db8::1") {
		t.Fatalf("IPv6/unknown transport %+v", r)
	}
}
