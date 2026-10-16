// SPDX-License-Identifier: MIT
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
)

type Node struct {
	Name  string `json:"name"`
	Group string `json:"group"`
}

// LoadNodes maps exact IPs to operator-maintained labels. The IP remains the
// accounting identity: names and roles never merge two clients implicitly.
func LoadNodes(path string) (map[string]Node, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var config struct {
		Nodes map[string]Node `json:"nodes"`
	}
	decoder := json.NewDecoder(io.LimitReader(f, (1<<20)+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON configuration object")
	}
	if config.Nodes == nil {
		return nil, fmt.Errorf("missing nodes object")
	}
	nodes := make(map[string]Node, len(config.Nodes))
	for raw, node := range config.Nodes {
		address, err := netip.ParseAddr(raw)
		if err != nil || address.Zone() != "" {
			return nil, fmt.Errorf("invalid node IP %q", raw)
		}
		key := address.Unmap().String()
		if _, exists := nodes[key]; exists {
			return nil, fmt.Errorf("duplicate node address %s", key)
		}
		node.Name, node.Group = strings.TrimSpace(node.Name), strings.TrimSpace(node.Group)
		if len(node.Name) > 128 || len(node.Group) > 64 || node.Name != clean(node.Name) || node.Group != clean(node.Group) {
			return nil, fmt.Errorf("invalid label for %s: use printable name <=128 bytes and group <=64 bytes", key)
		}
		nodes[key] = node
	}
	return nodes, nil
}
