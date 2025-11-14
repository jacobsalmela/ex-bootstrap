// SPDX-FileCopyrightText: 2025 OpenCHAMI Contributors
//
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func makeInventoryFile(t *testing.T, host string) string {
	t.Helper()
	tmp, err := os.CreateTemp("", "fw-status-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("bmcs:\n  - xname: x9000c1s0b0\n    ip: %s\n", host)
	if _, err := tmp.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	return tmp.Name()
}

func TestFirmwareStatusDetectsFailure(t *testing.T) {
	// Mock server that returns a firmware inventory with a download-failed condition
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService/FirmwareInventory/BMC") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/BMC",
				"Id":        "BMC",
				"Version":   "nc.1.10.1",
				"Status": map[string]any{
					"Health": "Warning",
					"State":  "Enabled",
					"Conditions": []map[string]any{
						{
							"Message":   "Firmware package specified in the ImageURI during a SimpleUpdate failed to download. Failed to connect to host.",
							"MessageId": "HPEFirmwareUpdate.1.0.DownloadFailed",
							"Severity":  "Warning",
							"Timestamp": "2000-01-01T08:33:17+00:00",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
	fwFile = makeInventoryFile(t, host)
	fwBatchSize = 1
	fwTargets = []string{"/redfish/v1/UpdateService/FirmwareInventory/BMC"}
	fwInsecure = true
	fwTimeout = 2 * time.Second
	// Ensure env
	t.Setenv("REDFISH_USER", "user")
	t.Setenv("REDFISH_PASSWORD", "pass")

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	cmd := firmwareStatusCmd
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	w.Close() //nolint:errcheck
	out, _ := io.ReadAll(r)
	output := string(out)

	if !strings.Contains(output, "In-progress updates: 0") {
		t.Fatalf("expected no in-progress updates, got:\n%s", output)
	}
	if !strings.Contains(output, "Errors:") || !strings.Contains(output, "HPEFirmwareUpdate.1.0.DownloadFailed") {
		t.Fatalf("expected error with MessageId in output, got:\n%s", output)
	}
}

func TestFirmwareStatusDetectsInstalling(t *testing.T) {
	// Mock server that returns a firmware inventory with an installing condition
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService/FirmwareInventory/BMC") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/BMC",
				"Id":        "BMC",
				"Version":   "nc.1.11.0",
				"Status": map[string]any{
					"Health": "OK",
					"State":  "Enabled",
					"Conditions": []map[string]any{
						{
							"Message":   "Installing firmware",
							"MessageId": "OEM.Installing",
							"Severity":  "OK",
							"Timestamp": "2000-01-01T08:33:17+00:00",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
	fwFile = makeInventoryFile(t, host)
	fwBatchSize = 1
	fwTargets = []string{"/redfish/v1/UpdateService/FirmwareInventory/BMC"}
	fwInsecure = true
	fwTimeout = 2 * time.Second
	// Ensure env
	t.Setenv("REDFISH_USER", "user")
	t.Setenv("REDFISH_PASSWORD", "pass")

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	cmd := firmwareStatusCmd
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	w.Close() //nolint:errcheck
	out, _ := io.ReadAll(r)
	output := string(out)

	if !strings.Contains(output, "In-progress updates: 1") {
		t.Fatalf("expected one in-progress update, got:\n%s", output)
	}
	if strings.Contains(output, "Errors:") {
		t.Fatalf("did not expect errors, got:\n%s", output)
	}
}

func TestFirmwareStatusPrefersUpdateServiceUpdating(t *testing.T) {
	// Mock server that returns UpdateService showing Updating and a benign firmware inventory
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService",
				"Id":        "UpdateService",
				"Status": map[string]any{
					"Health": "OK",
					"State":  "Updating",
				},
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService/FirmwareInventory/BMC") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/BMC",
				"Id":        "BMC",
				"Version":   "nc.1.12.0",
				"Status": map[string]any{
					"Health": "OK",
					"State":  "Enabled",
				},
			})
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
	fwFile = makeInventoryFile(t, host)
	fwBatchSize = 1
	fwTargets = []string{"/redfish/v1/UpdateService/FirmwareInventory/BMC"}
	fwInsecure = true
	fwTimeout = 2 * time.Second
	// Ensure env
	t.Setenv("REDFISH_USER", "user")
	t.Setenv("REDFISH_PASSWORD", "pass")

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	cmd := firmwareStatusCmd
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	w.Close() //nolint:errcheck
	out, _ := io.ReadAll(r)
	output := string(out)

	if !strings.Contains(output, "In-progress updates: 1") {
		t.Fatalf("expected one in-progress update (via UpdateService), got:\n%s", output)
	}
}

func TestFirmwareStatusPrefersUpdateServiceHealthCritical(t *testing.T) {
	// Mock server that returns UpdateService showing Critical health with a condition
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService",
				"Id":        "UpdateService",
				"Status": map[string]any{
					"Health": "Critical",
					"State":  "Enabled",
					"Conditions": []map[string]any{
						{
							"Message":   "Update service failed to start transfer",
							"MessageId": "OEM.UpdateService.TransferFailed",
							"Severity":  "Critical",
							"Timestamp": "2000-01-01T08:33:17+00:00",
						},
					},
				},
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService/FirmwareInventory/BMC") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/BMC",
				"Id":        "BMC",
				"Version":   "nc.1.9.0",
				"Status": map[string]any{
					"Health": "OK",
					"State":  "Enabled",
				},
			})
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
	fwFile = makeInventoryFile(t, host)
	fwBatchSize = 1
	fwTargets = []string{"/redfish/v1/UpdateService/FirmwareInventory/BMC"}
	fwInsecure = true
	fwTimeout = 2 * time.Second
	// Ensure env
	t.Setenv("REDFISH_USER", "user")
	t.Setenv("REDFISH_PASSWORD", "pass")

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	cmd := firmwareStatusCmd
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	w.Close() //nolint:errcheck
	out, _ := io.ReadAll(r)
	output := string(out)

	if !strings.Contains(output, "In-progress updates: 0") {
		t.Fatalf("expected no in-progress updates, got:\n%s", output)
	}
	if !strings.Contains(output, "Errors:") || !strings.Contains(output, "OEM.UpdateService.TransferFailed") {
		t.Fatalf("expected error with MessageId in output, got:\n%s", output)
	}
}

func TestFirmwareStatusDetectsInventoryHealthWarningNoConditions(t *testing.T) {
	// Mock server that returns a firmware inventory with Health Warning but no conditions
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService/FirmwareInventory/BMC") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/BMC",
				"Id":        "BMC",
				"Version":   "nc.1.8.0",
				"Status": map[string]any{
					"Health": "Warning",
					"State":  "Enabled",
				},
			})
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
	fwFile = makeInventoryFile(t, host)
	fwBatchSize = 1
	fwTargets = []string{"/redfish/v1/UpdateService/FirmwareInventory/BMC"}
	fwInsecure = true
	fwTimeout = 2 * time.Second
	// Ensure env
	t.Setenv("REDFISH_USER", "user")
	t.Setenv("REDFISH_PASSWORD", "pass")

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	cmd := firmwareStatusCmd
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	w.Close() //nolint:errcheck
	out, _ := io.ReadAll(r)
	output := string(out)

	if !strings.Contains(output, "In-progress updates: 0") {
		t.Fatalf("expected no in-progress updates, got:\n%s", output)
	}
	if !strings.Contains(output, "Errors:") || !strings.Contains(output, "health: Warning") {
		t.Fatalf("expected health warning surfaced as error, got:\n%s", output)
	}
}

func TestFirmwareStatusDetectsInventoryHealthCriticalWithCondition(t *testing.T) {
	// Mock server that returns a firmware inventory with Health Critical and a condition
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService/FirmwareInventory/BMC") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/BMC",
				"Id":        "BMC",
				"Version":   "nc.1.7.0",
				"Status": map[string]any{
					"Health": "Critical",
					"State":  "Enabled",
					"Conditions": []map[string]any{
						{
							"Message":   "Firmware install failed",
							"MessageId": "OEM.Firmware.InstallFailed",
							"Severity":  "Critical",
							"Timestamp": "2000-01-01T08:33:17+00:00",
						},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
	fwFile = makeInventoryFile(t, host)
	fwBatchSize = 1
	fwTargets = []string{"/redfish/v1/UpdateService/FirmwareInventory/BMC"}
	fwInsecure = true
	fwTimeout = 2 * time.Second
	// Ensure env
	t.Setenv("REDFISH_USER", "user")
	t.Setenv("REDFISH_PASSWORD", "pass")

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	cmd := firmwareStatusCmd
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	w.Close() //nolint:errcheck
	out, _ := io.ReadAll(r)
	output := string(out)

	if !strings.Contains(output, "In-progress updates: 0") {
		t.Fatalf("expected no in-progress updates, got:\n%s", output)
	}
	if !strings.Contains(output, "Errors:") || !strings.Contains(output, "OEM.Firmware.InstallFailed") {
		t.Fatalf("expected critical install failure MessageId in output, got:\n%s", output)
	}
}

func TestFirmwareStatusDetectsTaskServiceRunning(t *testing.T) {
	// Mock server that returns a running TaskService task and benign UpdateService/FirmwareInventory
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/TaskService/Tasks") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"Members": []map[string]any{{"@odata.id": "/redfish/v1/TaskService/Tasks/1"}},
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/TaskService/Tasks/1") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"Id":        "1",
				"Name":      "Firmware Update",
				"TaskState": "Running",
				"Message":   "Updating BIOS",
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService",
				"Id":        "UpdateService",
				"Status": map[string]any{
					"Health": "OK",
					"State":  "Enabled",
				},
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/UpdateService/FirmwareInventory/BMC") {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
				"@odata.id": "/redfish/v1/UpdateService/FirmwareInventory/BMC",
				"Id":        "BMC",
				"Version":   "nc.1.13.0",
				"Status": map[string]any{
					"Health": "OK",
					"State":  "Enabled",
				},
			})
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()

	host := strings.TrimPrefix(server.URL, "https://")
	fwFile = makeInventoryFile(t, host)
	fwBatchSize = 1
	fwTargets = []string{"/redfish/v1/UpdateService/FirmwareInventory/BMC"}
	fwInsecure = true
	fwTimeout = 2 * time.Second
	// Ensure env
	t.Setenv("REDFISH_USER", "user")
	t.Setenv("REDFISH_PASSWORD", "pass")

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = old }()

	cmd := firmwareStatusCmd
	cmd.SetContext(context.Background())
	if err := cmd.RunE(cmd, []string{}); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	w.Close() //nolint:errcheck
	out, _ := io.ReadAll(r)
	output := string(out)

	if !strings.Contains(output, "In-progress updates: 1") {
		t.Fatalf("expected one in-progress update via TaskService, got:\n%s", output)
	}
}
