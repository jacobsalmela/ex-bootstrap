# ochami-ex-bootstrap

A small CLI tool to discover bootable node NICs via BMC Redfish and produce a YAML inventory (`bmcs[]` and `nodes[]`).

This repository contains a lightweight Go implementation originally based on small scripts for creating an initial BMC inventory and then discovering node NICs through Redfish.

## Features

- Generate an initial `inventory.yaml` with a `bmcs` list (xname, MAC, IP) using `--init-bmcs`.
- Discover bootable NICs via Redfish on each BMC and allocate IPs from a given subnet.
- Trigger firmware updates via Redfish UpdateService SimpleUpdate.
- Output file format: a single YAML file with two top-level keys:
  - `bmcs`: list of management controllers (xname, mac, ip)
  - `nodes`: list of discovered node network records (xname, mac, ip)

## Layout

- `main.go` — minimal entrypoint that invokes the Cobra CLI.
- `cmd/` — Cobra commands:
  - `init-bmcs` — generate initial inventory with BMC entries
  - `discover` — discover bootable NICs via Redfish and update nodes[]
  - `firmware` — trigger firmware updates (BMC/BIOS) via SimpleUpdate
- `internal/` — code split by concern:
  - `inventory/` — YAML types (`Entry`, `FileFormat`)
  - `redfish/` — minimal Redfish client and bootable NIC heuristics
  - `netalloc/` — IP allocation using `github.com/metal-stack/go-ipam`
  - `xname/` — xname helpers and conversions
  - `initbmcs/` — helpers used by the `init-bmcs` command
  - `discover/` — discovery orchestration (Redfish + IP allocation)
- `examples/` — sample files (e.g., `inventory.yaml`).

## Build

This project uses Go modules. From the repo root:

```bash
# fetch modules and build
go mod tidy
go build -o ochami_bootstrap .
```

## Usage 

Show help:

```bash
./ochami_bootstrap --help
```

### 1) Generate an initial BMC inventory

```bash
./ochami_bootstrap init-bmcs --file examples/inventory.yaml \
  --chassis "x9000c1=02:23:28:01,x9000c3=02:23:28:03" \
  --bmc-subnet 192.168.100 \
  --nodes-per-chassis 32 \
  --nodes-per-bmc 2 \
  --start-nid 1
```

Writes `examples/inventory.yaml` with a `bmcs:` list and `nodes: []`.

### 2) Discover bootable NICs and allocate IPs

The discovery flow reads the YAML `--file` (must contain non-empty `bmcs[]`) and writes back the same file with updated `nodes[]`.

Required env vars:
- `REDFISH_USER` — Redfish username
- `REDFISH_PASSWORD` — Redfish password

Example (same subnet for BMCs and nodes):

```bash
export REDFISH_USER=admin
export REDFISH_PASSWORD=secret
./ochami_bootstrap discover \
  --file examples/inventory.yaml \
  --node-subnet 10.42.0.0/24 \
  --timeout 12s \
  --insecure \
  --ssh-pubkey ~/.ssh/id_rsa.pub   # optional: set AuthorizedKeys on each BMC
```

Example (separate subnets for BMCs and nodes):

```bash
export REDFISH_USER=admin
export REDFISH_PASSWORD=secret
./ochami_bootstrap discover \
  --file examples/inventory.yaml \
  --bmc-subnet 192.168.100.0/24 \
  --node-subnet 10.42.0.0/24 \
  --timeout 12s \
  --insecure \
  --ssh-pubkey ~/.ssh/id_rsa.pub   # optional: set AuthorizedKeys on each BMC
```

Notes:
- The program makes simple heuristic decisions about which NIC is bootable (UEFI path hints, DHCP addresses, or a MAC on an enabled interface).
- IP allocation is done with `github.com/metal-stack/go-ipam`. The code reserves `.1` (first host) as a gateway and avoids network/broadcast implicitly.
- You can specify `--bmc-subnet` and `--node-subnet` separately. If only one is provided, it will be used for both BMCs and nodes.
- If `--ssh-pubkey` is provided, the tool attempts a Redfish PATCH to `/redfish/v1/Managers/BMC/NetworkProtocol` with an OEM payload setting `SSHAdmin.AuthorizedKeys` to the contents of the file.

### 3) Trigger firmware updates

Use the `firmware` subcommand to invoke Redfish UpdateService SimpleUpdate on targets. You can specify either a preset `--type` (cc|nc|bios) or provide explicit `--targets` URIs.

Required env vars:
- `REDFISH_USER` — Redfish username
- `REDFISH_PASSWORD` — Redfish password

Examples:

```bash
# Update BMC firmware on all hosts in inventory.yaml
export REDFISH_USER=admin
export REDFISH_PASSWORD=secret
./ochami_bootstrap firmware \
  --file examples/inventory.yaml \
  --type cc \
  --image-uri http://10.0.0.1/images/bmc-firmware.bin \
  --protocol HTTP \
  --timeout 5m

# Update BIOS firmware using explicit targets (example Node0/Node1 BIOS paths)
./ochami_bootstrap firmware \
  --hosts 10.1.1.20,10.1.1.21 \
  --targets /redfish/v1/UpdateService/FirmwareInventory/Node0.BIOS,/redfish/v1/UpdateService/FirmwareInventory/Node1.BIOS \
  --image-uri http://10.0.0.1/images/bios.cap \
  --protocol HTTP
```

Notes:
- Preset `--type` values:
  - `cc` or `bmc`: targets BMC firmware (`/redfish/v1/UpdateService/FirmwareInventory/BMC`).
  - `nc`: same as BMC for now (adjust if your platform exposes a different target).
  - `bios`: uses two targets (`Node0.BIOS`, `Node1.BIOS`) by default; use `--targets` if your platform differs.
- You can provide `--hosts` (comma-separated hostnames/IPs) to override reading from `--file`.
- `--insecure` allows skipping TLS verification for BMC HTTPS endpoints.

## Debugging and dry runs

- Global `--debug` prints Redfish request methods and paths, plus response status codes, to stderr. No credentials are logged.
- Use `--dry-run` to plan actions without contacting hardware:
  - `discover --dry-run` lists BMCs that would be contacted, the subnet to use, and the output file; it does not patch SSH keys, discover NICs, or write files.
  - `firmware --dry-run` prints the SimpleUpdate action per host (image URI, targets, protocol) without posting.

Example:

```bash
./ochami_bootstrap --debug discover --file examples/inventory.yaml --node-subnet 10.42.0.0/24 --dry-run
./ochami_bootstrap --debug firmware --file examples/inventory.yaml --type cc --image-uri http://10.0.0.1/bmc.bin --dry-run
```

If a Redfish call fails, errors include the HTTP status and the body returned by the BMC where available to aid troubleshooting.

## Dependencies

- Go (module aware). The project will download dependencies with `go mod tidy`.
- `github.com/metal-stack/go-ipam` — used for IP allocation.
- `gopkg.in/yaml.v3` — YAML parsing and writing.

## Contributing / Next steps

- Add unit tests for the xname / MAC generation helpers and the Redfish parsing heuristics.
- Add input validation for chassis/macro formats if you require stricter MAC formatting.
- Consider adding a `--dry-run` mode for discovery to avoid writing changes while testing.

## License

Pick an appropriate license for your project. This repo currently has none specified.

If you'd like, I can also add a small `README` section that includes an example `inventory.yaml` and a quick test script to validate the discovery flow without real BMCs (e.g., a mock HTTP server).