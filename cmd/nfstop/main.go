// SPDX-License-Identifier: MIT
package main

import (
	"nfsmon/internal/app"
	"nfsmon/internal/collect"
	"nfsmon/internal/layout"
	"nfsmon/internal/native"
)

func main() {
	app.Main("nfstop", func(l layout.Layout, o app.Options) (collect.Source, error) {
		return native.Open(l, o.Protocol(), o.Group)
	})
}
