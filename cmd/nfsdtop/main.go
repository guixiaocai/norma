// SPDX-License-Identifier: MIT
package main

import (
	"nfsmon/internal/app"
	"nfsmon/internal/collect"
	"nfsmon/internal/layout"
	"nfsmon/internal/trace"
)

func main() {
	app.Main("nfsdtop", func(l layout.Layout, o app.Options) (collect.Source, error) {
		return trace.Open(l, o.Protocol(), o.Group, o.Interval)
	})
}
