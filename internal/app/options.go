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

var Version = "v0.2.0"
var Commit = "source"

type Options struct {
	Transport, Interface, Group, Layout, Sort     string
	Interval, Window, Duration                    time.Duration
	Text, Bits, Numeric, Version, Help, JSON, Ops bool
	Limit                                         int
	Filter                                        string
	Local                                         map[netip.Addr]bool
	Client                                        *netip.Prefix
}

func Parse(name string, args []string, out io.Writer) (Options, error) {
	o := Options{Group: "clients", Numeric: true, Interval: time.Second, Window: 5 * time.Minute, Sort: "now", Limit: 20}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&o.Transport, "p", "", "rdma (default), tcp, udp, all")
	fs.StringVar(&o.Transport, "transport", "", "transport selector")
	fs.StringVar(&o.Interface, "i", "", "local network interface; implies TCP unless -p is supplied")
	fs.BoolVar(&o.Text, "t", false, "text output; no terminal control")
	fs.BoolVar(&o.JSON, "json", false, "JSON Lines output, implies -t")
	fs.BoolVar(&o.Numeric, "n", true, "numeric client addresses (default)")
	names := fs.Bool("N", false, "resolve names asynchronously")
	bytes := fs.Bool("B", false, "automatic byte/s units (default)")
	fs.BoolVar(&o.Bits, "b", false, "automatic bit/s units")
	fs.BoolVar(&o.Ops, "ops", false, "show completed operations/s instead of bytes/s")
	fs.DurationVar(&o.Window, "w", 5*time.Minute, "average/peak window, for example 3m or 10m")
	fs.DurationVar(&o.Interval, "r", time.Second, "refresh/sample interval, for example 1s")
	seconds := fs.Float64("s", 0, "exit after this number of seconds (0: unlimited)")
	fs.StringVar(&o.Filter, "F", "", "client IP or CIDR network")
	fs.StringVar(&o.Group, "g", "clients", "clients, servers, users, groups, files")
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
		fmt.Fprintln(out, "\nExamples:\n  sudo "+name+"\n  sudo "+name+" -p tcp\n  sudo "+name+" -i eno2 -w 10m\n  sudo "+name+" -t -s 120\n\nKeys: q/Ctrl+C quit | B bytes/bits | s sort | t read/write/both/total | p pause display | ↑/↓ scroll\nREAD = client reads from server; WRITE = client writes to server. -i filters the interface's local IPs, not packet ingress.")
	}
	// Retain old nfstop launch shortcuts without making users choose a view.
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
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "p" || f.Name == "transport" {
			explicit = true
		}
	})
	if !explicit {
		if o.Interface != "" {
			o.Transport = "tcp"
		} else if env := os.Getenv("NFSMON_TRANSPORT"); env != "" {
			o.Transport = env
		} else {
			o.Transport = "rdma"
		}
	}
	switch o.Transport {
	case "rdma", "tcp", "udp", "all":
	default:
		return o, fmt.Errorf("-p must be rdma, tcp, udp or all")
	}
	switch o.Group {
	case "clients", "servers", "users", "groups", "files":
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
