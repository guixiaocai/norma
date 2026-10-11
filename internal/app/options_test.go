// SPDX-License-Identifier: MIT
package app

import (
	"bytes"
	"nfsmon/internal/collect"
	"nfsmon/internal/layout"
	"os"
	"strings"
	"testing"
)

func TestParseBackend(t *testing.T) {
	t.Setenv("NORMA_TRANSPORT", "")
	t.Setenv("NFSMON_TRANSPORT", "")
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "default", want: "native"},
		{name: "native", args: []string{"--backend", "native"}, want: "native"},
		{name: "bpftrace", args: []string{"--backend=bpftrace"}, want: "bpftrace"},
		{name: "legacy simple shortcut", args: []string{"simple", "--backend", "bpftrace"}, want: "bpftrace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			o, err := Parse("norma", tc.args, &out)
			if err != nil {
				t.Fatal(err)
			}
			if o.Backend != tc.want {
				t.Fatalf("backend = %q, want %q", o.Backend, tc.want)
			}
			if tc.name == "legacy simple shortcut" && !o.Text {
				t.Fatal("simple shortcut must still select text output")
			}
		})
	}
}

func TestTransportPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name, primary, legacy, want string
		args                        []string
	}{
		{name: "default", want: "rdma"},
		{name: "norma environment", primary: "tcp", want: "tcp"},
		{name: "legacy environment", legacy: "udp", want: "udp"},
		{name: "norma overrides legacy", primary: "tcp", legacy: "udp", want: "tcp"},
		{name: "explicit transport overrides environment", primary: "tcp", legacy: "udp", args: []string{"-p", "rdma"}, want: "rdma"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NORMA_TRANSPORT", tc.primary)
			t.Setenv("NFSMON_TRANSPORT", tc.legacy)
			var out bytes.Buffer
			o, err := Parse("norma", tc.args, &out)
			if err != nil {
				t.Fatal(err)
			}
			if o.Transport != tc.want {
				t.Fatalf("transport = %q, want %q", o.Transport, tc.want)
			}
		})
	}
}

func TestInvalidBackendBeforeLayoutResolution(t *testing.T) {
	oldArgs := os.Args
	os.Args = []string{"norma", "--backend", "unknown", "--layout", "/does-not-exist/norma-layout.json"}
	t.Cleanup(func() { os.Args = oldArgs })
	opened := false
	err := entry("norma", func(layout.Layout, Options) (collect.Source, error) {
		opened = true
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "--backend must be native or bpftrace") {
		t.Fatalf("got %v, want backend validation error before reading layout", err)
	}
	if opened {
		t.Fatal("collector opened with an invalid backend")
	}
}

func TestNormaHelpAndVersion(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "help", args: []string{"--help"}, want: "Usage: norma [options]"},
		{name: "version", args: []string{"--version"}, want: "norma " + Version + " (" + Commit + ")"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			o, err := Parse("norma", tc.args, &out)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("output %q does not contain %q", out.String(), tc.want)
			}
			if tc.name == "help" && (!o.Help || !strings.Contains(out.String(), "-backend")) {
				t.Fatal("help must expose the backend selector")
			}
			if tc.name == "version" && !o.Version {
				t.Fatal("version flag was not set")
			}
		})
	}
}
