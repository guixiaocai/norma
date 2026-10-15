// SPDX-License-Identifier: MIT
package app

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"nfsmon/internal/layout"
)

func healthyDoctorHost() doctorHost {
	files := map[string]string{
		"/etc/os-release":       "PRETTY_NAME=\"Rocky Linux 8.10 (Green Obsidian)\"\n",
		"/proc/fs/nfsd/threads": "8\n",
		"/proc/kallsyms":        "0 t nfsd_readv [nfsd]\n0 t nfsd_splice_read [nfsd]\n0 t nfsd_vfs_write [nfsd]\n",
	}
	return doctorHost{
		arch: "amd64", system: "linux", euid: 0,
		readFile: func(path string) ([]byte, error) {
			if value, ok := files[path]; ok {
				return []byte(value), nil
			}
			return nil, os.ErrNotExist
		},
		lookPath: func(s string) (string, error) { return "/usr/bin/" + s, nil },
		kernel:   func() (string, error) { return "4.18.0-553.el8_10.x86_64", nil },
		resolve: func(string) (layout.Layout, error) {
			return layout.Layout{Source: "matching headers", Hooks: [3]layout.Hook{{Symbol: "nfsd_readv"}, {Symbol: "nfsd_splice_read"}, {Symbol: "nfsd_vfs_write"}}}, nil
		},
		bpfVersion: func() (string, error) { return "bpftrace v0.16.0", nil },
	}
}

func TestDoctorPreparedEL8WithoutBTF(t *testing.T) {
	host := healthyDoctorHost()
	host.lookPath = func(s string) (string, error) { t.Fatalf("native doctor unexpectedly needs %s", s); return "", nil }
	var out bytes.Buffer
	if err := runDoctor(Options{Backend: "native", Transport: "all"}, &out, host); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Rocky Linux 8.10", "matching headers", "Prerequisite checks passed", "does not load or attach probes"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in %s", want, out.String())
		}
	}
}

func TestDoctorMissingLayoutGivesEL8Remedy(t *testing.T) {
	host := healthyDoctorHost()
	host.resolve = func(path string) (layout.Layout, error) {
		if path != "/reviewed.json" {
			t.Errorf("explicit layout lost: %s", path)
		}
		return layout.Layout{}, fmt.Errorf("layout unavailable")
	}
	var out bytes.Buffer
	if err := runDoctor(Options{Backend: "native", Layout: "/reviewed.json"}, &out, host); err == nil {
		t.Fatal("expected failed prerequisites")
	}
	for _, want := range []string{"kernel-devel-$(uname -r)", "prepare-layout.sh", "norma --demo"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing remedy %q", want)
		}
	}
}

func TestDoctorRejectsStoppedServerAndMissingHooks(t *testing.T) {
	host := healthyDoctorHost()
	host.readFile = func(path string) ([]byte, error) {
		if path == "/proc/fs/nfsd/threads" {
			return []byte("0\n"), nil
		}
		if path == "/proc/kallsyms" {
			return []byte("0 t unrelated\n"), nil
		}
		return nil, os.ErrNotExist
	}
	var out bytes.Buffer
	err := runDoctor(Options{Backend: "native"}, &out, host)
	if err == nil || !strings.Contains(err.Error(), "4 failed") {
		t.Fatalf("got %v\n%s", err, out.String())
	}
}

func TestDoctorBpftraceVersion(t *testing.T) {
	for _, version := range []string{"bpftrace v0.15.0", "unknown"} {
		host := healthyDoctorHost()
		host.bpfVersion = func() (string, error) { return version, nil }
		var out bytes.Buffer
		if err := runDoctor(Options{Backend: "bpftrace"}, &out, host); err == nil {
			t.Errorf("accepted %s", version)
		}
	}
	for _, version := range []string{"bpftrace v0.16.0", "bpftrace v0.24.1", "bpftrace v1.0.0"} {
		if !supportedBpftrace(version) {
			t.Errorf("rejected %s", version)
		}
	}
}
