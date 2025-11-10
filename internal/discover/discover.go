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
		macs, err := redfish.DiscoverBootableMACs(ctx, host, user, pass, insecure, timeout)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: %s: discover: %v\n", b.Xname, err)
			continue
		}
		if len(macs) == 0 {
			fmt.Fprintf(os.Stderr, "WARN: %s: no NICs discovered\n", b.Xname)
			continue
		}

		// Use only the first bootable MAC for PXE booting
		mac := macs[0]
		nodeX := xname.BMCXnameToNode(b.Xname)

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
