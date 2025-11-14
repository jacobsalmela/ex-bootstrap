// SPDX-FileCopyrightText: 2025 OpenCHAMI Contributors
//
// SPDX-License-Identifier: MIT
// SPDX-FileCopyrightText: 2025 OpenCHAMI Contributors
//
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"bootstrap/internal/inventory"
	"bootstrap/internal/redfish"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	// reuse firmware flags (made persistent)
	fwStatusInterval time.Duration
	fwFormat         string
)

var firmwareStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Query BMC firmware versions and in-progress updates",
	RunE: func(cmd *cobra.Command, args []string) error { // nolint:revive
		user := os.Getenv("REDFISH_USER")
		pass := os.Getenv("REDFISH_PASSWORD")
		if user == "" || pass == "" {
			return errors.New("REDFISH_USER and REDFISH_PASSWORD env vars are required")
		}

		// Determine hosts to target (reuse logic from firmware.go)
		hosts := []string{}
		if strings.TrimSpace(fwHostsCSV) != "" {
			for _, h := range strings.Split(fwHostsCSV, ",") {
				h = strings.TrimSpace(h)
				if h != "" {
					hosts = append(hosts, h)
				}
			}
		} else {
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

		if len(hosts) == 0 {
			return fmt.Errorf("no hosts to query")
		}

		// Determine targets. Honor --targets if provided, otherwise use --type like the update command.
		targets := fwTargets
		if len(targets) == 0 {
			typeName := fwType
			if strings.TrimSpace(typeName) == "" {
				// default to bmc when not specified
				typeName = "bmc"
			}
			var err error
			targets, err = defaultTargets(typeName)
			if err != nil {
				return err
			}
		}

		// Results aggregation
		var mu sync.Mutex
		versionCounts := map[string]int{}
		inProgress := int32(0)
		errorsList := map[string]string{}

		// Collect per-target summaries for JSON output
		type hostSummary struct {
			Host             string `json:"host"`
			Target           string `json:"target"`
			ObservedVersion  string `json:"observed_version"`
			RequestedVersion string `json:"requested_version,omitempty"`
			Status           string `json:"status"` // one of: in-progress, error, idle
			Error            string `json:"error,omitempty"`
		}
		var hostSummaries []hostSummary

		sem := make(chan struct{}, max(1, fwBatchSize))
		var wg sync.WaitGroup
		for _, host := range hosts {
			wg.Add(1)
			h := host
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				ctx := cmd.Context()
				if fwTimeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, fwTimeout)
					defer cancel()
				}

				// Check UpdateService first (preferred source for overall update activity)
				var perr string
				var anyInProgress bool
				us, err := redfish.GetUpdateServiceStatus(ctx, h, user, pass, fwInsecure, fwTimeout)
				if err == nil {
					health := strings.ToLower(us.Health)
					state := strings.ToLower(us.State)
					if health != "ok" {
						// collect condition messages as errors
						for _, c := range us.Conditions {
							if c.MessageID != "" {
								if perr == "" {
									perr = fmt.Sprintf("%s (%s)", c.MessageID, c.Message)
								} else {
									perr = perr + "; " + fmt.Sprintf("%s (%s)", c.MessageID, c.Message)
								}
							} else {
								if perr == "" {
									perr = c.Message
								} else {
									perr = perr + "; " + c.Message
								}
							}
						}
					} else if state == "updating" {
						anyInProgress = true
					}
				}

				// If UpdateService and inventory did not indicate progress, check TaskService for running jobs
				if !anyInProgress {
					if tasks, err := redfish.GetActiveUpdateTasks(ctx, h, user, pass, fwInsecure, fwTimeout); err == nil {
						if len(tasks) > 0 {
							anyInProgress = true
						}
					}
				}

				// Query each target separately and record per-target summaries
				for _, target := range targets {
					var perrTarget string
					var verTarget string
					var anyInProgressTarget bool

					inv, err := redfish.GetFirmwareInventory(ctx, h, user, pass, fwInsecure, fwTimeout, target)
					if err != nil {
						perrTarget = err.Error()
					} else {
						verTarget = inv.Version
						// If the inventory reports a non-OK Health, treat as error and include conditions
						if strings.ToLower(inv.Health) != "" && !strings.EqualFold(inv.Health, "OK") {
							if len(inv.Conditions) > 0 {
								for _, c := range inv.Conditions {
									if c.MessageID != "" {
										if perrTarget == "" {
											perrTarget = fmt.Sprintf("%s (%s)", c.MessageID, c.Message)
										} else {
											perrTarget = perrTarget + "; " + fmt.Sprintf("%s (%s)", c.MessageID, c.Message)
										}
									} else {
										if perrTarget == "" {
											perrTarget = c.Message
										} else {
											perrTarget = perrTarget + "; " + c.Message
										}
									}
								}
							} else {
								perrTarget = fmt.Sprintf("health: %s", inv.Health)
							}
						}

						st := strings.ToLower(inv.State)
						if st != "" && st != "enabled" && st != "ok" {
							anyInProgressTarget = true
						}
						for _, c := range inv.Conditions {
							m := strings.ToLower(c.Message)
							if c.Severity == "Critical" || strings.Contains(m, "failed") || strings.Contains(m, "error") {
								if c.MessageID != "" {
									if perrTarget == "" {
										perrTarget = fmt.Sprintf("%s (%s)", c.MessageID, c.Message)
									} else {
										perrTarget = perrTarget + "; " + fmt.Sprintf("%s (%s)", c.MessageID, c.Message)
									}
								} else {
									if perrTarget == "" {
										perrTarget = c.Message
									} else {
										perrTarget = perrTarget + "; " + c.Message
									}
								}
								continue
							}
							if strings.Contains(m, "in progress") || strings.Contains(m, "install") || strings.Contains(m, "installing") || strings.Contains(m, "running") || strings.Contains(m, "downloading") || strings.Contains(m, "download in progress") {
								anyInProgressTarget = true
							}
						}
					}

					// Determine observed version fallback
					if verTarget == "" {
						verTarget = "(unknown)"
					}

					// Build status for this target: combine host-level and target-level info
					status := "idle"
					// perr (host-level) may have been set from UpdateService; include it
					combinedErr := perr
					if perrTarget != "" {
						if combinedErr == "" {
							combinedErr = perrTarget
						} else {
							combinedErr = combinedErr + "; " + perrTarget
						}
					}
					if combinedErr != "" {
						status = "error"
					} else if anyInProgress || anyInProgressTarget {
						status = "in-progress"
					}

					// Update aggregates and per-target list
					mu.Lock()
					versionCounts[verTarget]++
					if combinedErr != "" {
						// use host+target key so multiple targets per host are visible
						errorsList[fmt.Sprintf("%s %s", h, target)] = combinedErr
					}
					if status == "in-progress" {
						atomic.AddInt32(&inProgress, 1)
					}
					hostSummaries = append(hostSummaries, hostSummary{
						Host:             h,
						Target:           target,
						ObservedVersion:  verTarget,
						RequestedVersion: fwExpectedVersion,
						Status:           status,
						Error:            combinedErr,
					})
					mu.Unlock()
				}
			}()
		}
		wg.Wait()

		// JSON format option
		if strings.EqualFold(fwFormat, "json") {
			out, err := json.MarshalIndent(hostSummaries, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			return nil
		}

		// Print human-readable summary
		fmt.Println("Firmware status summary:")
		if strings.EqualFold(fwType, "bios") {
			// For BIOS checks, report both BMC count and total targets checked
			fmt.Printf("  Total BMCs: %d\n", len(hosts))
			fmt.Printf("  Total BIOS targets checked: %d\n", len(hostSummaries))
		} else {
			fmt.Printf("  Total hosts: %d\n", len(hosts))
		}
		fmt.Printf("  In-progress updates: %d\n", atomic.LoadInt32(&inProgress))
		fmt.Println("  Versions:")
		for v, c := range versionCounts {
			fmt.Printf("    %s: %d\n", v, c)
		}
		if len(errorsList) > 0 {
			fmt.Println("  Errors:")
			for h, e := range errorsList {
				fmt.Printf("    %s: %s\n", h, e)
			}
		}

		return nil
	},
}

func init() {
	firmwareCmd.AddCommand(firmwareStatusCmd)
	firmwareStatusCmd.Flags().DurationVar(&fwStatusInterval, "interval", 5*time.Second, "poll interval (not used in single-run summary, reserved for future watch command)")
	firmwareStatusCmd.Flags().StringVar(&fwFormat, "format", "", "output format: json")
}
