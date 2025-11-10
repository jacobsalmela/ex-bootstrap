package redfish

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsBootable_UefiPXE(t *testing.T) {
	nic := rfEthernetInterface{UefiDevicePath: "VenHw(PXE)"}
	if !isBootable(nic) {
		t.Fatal("expected bootable due to UEFI PXE")
	}
}

func TestIsBootable_DHCPOrigin(t *testing.T) {
	nic := rfEthernetInterface{IPv4Addresses: []struct {
		Address string "json:\"Address\""
		Origin  string "json:\"AddressOrigin\""
	}{{Address: "10.0.0.2", Origin: "DHCP"}}}
	if !isBootable(nic) {
		t.Fatal("expected bootable due to DHCP origin")
	}
}

func TestIsBootable_MACEnabled(t *testing.T) {
	nic := rfEthernetInterface{MACAddress: "AA:BB:CC:DD:EE:FF"}
	if !isBootable(nic) {
		t.Fatal("expected bootable with MAC and default enabled")
	}
}

func TestIsBootable_MACDisabled(t *testing.T) {
	enabled := false
	nic := rfEthernetInterface{MACAddress: "AA:BB:CC:DD:EE:FF", InterfaceEnabled: &enabled}
	if isBootable(nic) {
		t.Fatal("expected not bootable when interface disabled")
	}
}

func TestIsBootable_False(t *testing.T) {
	if isBootable(rfEthernetInterface{}) {
		t.Fatal("expected not bootable for empty NIC")
	}
}

func TestIsValidMAC(t *testing.T) {
	tests := []struct {
		name string
		mac  string
		want bool
	}{
		{
			name: "Valid MAC with colons",
			mac:  "00:40:a6:88:d9:01",
			want: true,
		},
		{
			name: "Valid MAC with dashes",
			mac:  "00-40-a6-88-d9-01",
			want: true,
		},
		{
			name: "Valid MAC uppercase",
			mac:  "AA:BB:CC:DD:EE:FF",
			want: true,
		},
		{
			name: "Valid MAC lowercase",
			mac:  "aa:bb:cc:dd:ee:ff",
			want: true,
		},
		{
			name: "Invalid MAC - Not Available",
			mac:  "Not Available",
			want: false,
		},
		{
			name: "Invalid MAC - empty string",
			mac:  "",
			want: false,
		},
		{
			name: "Invalid MAC - wrong format",
			mac:  "00:40:a6:88:d9",
			want: false,
		},
		{
			name: "Invalid MAC - invalid characters",
			mac:  "00:40:a6:88:d9:zz",
			want: false,
		},
		{
			name: "Invalid MAC - too long",
			mac:  "00:40:a6:88:d9:01:02",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidMAC(tt.mac)
			if got != tt.want {
				t.Errorf("isValidMAC(%q) = %v, want %v", tt.mac, got, tt.want)
			}
		})
	}
}

func TestClientURLs(t *testing.T) {
	host := "example.com"
	user := "admin"
	pass := "password"
	insecure := true
	tests := []struct {
		name      string
		call      func(c *client) error
		wantPaths []string
	}{
		{
			name: "GET Systems",
			call: func(c *client) error {
				_, err := c.firstSystemPath(context.Background())
				return err
			},
			wantPaths: []string{"/redfish/v1/Systems"},
		},
		{
			name: "GET EthernetInterfaces for System",
			call: func(c *client) error {
				_, err := c.listEthernetInterfaces(context.Background(), "/Systems/1")
				return err
			},
			wantPaths: []string{
				"/redfish/v1/Systems/1/EthernetInterfaces",
				"/redfish/v1/Systems/1/EthernetInterfaces/1",
			},
		},
		{
			name: "POST SimpleUpdate",
			call: func(c *client) error {
				return c.post(context.Background(), "/UpdateService/Actions/SimpleUpdate", map[string]string{})
			},
			wantPaths: []string{"/redfish/v1/UpdateService/Actions/SimpleUpdate"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotPaths []string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPaths = append(gotPaths, r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				// Return mock Redfish responses
				switch r.URL.Path {
				case "/redfish/v1/Systems":
					w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1"}]}`)) //nolint:errcheck
				case "/redfish/v1/Systems/1/EthernetInterfaces":
					w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/EthernetInterfaces/1"}]}`)) //nolint:errcheck
				case "/redfish/v1/Systems/1/EthernetInterfaces/1":
					w.Write([]byte(`{"Id":"1","MACAddress":"aa:bb:cc:dd:ee:ff"}`)) //nolint:errcheck
				default:
					w.Write([]byte(`{}`)) //nolint:errcheck
				}
			}))
			defer ts.Close()

			c := newClient(host, user, pass, insecure, 0)
			c.base = ts.URL + "/redfish/v1"

			if err := tt.call(c); err != nil {
				t.Fatalf("call failed: %v", err)
			}
			// Check that all expected paths were requested
			if len(gotPaths) != len(tt.wantPaths) {
				t.Errorf("got %d requests, want %d", len(gotPaths), len(tt.wantPaths))
			}
			for i, wantPath := range tt.wantPaths {
				if i >= len(gotPaths) {
					t.Errorf("missing request %d: want %q", i, wantPath)
					continue
				}
				if gotPaths[i] != wantPath {
					t.Errorf("request %d: got path %q, want %q", i, gotPaths[i], wantPath)
				}
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	c := &client{base: "https://example.com/redfish/v1"}
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "Relative path",
			path: "/Systems",
			want: "https://example.com/redfish/v1/Systems",
		},
		{
			name: "Absolute URL",
			path: "http://other.com/Systems",
			want: "http://other.com/Systems",
		},
		{
			name: "Already resolved path",
			path: "https://example.com/redfish/v1/Systems",
			want: "https://example.com/redfish/v1/Systems",
		},
		{
			name: "Path without leading slash",
			path: "Systems",
			want: "https://example.com/redfish/v1/Systems",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.resolvePath(tt.path)
			if got != tt.want {
				t.Errorf("resolvePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestDiscoverBootableMACs(t *testing.T) {
	var gotPaths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		// Return mock Redfish responses
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/Self"}]}`)) //nolint:errcheck
		case "/redfish/v1/Systems/Self/EthernetInterfaces":
			_, _ = w.Write([]byte(`{
				"Members":[
					{"@odata.id":"/redfish/v1/Systems/Self/EthernetInterfaces/1"},
					{"@odata.id":"/redfish/v1/Systems/Self/EthernetInterfaces/2"}
				]
			}`))
		case "/redfish/v1/Systems/Self/EthernetInterfaces/1":
			_, _ = w.Write([]byte(`{
				"Id":"1",
				"Name":"NIC 1",
				"MACAddress":"aa:bb:cc:dd:ee:ff",
				"UefiDevicePath":"PciRoot(0x0)/Pci(0x1C,0x0)/Pci(0x0,0x0)/MAC(AABBCCDDEEFF,0x1)"
			}`))
		case "/redfish/v1/Systems/Self/EthernetInterfaces/2":
			_, _ = w.Write([]byte(`{
				"Id":"2",
				"Name":"NIC 2",
				"MACAddress":"11:22:33:44:55:66",
				"IPv4Addresses":[{"Address":"10.0.0.2","AddressOrigin":"DHCP"}]
			}`))
		default:
			w.Write([]byte(`{}`)) //nolint:errcheck
		}
	}))
	defer ts.Close()

	// Create a client with the test server's URL
	c := newClient("example.com", "admin", "password", true, 0)
	c.base = ts.URL + "/redfish/v1"

	// First get the system path
	sysPath, err := c.firstSystemPath(context.Background())
	if err != nil {
		t.Fatalf("firstSystemPath failed: %v", err)
	}

	// Then get the ethernet interfaces for that system
	nics, err := c.listEthernetInterfaces(context.Background(), sysPath)
	if err != nil {
		t.Fatalf("listEthernetInterfaces failed: %v", err)
	}

	// Convert to MAC strings
	var macStrings []string
	for _, nic := range nics {
		if nic.MACAddress != "" {
			macStrings = append(macStrings, nic.MACAddress)
		}
	}

	// Check that we got the expected MACs
	expectedMACs := []string{"aa:bb:cc:dd:ee:ff", "11:22:33:44:55:66"}
	if len(macStrings) != len(expectedMACs) {
		t.Errorf("got %d MACs, want %d", len(macStrings), len(expectedMACs))
	}
	for i, want := range expectedMACs {
		if i >= len(macStrings) {
			t.Errorf("missing MAC %d: want %q", i, want)
			continue
		}
		if macStrings[i] != want {
			t.Errorf("MAC %d: got %q, want %q", i, macStrings[i], want)
		}
	}

	// Verify the correct Redfish paths were requested
	expectedPaths := []string{
		"/redfish/v1/Systems",
		"/redfish/v1/Systems/Self/EthernetInterfaces",
		"/redfish/v1/Systems/Self/EthernetInterfaces/1",
		"/redfish/v1/Systems/Self/EthernetInterfaces/2",
	}
	if len(gotPaths) != len(expectedPaths) {
		t.Errorf("got %d requests, want %d", len(gotPaths), len(expectedPaths))
	}
	for i, want := range expectedPaths {
		if i >= len(gotPaths) {
			t.Errorf("missing request %d: want %q", i, want)
			continue
		}
		if gotPaths[i] != want {
			t.Errorf("request %d: got path %q, want %q", i, gotPaths[i], want)
		}
	}
}

func TestDiscoverBootableMACs_WithInvalidMACs(t *testing.T) {
	var gotPaths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		// Simulate HPE Cray system with "Not Available" MACs
		switch r.URL.Path {
		case "/redfish/v1/Systems":
			w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/Node0"}]}`)) //nolint:errcheck
		case "/redfish/v1/Systems/Node0/EthernetInterfaces":
			_, _ = w.Write([]byte(`{
				"Members":[
					{"@odata.id":"/redfish/v1/Systems/Node0/EthernetInterfaces/HPCNet2"},
					{"@odata.id":"/redfish/v1/Systems/Node0/EthernetInterfaces/HPCNet3"},
					{"@odata.id":"/redfish/v1/Systems/Node0/EthernetInterfaces/ManagementEthernet"}
				]
			}`))
		case "/redfish/v1/Systems/Node0/EthernetInterfaces/HPCNet2":
			_, _ = w.Write([]byte(`{
				"Id":"HPCNet2",
				"Name":"HPCNet2",
				"Description":"SS11 200Gb 2P NIC Mezz REV02 (HSN)",
				"MACAddress":"Not Available",
				"PermanentMACAddress":""
			}`))
		case "/redfish/v1/Systems/Node0/EthernetInterfaces/HPCNet3":
			_, _ = w.Write([]byte(`{
				"Id":"HPCNet3",
				"Name":"HPCNet3",
				"Description":"SS11 200Gb 2P NIC Mezz REV02 (HSN)",
				"MACAddress":"Not Available",
				"PermanentMACAddress":""
			}`))
		case "/redfish/v1/Systems/Node0/EthernetInterfaces/ManagementEthernet":
			w.Write([]byte(`{
				"Id":"ManagementEthernet",
				"Name":"Management Ethernet",
				"Description":"Node Maintenance Network",
				"MACAddress":"00:40:a6:88:d9:01",
				"PermanentMACAddress":"00:40:a6:88:d9:01"
			}`))
		default:
			w.Write([]byte(`{}`)) //nolint:errcheck
		}
	}))
	defer ts.Close()

	// Create a client with the test server's URL
	c := newClient("example.com", "admin", "password", true, 0)
	c.base = ts.URL + "/redfish/v1"

	// First get the system path
	sysPath, err := c.firstSystemPath(context.Background())
	if err != nil {
		t.Fatalf("firstSystemPath failed: %v", err)
	}

	// Then get the ethernet interfaces for that system
	nics, err := c.listEthernetInterfaces(context.Background(), sysPath)
	if err != nil {
		t.Fatalf("listEthernetInterfaces failed: %v", err)
	}

	// Convert to MAC strings, filtering out invalid MACs
	var macStrings []string
	for _, nic := range nics {
		if isValidMAC(nic.MACAddress) {
			macStrings = append(macStrings, nic.MACAddress)
		}
	}

	// Check that we only got the valid MAC (ManagementEthernet)
	expectedMACs := []string{"00:40:a6:88:d9:01"}
	if len(macStrings) != len(expectedMACs) {
		t.Errorf("got %d MACs, want %d", len(macStrings), len(expectedMACs))
	}
	for i, want := range expectedMACs {
		if i >= len(macStrings) {
			t.Errorf("missing MAC %d: want %q", i, want)
			continue
		}
		if macStrings[i] != want {
			t.Errorf("MAC %d: got %q, want %q", i, macStrings[i], want)
		}
	}

	// Verify all interfaces were queried but only valid MACs returned
	expectedPaths := []string{
		"/redfish/v1/Systems",
		"/redfish/v1/Systems/Node0/EthernetInterfaces",
		"/redfish/v1/Systems/Node0/EthernetInterfaces/HPCNet2",
		"/redfish/v1/Systems/Node0/EthernetInterfaces/HPCNet3",
		"/redfish/v1/Systems/Node0/EthernetInterfaces/ManagementEthernet",
	}
	if len(gotPaths) != len(expectedPaths) {
		t.Errorf("got %d requests, want %d", len(gotPaths), len(expectedPaths))
	}
	for i, want := range expectedPaths {
		if i >= len(gotPaths) {
			t.Errorf("missing request %d: want %q", i, want)
			continue
		}
		if gotPaths[i] != want {
			t.Errorf("request %d: got path %q, want %q", i, gotPaths[i], want)
		}
	}
}
