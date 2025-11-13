// SPDX-FileCopyrightText: 2025 OpenCHAMI Contributors
//
// SPDX-License-Identifier: MIT

// Package initbmcs provides functions to generate BMC entries for initial inventory.
package initbmcs

import (
	"fmt"
	"strings"

	"bootstrap/internal/inventory"
	"bootstrap/internal/netalloc"
)

func getBmcID(n int) int { return (n + 1) / 2 } //nolint:unused
func getSlot(n int) int  { return ((n - 1) / 4) % 8 }
func getBlade(n int) int { return ((n - 1) / 2) % 2 }

func getNCXname(chassis string, n int) string {
	return fmt.Sprintf("%ss%db%d", chassis, getSlot(n), getBlade(n))
}

func getNCMAC(macStart string, n int) string {
	return fmt.Sprintf("%s:%d%d:%d0", macStart, 3, getSlot(n), getBlade(n))
}

// ParseChassisSpec parses a chassis specification string into a map of chassis xnames to MAC prefixes.
func ParseChassisSpec(spec string) map[string]string {
	out := map[string]string{}
	if strings.TrimSpace(spec) == "" {
		return out
	}
	parts := strings.Split(spec, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if k != "" && v != "" {
			out[k] = v
		}
	}
	return out
}

// Generate creates the BMC entries for an initial inventory.
// bmcSubnet should be in CIDR notation, e.g. "192.168.100.0/24"
func Generate(chassis map[string]string, nodesPerChassis, nodesPerBMC, startNID int, bmcSubnet string) ([]inventory.Entry, error) {
	alloc, err := netalloc.NewAllocator(bmcSubnet)
	if err != nil {
		return nil, fmt.Errorf("bmc subnet init: %w", err)
	}

	var bmcs []inventory.Entry
	nid := startNID
	for c, macPref := range chassis {
		for i := nid; i < nid+nodesPerChassis; i += nodesPerBMC {
			x := getNCXname(c, i)
			ip, err := alloc.Next()
			if err != nil {
				return nil, fmt.Errorf("allocate IP for %s: %w", x, err)
			}
			mac := strings.ToLower(getNCMAC(macPref, i))
			bmcs = append(bmcs, inventory.Entry{Xname: x, MAC: mac, IP: ip})
		}
		nid = nid + nodesPerChassis
	}
	return bmcs, nil
}
