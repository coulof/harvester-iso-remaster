# Migration: `yaml-to-cmdline.py` to `harvester-cmdline`

This document records the design decisions and flawed behaviors in the legacy Python implementation (`yaml-to-cmdline.py`) that were **deliberately dropped** during the rewrite in Go (`harvester-cmdline`), backed by `github.com/harvester/harvester-installer/pkg/config`.

Do not reintroduce any of these behaviors into the Go codebase or wrapper scripts.

---

## 1. Dropped: Hand-Rolled Table & Duplicate camelCase Key Emission

### Legacy Flaw
The Python script maintained a hardcoded key-mapping dictionary. In earlier iterations, keys were emitted in both camelCase and snake_case or inconsistently converted.

### Why It Was Dropped
In the Harvester installer engine:
- `pkg/config/schemas.go` runs the `FuzzyNames` mapper, collapsing camelCase and snake_case keys into identical internal fields.
- Crucially, `pkg/util/cmdline.go:toNetworkInterfaces` hardcodes the exact snake_case path:
  `values.GetValue(data, "install", "management_interface", "interfaces")`
- Only that exact snake_case key is parsed from `hwAddr:...,name:...` format into structured `[]NetworkInterface` structs.
- If both camelCase (`managementInterface.interfaces`) and snake_case are present in `/proc/cmdline`, both land in Go's internal map. Due to non-deterministic Go map iteration order, the raw string from camelCase could overwrite the parsed struct array.

### Go Implementation
- Canonical external spelling is strictly **snake_case only** (`convert.ToYAMLKey(k)`).
- Keys are derived automatically from the pinned `HarvesterConfig` struct tags via `convert.EncodeToMap` and `convert.ToYAMLKey`. No static key lookup tables exist.

---

## 2. Dropped: `iso_url="local"` Default Sentinel

### Legacy Flaw
When `install.iso_url` was omitted in the YAML configuration, the Python script defaulted `harvester.install.iso_url="local"`.

### Why It Was Dropped
- `"local"` is an undocumented string sentinel that is neither recognized nor handled by Harvester's installer engine or cOS live-boot scripts.
- In virtual media and local ISO boots, `iso_url` must be completely omitted. Providing an invalid or non-URL string causes network image fetching failures or confuses unattended boot parsing.

### Go Implementation
- When `install.iso_url` is unset, the parameter is completely omitted from the command line.
- When set, `install.iso_url` is strictly validated to be an absolute HTTP or HTTPS URL.

---

## 3. Dropped: Fallback Hand-Rolled YAML Parser

### Legacy Flaw
The Python script included a ~100-line regex-based fallback YAML parser for environments where PyYAML was not installed.

### Why It Was Dropped
- The fallback parser silently corrupted multi-line strings, block scalar indentations, `#` comment handling, and nested dictionaries.
- Silent parser divergences produced subtly malformed boot strings that failed during unattended node installation.

### Go Implementation
- Uses Harvester's official YAML configuration loader: `config.LoadHarvesterConfig(yamlBytes)` (which leverages `gopkg.in/yaml.v3`).
- Rejects malformed YAML immediately with exit code 1.

---

## 4. Dropped: Embedding DNS Resolvers Inside Dracut `ip=`

### Legacy Flaw
The Python script attempted to append DNS resolver IPs into the Dracut static IP specification string:
`ip=<client-ip>:<peer-ip>:<gateway-ip>:<netmask>:<hostname>:<interface>:<dns1>:<dns2>...`

### Why It Was Dropped
- According to `man dracut.cmdline`:
  `ip=<client-ip>:[<peer-ip>]:<gateway-ip>:<netmask>:<hostname>:<interface>:{none|off|static}[:[<mtu>][:<macaddr>]]`
- DNS servers are **not** positional parameters of the `ip=` syntax. Putting IP addresses after `<interface>` corrupts the `<autoconf>`, `<mtu>`, and `<macaddr>` fields.

### Go Implementation
- Harvester and Dracut support **exactly one `ip=` parameter**.
- Static configuration emits:
  `ip=<client-ip>::<gateway-ip>:<netmask>:<hostname>:<interface>:none[:<mtu>]`
- DNS resolvers are emitted as separate, repeated Dracut arguments:
  `nameserver=<ip1> nameserver=<ip2>`

---

## 5. Dropped: Discarding Multiple SSH Keys

### Legacy Flaw
The Python script inspected `os.ssh_authorized_keys`, warned that the command line had no append semantics, and discarded all keys after the first.

### Why It Was Dropped
- In Harvester's `pkg/util/cmdline.go:ParseCmdLine`:
  ```go
  existing, ok := values.GetValue(data, keys...)
  if ok {
      switch v := existing.(type) {
      case string:
          values.PutValue(data, []string{v, value}, keys...)
      case []string:
          values.PutValue(data, append(v, value), keys...)
      }
  }
  ```
- A repeated key on the kernel command line promotes the value to `[]string` and appends. This is the official and intended mechanism to supply multiple list entries.

### Go Implementation
- Emits one repeated key per element for `harvester.os.ssh_authorized_keys`, `harvester.os.dns_nameservers`, `harvester.os.ntp_servers`, and `harvester.install.management_interface.interfaces`.
- All keys accumulate properly into the target slices during Harvester's `ParseCmdLine`.

---

## 6. Dropped: Interface Name Guessing (`enp1s0`)

### Legacy Flaw
When a configuration omitted the network interface name under `install.management_interface.interfaces`, the Python script silently fell back to assuming `"enp1s0"`.

### Why It Was Dropped
- PowerEdge servers use various naming schemes (`eno1`, `ens1f0np0`, `enp1s0f0`) depending on BIOS settings, LOM vs PCIe NICs, and kernel naming rules (`net.ifnames=1`).
- Silently guessing an interface name causes boot-time link acquisition hangs that are difficult to debug remotely.

### Go Implementation
- Fails fast during validation: if static network configuration is requested and no interface name is specified, the tool errors out and demands explicit interface configuration.

---

## 7. Dropped: Silent or Uninformative Buffer Length Guard

### Legacy Flaw
The Python script raised a generic exception if the output exceeded 2000 bytes, with no diagnostic on what caused the bloat.

### Go Implementation
- The Go binary exits with code `3` (`SizeLimitError`) and reports the top 5 largest parameter contributors and their exact byte lengths to `stderr` (e.g. large SSH keys, long base64 blobs).
- The maximum length is configurable via `--max-len N`.
