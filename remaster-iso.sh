#!/usr/bin/env bash
#
# remaster-iso.sh — Remaster Harvester v1.8 ISO with baked-in unattended GRUB configuration.
#
# Works natively on Linux and macOS (Apple Silicon / Intel).
# Extracts the official Harvester ISO, injects automated installation parameters
# into GRUB (from a Harvester YAML configuration and/or an iPXE script),
# and repacks a bootable hybrid UEFI/GPT ISO.
#
# Reference: SUSE Virtualization / Harvester build pipeline (package-harvester-os)
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GO_CMDLINE="${SCRIPT_DIR}/bin/harvester-cmdline"
YAML_CONVERTER="${SCRIPT_DIR}/yaml-to-cmdline.py"

# Defaults
SOURCE_ISO=""
CONFIG_FILE=""
IPXE_FILE=""
MODE="create"
OUTPUT_ISO=""
GRUB_TIMEOUT=3
VOLUME_LABEL="COS_LIVE"
EXTRA_CMDLINE=""

usage() {
    cat <<EOF
Usage: $(basename "$0") [options]

Required Options:
  --source-iso <path>    Path to original Harvester ISO (e.g. harvester-v1.8.2-amd64.iso)
  --output-iso <path>    Path to output remastered ISO (default: ./harvester-v1.8.2-<mode>.iso)

Configuration Source (at least one required):
  --config-file <path>   Path to Harvester config YAML (e.g. config-create.yaml)
  --ipxe-file <path>     Path to iPXE script (e.g. csc/ipxe) to extract kernel boot arguments

Optional:
  --mode <create|join>   Cluster deployment mode (default: create)
  --timeout <seconds>    GRUB menu timeout in seconds (default: 3)
  --volume-id <label>    ISO Volume Label (default: COS_LIVE)
  --extra-cmdline <args> Additional kernel parameters to append
  -h, --help             Show this help message and exit

Dependencies:
  - xorriso
  - mcopy (from mtools package)
  - mkfs.vfat or mkfs.fat (from dosfstools package)
  - python3

Examples:
  # Using iPXE script + Config YAML:
  $(basename "$0") \\
    --source-iso ./harvester-v1.8.2-amd64.iso \\
    --ipxe-file ./csc/ipxe \\
    --config-file ./csc/config-create-01.yaml \\
    --output-iso ./harvester-v1.8.2-create.iso

  # Using Config YAML directly (baked-in parameters):
  $(basename "$0") \\
    --source-iso ./harvester-v1.8.2-amd64.iso \\
    --config-file ./provisioning/harvester-config/config-create.yaml \\
    --mode create \\
    --output-iso ./harvester-v1.8.2-create.iso
EOF
    exit 1
}

# Parse Arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --source-iso)
            SOURCE_ISO="$2"
            shift 2
            ;;
        --config-file)
            CONFIG_FILE="$2"
            shift 2
            ;;
        --ipxe-file)
            IPXE_FILE="$2"
            shift 2
            ;;
        --mode)
            MODE="$2"
            shift 2
            ;;
        --output-iso)
            OUTPUT_ISO="$2"
            shift 2
            ;;
        --timeout)
            GRUB_TIMEOUT="$2"
            shift 2
            ;;
        --volume-id)
            VOLUME_LABEL="$2"
            shift 2
            ;;
        --extra-cmdline)
            EXTRA_CMDLINE="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "[-] Error: Unknown argument: $1" >&2
            usage
            ;;
    esac
done

# Validate Required Inputs
if [[ -z "$SOURCE_ISO" ]]; then
    echo "[-] Error: Missing --source-iso argument." >&2
    usage
fi

if [[ -z "$CONFIG_FILE" ]] && [[ -z "$IPXE_FILE" ]]; then
    echo "[-] Error: You must provide at least one of --config-file or --ipxe-file." >&2
    usage
fi

if [[ "$MODE" != "create" && "$MODE" != "join" ]]; then
    echo "[-] Error: --mode must be either 'create' or 'join' (received: '$MODE')." >&2
    exit 1
fi

if [[ -z "$OUTPUT_ISO" ]]; then
    OUTPUT_ISO="./harvester-v1.8.2-${MODE}.iso"
fi

if [[ ! -f "$SOURCE_ISO" ]]; then
    echo "[-] Error: Source ISO '$SOURCE_ISO' does not exist." >&2
    exit 1
fi

if [[ -n "$CONFIG_FILE" ]] && [[ ! -f "$CONFIG_FILE" ]]; then
    echo "[-] Error: Configuration file '$CONFIG_FILE' does not exist." >&2
    exit 1
fi

if [[ -n "$IPXE_FILE" ]] && [[ ! -f "$IPXE_FILE" ]]; then
    echo "[-] Error: iPXE file '$IPXE_FILE' does not exist." >&2
    exit 1
fi

# Detect FAT formatting tool (mkfs.vfat on Linux, mkfs.fat on macOS Homebrew)
MKFS_VFAT_BIN=""
if command -v mkfs.vfat >/dev/null 2>&1; then
    MKFS_VFAT_BIN="mkfs.vfat"
elif command -v mkfs.fat >/dev/null 2>&1; then
    MKFS_VFAT_BIN="mkfs.fat"
fi

# Assert Tooling
MISSING_TOOLS=()
for tool in xorriso mcopy python3; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        MISSING_TOOLS+=("$tool")
    fi
done

if [[ -z "$MKFS_VFAT_BIN" ]]; then
    MISSING_TOOLS+=("mkfs.vfat/mkfs.fat")
fi

if [[ ${#MISSING_TOOLS[@]} -gt 0 ]]; then
    echo "[-] Error: Missing required tools: ${MISSING_TOOLS[*]}" >&2
    echo "    Install them via:" >&2
    echo "      - macOS:           brew install xorriso mtools dosfstools" >&2
    echo "      - openSUSE / SLES: sudo zypper in -y xorriso mtools dosfstools python3" >&2
    echo "      - Ubuntu / Debian: sudo apt-get install -y xorriso mtools dosfstools python3" >&2
    echo "      - RHEL / Rocky:    sudo dnf install -y xorriso mtools dosfstools python3" >&2
    exit 1
fi

# Set up temporary working directory
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/harv-remaster.XXXXXX")"
EXTRACT_DIR="${TMP_DIR}/iso"
UEFI_IMG="${TMP_DIR}/uefi.img"

cleanup() {
    local exit_code=$?
    if [[ -d "$TMP_DIR" ]]; then
        rm -rf "$TMP_DIR"
    fi
    exit $exit_code
}
trap cleanup EXIT INT TERM

echo "[+] Step 1/5: Determining kernel cmdline parameters..."
CMDLINE_PARAMS=""
BAKED_PARAMS=""

if [[ -n "$CONFIG_FILE" ]]; then
    echo "    Translating YAML config ($CONFIG_FILE) into baked kernel arguments..."
    if [[ -x "$GO_CMDLINE" ]]; then
        BAKED_PARAMS="$("$GO_CMDLINE" --allow-secrets-on-cmdline --mode "$MODE" "$CONFIG_FILE")"
    elif command -v harvester-cmdline >/dev/null 2>&1; then
        BAKED_PARAMS="$(harvester-cmdline --allow-secrets-on-cmdline --mode "$MODE" "$CONFIG_FILE")"
    else
        BAKED_PARAMS="$(python3 "$YAML_CONVERTER" "$CONFIG_FILE" --mode "$MODE")"
    fi
fi

if [[ -n "$IPXE_FILE" ]]; then
    echo "    Extracting arguments from iPXE script ($IPXE_FILE)..."
    # Read the 'kernel' line, remove 'kernel <kernel-path>', remove 'root=live:http...',
    # remove 'initrd=...' (handled separately in grub.cfg).
    CMDLINE_PARAMS=$(python3 -c "
import sys, re
with open(sys.argv[1]) as f:
    for line in f:
        line = line.strip()
        if line.startswith('kernel'):
            parts = line.split()[2:] # Skip 'kernel' and kernel image path
            filtered = []
            for p in parts:
                if p.startswith('root=live:http'):
                    continue # Keep local squashfs on ISO
                if p.startswith('initrd='):
                    continue # Handled by GRUB
                filtered.append(p)
            print(' '.join(filtered))
            break
" "$IPXE_FILE")
else
    CMDLINE_PARAMS="$BAKED_PARAMS"
fi

# If config specifies ttyS0 or if serial console is requested, ensure both consoles are set
SERIAL_CONSOLE=""
if [[ -n "$CONFIG_FILE" ]] && grep -q "ttyS0" "$CONFIG_FILE"; then
    SERIAL_CONSOLE="console=ttyS0,115200n8 console=tty1"
elif [[ -n "$IPXE_FILE" ]] && grep -q "ttyS0" "$IPXE_FILE"; then
    SERIAL_CONSOLE="console=ttyS0,115200n8 console=tty1"
fi

if [[ -n "$SERIAL_CONSOLE" ]]; then
    if [[ "$CMDLINE_PARAMS" != *"console=ttyS0"* ]]; then
        CMDLINE_PARAMS="$(echo "$CMDLINE_PARAMS" | sed 's/console=tty1//g') ${SERIAL_CONSOLE}"
    fi
    if [[ -n "$BAKED_PARAMS" ]] && [[ "$BAKED_PARAMS" != *"console=ttyS0"* ]]; then
        BAKED_PARAMS="$(echo "$BAKED_PARAMS" | sed 's/console=tty1//g') ${SERIAL_CONSOLE}"
    fi
fi

# Append any user-provided extra cmdline
if [[ -n "$EXTRA_CMDLINE" ]]; then
    CMDLINE_PARAMS="${CMDLINE_PARAMS} ${EXTRA_CMDLINE}"
    if [[ -n "$BAKED_PARAMS" ]]; then
        BAKED_PARAMS="${BAKED_PARAMS} ${EXTRA_CMDLINE}"
    fi
fi

# Trim whitespace
CMDLINE_PARAMS="$(echo "$CMDLINE_PARAMS" | xargs)"
BAKED_PARAMS="$(echo "$BAKED_PARAMS" | xargs)"

if [[ -z "$CMDLINE_PARAMS" ]]; then
    echo "[-] Error: Failed to determine kernel parameters." >&2
    exit 1
fi

echo "    Primary kernel parameters:"
echo "    >>> $CMDLINE_PARAMS <<<"
if [[ -n "$BAKED_PARAMS" && "$BAKED_PARAMS" != "$CMDLINE_PARAMS" ]]; then
    echo "    Offline baked kernel parameters:"
    echo "    >>> $BAKED_PARAMS <<<"
fi

echo "[+] Step 2/5: Extracting source ISO ($SOURCE_ISO)..."
mkdir -p "$EXTRACT_DIR"
xorriso -osirrox on -indev "$SOURCE_ISO" -extract / "$EXTRACT_DIR"
chmod -R u+w "$EXTRACT_DIR"

echo "[+] Step 3/5: Injecting GRUB boot configuration..."
# 3a. Inject into harvester.cfg (sourced by grub.cfg via ${extra_iso_cmdline})
cat > "${EXTRACT_DIR}/boot/grub2/harvester.cfg" <<EOF
# Generated by Harvester vmedia remastering tool
set harvester_version=v1.8.2
set extra_iso_cmdline="${CMDLINE_PARAMS}"
set extra_baked_cmdline="${BAKED_PARAMS}"
EOF

# 3b. Update GRUB timeout & menu titles portably with Python
MODE_UPPER="$(echo "$MODE" | tr '[:lower:]' '[:upper:]')"
HAS_BAKED_DIFF="false"
if [[ -n "$BAKED_PARAMS" && "$BAKED_PARAMS" != "$CMDLINE_PARAMS" ]]; then
    HAS_BAKED_DIFF="true"
fi

python3 -c "
import sys, re
grub_cfg = sys.argv[1]
timeout = sys.argv[2]
mode_upper = sys.argv[3]
has_baked = sys.argv[4] == 'true'
try:
    with open(grub_cfg, 'r') as f:
        content = f.read()
    # Update timeout
    content = re.sub(r'set timeout=\d+', f'set timeout={timeout}', content)
    # Update title
    content = re.sub(r'menuentry \"Harvester Installer \$\{harvester_version\}\"', f'menuentry \"Harvester Installer \${{harvester_version}} ({mode_upper} Mode - Automated)\"', content)
    
    if has_baked:
        offline_entry = '''
menuentry \"Harvester Installer \${harvester_version} (''' + mode_upper + ''' Mode - Offline Baked)\" --class os --unrestricted {
    echo Loading kernel...
    \$linux (\$root)/boot/x86_64/loader/linux cdroot root=live:CDLABEL=COS_LIVE rd.live.dir=/ rd.live.squashimg=rootfs.squashfs rd.cos.disable net.ifnames=1 \${extra_baked_cmdline}
    echo Loading initrd...
    \$initrd (\$root)/boot/x86_64/loader/initrd
}
'''
        first_menu_end = content.find('}\n\nmenuentry')
        if first_menu_end != -1:
            content = content[:first_menu_end+2] + offline_entry + content[first_menu_end+2:]

    with open(grub_cfg, 'w') as f:
        f.write(content)
except Exception as e:
    sys.stderr.write(f'Error updating {grub_cfg}: {e}\n')
    sys.exit(1)
" "${EXTRACT_DIR}/boot/grub2/grub.cfg" "$GRUB_TIMEOUT" "$MODE_UPPER" "$HAS_BAKED_DIFF"

# 3c. Archive files into ISO for operator traceability & local recovery
mkdir -p "${EXTRACT_DIR}/harvester-config"
if [[ -n "$CONFIG_FILE" ]]; then
    CONFIG_BASENAME="$(basename "$CONFIG_FILE")"
    cp "$CONFIG_FILE" "${EXTRACT_DIR}/harvester-config/${CONFIG_BASENAME}"
    cp "$CONFIG_FILE" "${EXTRACT_DIR}/${CONFIG_BASENAME}"
fi

if [[ -n "$IPXE_FILE" ]]; then
    cp "$IPXE_FILE" "${EXTRACT_DIR}/harvester-config/ipxe-${MODE}"
fi

echo "[+] Step 4/5: Rebuilding FAT32 EFI boot image (COS_GRUB)..."
dd if=/dev/zero of="${UEFI_IMG}" bs=1k count=4096 status=none
"$MKFS_VFAT_BIN" "${UEFI_IMG}" -n COS_GRUB
mcopy -s -i "${UEFI_IMG}" "${EXTRACT_DIR}/EFI" ::

echo "[+] Step 5/5: Repacking bootable hybrid ISO (${OUTPUT_ISO})..."
mkdir -p "$(dirname "$OUTPUT_ISO")"
rm -f "$OUTPUT_ISO"
xorriso -volid "$VOLUME_LABEL" \
    -joliet on -padding 0 \
    -outdev "$OUTPUT_ISO" \
    -map "$EXTRACT_DIR" / -chmod 0755 -- \
    -append_partition 2 0xef "$UEFI_IMG" \
    -boot_image any cat_path="boot/boot.catalog" \
    -boot_image any cat_hidden=on \
    -boot_image any efi_path=--interval:appended_partition_2:all:: \
    -boot_image any platform_id=0xef \
    -boot_image any appended_part_as=gpt \
    -boot_image any partition_offset=16

# Generate SHA-512 Checksum
echo "[+] Calculating SHA512 checksum..."
CHECKSUM_FILE="${OUTPUT_ISO}.sha512"
if command -v sha512sum >/dev/null 2>&1; then
    sha512sum "$OUTPUT_ISO" > "$CHECKSUM_FILE"
elif command -v shasum >/dev/null 2>&1; then
    shasum -a 512 "$OUTPUT_ISO" > "$CHECKSUM_FILE"
fi

echo ""
echo "================================================================="
echo "[✓] Successfully created remastered Harvester ISO"
echo "================================================================="
echo "    Output ISO:    $OUTPUT_ISO"
if [[ -f "$CHECKSUM_FILE" ]]; then
    echo "    SHA-512:       $(cat "$CHECKSUM_FILE" | awk '{print $1}')"
fi
echo "    Mode:          ${MODE_UPPER}"
echo "    GRUB Timeout:  ${GRUB_TIMEOUT}s"
echo "    Kernel Args:   ${CMDLINE_PARAMS}"
echo "================================================================="
