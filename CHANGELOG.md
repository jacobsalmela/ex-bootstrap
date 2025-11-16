<!--
SPDX-FileCopyrightText: 2025 OpenCHAMI Contributors
SPDX-License-Identifier: MIT

Keep a Changelog
https://keepachangelog.com/en/1.0.0/

This file documents notable changes for the ochami-ex-bootstrap project.
The format is based on "Keep a Changelog" and this repository follows
semantic versioning for releases.
-->

# Changelog

All notable changes to this project will be documented in this file.

The format is based on "Keep a Changelog" and this project adheres to
semantic versioning: https://semver.org/

## [Unreleased]

- No unreleased changes at time of initial release.

## [1.0.0] - 2025-11-16

### Added
- Initial project scaffold and CLI using Cobra (`main.go`, `cmd/`).
- `init-bmcs` command to generate a baseline `inventory.yaml` with `bmcs[]` and `nodes[]` entries.
- `discover` command to query BMC Redfish endpoints, discover bootable NICs, and allocate node IPs.
- `firmware` command to perform Redfish UpdateService SimpleUpdate with:
  - `--type` presets (cc|nc|bios) and `--targets` override
  - `--image-uri`, `--protocol`, `--batch-size` for parallelism
  - `--expected-version` and `--force` to guard updates
  - `--dry-run` for planning without contacting hardware
- `firmware status` subcommand to query firmware versions and report in-progress updates and errors.
- Redfish helpers to fetch FirmwareInventory and UpdateService status.
- `netalloc` IP allocator wrapper using `github.com/metal-stack/go-ipam`.
- Tests and example mocks for Redfish interactions (httptest based).
- CI workflows: GitHub Actions for linting and release pipelines; basic golangci-lint configuration.

### Changed
- Status command provides per-target summaries and supports JSON output for machine parsing.
- `firmware status` prefers `UpdateService` status as authoritative for in-progress detection and includes `MessageId` in error output.
- Added heuristic TaskService checks to detect active firmware update jobs when UpdateService/FirmwareInventory are inconclusive.
- FirmwareInventory parsing extended to expose `Status.Health` and include Health-based error reporting (important for BIOS/Node inventories).

### Fixed
- Fix xname generation issues for node naming (e.g., produce `x9000c1s0b0n0`/`n1`).
- MAC address validation and discovery fixes across multi-system BMCs.
- Multiple test fixes and stability improvements (deterministic test expectations, thread-safety in parallel tests).
- Improve crash/hard-failure resiliency in Redfish client (better path resolution and response handling).

### Tests
- Unit tests added for firmware update parallelism, status detection, and Redfish parsing.
- Tests added for IP allocation behavior and `netalloc` integration.

### CI / Tooling
- Add GitHub Actions workflows for linting, release and scorecard reporting.
- Add golangci-lint configuration and fix a number of linter warnings across the repo.

### Notes
- This initial release provides a lightweight, test-focused CLI for generating inventories, discovering bootable NICs, allocating node IPs, and performing Redfish-based firmware updates. It is intentionally conservative with heuristics: it uses multiple signals (UpdateService, FirmwareInventory, TaskService) to infer in-progress updates and surfaces vendor `MessageId`s where available to aid troubleshooting.

If you rely on reserved gateway IPs in your network planning, be aware the allocator permits allocation of `.1` by default — update your deploy scripts or use a custom allocator constructor if you need to preserve a reservation for the gateway address.

## Authors
- OpenCHAMI contributors

## License
- MIT (see repository files)
