# Harvester ISO Remaster (`harvester-iso-remaster`)

[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Harvester Version](https://img.shields.io/badge/harvester-v1.8.2-blue)](https://github.com/harvester/harvester/releases/tag/v1.8.2)
[![Go Report](https://img.shields.io/badge/go-1.26-blue)](go.mod)

`harvester-iso-remaster` is a complete toolchain for creating automated, configuration-baked SUSE Virtualization (Harvester) v1.8 ISO images for unattended installations via Dell PowerEdge iDRAC Redfish Virtual Media, physical USB, or CD-ROM.

---

## 1. Architecture & How It Works

Harvester provides two primary unattended deployment mechanisms:
1. **Network Config Fetch (`config_url`)**: Downloads `vmlinuz` and `initrd`, then fetches YAML configuration over HTTP/HTTPS at boot time.
2. **Virtual Media Boot (vmedia)**: Mounts a bootable ISO over iDRAC. The ~4GB `rootfs.squashfs` is already local on the virtual media (`root=live:CDLABEL=COS_LIVE`), eliminating network transfer overhead.

### The Remastering Pipeline
```text
Site YAML Config (e.g. examples/config-create.yaml)
  │
  ▼
[harvester-cmdline (Go CLI)]
  ├─ Parses config via github.com/harvester/harvester-installer/pkg/config (v1.8.2)
  ├─ Emits canonical snake_case harvester.* parameters
  ├─ Emits correct Dracut network arguments (bond=, vlan=, ifname=, ip=, nameserver=)
  └─ Enforces 2048-byte x86_64 kernel buffer limit and secrets guard
  │
  ▼
[remaster-iso.sh]
  ├─ Extracts upstream Harvester ISO (xorriso)
  ├─ Bakes arguments into /boot/grub2/harvester.cfg (${extra_iso_cmdline})
  ├─ Sets GRUB countdown & automated menu entry title
  ├─ Rebuilds FAT32 UEFI boot image COS_GRUB (mcopy / mkfs.vfat)
  └─ Repacks bootable hybrid UEFI/GPT ISO & generates SHA512 checksum (xorriso)
```

At boot time, Harvester's installer engine reads `/proc/cmdline`. Finding `harvester.install.automatic=true` without an external `config_url`, it immediately executes automated unattended installation directly from the baked parameters.

---

## 2. Security & Buffer Limits

### Why `config_url` is Preferred When Available
Whenever an HTTP/HTTPS fileserver is accessible to target nodes at boot time, **prefer `config_url`** over embedding configurations on the kernel command line:
- **Buffer Limits**: The Linux kernel on x86_64 enforces `COMMAND_LINE_SIZE = 2048` bytes. Large configurations with multiple SSH keys, sysctls, long disk paths, or bond options can exceed this buffer.
- **Secrets Security**: Kernel parameters are world-readable via `/proc/cmdline`, logged in `dmesg`, and captured in BMC/iDRAC console history.

### Secrets Guard on Kernel Command Line
By default, `harvester-cmdline` **refuses** to emit sensitive credentials (`os.password` or `token`) to stdout.

To permit secrets on the command line, explicitly pass:
```bash
--allow-secrets-on-cmdline
```

When enabled, a warning is printed to `stderr` on every execution. In audit-sensitive environments, store secrets in `/oem` CloudInit files or serve encrypted configs over `config_url`.

---

## 3. Required Packages & Installation

The toolchain runs natively on Linux without containers or wrapper layers.

### Required Tools
- `xorriso` — ISO9660 / Rock Ridge / Joliet / El Torito manipulation
- `mcopy` (from `mtools`) — Copies EFI files into FAT32 boot images
- `mkfs.vfat` (from `dosfstools`) — Formats the 4MB UEFI system partition image
- `go` (>= 1.26) — To compile `harvester-cmdline` (vendored offline)

### Package Installation by Distribution
- **openSUSE Leap / SLES / SLE Micro:**
  ```bash
  sudo zypper in -y xorriso mtools dosfstools go
  ```
- **Ubuntu / Debian:**
  ```bash
  sudo apt-get update && sudo apt-get install -y xorriso mtools dosfstools golang-go
  ```
- **RHEL / Rocky Linux / AlmaLinux:**
  ```bash
  sudo dnf install -y xorriso mtools dosfstools golang
  ```
- **macOS (Homebrew):**
  ```bash
  brew install xorriso mtools dosfstools go
  ```

---

## 4. Quickstart via Taskfile

```bash
# 1. Verify native tooling dependencies
task check-deps

# 2. Build the Go parameter engine
task build-cmdline

# 3. Remaster CREATE ISO (Node 1)
task remaster-create SOURCE_ISO=/path/to/harvester-v1.8.2-amd64.iso

# 4. Remaster JOIN ISO (Secondary Nodes)
task remaster-join SOURCE_ISO=/path/to/harvester-v1.8.2-amd64.iso

# 5. Clean generated artifacts
task clean
```

---

## 5. Direct CLI Usage

### A. Remastering Script (`remaster-iso.sh`)
```bash
./remaster-iso.sh \
  --source-iso /path/to/harvester-v1.8.2-amd64.iso \
  --config-file ./examples/config-create.yaml \
  --mode create \
  --output-iso ./harvester-v1.8.2-create.iso \
  --timeout 3
```

#### CLI Options
| Option | Required | Default | Description |
|---|---|---|---|
| `--source-iso` | Yes | - | Path to upstream Harvester v1.8 ISO |
| `--config-file` | Yes | - | Path to Harvester configuration YAML |
| `--mode` | Yes | `create` | Deployment mode: `create` or `join` |
| `--output-iso` | Yes | - | Destination path for remastered ISO |
| `--timeout` | No | `3` | GRUB boot menu countdown in seconds |
| `--volume-id` | No | `COS_LIVE` | ISO volume label (must remain `COS_LIVE` for Harvester dracut) |
| `--extra-cmdline` | No | - | Additional kernel arguments to append |

### B. Parameter Engine (`harvester-cmdline`)
```text
harvester-cmdline [flags] <config.yaml>

Flags:
  --mode create|join            override install.mode
  --format raw|grub|ipxe        output form (default raw: one space-joined line)
  --no-dracut                   omit ip=/nameserver=/vlan=/ifname=
  --max-len N                   size guard, default 2000 (against 2048-byte limit)
  --allow-secrets-on-cmdline    permit os.password / token (off by default)
  --validate-only               validate and exit, emit nothing
  --version                     print tool version and the pinned installer tag
```

#### Exit Codes
- `0`: Success (output to `stdout`)
- `1`: Validation Failure
- `2`: Usage / CLI syntax error
- `3`: Size limit exceeded (`SizeLimitError`, top contributors to `stderr`)

---

## 6. iDRAC Redfish Virtual Media Deployment

Once the remastered ISO is generated, serve it via your local Hauler fileserver or HTTP server and attach it to target Dell PowerEdge servers via Redfish.

### Step 1: Insert Virtual Media
```http
POST https://<idrac-ip>/redfish/v1/Managers/iDRAC.Embedded.1/VirtualMedia/CD/Actions/VirtualMedia.InsertMedia
Content-Type: application/json

{
  "Image": "http://<LOCAL_FILESERVER_IP>:8080/harvester-v1.8.2-create.iso",
  "Inserted": true,
  "WriteProtected": true
}
```

### Step 2: One-Time Boot Override (UEFI CD / Virtual Media)
```http
PATCH https://<idrac-ip>/redfish/v1/Systems/System.Embedded.1
Content-Type: application/json

{
  "Boot": {
    "BootSourceOverrideTarget": "Cd",
    "BootSourceOverrideMode": "UEFI",
    "BootSourceOverrideEnabled": "Once"
  }
}
```

### Step 3: Reboot Server
```http
POST https://<idrac-ip>/redfish/v1/Systems/System.Embedded.1/Actions/ComputerSystem.Reset
Content-Type: application/json

{
  "ResetType": "ForceRestart"
}
```

---

## 7. Developer Guide & Tests

Dependencies are fully vendored in `vendor/` for air-gapped development.

```bash
# Build binary into bin/harvester-cmdline
make build

# Run unit tests, round-trip oracle, golden files, and regression tests
make test

# Refresh golden files (.cmdline) in testdata/
make test-update

# Run linting (go vet and golangci-lint)
make lint
```

---

## 8. Automated Releases & Upstream Tracking

A scheduled GitHub Actions workflow checks [harvester/harvester-installer](https://github.com/harvester/harvester-installer/releases) weekly (every Sunday at 00:00 UTC) for new stable releases (excluding pre-releases and `-rc*` candidates).

When a new GA version (e.g., `v1.8.3` or `v1.9.0`) is published upstream:
1. `go.mod` and offline `vendor/` are bumped to the upstream tag.
2. The verification test suite and round-trip oracle are executed.
3. Multi-architecture binaries (`linux-amd64`, `linux-arm64`, `darwin-amd64`, `darwin-arm64`) and release tarballs are compiled and tagged.
4. A matching GitHub release is published with SHA-256 checksums.

Manual builds for specific upstream tags can also be triggered via `workflow_dispatch`.

---

## 9. License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for terms. See [NOTICE](NOTICE) for third-party component acknowledgments (including Apache-2.0 upstream Harvester dependencies).
