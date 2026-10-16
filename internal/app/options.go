// SPDX-License-Identifier: MIT
package app

import (
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"
)

var Version = "v0.5.0"
var Commit = "source"

type Options struct {
	Backend, Transport, Interface, Group, Layout, Sort string
	Interval, Window, Duration                         time.Duration
	Text, Bits, Numeric, Version, Help, JSON, Ops      bool
	Demo, Doctor, NoColor                              bool
	Limit                                              int
	Filter                                             string
	FileFilter, Config                                 string
	Nodes                                              map[string]Node
	Local                                              map[netip.Addr]bool
	Client                                             *netip.Prefix
}

func Parse(name string, args []string, out io.Writer) (Options, error) {
	o := Options{Group: "clients", Numeric: true, Interval: time.Second, Window: 5 * time.Minute, Sort: "now", Limit: 20}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.Backend, "backend", "native", "collector: native (default) or bpftrace")
	fs.StringVar(&o.Transport, "p", "", "all (default), rdma, tcp, udp")
	fs.StringVar(&o.Transport, "transport", "", "transport selector")
	fs.StringVar(&o.Interface, "i", "", "local network interface; implies TCP unless -p is supplied")
	fs.BoolVar(&o.Text, "t", false, "text output; no terminal control")
	fs.BoolVar(&o.JSON, "json", false, "JSON Lines output, implies -t")
	fs.BoolVar(&o.Demo, "demo", false, "preview with synthetic data; no root or NFS server needed")
	fs.BoolVar(&o.Doctor, "doctor", false, "check host prerequisites without loading probes")
	fs.BoolVar(&o.NoColor, "no-color", os.Getenv("NO_COLOR") != "", "disable dashboard colors (also NO_COLOR)")
	fs.BoolVar(&o.Numeric, "n", true, "numeric client addresses (default)")
	names := fs.Bool("N", false, "resolve names asynchronously")
	bytes := fs.Bool("B", false, "automatic byte/s units (default)")
	fs.BoolVar(&o.Bits, "b", false, "automatic bit/s units")
	fs.BoolVar(&o.Ops, "ops", false, "show completed operations/s instead of bytes/s")
	fs.DurationVar(&o.Window, "w", 5*time.Minute, "average/peak window, for example 3m or 10m")
	fs.DurationVar(&o.Interval, "r", time.Second, "refresh/sample interval, for example 1s")
	seconds := fs.Float64("s", 0, "exit after this number of seconds (0: unlimited)")
	fs.StringVar(&o.Filter, "F", "", "client IP or CIDR network")
	fs.StringVar(&o.FileFilter, "file-filter", "", "case-insensitive filename hint substring (not an absolute path)")
	fs.StringVar(&o.Config, "config", os.Getenv("NORMA_CONFIG"), "node naming/group configuration JSON")
	fs.StringVar(&o.Group, "g", "clients", "clients, servers, users, groups, files, transports, nodegroups")
	fs.StringVar(&o.Sort, "sort", "now", "now, 1m, window, peak, total, name")
	fs.IntVar(&o.Limit, "l", 20, "maximum entities in text output (0: all)")
	fs.StringVar(&o.Layout, "layout", "", "advanced: kernel layout JSON file")
	fs.BoolVar(&o.Version, "v", false, "print version")
	fs.BoolVar(&o.Version, "version", false, "print version")
	fs.BoolVar(&o.Help, "h", false, "show help")
	fs.BoolVar(&o.Help, "help", false, "show help")
	fs.Usage = func() {
		fmt.Fprintf(out, "%s %s — NFS server I/O by client\n\nUsage: %s [options]\n\n", name, Version, name)
		fs.PrintDefaults()
		fmt.Fprintln(out, "\nExamples:\n  "+name+" --demo\n  sudo "+name+" --doctor\n  sudo "+name+"\n  sudo "+name+" --config /path/to/nodes.json\n  sudo "+name+" -g transports\n  sudo "+name+" -g files --file-filter dataset\n  sudo "+name+" --backend bpftrace\n  sudo "+name+" -p rdma\n  sudo "+name+" -i eno2 -w 10m\n  sudo "+name+" -t -s 120\n\nKeys: q/Ctrl+C quit | B units | o ops | s sort | t direction | T transport | g group | / filter\n      ↑/↓ select | Enter drill down | u files/UID | d diagnostics | Esc back | p pause\nREAD = client reads from server; WRITE = client writes to server. -i filters the interface's local IPs, not packet ingress.")
	}
	// Retain positional launch shortcuts without making users choose a view.
	if len(args) > 0 && (args[0] == "clients" || args[0] == "tui" || args[0] == "simple") {
		if args[0] == "simple" {
			o.Text = true
		}
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if o.Help {
		fs.Usage()
		return o, nil
	}
	if o.Version {
		fmt.Fprintf(out, "%s %s (%s)\n", name, Version, Commit)
		return o, nil
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if o.Demo && o.Doctor {
		return o, fmt.Errorf("--demo and --doctor cannot be used together")
	}
	if o.Doctor && o.JSON {
		return o, fmt.Errorf("--doctor produces a readable checklist; --json is for collection")
	}
	switch o.Backend {
	case "native", "bpftrace":
	default:
		return o, fmt.Errorf("--backend must be native or bpftrace (got %q)", o.Backend)
	}
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "p" || f.Name == "transport" {
			explicit = true
		}
	})
	if !explicit {
		if o.Interface != "" {
			o.Transport = "tcp"
		} else if env := os.Getenv("NORMA_TRANSPORT"); env != "" {
			o.Transport = env
		} else if env := os.Getenv("NFSMON_TRANSPORT"); env != "" {
			o.Transport = env
		} else {
			o.Transport = "all"
		}
	}
	switch o.Transport {
	case "rdma", "tcp", "udp", "all":
	default:
		return o, fmt.Errorf("-p must be rdma, tcp, udp or all")
	}
	switch o.Group {
	case "clients", "servers", "users", "groups", "files", "transports", "nodegroups":
	default:
		return o, fmt.Errorf("unknown group %q", o.Group)
	}
	switch o.Sort {
	case "now", "1m", "window", "peak", "total", "name":
	default:
		return o, fmt.Errorf("unknown sort %q", o.Sort)
	}
	if o.Window < time.Second || o.Window > 24*time.Hour {
		return o, fmt.Errorf("-w must be between 1s and 24h")
	}
	if o.Interval < 250*time.Millisecond || o.Interval > time.Minute {
		return o, fmt.Errorf("-r must be between 250ms and 1m")
	}
	if o.Limit < 0 || *seconds < 0 || math.IsInf(*seconds, 0) || math.IsNaN(*seconds) || *seconds > (24*time.Hour).Seconds() {
		return o, fmt.Errorf("-l must be nonnegative; -s must be finite and between 0 and 86400 seconds")
	}
	o.Duration = time.Duration(*seconds * float64(time.Second))
	o.Numeric = !*names && o.Numeric
	if *bytes {
		o.Bits = false
	}
	if o.JSON {
		o.Text = true
	}
	if o.Filter != "" {
		var p netip.Prefix
		if strings.Contains(o.Filter, "/") {
			x, err := netip.ParsePrefix(o.Filter)
			if err != nil {
				return o, err
			}
			p = x.Masked()
		} else {
			a, err := netip.ParseAddr(o.Filter)
			if err != nil {
				return o, err
			}
			a = a.Unmap()
			p = netip.PrefixFrom(a, a.BitLen())
		}
		o.Client = &p
	}
	if o.Interface != "" {
		iface, err := net.InterfaceByName(o.Interface)
		if err != nil {
			return o, err
		}
		addresses, err := iface.Addrs()
		if err != nil {
			return o, err
		}
		o.Local = make(map[netip.Addr]bool)
		for _, v := range addresses {
			if p, err := netip.ParsePrefix(v.String()); err == nil {
				o.Local[p.Addr().Unmap()] = true
			}
		}
		if len(o.Local) == 0 {
			return o, fmt.Errorf("interface %s has no IP addresses", o.Interface)
		}
	}
	if o.Config != "" {
		nodes, err := LoadNodes(o.Config)
		if err != nil {
			return o, fmt.Errorf("node configuration: %w", err)
		}
		o.Nodes = nodes
	}
	return o, nil
}
func (o Options) Protocol() uint32 {
	switch o.Transport {
	case "rdma":
		return 256
	case "tcp":
		return 6
	case "udp":
		return 17
	}
	return 0
}
