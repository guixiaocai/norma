// SPDX-License-Identifier: MIT
package main

import (
	"nfsmon/internal/app"
	"nfsmon/internal/collect"
	"nfsmon/internal/layout"
	"nfsmon/internal/native"
	"nfsmon/internal/trace"
)

func main() {
	app.Main("norma", func(l layout.Layout, o app.Options) (collect.Source, error) {
		if o.Backend == "bpftrace" {
			return trace.Open(l, 0, o.Group, o.Interval)
		}
		return native.Open(l, 0, o.Group)
	})
}
