// SPDX-License-Identifier: MIT
package app

import (
	"errors"
	"flag"
	"fmt"
	"nfsmon/internal/collect"
	"nfsmon/internal/layout"
	"os"
)

type Factory func(layout.Layout, Options) (collect.Source, error)

func Main(name string, open Factory) {
	if err := entry(name, open); err != nil {
		fmt.Fprintln(os.Stderr, name+": "+err.Error())
		os.Exit(1)
	}
}
func entry(name string, open Factory) error {
	if len(os.Args) == 4 && os.Args[1] == "--layout-object" {
		b, err := layout.ExportObject(os.Args[2], os.Args[3])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, string(b))
		return err
	}
	o, err := Parse(name, os.Args[1:], os.Stdout)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil || o.Help || o.Version {
		return err
	}
	l, err := layout.Resolve(o.Layout)
	if err != nil {
		return err
	}
	src, err := open(l, o)
	if err != nil {
		return err
	}
	defer src.Close() // Runs after normal exit, errors and terminal restoration.
	return Run(name, o, src)
}
