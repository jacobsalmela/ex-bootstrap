package discover

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"bootstrap/internal/inventory"
	"bootstrap/internal/netalloc"
	"bootstrap/internal/redfish"
	"bootstrap/internal/xname"
)

// UpdateNodes reads existing nodes for reservations, discovers bootable NICs per BMC,
// allocates IPs, and returns the new nodes list.
func UpdateNodes(doc *inventory.FileFormat, subnet string, user, pass string, insecure bool, timeout time.Duration) ([]inventory.Entry, error) {
	alloc, err := netalloc.NewAllocator(subnet)
	if err != nil {
		return nil, fmt.Errorf("ipam init: %w", err)
	}
	for _, n := range doc.Nodes {
		if ip := net.ParseIP(n.IP); ip != nil {
			alloc.Reserve(ip.String())
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
		for _, sysMacs := range systemMACs {
			if len(sysMacs.MACs) == 0 {
				fmt.Fprintf(os.Stderr, "WARN: %s %s: no NICs discovered\n", b.Xname, sysMacs.SystemPath)
				continue
			}

			// Use only the first bootable MAC for PXE booting
			mac := sysMacs.MACs[0]
			nodeX := xname.BMCXnameToNode(b.Xname)

			// If there are multiple systems, append the system identifier
			// Extract system name from path (e.g., /redfish/v1/Systems/Node0 -> Node0)
			if len(systemMACs) > 1 {
				parts := strings.Split(sysMacs.SystemPath, "/")
				if len(parts) > 0 {
					sysName := parts[len(parts)-1]
					// Append system name to xname (e.g., x1000c0s0b0n0 -> x1000c0s0b0n0-Node0)
					nodeX = fmt.Sprintf("%s-%s", nodeX, sysName)
				}
			}

			existing := findByXname(doc.Nodes, nodeX)
			ipStr := ""
			if existing != nil && net.ParseIP(existing.IP) != nil {
				ipStr = existing.IP
				alloc.Reserve(ipStr)
			} else {
				var err error
				ipStr, err = alloc.Next()
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
