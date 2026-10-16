// SPDX-License-Identifier: BSD-2-Clause
package trace

import (
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"nfsmon/internal/collect"
	"nfsmon/internal/layout"
)

func TestFrameWarnsOnIncompleteDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, bucket, max string
		warning           bool
	}{
		{"missing bucket", "1", "4000", true},
		{"missing maximum", "2", "0", true},
		{"complete", "2", "4000", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("true")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			s := Source{cmd: cmd, frames: make(chan frame, 1), done: make(chan struct{}), stop: make(chan struct{})}
			input := "NFSMON_BEGIN\n@dc[192.0.2.1, 192.0.2.2, 6, 0]: 2\n@dm[192.0.2.1, 192.0.2.2, 6, 0]: " + tc.max + "\n@dh[192.0.2.1, 192.0.2.2, 6, 0, 2]: " + tc.bucket + "\nNFSMON_END\n"
			s.consume(strings.NewReader(input))
			f := <-s.frames
			if f.err != nil {
				t.Fatal(f.err)
			}
			if (f.snapshot.Health != "") != tc.warning {
				t.Fatalf("health=%q", f.snapshot.Health)
			}
			if len(f.snapshot.Diagnostics) != 1 || f.snapshot.Diagnostics[0].Read.Calls != 2 {
				t.Fatalf("raw diagnostics lost: %+v", f.snapshot)
			}
			if f.snapshot.Diagnostics[0].Read.QuantileAvailable() == tc.warning {
				t.Fatalf("quantile availability with warning=%v", tc.warning)
			}
		})
	}
}

func TestParseTransportDetailsSummariesAndDiagnostics(t *testing.T) {
	records, clients := map[string]*collect.Record{}, map[string]*collect.Record{}
	diagnostics, health := map[string]*collect.Diagnostic{}, map[string]uint64{}
	lines := []string{
		"@rb[192.0.2.1, 192.0.2.2, 256, 1000, 1001, 259, 18446744073709551615]: 4096",
		"@ro[192.0.2.1, 192.0.2.2, 256, 1000, 1001, 259, 18446744073709551615]: 2",
		"@crb[192.0.2.1, 192.0.2.2, 256]: 8192",
		"@cro[192.0.2.1, 192.0.2.2, 256]: 4",
		"@cwb[2001:db8::1, 2001:db8::2, 6]: 1024",
		"@cwo[2001:db8::1, 2001:db8::2, 6]: 1",
		"@crb[192.0.2.1, 192.0.2.2, 999]: 128",
		"@dc[192.0.2.1, 192.0.2.2, 256, 0]: 5",
		"@de[192.0.2.1, 192.0.2.2, 256, 0]: 1",
		"@dn[192.0.2.1, 192.0.2.2, 256, 0]: 9000",
		"@dm[192.0.2.1, 192.0.2.2, 256, 0]: 4000",
		"@dh[192.0.2.1, 192.0.2.2, 256, 0, 0]: 2",
		"@dh[192.0.2.1, 192.0.2.2, 256, 0, 2]: 3",
		"@dc[192.0.2.1, 192.0.2.2, 256, 1]: 2",
		"@de[192.0.2.1, 192.0.2.2, 256, 1]: 2",
		"@failed: 3", "@badreads: 1",
	}
	for _, line := range lines {
		if err := parseLine(line, records, clients, diagnostics, health); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
	}
	if len(records) != 1 || len(clients) != 3 || len(diagnostics) != 1 {
		t.Fatalf("record sets %d %d %d", len(records), len(clients), len(diagnostics))
	}
	for _, r := range records {
		if r.Transport != 256 || r.UID != 1000 || r.GID != 1001 || r.Device != 259 || r.Inode != ^uint64(0) || r.Counters.Read != 4096 || r.Counters.ReadOps != 2 {
			t.Fatalf("detail %+v", r)
		}
	}
	r := clients["2001:db8::1,2001:db8::2,6"]
	if r == nil || r.Client != netip.MustParseAddr("2001:db8::1") || r.Counters.Write != 1024 || r.Counters.WriteOps != 1 {
		t.Fatalf("IPv6 client %+v", r)
	}
	if clients["192.0.2.1,192.0.2.2,999"].Transport != 999 {
		t.Fatal("unknown transport lost")
	}
	for _, d := range diagnostics {
		if d.Read.Calls != 5 || d.Read.Errors != 1 || d.Read.TotalNS != 9000 || d.Read.MaxNS != 4000 || d.Read.Buckets[0] != 2 || d.Read.Buckets[2] != 3 || d.Write.Errors != 2 || d.Write.Calls != 2 {
			t.Fatalf("diagnostic %+v", d)
		}
	}
	if health["failed"] != 3 || health["badreads"] != 1 {
		t.Fatalf("health %+v", health)
	}
}

func TestParserRejectsInvalidKeys(t *testing.T) {
	for _, line := range []string{
		"@rb[192.0.2.1, 192.0.2.2]: 1",
		"@rb[192.0.2.1, 192.0.2.2, 4294967296]: 1",
		"@dc[192.0.2.1, 192.0.2.2, 6, 2]: 1",
		"@dh[192.0.2.1, 192.0.2.2, 6, 0, 32]: 1",
		"@dh[192.0.2.1, 192.0.2.2, 6, 0]: 1",
		"@dn[invalid, 192.0.2.2, 6, 0]: 1",
		"@crb[192.0.2.1, 192.0.2.2, 17]: -1",
	} {
		if err := parseLine(line, map[string]*collect.Record{}, map[string]*collect.Record{}, map[string]*collect.Diagnostic{}, map[string]uint64{}); err == nil {
			t.Errorf("accepted %s", line)
		}
	}
}

func TestScriptRetainsAllDetailsAndTimingOnFailure(t *testing.T) {
	l := layout.Layout{}
	for i := range l.Hooks {
		l.Hooks[i] = layout.Hook{Symbol: []string{"nfsd_readv", "nfsd_splice_read", "nfsd_vfs_write"}[i], RqArg: 1, FileArg: 2, CountArg: 4, CountBytes: 4, FileWrapper: layout.DirectFile}
	}
	s := Script(l, 0, "clients", time.Second)
	if path := os.Getenv("NORMA_BPFTRACE_SCRIPT"); path != "" {
		if err := os.WriteFile(path, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if s != Script(l, 0, "files", time.Second) {
		t.Fatal("collector script depends on display group")
	}
	for _, want := range []string{"$cip,$sip,$proto,$uid,$gid,$dev,$ino", "@crb[$cip,$sip,$proto]", "@dh[$cip,$sip,$proto,$write,$bucket]", "@de[$cip,$sip,$proto,$write]", "@started[tid,0]", "clear(@dh)", "print(@crb)"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Index(s, "@dc[") > strings.Index(s, "if ((uint32)retval!=0)") {
		t.Fatal("timing must include failed returns")
	}
	if strings.Contains(s, "$rq && $file && $countp") || strings.Contains(s, "/@cp[") {
		t.Fatal("count/file availability must not suppress failure diagnostics")
	}
}
