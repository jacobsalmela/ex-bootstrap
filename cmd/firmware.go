// SPDX-FileCopyrightText: 2025 OpenCHAMI Contributors
//
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"bootstrap/internal/inventory"
	"bootstrap/internal/redfish"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	fwFile            string
	fwHostsCSV        string
	fwType            string
	fwImageURI        string
	fwTargets         []string
	fwProtocol        string
	fwInsecure        bool
	fwTimeout         time.Duration
	fwDryRun          bool
	fwForce           bool
	fwExpectedVersion string
	fwBatchSize       int
)

// defaultTargets returns target list for shorthand types.
func defaultTargets(t string) ([]string, error) {
	switch strings.ToLower(t) {
	case "cc", "bmc":
		return []string{"/redfish/v1/UpdateService/FirmwareInventory/BMC"}, nil
	case "nc":
		return []string{"/redfish/v1/UpdateService/FirmwareInventory/BMC"}, nil
	case "bios":
		return []string{
			"/redfish/v1/UpdateService/FirmwareInventory/Node0.BIOS",
			"/redfish/v1/UpdateService/FirmwareInventory/Node1.BIOS",
		}, nil
	default:
		return nil, fmt.Errorf("unknown firmware type: %s (use cc|nc|bios or specify --targets)", t)
	}
}

var firmwareCmd = &cobra.Command{
	Use:   "firmware",
	Short: "Update firmware via Redfish SimpleUpdate",
	RunE: func(cmd *cobra.Command, args []string) error { //nolint:revive
		if fwFile == "" && fwHostsCSV == "" {
			return errors.New("at least one of --file or --hosts is required")
		}
		if fwImageURI == "" {
			return errors.New("--image-uri is required")
		}
		if len(fwTargets) == 0 {
			if fwType == "" {
				return errors.New("--type is required when --targets is not provided (one of cc|nc|bios)")
			}
			var err error
			fwTargets, err = defaultTargets(fwType)
			if err != nil {
				return err
			}
		}

		user := os.Getenv("REDFISH_USER")
		pass := os.Getenv("REDFISH_PASSWORD")
		if user == "" || pass == "" {
			return fmt.Errorf("REDFISH_USER and REDFISH_PASSWORD env vars are required")
		}

		// Determine hosts to target
		hosts := []string{}
		if strings.TrimSpace(fwHostsCSV) != "" {
			for _, h := range strings.Split(fwHostsCSV, ",") {
				h = strings.TrimSpace(h)
				if h != "" {
					hosts = append(hosts, h)
				}
			}
		} else {
			// Load from inventory file
			raw, err := os.ReadFile(fwFile)
			if err != nil {
				return err
			}
			var doc inventory.FileFormat
			if err := yaml.Unmarshal(raw, &doc); err != nil {
				return err
			}
			if len(doc.BMCs) == 0 {
				return fmt.Errorf("input must contain non-empty bmcs[]")
			}
			for _, b := range doc.BMCs {
				host := b.IP
				if host == "" {
					host = b.Xname
				}
				hosts = append(hosts, host)
			}
		}

		// Apply firmware update to each host
		if fwBatchSize <= 1 {
			// Serial execution
			for _, host := range hosts {
				ctx := cmd.Context()
				var cancel context.CancelFunc
				if fwTimeout > 0 {
					ctx, cancel = context.WithTimeout(ctx, fwTimeout)
				}
				if fwDryRun {
					dryRunMsg := fmt.Sprintf("[dry-run] would POST SimpleUpdate on %s with image=%s targets=%v protocol=%s",
						host, fwImageURI, fwTargets, fwProtocol)
					if fwExpectedVersion != "" {
						dryRunMsg += fmt.Sprintf(" expected-version=%s", fwExpectedVersion)
						if fwForce {
							dryRunMsg += " (force=true)"
						}
					}
					fmt.Println(dryRunMsg)
					if cancel != nil {
						cancel()
					}
					continue
				}
				err := redfish.SimpleUpdate(ctx, host, user, pass, fwInsecure, fwTimeout, fwImageURI, fwTargets, fwProtocol, fwExpectedVersion, fwForce)
				if cancel != nil {
					cancel()
				}
				if err != nil {
					// Check if this is a "skipping update" message
					if strings.Contains(err.Error(), "skipping update") {
						fmt.Printf("%s: %v\n", host, err)
					} else {
						fmt.Fprintf(os.Stderr, "WARN: %s: firmware update failed: %v\n", host, err)
					}
				} else {
					fmt.Printf("Triggered firmware update on %s\n", host)
				}
			}
		} else {
			// Parallel execution with semaphore to limit concurrency
			var wg sync.WaitGroup
			sem := make(chan struct{}, fwBatchSize)
			var mu sync.Mutex // Protect stdout/stderr writes

			for _, host := range hosts {
				wg.Add(1)
				go func(h string) {
					defer wg.Done()
					sem <- struct{}{}        // Acquire semaphore
					defer func() { <-sem }() // Release semaphore

					ctx := cmd.Context()
					var cancel context.CancelFunc
					if fwTimeout > 0 {
						ctx, cancel = context.WithTimeout(ctx, fwTimeout)
					}
					if cancel != nil {
						defer cancel()
					}

					if fwDryRun {
						dryRunMsg := fmt.Sprintf("[dry-run] would POST SimpleUpdate on %s with image=%s targets=%v protocol=%s",
							h, fwImageURI, fwTargets, fwProtocol)
						if fwExpectedVersion != "" {
							dryRunMsg += fmt.Sprintf(" expected-version=%s", fwExpectedVersion)
							if fwForce {
								dryRunMsg += " (force=true)"
							}
						}
						mu.Lock()
						fmt.Println(dryRunMsg)
						mu.Unlock()
						return
					}

					err := redfish.SimpleUpdate(ctx, h, user, pass, fwInsecure, fwTimeout, fwImageURI, fwTargets, fwProtocol, fwExpectedVersion, fwForce)

					mu.Lock()
					if err != nil {
						// Check if this is a "skipping update" message
						if strings.Contains(err.Error(), "skipping update") {
							fmt.Printf("%s: %v\n", h, err)
						} else {
							fmt.Fprintf(os.Stderr, "WARN: %s: firmware update failed: %v\n", h, err)
						}
					} else {
						fmt.Printf("Triggered firmware update on %s\n", h)
					}
					mu.Unlock()
				}(host)
			}
			wg.Wait()
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(firmwareCmd)
	// Make flags persistent so subcommands (like `firmware status`) inherit them
	firmwareCmd.PersistentFlags().StringVarP(&fwFile, "file", "f", "", "Inventory file to read bmcs[] from when --hosts is not provided")
	firmwareCmd.PersistentFlags().StringVar(&fwHostsCSV, "hosts", "", "Comma-separated list of BMC hosts to target (overrides --file)")
	firmwareCmd.PersistentFlags().StringVar(&fwType, "type", "", "Firmware type preset: cc|nc|bios (ignored if --targets provided)")
	firmwareCmd.PersistentFlags().StringVar(&fwImageURI, "image-uri", "", "Firmware image URI accessible by BMC (required)")
	firmwareCmd.PersistentFlags().StringSliceVar(&fwTargets, "targets", nil, "Explicit FirmwareInventory target URIs (advanced)")
	firmwareCmd.PersistentFlags().StringVar(&fwProtocol, "protocol", "HTTP", "TransferProtocol for SimpleUpdate (HTTP/HTTPS)")
	firmwareCmd.PersistentFlags().BoolVar(&fwInsecure, "insecure", true, "allow insecure TLS to BMCs")
	firmwareCmd.PersistentFlags().DurationVar(&fwTimeout, "timeout", 5*time.Minute, "per-BMC firmware request timeout")
	firmwareCmd.PersistentFlags().BoolVar(&fwDryRun, "dry-run", false, "plan only: print SimpleUpdate actions without posting")
	firmwareCmd.PersistentFlags().BoolVar(&fwForce, "force", false, "force update even if already at expected version")
	firmwareCmd.PersistentFlags().StringVar(&fwExpectedVersion, "expected-version", "", "expected version string; skip update if already at this version (unless --force)")
	firmwareCmd.PersistentFlags().IntVar(&fwBatchSize, "batch-size", 0, "number of concurrent firmware updates (0 or 1 = serial, >1 = parallel)")
}
