# AGENTS.md — `harvester-iso-remaster`

> Loaded by `opencode` / AI agents at session start. Read this fully before touching any file.
> If anything here conflicts with what you infer from the code, **this file wins** — raise the conflict instead of silently resolving it.

---

## 1. What we are building

`harvester-iso-remaster` is a complete toolchain for creating automated, configuration-baked SUSE Virtualization (Harvester) v1.8 ISO images for unattended installations via Dell PowerEdge iDRAC Redfish Virtual Media, physical USB, or CD-ROM.

It comprises two synchronized components:
1. **`harvester-cmdline`**: High-performance Go CLI backed by `github.com/harvester/harvester-installer/pkg/config` (pinned at `v1.8.2`) that translates YAML configuration into `harvester.*` and Dracut kernel parameters, enforcing buffer limits, secrets protection, and round-trip fidelity.
2. **`remaster-iso.sh`**: Portable remastering engine that extracts official Harvester ISOs, bakes automated boot parameters into GRUB (`/boot/grub2/harvester.cfg`), rebuilds FAT32 UEFI boot images (`mcopy`), and repacks bootable hybrid GPT ISOs (`xorriso`).

---

## 2. Hard constraints — design *within* them

**C1 — Backed strictly by Harvester installer's own package.**
Do not hand-roll a YAML parser or maintain static tables of `harvester.*` key mappings. The generic emitter derives canonical parameter names dynamically from `HarvesterConfig` struct tags via `convert.EncodeToMap` and `convert.ToYAMLKey`.

**C2 — Canonical external spelling is strictly snake_case.**
The installer's `toNetworkInterfaces` hardcodes the exact snake_case path:
`values.GetValue(data, "install", "management_interface", "interfaces")`
Emitting camelCase duplicates (`managementInterface.interfaces`) causes non-deterministic map resolution in Go where camelCase strings can overwrite parsed interface structs. **Emit snake_case only, never both.**

**C3 — Command-line buffer limit (2048 bytes).**
The Linux kernel on x86_64 enforces `COMMAND_LINE_SIZE = 2048`. `harvester-cmdline` enforces a safety threshold (`--max-len`, default `2000` bytes). When exceeded, it **fails hard with exit code 3**, reporting the largest parameter contributors to `stderr`. It must never silently truncate.

**C4 — Secrets refused on command line by default.**
Kernel command-line arguments land in `/proc/cmdline`, `dmesg`, bootloader configs, and BMC logs. The tool refuses to emit `os.password` or `token` by default (exit code 1). Operators must explicitly pass `--allow-secrets-on-cmdline`, which logs a warning to `stderr` on every execution.

**C5 — Dracut parameter invariants.**
- Exactly one `ip=` parameter is supported.
- DNS servers are **not** fields of `ip=`; they must be emitted as separate `nameserver=<ip>` arguments.
- Static network configuration must resolve deterministic interface names; never guess or fall back to `"enp1s0"`.
- When multiple NICs are defined, emit `bond=mgmt-bo:...`. When `vlan_id` is set, emit `vlan=...` and target the VLAN device in `ip=`.
- Emit `ifname=<name>:<mac>` when a MAC is known.

**C6 — Air-gap integrity.**
All dependencies are vendored in `vendor/`. Builds must succeed completely offline (`go build -mod=vendor`).

---

## 3. Repository layout

```text
.
├── AGENTS.md                # Agent instructions & invariants (this file)
├── README.md                # Toolchain documentation & worked examples
├── LICENSE                  # MIT License
├── NOTICE                   # Third-party Apache-2.0 notice for harvester-installer
├── Makefile                 # build, test, test-update, lint, vendor
├── Taskfile.yml             # Task runner for CREATE and JOIN remastering
├── remaster-iso.sh          # Native ISO remastering script
├── go.mod                   # Pinned to harvester-installer v1.8.2
├── go.sum
├── vendor/                  # Committed vendor tree
├── cmd/
│   └── harvester-cmdline/
│       └── main.go          # CLI entrypoint, flag parsing, exit codes
├── internal/
│   ├── emit/                # Generic emitter, special cases, dracut params, size check
│   │   ├── emit.go
│   │   └── emit_test.go
│   └── validate/            # Configuration validation logic
│       ├── validate.go
│       └── validate_test.go
├── cmdline_test.go          # Top-level round-trip oracle & regression test suite
├── examples/                # Canonical example CREATE and JOIN configurations
└── testdata/                # Test fixtures and .cmdline golden files
```

---

## 4. Verification Ladder

Every code change must pass:

1. **Static Analysis & Linting:**
   ```bash
   make lint
   ```
2. **Go Unit & Integration Suite:**
   ```bash
   make test
   ```
   Must pass:
   - **Round-Trip Oracle (`TestRoundTripOracle`)**: Asserts `want == got` after round-tripping through installer's `ParseCmdLine` and schema mapper.
   - **Golden Files (`TestGoldenFiles`)**: Asserts bit-for-bit equivalence against `testdata/*.cmdline`.
   - **Python Regressions (`TestRegression_PythonBugs`)**: Explicit assertions against legacy flaws (no camelCase, no `iso_url="local"`, no DNS in `ip=`, at most 1 `ip=`, quoted `#` survival, leading zero preservation).
3. **Build Binary & Verify Version:**
   ```bash
   make build
   ./bin/harvester-cmdline --version
   ```
4. **Golden File Refresh:**
   When intentionally altering emitted output, update golden files via:
   ```bash
   make test-update
   ```
