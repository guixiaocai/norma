// SPDX-License-Identifier: MIT
package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"nfsmon/internal/layout"
)

type doctorHost struct {
	arch, system string
	euid         int
	readFile     func(string) ([]byte, error)
	lookPath     func(string) (string, error)
	kernel       func() (string, error)
	resolve      func(string) (layout.Layout, error)
	bpfVersion   func() (string, error)
}

// Doctor only reads prerequisites. It never loads BPF, changes services or installs packages.
func Doctor(o Options, out io.Writer) error {
	return runDoctor(o, out, doctorHost{
		arch: runtime.GOARCH, system: runtime.GOOS, euid: os.Geteuid(),
		readFile: os.ReadFile, lookPath: exec.LookPath, kernel: layout.Kernel, resolve: layout.Resolve,
		bpfVersion: func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			b, err := exec.CommandContext(ctx, "bpftrace", "--version").CombinedOutput()
			return strings.TrimSpace(string(b)), err
		},
	})
}

func runDoctor(o Options, out io.Writer, host doctorHost) error {
	failures := 0
	check := func(status, name, detail string) {
		fmt.Fprintf(out, "  %-4s  %-16s %s\n", status, name, clean(detail))
		if status == "FAIL" {
			failures++
		}
	}
	fmt.Fprintf(out, "norma %s · host check\nRead-only prerequisites for %s / NFS %s\n\n", Version, o.Backend, o.Transport)
	if b, err := host.readFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				check("INFO", "Distribution", strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`))
			}
		}
	}
	if host.system == "linux" && host.arch == "amd64" {
		check("OK", "Architecture", "Linux x86_64")
	} else {
		check("FAIL", "Architecture", host.system+"/"+host.arch+"; this release requires Linux x86_64")
	}
	kernel, err := host.kernel()
	if err != nil {
		check("FAIL", "Kernel", err.Error())
	} else {
		check("INFO", "Kernel", kernel)
	}
	if host.euid == 0 {
		check("OK", "User", "root; kernel policy can still restrict BPF/probes")
	} else {
		check("WARN", "User", "run sudo norma --doctor and sudo norma for normal collection; --demo needs no root")
	}
	if b, err := host.readFile("/proc/fs/nfsd/threads"); err == nil {
		n, parseErr := strconv.Atoi(strings.TrimSpace(string(b)))
		if parseErr == nil && n > 0 {
			check("OK", "NFS server", fmt.Sprintf("%d NFSD threads", n))
		} else {
			check("FAIL", "NFS server", "NFSD has no active threads; run on the server with NFS exports already serving")
		}
	} else {
		check("FAIL", "NFS server", "cannot read /proc/fs/nfsd/threads; check NFS server status (systemctl status nfs-server)")
	}
	if _, err := host.readFile("/sys/kernel/btf/vmlinux"); err == nil {
		check("INFO", "Kernel BTF", "available; layout resolution also checks NFSD prototypes")
	} else {
		check("INFO", "Kernel BTF", "unavailable; a matching prepared layout can be used")
	}
	l, layoutErr := host.resolve(o.Layout)
	if layoutErr == nil {
		check("OK", "NFSD layout", l.Source)
		if b, err := host.readFile("/proc/kallsyms"); err == nil {
			symbols := map[string]bool{}
			for _, line := range strings.Split(string(b), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					symbols[fields[2]] = true
				}
			}
			for _, h := range l.Hooks {
				if symbols[h.Symbol] {
					check("OK", "Probe symbol", h.Symbol)
				} else {
					check("FAIL", "Probe symbol", h.Symbol+" missing from /proc/kallsyms")
				}
			}
		} else {
			check("WARN", "Probe symbols", "cannot read /proc/kallsyms; symbol availability not checked")
		}
	} else {
		check("FAIL", "NFSD layout", layoutErr.Error())
	}
	if o.Backend == "bpftrace" {
		if _, err := host.lookPath("bpftrace"); err != nil {
			check("FAIL", "bpftrace", "not installed; use native (default), or install bpftrace >= 0.16")
		} else if version, err := host.bpfVersion(); err != nil {
			check("FAIL", "bpftrace", "could not run bpftrace --version: "+err.Error())
		} else if !supportedBpftrace(version) {
			check("FAIL", "bpftrace", version+"; requires a recognizable version >= 0.16")
		} else {
			check("OK", "bpftrace", version)
		}
	} else {
		check("OK", "Collector", "embedded native eBPF; no Go, clang or bpftrace needed at runtime")
	}
	if b, err := host.readFile("/sys/kernel/security/lockdown"); err == nil && !strings.Contains(string(b), "[none]") {
		check("WARN", "Lockdown", strings.TrimSpace(string(b))+"; security policy may prevent tracing")
	}
	if layoutErr != nil && strings.HasPrefix(kernel, "4.18.0-") && strings.Contains(kernel, ".el8") {
		fmt.Fprintln(out, "\nEL8 / Rocky Linux 8 layout preparation:")
		for _, tool := range []string{"gcc", "make"} {
			if _, err := host.lookPath(tool); err != nil {
				check("INFO", "Layout helper", tool+" missing (only needed for header preparation)")
			}
		}
		header := filepath.Join("/lib/modules", kernel, "build/include/generated/utsrelease.h")
		b, err := host.readFile(header)
		if err != nil || !strings.Contains(string(b), `"`+kernel+`"`) {
			check("INFO", "Kernel headers", "matching kernel-devel is missing or mismatched")
		}
		fmt.Fprintln(out, "  sudo dnf install gcc make \"kernel-devel-$(uname -r)\"")
		fmt.Fprintln(out, "  sudo /usr/local/share/norma/tools/prepare-layout.sh")
		fmt.Fprintln(out, "  sudo norma --doctor")
		fmt.Fprintln(out, "  For a custom prefix, use <prefix>/share/norma/tools/prepare-layout.sh.")
		fmt.Fprintln(out, "  Headers must match the running kernel exactly; see docs/rocky-linux-8.10.md if the package is unavailable.")
	}
	fmt.Fprintln(out, "\nThis check does not load or attach probes, test BPF permissions, or validate workload counts.")
	if failures > 0 {
		fmt.Fprintln(out, "Fix the failed prerequisites above. Preview the interface anytime: norma --demo")
		return fmt.Errorf("host check found %d failed prerequisite(s)", failures)
	}
	fmt.Fprintln(out, "Prerequisite checks passed. Next: sudo norma (or retain your --layout / --backend options).")
	return nil
}

func supportedBpftrace(version string) bool {
	m := regexp.MustCompile(`\bbpftrace\s+v?(\d+)\.(\d+)(?:\.|\b)`).FindStringSubmatch(version)
	if len(m) != 3 {
		return false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	return major > 0 || minor >= 16
}
