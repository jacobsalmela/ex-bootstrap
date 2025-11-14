// SPDX-FileCopyrightText: 2025 OpenCHAMI Contributors
//
// SPDX-License-Identifier: MIT

// Package cmd implements the CLI commands.
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"bootstrap/internal/discover"
	"bootstrap/internal/inventory"
	"bootstrap/internal/redfish"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	discFile       string
	discBMCSubnet  string
	discNodeSubnet string
	discInsecure   bool
	discTimeout    time.Duration
	discSSHPubKey  string
	discDryRun     bool
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover bootable node NICs via Redfish and update nodes[]",
	RunE: func(cmd *cobra.Command, args []string) error { //nolint:revive
		if discFile == "" {
			return fmt.Errorf("--file is required")
		}
		// Validate subnet flags - at least one must be provided
		if discBMCSubnet == "" && discNodeSubnet == "" {
			return fmt.Errorf("at least one of --bmc-subnet or --node-subnet is required")
		}
		// If only one subnet is provided, use it for both
		if discBMCSubnet == "" {
			discBMCSubnet = discNodeSubnet
		}
		if discNodeSubnet == "" {
			discNodeSubnet = discBMCSubnet
		}
		user := os.Getenv("REDFISH_USER")
		pass := os.Getenv("REDFISH_PASSWORD")
		if user == "" || pass == "" {
			return fmt.Errorf("REDFISH_USER and REDFISH_PASSWORD env vars are required")
		}

		raw, err := os.ReadFile(discFile)
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

		// Dry-run: only show what would be contacted and exit.
		if discDryRun {
			hosts := make([]string, 0, len(doc.BMCs))
			for _, b := range doc.BMCs {
				host := b.IP
				if host == "" {
					host = b.Xname
				}
				hosts = append(hosts, host)
			}
			fmt.Printf("[dry-run] would contact %d BMC(s): %v\n", len(hosts), hosts)
			if discBMCSubnet == discNodeSubnet {
				fmt.Printf("[dry-run] would allocate BMC and node IPs from subnet %s and write back to %s\n", discNodeSubnet, discFile)
			} else {
				fmt.Printf("[dry-run] would allocate BMC IPs from subnet %s and node IPs from subnet %s, writing to %s\n", discBMCSubnet, discNodeSubnet, discFile)
			}
			if discSSHPubKey != "" {
				fmt.Printf("[dry-run] would set SSH authorized keys on each BMC from %s\n", discSSHPubKey)
			}
			return nil
		}

		// Optionally set SSH authorized keys on each BMC if provided.
		if discSSHPubKey != "" {
			keyBytes, err := os.ReadFile(discSSHPubKey)
			if err != nil {
				return fmt.Errorf("read ssh pubkey: %w", err)
			}
			authorized := string(keyBytes)
			for _, b := range doc.BMCs {
				host := b.IP
				if host == "" {
					host = b.Xname
				}
				ctx := cmd.Context()
				if discTimeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, discTimeout)
					defer cancel()
				}
				if err := redfish.SetAuthorizedKeys(ctx, host, user, pass, discInsecure, discTimeout, authorized); err != nil {
					fmt.Fprintf(os.Stderr, "WARN: %s: set authorized keys: %v\n", b.Xname, err)
				}
			}
		}

		nodes, err := discover.UpdateNodes(&doc, discBMCSubnet, discNodeSubnet, user, pass, discInsecure, discTimeout)
		if err != nil {
			return err
		}
		doc.Nodes = nodes
		bytes, err := yaml.Marshal(&doc)
		if err != nil {
			return err
		}
		if err := os.WriteFile(discFile, bytes, 0o644); err != nil {
			return err
		}
		fmt.Printf("Updated %s with %d node record(s)\n", discFile, len(nodes))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().StringVarP(&discFile, "file", "f", "", "YAML file containing bmcs[] and nodes[] (nodes will be overwritten)")
	discoverCmd.Flags().StringVar(&discBMCSubnet, "bmc-subnet", "", "CIDR for BMC IPs, e.g. 192.168.100.0/24 (if not specified, uses --node-subnet)")
	discoverCmd.Flags().StringVar(&discNodeSubnet, "node-subnet", "", "CIDR for node IPs, e.g. 10.42.0.0/24 (if not specified, uses --bmc-subnet)")
	discoverCmd.Flags().BoolVar(&discInsecure, "insecure", true, "allow insecure TLS to BMCs")
	discoverCmd.Flags().DurationVar(&discTimeout, "timeout", 12*time.Second, "per-BMC discovery timeout")
	discoverCmd.Flags().StringVar(&discSSHPubKey, "ssh-pubkey", "", "Path to an SSH public key to set as AuthorizedKeys on each BMC (optional)")
	discoverCmd.Flags().BoolVar(&discDryRun, "dry-run", false, "plan only: print which BMCs would be contacted and exit")
}
