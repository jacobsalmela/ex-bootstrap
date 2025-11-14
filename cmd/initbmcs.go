// SPDX-FileCopyrightText: 2025 OpenCHAMI Contributors
//
// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"os"

	"bootstrap/internal/initbmcs"
	"bootstrap/internal/inventory"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	initFile         string
	initChassis      string
	initBMCSubnet    string
	initNodesPerChas int
	initNodesPerBMC  int
	initStartNID     int
)

var initBmcsCmd = &cobra.Command{
	Use:   "init-bmcs",
	Short: "Generate initial inventory with BMC entries",
	RunE: func(cmd *cobra.Command, args []string) error { //nolint:revive
		if initFile == "" {
			return fmt.Errorf("--file is required")
		}
		if initBMCSubnet == "" {
			return fmt.Errorf("--bmc-subnet is required")
		}
		chassis := initbmcs.ParseChassisSpec(initChassis)
		if len(chassis) == 0 {
			return fmt.Errorf("--chassis must specify at least one entry, e.g. x9000c1=02:23:28:01")
		}
		bmcs, err := initbmcs.Generate(chassis, initNodesPerChas, initNodesPerBMC, initStartNID, initBMCSubnet)
		if err != nil {
			return err
		}
		doc := inventory.FileFormat{BMCs: bmcs, Nodes: nil}
		bytes, err := yaml.Marshal(&doc)
		if err != nil {
			return err
		}
		if err := os.WriteFile(initFile, bytes, 0o644); err != nil {
			return err
		}
		fmt.Printf("Wrote initial BMC inventory to %s with %d entries\n", initFile, len(bmcs))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initBmcsCmd)
	initBmcsCmd.Flags().StringVarP(&initFile, "file", "f", "", "Output YAML file containing bmcs[] and nodes[]")
	initBmcsCmd.Flags().StringVar(&initChassis, "chassis", "x9000c1=02:23:28:01,x9000c3=02:23:28:03", "comma-separated chassis=macprefix list")
	initBmcsCmd.Flags().StringVar(&initBMCSubnet, "bmc-subnet", "192.168.100.0/24", "BMC subnet in CIDR notation, e.g. 192.168.100.0/24")
	initBmcsCmd.Flags().IntVar(&initNodesPerChas, "nodes-per-chassis", 32, "number of nodes per chassis")
	initBmcsCmd.Flags().IntVar(&initNodesPerBMC, "nodes-per-bmc", 2, "number of nodes managed by each BMC")
	initBmcsCmd.Flags().IntVar(&initStartNID, "start-nid", 1, "starting node id (1-based)")
}
