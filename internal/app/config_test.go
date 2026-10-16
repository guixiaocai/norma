// SPDX-License-Identifier: MIT
package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNodeConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"IPv4 and IPv6", `{"nodes":{"192.0.2.21":{"name":"compute01","group":"compute"},"2001:db8::1":{"name":"计算节点","group":"compute"}}}`, true},
		{"mapped address", `{"nodes":{"::ffff:192.0.2.21":{"name":"compute01"}}}`, true},
		{"unknown property", `{"nodes":{"192.0.2.21":{"label":"compute01"}}}`, false},
		{"invalid IP", `{"nodes":{"not-an-ip":{"name":"compute01"}}}`, false},
		{"duplicate identity", `{"nodes":{"192.0.2.21":{},"::ffff:192.0.2.21":{}}}`, false},
		{"control character", `{"nodes":{"192.0.2.21":{"name":"x\u001by"}}}`, false},
		{"trailing data", `{"nodes":{}} {}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nodes.json")
			if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			nodes, err := LoadNodes(path)
			if (err == nil) != tc.valid {
				t.Fatalf("nodes=%v, err=%v", nodes, err)
			}
			if tc.name == "mapped address" && nodes["192.0.2.21"].Name != "compute01" {
				t.Fatal("mapped IP was not normalized")
			}
		})
	}
}
