package discover

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"bootstrap/internal/inventory"
	"bootstrap/internal/netalloc"
	"bootstrap/internal/redfish"
	"bootstrap/internal/xname"
)

// UpdateNodes reads existing nodes for reservations, discovers bootable NICs per BMC,
// allocates IPs, and returns the new nodes list.
func UpdateNodes(doc *inventory.FileFormat, bmcSubnet, nodeSubnet string, user, pass string, insecure bool, timeout time.Duration) ([]inventory.Entry, error) {
	// Create allocator for node IPs
	nodeAlloc, err := netalloc.NewAllocator(nodeSubnet)
	if err != nil {
		return nil, fmt.Errorf("node ipam init: %w", err)
	}

	// Reserve existing node IPs
	for _, n := range doc.Nodes {
		if ip := net.ParseIP(n.IP); ip != nil {
			nodeAlloc.Reserve(ip.String())
		}
	}

	// Create BMC allocator if subnet is different, otherwise reuse node allocator
	var bmcAlloc *netalloc.Allocator
	if bmcSubnet == nodeSubnet {
		bmcAlloc = nodeAlloc
	} else {
		bmcAlloc, err = netalloc.NewAllocator(bmcSubnet)
		if err != nil {
			return nil, fmt.Errorf("bmc ipam init: %w", err)
		}
		// Reserve existing BMC IPs
		for _, b := range doc.BMCs {
			if ip := net.ParseIP(b.IP); ip != nil {
				bmcAlloc.Reserve(ip.String())
			}
		}
	}

	out := make([]inventory.Entry, 0, len(doc.BMCs))

	for _, b := range doc.BMCs {
		host := b.IP
		if host == "" {
			host = b.Xname
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		systemMACs, err := redfish.DiscoverAllBootableMACs(ctx, host, user, pass, insecure, timeout)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: %s: discover: %v\n", b.Xname, err)
			continue
		}
		if len(systemMACs) == 0 {
			fmt.Fprintf(os.Stderr, "WARN: %s: no systems discovered\n", b.Xname)
			continue
		}

		// Process each system (e.g., Node0, Node1) found on this BMC
		for sysIdx, sysMacs := range systemMACs {
			if len(sysMacs.MACs) == 0 {
				fmt.Fprintf(os.Stderr, "WARN: %s %s: no NICs discovered\n", b.Xname, sysMacs.SystemPath)
				continue
			}

			// Use only the first bootable MAC for PXE booting
			mac := sysMacs.MACs[0]

			// Generate node xname with proper node number
			// For single-system BMCs, use node 0
			// For multi-system BMCs, use the system index as node number
			nodeX := xname.BMCXnameToNodeN(b.Xname, sysIdx)

			existing := findByXname(doc.Nodes, nodeX)
			ipStr := ""
			if existing != nil && net.ParseIP(existing.IP) != nil {
				ipStr = existing.IP
				nodeAlloc.Reserve(ipStr)
			} else {
				var err error
				ipStr, err = nodeAlloc.Next()
				if err != nil {
					return nil, fmt.Errorf("ip allocate for %s: %w", nodeX, err)
				}
			}
			out = append(out, inventory.Entry{Xname: nodeX, MAC: mac, IP: ipStr})
		}
	}
	return out, nil
}

func findByXname(list []inventory.Entry, x string) *inventory.Entry {
	for i := range list {
		if list[i].Xname == x {
			return &list[i]
		}
	}
	return nil
}
