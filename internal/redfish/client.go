package redfish

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"bootstrap/internal/diag"
)

type client struct {
	base string
	http *http.Client
	user string
	pass string
}

func newClient(host, user, pass string, insecure bool, timeout time.Duration) *client {
	tr := &http.Transport{}
	if insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &client{
		base: "https://" + host + "/redfish/v1",
		http: &http.Client{Timeout: timeout, Transport: tr},
		user: user,
		pass: pass,
	}
}

type rfCollection struct {
	Members []struct {
		OID string `json:"@odata.id"`
	} `json:"Members"`
}

type rfEthernetInterface struct {
	ID               string `json:"Id"`
	Name             string `json:"Name"`
	InterfaceEnabled *bool  `json:"InterfaceEnabled"`
	MACAddress       string `json:"MACAddress"`
	UefiDevicePath   string `json:"UefiDevicePath"`
	IPv4Addresses    []struct {
		Address string `json:"Address"`
		Origin  string `json:"AddressOrigin"`
	} `json:"IPv4Addresses"`
}

func (c *client) get(ctx context.Context, path string, v any) error {
	path = c.resolvePath(path)
	diag.Logf("GET %s", path)
	req, err := http.NewRequestWithContext(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint:errcheck
	diag.Logf("GET %s -> %s", path, resp.Status)
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("redfish %s: %s: %s", path, resp.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (c *client) post(ctx context.Context, path string, body any) error {
	path = c.resolvePath(path)
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	diag.Logf("POST %s", path)
	req, err := http.NewRequestWithContext(ctx, "POST", path, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint:errcheck
	diag.Logf("POST %s -> %s", path, resp.Status)
	if resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("redfish POST %s: %s: %s", path, resp.Status, strings.TrimSpace(string(rb)))
	}
	return nil
}

func (c *client) patch(ctx context.Context, path string, body any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	diag.Logf("PATCH %s", path)
	req, err := http.NewRequestWithContext(ctx, "PATCH", c.base+path, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.user, c.pass)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint:errcheck
	diag.Logf("PATCH %s -> %s", path, resp.Status)
	if resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("redfish PATCH %s: %s: %s", path, resp.Status, strings.TrimSpace(string(rb)))
	}
	return nil
}

func (c *client) firstSystemPath(ctx context.Context) (string, error) {
	var coll rfCollection
	if err := c.get(ctx, "/Systems", &coll); err != nil {
		return "", err
	}
	if len(coll.Members) == 0 {
		return "", errors.New("no systems reported by BMC")
	}
	return coll.Members[0].OID, nil
}

func (c *client) listSystemPaths(ctx context.Context) ([]string, error) {
	var coll rfCollection
	if err := c.get(ctx, "/Systems", &coll); err != nil {
		return nil, err
	}
	if len(coll.Members) == 0 {
		return nil, errors.New("no systems reported by BMC")
	}
	paths := make([]string, len(coll.Members))
	for i, member := range coll.Members {
		paths[i] = member.OID
	}
	return paths, nil
}

func (c *client) listEthernetInterfaces(ctx context.Context, sysPath string) ([]rfEthernetInterface, error) {
	var coll rfCollection
	if err := c.get(ctx, sysPath+"/EthernetInterfaces", &coll); err != nil {
		return nil, err
	}
	var out []rfEthernetInterface
	for _, m := range coll.Members {
		var nic rfEthernetInterface
		if err := c.get(ctx, m.OID, &nic); err != nil {
			return nil, err
		}
		out = append(out, nic)
	}
	return out, nil
}

func isBootable(n rfEthernetInterface) bool {
	uefi := strings.ToLower(n.UefiDevicePath)
	if strings.Contains(uefi, "pxe") || strings.Contains(uefi, "ipv4") || strings.Contains(uefi, "ipv6") || strings.Contains(uefi, "mac(") {
		return true
	}
	for _, a := range n.IPv4Addresses {
		if strings.EqualFold(a.Origin, "dhcp") {
			return true
		}
	}
	if n.MACAddress != "" && (n.InterfaceEnabled == nil || *n.InterfaceEnabled) {
		return true
	}
	return false
}

// isValidMAC checks if a MAC address string is valid
func isValidMAC(mac string) bool {
	if mac == "" || strings.EqualFold(mac, "Not Available") {
		return false
	}
	// Basic MAC address format validation (handles both : and - separators)
	// Valid formats: xx:xx:xx:xx:xx:xx or xx-xx-xx-xx-xx-xx
	parts := strings.FieldsFunc(mac, func(r rune) bool {
		return r == ':' || r == '-'
	})
	if len(parts) != 6 {
		return false
	}
	for _, part := range parts {
		if len(part) != 2 {
			return false
		}
		for _, c := range part {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// SystemMACs represents the bootable MAC addresses for a single system.
type SystemMACs struct {
	SystemPath string
	MACs       []string
}

// DiscoverAllBootableMACs returns bootable MAC addresses for all systems on a BMC.
// Returns a slice of SystemMACs, one entry per system (e.g., Node0, Node1).
func DiscoverAllBootableMACs(ctx context.Context, host, user, pass string, insecure bool, timeout time.Duration) ([]SystemMACs, error) {
	c := newClient(host, user, pass, insecure, timeout)
	sysPaths, err := c.listSystemPaths(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]SystemMACs, 0, len(sysPaths))
	for _, sysPath := range sysPaths {
		nics, err := c.listEthernetInterfaces(ctx, sysPath)
		if err != nil {
			// Skip this system but continue with others
			continue
		}

		// collect bootable MACs, fallback to first valid MAC if none
		macs := make([]string, 0, len(nics))
		for _, nic := range nics {
			if !isValidMAC(nic.MACAddress) {
				continue
			}
			if isBootable(nic) {
				macs = append(macs, strings.ToLower(nic.MACAddress))
			}
		}
		if len(macs) == 0 {
			for _, nic := range nics {
				if isValidMAC(nic.MACAddress) {
					macs = append(macs, strings.ToLower(nic.MACAddress))
					break
				}
			}
		}

		if len(macs) > 0 {
			result = append(result, SystemMACs{
				SystemPath: sysPath,
				MACs:       macs,
			})
		}
	}
	return result, nil
}

// DiscoverBootableMACs returns MAC addresses of bootable NICs for the first system on a BMC.
// Deprecated: Use DiscoverAllBootableMACs to discover all systems on a BMC.
func DiscoverBootableMACs(ctx context.Context, host, user, pass string, insecure bool, timeout time.Duration) ([]string, error) {
	c := newClient(host, user, pass, insecure, timeout)
	sysPath, err := c.firstSystemPath(ctx)
	if err != nil {
		return nil, err
	}
	nics, err := c.listEthernetInterfaces(ctx, sysPath)
	if err != nil {
		return nil, err
	}
	// collect bootable, fallback to first valid MAC if none
	macs := make([]string, 0, len(nics))
	for _, nic := range nics {
		if !isValidMAC(nic.MACAddress) {
			continue
		}
		if isBootable(nic) {
			macs = append(macs, strings.ToLower(nic.MACAddress))
		}
	}
	if len(macs) == 0 {
		for _, nic := range nics {
			if isValidMAC(nic.MACAddress) {
				macs = append(macs, strings.ToLower(nic.MACAddress))
				break
			}
		}
	}
	return macs, nil
}

// SimpleUpdate triggers a firmware update via Redfish SimpleUpdate action.
// imageURI is a URL accessible by the BMC (e.g., http/https), targets are the FirmwareInventory targets.
// transferProtocol is typically "HTTP" or "HTTPS".
func SimpleUpdate(ctx context.Context, host, user, pass string, insecure bool, timeout time.Duration, imageURI string, targets []string, transferProtocol string) error {
	c := newClient(host, user, pass, insecure, timeout)
	payload := map[string]any{
		"ImageURI":         imageURI,
		"TransferProtocol": transferProtocol,
		"Targets":          targets,
	}
	// Vendor path per provided examples
	return c.post(ctx, "/UpdateService/Actions/SimpleUpdate", payload)
}

// SetAuthorizedKeys configures the SSH authorized keys on a BMC.
// The Redfish path used is /Managers/BMC/NetworkProtocol with an OEM payload.
func SetAuthorizedKeys(ctx context.Context, host, user, pass string, insecure bool, timeout time.Duration, authorizedKey string) error {
	c := newClient(host, user, pass, insecure, timeout)
	payload := map[string]any{
		"Oem": map[string]any{
			"SSHAdmin": map[string]any{
				"AuthorizedKeys": authorizedKey,
			},
		},
	}
	return c.patch(ctx, "/Managers/BMC/NetworkProtocol", payload)
}

func (c *client) resolvePath(path string) string {
	// If it's already an absolute URL, return as-is
	if strings.HasPrefix(path, "http") {
		return path
	}
	// If it already has the base prefix, return as-is
	if strings.HasPrefix(path, c.base) {
		return path
	}
	// If it starts with /redfish/v1, it's an absolute Redfish path, so just prepend the scheme+host
	if strings.HasPrefix(path, "/redfish/v1") {
		// Extract the scheme+host from c.base
		baseURL := c.base[:strings.Index(c.base, "/redfish/v1")]
		return baseURL + path
	}
	// Otherwise, it's a relative path, so append to base
	if strings.HasPrefix(path, "/") {
		return c.base + path
	}
	return c.base + "/" + path
}
