#!/usr/bin/env python3
"""
yaml-to-cmdline.py — Convert Harvester configuration YAML to kernel cmdline arguments.

Parses Harvester install configuration (CREATE or JOIN mode) and emits the
exact dot-notated 'harvester.*' and Dracut kernel parameters.

Design:
- Strictly requires PyYAML (no divergent hand-rolled parser).
- Table-driven declarative mapping for canonical Harvester snake_case parameters.
- Validates required parameters per install mode.
- Supports --exclude-secrets to omit passwords and tokens for security compliance.
- Enforces a hard command-line length limit (default 2000 bytes) against COMMAND_LINE_SIZE.
- Emits correct Dracut ip= and nameserver= syntax without MTU/DNS corruption.
"""

import sys
import os
import argparse
from typing import Any, Callable, Dict, List, Optional, Tuple

try:
    import yaml
except ImportError:
    sys.stderr.write(
        "[-] Error: PyYAML is required by yaml-to-cmdline.py but is not installed.\n"
        "    Please install PyYAML (e.g. 'pip install pyyaml' or distribution package).\n"
    )
    sys.exit(1)

MAX_CMDLINE_LEN = 2000


def get_nested(d: Dict[str, Any], path: str, default: Any = None) -> Any:
    """Retrieve value from nested dictionary using dot-separated path."""
    curr = d
    for part in path.split("."):
        if not isinstance(curr, dict) or part not in curr:
            return default
        curr = curr[part]
    return curr


def format_str(val: Any) -> Optional[str]:
    if val is None or val == "":
        return None
    s = str(val).strip()
    return f'"{s}"' if s else None


def format_raw(val: Any) -> Optional[str]:
    if val is None or val == "":
        return None
    s = str(val).strip()
    return s if s else None


def format_int(val: Any) -> Optional[str]:
    if val is None or val == "":
        return None
    try:
        return str(int(val))
    except (ValueError, TypeError):
        return None


def format_bool(val: Any) -> Optional[str]:
    if val is None:
        return None
    if isinstance(val, bool):
        return "true" if val else "false"
    s = str(val).strip().lower()
    if s in ("true", "yes", "1"):
        return "true"
    if s in ("false", "no", "0"):
        return "false"
    return None


def format_list_comma(val: Any) -> Optional[str]:
    if val is None:
        return None
    if isinstance(val, list):
        items = [str(item).strip() for item in val if item is not None and str(item).strip()]
        return f'"{",".join(items)}"' if items else None
    s = str(val).strip()
    return f'"{s}"' if s else None


# Declarative table of Harvester schema mappings
# Tuple format: (yaml_path, cmdline_key, formatter, is_secret)
MAPPINGS: List[Tuple[str, str, Callable[[Any], Optional[str]], bool]] = [
    ("scheme_version", "harvester.scheme_version", format_int, False),
    ("install.mode", "harvester.install.mode", format_raw, False),
    ("server_url", "harvester.server_url", format_str, False),
    ("install.server_url", "harvester.server_url", format_str, False),
    ("token", "harvester.token", format_str, True),
    ("install.token", "harvester.token", format_str, True),
    ("os.hostname", "harvester.os.hostname", format_str, False),
    ("os.password", "harvester.os.password", format_str, True),
    ("os.dns_nameservers", "harvester.os.dns_nameservers", format_list_comma, False),
    ("os.ntp_servers", "harvester.os.ntp_servers", format_list_comma, False),
    ("install.device", "harvester.install.device", format_str, False),
    ("install.data_disk", "harvester.install.data_disk", format_str, False),
    ("install.role", "harvester.install.role", format_str, False),
    ("install.vip", "harvester.install.vip", format_str, False),
    ("install.vip_mode", "harvester.install.vip_mode", format_str, False),
    ("install.vip_hw_addr", "harvester.install.vip_hw_addr", format_str, False),
    ("install.skipchecks", "harvester.install.skipchecks", format_bool, False),
    ("install.iso_url", "harvester.install.iso_url", format_str, False),
    ("install.config_url", "harvester.install.config_url", format_str, False),
    ("install.tty", "harvester.install.tty", format_str, False),
    ("install.management_interface.method", "harvester.install.management_interface.method", format_str, False),
    ("install.management_interface.ip", "harvester.install.management_interface.ip", format_str, False),
    ("install.management_interface.subnet_mask", "harvester.install.management_interface.subnet_mask", format_str, False),
    ("install.management_interface.gateway", "harvester.install.management_interface.gateway", format_str, False),
    ("install.management_interface.vlan_id", "harvester.install.management_interface.vlan_id", format_int, False),
    ("install.management_interface.mtu", "harvester.install.management_interface.mtu", format_int, False),
    ("install.management_interface.bond_options.mode", "harvester.install.management_interface.bond_options.mode", format_str, False),
    ("install.management_interface.bond_options.miimon", "harvester.install.management_interface.bond_options.miimon", format_int, False),
]


def format_management_interfaces(ifaces: Any) -> Optional[str]:
    """Format management_interface.interfaces list into Harvester format."""
    if not isinstance(ifaces, list) or not ifaces:
        return None
    formatted = []
    for iface in ifaces:
        if isinstance(iface, dict):
            name = iface.get("name")
            hw = iface.get("hwAddr")
            if hw and name:
                formatted.append(f"hwAddr:{hw},name:{name}")
            elif name:
                formatted.append(f"name:{name}")
            elif hw:
                formatted.append(f"hwAddr:{hw}")
        elif isinstance(iface, str) and iface.strip():
            formatted.append(f"name:{iface.strip()}")
    return f'"{",".join(formatted)}"' if formatted else None


def format_ssh_authorized_keys(keys: Any) -> Optional[str]:
    """
    Format SSH authorized keys.
    Note: The kernel command line cannot reliably append repeated list items.
    If multiple keys are present, only the first key is emitted and a warning is logged.
    """
    if isinstance(keys, list) and keys:
        if len(keys) > 1:
            sys.stderr.write(
                "[!] Warning: Multiple ssh_authorized_keys provided. Kernel cmdline has no append semantics;\n"
                "    emitting only the first key. Store full key sets in config_url or /oem CloudInit.\n"
            )
        first = str(keys[0]).strip()
        return f'"{first}"' if first else None
    if isinstance(keys, str) and keys.strip():
        return f'"{keys.strip()}"'
    return None


def format_dracut_network(mgmt: Dict[str, Any], hostname: str, dns_list: Any) -> List[str]:
    """
    Generate Dracut network kernel parameters.
    Syntax: ip=<client-ip>:[<peer-ip>]:<gateway-ip>:<netmask>:<hostname>:<interface>:{none|off|static}
    DNS servers are emitted as separate nameserver=<ip> arguments (not in the ip= string).
    """
    params = []
    method = mgmt.get("method", "dhcp")

    if method == "dhcp":
        params.append("ip=dhcp")
        return params

    if method == "static":
        ip = mgmt.get("ip")
        gateway = mgmt.get("gateway", "")
        mask = mgmt.get("subnet_mask", "")
        ifaces = mgmt.get("interfaces", [])

        # Determine primary interface name
        iface_name = ""
        if ifaces and isinstance(ifaces, list):
            first = ifaces[0]
            if isinstance(first, dict):
                iface_name = first.get("name", "")
            elif isinstance(first, str):
                iface_name = first

        if not iface_name:
            iface_name = "enp1s0"

        # Check for bonding
        bond_opts = mgmt.get("bond_options", {})
        if isinstance(bond_opts, dict) and bond_opts.get("mode"):
            bond_mode = bond_opts.get("mode")
            bond_miimon = bond_opts.get("miimon", 100)
            slaves = []
            for iface in ifaces:
                if isinstance(iface, dict) and iface.get("name"):
                    slaves.append(iface["name"])
                elif isinstance(iface, str):
                    slaves.append(iface)
            if slaves:
                params.append(f"bond=mgmt-bo:{','.join(slaves)}:mode={bond_mode},miimon={bond_miimon}")
                iface_name = "mgmt-bo"

        # Check for VLAN
        vlan_id = mgmt.get("vlan_id")
        if vlan_id is not None and str(vlan_id).isdigit() and int(vlan_id) > 0:
            vlan_dev = f"{iface_name}.{vlan_id}"
            params.append(f"vlan={vlan_dev}:{iface_name}")
            iface_name = vlan_dev

        if ip:
            # Correct Dracut static IP specification (no nameservers in ip= field)
            params.append(f"ip={ip}::{gateway}:{mask}:{hostname}:{iface_name}:none")

        # Emit separate nameserver= arguments for Dracut
        if isinstance(dns_list, list):
            for dns in dns_list:
                dns_str = str(dns).strip()
                if dns_str:
                    params.append(f"nameserver={dns_str}")
        elif isinstance(dns_list, str) and dns_list.strip():
            for dns_str in dns_list.split(","):
                dns_str = dns_str.strip()
                if dns_str:
                    params.append(f"nameserver={dns_str}")

    return params


def validate_config(cfg: Dict[str, Any], mode: str, exclude_secrets: bool = False) -> List[str]:
    """Validate required Harvester install configuration fields."""
    errors = []

    if mode not in ("create", "join"):
        errors.append(f"Invalid install mode '{mode}'. Must be 'create' or 'join'.")

    # Target disk
    device = get_nested(cfg, "install.device")
    if not device:
        errors.append("Missing required field: install.device")

    # Hostname
    hostname = get_nested(cfg, "os.hostname")
    if not hostname:
        errors.append("Missing required field: os.hostname")

    # Secrets (unless --exclude-secrets is specified)
    if not exclude_secrets:
        password = get_nested(cfg, "os.password")
        if not password:
            errors.append("Missing required field: os.password")

        token = cfg.get("token") or get_nested(cfg, "install.token")
        if not token:
            errors.append("Missing required field: token (or install.token)")

    # Mode-specific requirements
    if mode == "create":
        vip = get_nested(cfg, "install.vip")
        if not vip:
            errors.append("Create mode requires 'install.vip'")
        vip_mode = get_nested(cfg, "install.vip_mode")
        if not vip_mode:
            errors.append("Create mode requires 'install.vip_mode' (e.g. 'static' or 'dhcp')")
    elif mode == "join":
        server_url = cfg.get("server_url") or get_nested(cfg, "install.server_url")
        if not server_url:
            errors.append("Join mode requires 'server_url' (pointing to cluster VIP)")

    # Management network validation
    mgmt = get_nested(cfg, "install.management_interface", {})
    if isinstance(mgmt, dict):
        method = mgmt.get("method", "dhcp")
        if method == "static":
            if not mgmt.get("ip"):
                errors.append("Static management interface requires 'install.management_interface.ip'")
            if not mgmt.get("subnet_mask"):
                errors.append("Static management interface requires 'install.management_interface.subnet_mask'")
            if not mgmt.get("gateway"):
                errors.append("Static management interface requires 'install.management_interface.gateway'")

    return errors


def flatten_config(
    cfg: Dict[str, Any],
    mode_override: Optional[str] = None,
    exclude_secrets: bool = False,
    extra_cmdline: Optional[str] = None,
    max_len: int = MAX_CMDLINE_LEN,
) -> str:
    """Convert YAML dictionary to kernel command line string."""
    install_section = cfg.get("install", {})
    mode = mode_override or install_section.get("mode", "create")

    # Validate configuration
    errors = validate_config(cfg, mode, exclude_secrets=exclude_secrets)
    if errors:
        for err in errors:
            sys.stderr.write(f"[-] Configuration Error: {err}\n")
        raise ValueError(f"Configuration validation failed: {'; '.join(errors)}")

    params: List[str] = []

    # Automatic installation sentinel
    params.append("harvester.install.automatic=true")

    # Table-driven mapping
    seen_keys = set()
    for yaml_path, cmdline_key, formatter, is_secret in MAPPINGS:
        if is_secret and exclude_secrets:
            continue
        if cmdline_key in seen_keys:
            continue

        val = get_nested(cfg, yaml_path)
        if val is None:
            continue

        formatted = formatter(val)
        if formatted is not None:
            params.append(f"{cmdline_key}={formatted}")
            seen_keys.add(cmdline_key)

    # Management interfaces list
    mgmt = get_nested(cfg, "install.management_interface", {})
    if isinstance(mgmt, dict):
        ifaces = mgmt.get("interfaces")
        if_str = format_management_interfaces(ifaces)
        if if_str:
            params.append(f"harvester.install.management_interface.interfaces={if_str}")

    # SSH Authorized Keys
    if not exclude_secrets:
        ssh_keys = get_nested(cfg, "os.ssh_authorized_keys")
        ssh_str = format_ssh_authorized_keys(ssh_keys)
        if ssh_str:
            params.append(f"harvester.os.ssh_authorized_keys={ssh_str}")

    # Dracut network parameters
    hostname = get_nested(cfg, "os.hostname", "")
    dns_list = get_nested(cfg, "os.dns_nameservers", [])
    dracut_params = format_dracut_network(mgmt, hostname, dns_list)
    params.extend(dracut_params)

    # Extra cmdline parameters
    if extra_cmdline:
        params.append(extra_cmdline.strip())

    result = " ".join(params)

    # Secret exposure warning
    if not exclude_secrets:
        has_secret = any(k in result for k in ("harvester.token=", "harvester.os.password="))
        if has_secret:
            sys.stderr.write(
                "[!] Warning: Emitting plaintext secrets on kernel command line.\n"
                "    /proc/cmdline is world-readable and captured in systemd/dmesg logs.\n"
                "    For production, prefer config_url or an embedded ISO config file with --exclude-secrets.\n"
            )

    # Buffer length guard
    if len(result) > max_len:
        sys.stderr.write(
            f"[-] Error: Generated kernel command line length ({len(result)} bytes) exceeds\n"
            f"    safety limit ({max_len} bytes) against x86_64 COMMAND_LINE_SIZE (2048 bytes).\n"
        )
        raise ValueError(f"Command line length ({len(result)}) exceeds limit ({max_len})")

    return result


def main():
    parser = argparse.ArgumentParser(
        description="Convert Harvester YAML configuration to kernel boot parameters."
    )
    parser.add_argument("config_file", help="Path to Harvester configuration YAML file")
    parser.add_argument(
        "--mode",
        choices=["create", "join"],
        default=None,
        help="Override install mode (default: from config file)",
    )
    parser.add_argument(
        "--exclude-secrets",
        action="store_true",
        help="Omit password, token, and SSH keys from cmdline (recommended when config file is baked into ISO)",
    )
    parser.add_argument(
        "--max-length",
        type=int,
        default=MAX_CMDLINE_LEN,
        help=f"Maximum allowed kernel command-line length (default: {MAX_CMDLINE_LEN})",
    )
    parser.add_argument(
        "--extra-cmdline",
        default=None,
        help="Additional kernel parameters to append",
    )
    args = parser.parse_args()

    if not os.path.isfile(args.config_file):
        sys.stderr.write(f"[-] Error: Configuration file '{args.config_file}' not found.\n")
        sys.exit(1)

    try:
        with open(args.config_file, "r", encoding="utf-8") as f:
            cfg = yaml.safe_load(f) or {}
    except Exception as e:
        sys.stderr.write(f"[-] Error parsing YAML in '{args.config_file}': {e}\n")
        sys.exit(1)

    try:
        cmdline = flatten_config(
            cfg,
            mode_override=args.mode,
            exclude_secrets=args.exclude_secrets,
            extra_cmdline=args.extra_cmdline,
            max_len=args.max_length,
        )
        print(cmdline)
    except Exception as e:
        sys.stderr.write(f"[-] Generation failed: {e}\n")
        sys.exit(1)


if __name__ == "__main__":
    main()
