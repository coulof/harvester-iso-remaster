package emit

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/harvester/harvester-installer/pkg/config"
	"github.com/rancher/mapper/convert"
)

// DefaultMaxCmdlineLen is the default maximum command line length (COMMAND_LINE_SIZE is 2048 on x86_64).
const DefaultMaxCmdlineLen = 2000

// Options configures command line parameter emission.
type Options struct {
	ModeOverride          string
	AllowSecretsOnCmdline bool
	NoDracut              bool
	MaxLen                int
	Format                string
}

// Contributor records the contribution of a single parameter to the overall length.
type Contributor struct {
	Param  string
	Length int
}

// SizeLimitError indicates that the generated command line exceeds the configured size limit.
type SizeLimitError struct {
	TotalLength  int
	Limit        int
	Contributors []Contributor
}

func (e *SizeLimitError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("generated command line length (%d bytes) exceeds limit of %d bytes against x86_64 COMMAND_LINE_SIZE (2048 bytes).\n", e.TotalLength, e.Limit))
	sb.WriteString("Top largest parameter contributors:\n")
	count := len(e.Contributors)
	if count > 5 {
		count = 5
	}
	for i := 0; i < count; i++ {
		c := e.Contributors[i]
		preview := c.Param
		if len(preview) > 60 {
			preview = preview[:57] + "..."
		}
		sb.WriteString(fmt.Sprintf("  - [%d bytes] %s\n", c.Length, preview))
	}
	return sb.String()
}

// Emit converts a HarvesterConfig and raw YAML map into kernel command-line parameters.
func Emit(cfg *config.HarvesterConfig, rawYAML map[string]any, opts Options) (string, error) {
	if cfg == nil {
		return "", errors.New("nil configuration provided")
	}

	maxLen := opts.MaxLen
	if maxLen <= 0 {
		maxLen = DefaultMaxCmdlineLen
	}

	// 1. Convert config to map
	data, err := convert.EncodeToMap(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to encode HarvesterConfig to map: %w", err)
	}

	// 2. Build harvester.* parameters
	harvesterParams, err := emitHarvesterParams(cfg, data, rawYAML, opts)
	if err != nil {
		return "", err
	}

	allParams := make([]string, 0, len(harvesterParams)+10)
	allParams = append(allParams, harvesterParams...)

	// 3. Build Dracut parameters if enabled
	if !opts.NoDracut {
		dracutParams, err := emitDracutParams(cfg)
		if err != nil {
			return "", err
		}
		allParams = append(allParams, dracutParams...)
	}

	// 4. Render space-joined command line
	rawLine := strings.Join(allParams, " ")

	// 5. Size check
	if len(rawLine) > maxLen {
		contributors := make([]Contributor, len(allParams))
		for i, p := range allParams {
			contributors[i] = Contributor{
				Param:  p,
				Length: len(p),
			}
		}
		sort.Slice(contributors, func(i, j int) bool {
			return contributors[i].Length > contributors[j].Length
		})
		return "", &SizeLimitError{
			TotalLength:  len(rawLine),
			Limit:        maxLen,
			Contributors: contributors,
		}
	}

	// 6. Format output
	switch strings.ToLower(opts.Format) {
	case "", "raw":
		return rawLine, nil
	case "grub":
		return fmt.Sprintf("set extra_iso_cmdline=\"%s\"", rawLine), nil
	case "ipxe":
		return fmt.Sprintf("imgargs %s", rawLine), nil
	default:
		return "", fmt.Errorf("unsupported output format '%s': must be raw, grub, or ipxe", opts.Format)
	}
}

func emitHarvesterParams(cfg *config.HarvesterConfig, data map[string]any, rawYAML map[string]any, opts Options) ([]string, error) {
	var params []string

	// §5.3: harvester.install.automatic=true is always emitted
	params = append(params, "harvester.install.automatic=true")

	// §5.3: harvester.scheme_version taken from loaded config if set
	if cfg.SchemeVersion != 0 {
		params = append(params, fmt.Sprintf("harvester.scheme_version=%d", cfg.SchemeVersion))
	}

	// Generic recursive emitter for other harvester.* fields
	genericParams, err := walkAndEmit("harvester", data, rawYAML, []string{}, opts)
	if err != nil {
		return nil, err
	}

	// Filter out automatic and scheme_version from generic params to avoid duplicates
	for _, p := range genericParams {
		if strings.HasPrefix(p, "harvester.install.automatic=") || strings.HasPrefix(p, "harvester.scheme_version=") {
			continue
		}
		// If mode override is active, override install.mode
		if opts.ModeOverride != "" && strings.HasPrefix(p, "harvester.install.mode=") {
			continue
		}
		params = append(params, p)
	}

	if opts.ModeOverride != "" {
		params = append(params, fmt.Sprintf("harvester.install.mode=%s", opts.ModeOverride))
	}

	return params, nil
}

func walkAndEmit(prefix string, data map[string]any, rawYAML map[string]any, path []string, opts Options) ([]string, error) {
	var params []string

	// Sort keys for deterministic output
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := data[k]
		currentPath := append(path, k)
		yamlKey := convert.ToYAMLKey(k)
		keyName := prefix + "." + yamlKey

		// Check if this is a secret
		if !opts.AllowSecretsOnCmdline {
			if keyName == "harvester.token" || keyName == "harvester.os.password" {
				continue
			}
		}

		// §5.3 Special case: install.management_interface.interfaces
		if keyName == "harvester.install.management_interface.interfaces" {
			ifSlice, ok := v.([]any)
			if ok && len(ifSlice) > 0 {
				for _, ifaceObj := range ifSlice {
					ifaceMap, ok := ifaceObj.(map[string]any)
					if !ok {
						continue
					}
					var parts []string
					hwAddr, _ := ifaceMap["hwAddr"].(string)
					name, _ := ifaceMap["name"].(string)

					if strings.Contains(hwAddr, "\"") || strings.Contains(name, "\"") {
						return nil, fmt.Errorf("interface name or hwAddr contains double quote (\")")
					}

					if hwAddr != "" {
						parts = append(parts, "hwAddr:"+hwAddr)
					}
					if name != "" {
						parts = append(parts, "name:"+name)
					}
					if len(parts) > 0 {
						valStr := strings.Join(parts, ",")
						params = append(params, formatParam(keyName, valStr))
					}
				}
			}
			continue
		}

		switch val := v.(type) {
		case map[string]any:
			// Recurse into nested maps
			subParams, err := walkAndEmit(keyName, val, rawYAML, currentPath, opts)
			if err != nil {
				return nil, err
			}
			params = append(params, subParams...)

		case []any:
			if len(val) == 0 {
				continue
			}
			// Repeated key for each element
			for _, item := range val {
				itemStr := fmt.Sprintf("%v", item)
				if strings.Contains(itemStr, "\"") {
					return nil, fmt.Errorf("value for %s contains double quote (\")", keyName)
				}
				params = append(params, formatParam(keyName, itemStr))
			}

		case string:
			if strings.Contains(val, "\"") {
				return nil, fmt.Errorf("value for %s contains double quote (\")", keyName)
			}
			if val == "" {
				continue
			}
			params = append(params, formatParam(keyName, val))

		case bool:
			// Check if zero value (false) and was not present in raw YAML
			if !val && !rawYAMLHasPath(rawYAML, currentPath) {
				continue
			}
			params = append(params, fmt.Sprintf("%s=%t", keyName, val))

		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			numStr := fmt.Sprintf("%d", val)
			if numStr == "0" && !rawYAMLHasPath(rawYAML, currentPath) {
				continue
			}
			params = append(params, fmt.Sprintf("%s=%s", keyName, numStr))

		case float32, float64:
			fVal := fmt.Sprintf("%v", val)
			if (fVal == "0" || fVal == "0.0") && !rawYAMLHasPath(rawYAML, currentPath) {
				continue
			}
			params = append(params, fmt.Sprintf("%s=%s", keyName, fVal))

		default:
			if val == nil {
				continue
			}
			valStr := fmt.Sprintf("%v", val)
			if strings.Contains(valStr, "\"") {
				return nil, fmt.Errorf("value for %s contains double quote (\")", keyName)
			}
			params = append(params, formatParam(keyName, valStr))
		}
	}

	return params, nil
}

// formatParam formats a key-value parameter, quoting if it contains spaces.
func formatParam(key, val string) string {
	if strings.Contains(val, " ") {
		return fmt.Sprintf("%s=\"%s\"", key, val)
	}
	return fmt.Sprintf("%s=%s", key, val)
}

// rawYAMLHasPath checks if a given struct-based path exists in the raw unmarshalled YAML.
func rawYAMLHasPath(raw map[string]any, path []string) bool {
	if raw == nil || len(path) == 0 {
		return false
	}

	curr := any(raw)
	for _, seg := range path {
		m, ok := curr.(map[string]any)
		if !ok {
			return false
		}

		yamlKey := convert.ToYAMLKey(seg)
		lowerKey := strings.ToLower(seg)

		var matchedVal any
		found := false

		for k, v := range m {
			if k == seg || k == yamlKey || strings.ToLower(k) == lowerKey || convert.ToYAMLKey(k) == yamlKey {
				matchedVal = v
				found = true
				break
			}
		}

		if !found {
			return false
		}
		curr = matchedVal
	}

	return true
}

func emitDracutParams(cfg *config.HarvesterConfig) ([]string, error) {
	mgmt := cfg.Install.ManagementInterface
	var params []string

	method := mgmt.Method
	if method == "" {
		method = config.NetworkMethodDHCP
	}

	switch method {
	case config.NetworkMethodDHCP:
		params = append(params, "ip=dhcp")
		// Emit deterministic ifname=<name>:<mac> if known
		for _, iface := range mgmt.Interfaces {
			if iface.Name != "" && iface.HwAddr != "" {
				params = append(params, fmt.Sprintf("ifname=%s:%s", iface.Name, iface.HwAddr))
			}
		}
		return params, nil

	case config.NetworkMethodStatic:
		if len(mgmt.Interfaces) == 0 {
			return nil, errors.New("cannot determine boot interface: install.management_interface.interfaces is empty")
		}

		// Check interface names
		for i, iface := range mgmt.Interfaces {
			if iface.Name == "" {
				return nil, fmt.Errorf("cannot determine boot interface: interface at index %d has no name", i)
			}
		}

		baseIface := mgmt.Interfaces[0].Name

		// Handle bonding if multiple interfaces
		if len(mgmt.Interfaces) > 1 {
			baseIface = config.MgmtBondInterfaceName // "mgmt-bo"
			slaves := make([]string, len(mgmt.Interfaces))
			for i, iface := range mgmt.Interfaces {
				slaves[i] = iface.Name
			}

			bondStr := fmt.Sprintf("bond=%s:%s", baseIface, strings.Join(slaves, ","))
			var bondOpts []string
			if len(mgmt.BondOptions) > 0 {
				// Sort bond option keys for determinism
				var optKeys []string
				for k := range mgmt.BondOptions {
					optKeys = append(optKeys, k)
				}
				sort.Strings(optKeys)
				for _, k := range optKeys {
					bondOpts = append(bondOpts, fmt.Sprintf("%s=%s", k, mgmt.BondOptions[k]))
				}
			}
			if len(bondOpts) > 0 {
				bondStr += ":" + strings.Join(bondOpts, ",")
			}
			params = append(params, bondStr)
		}

		targetIface := baseIface

		// Handle VLAN
		if mgmt.VlanID > 0 {
			vlanDev := fmt.Sprintf("%s.%d", baseIface, mgmt.VlanID)
			params = append(params, fmt.Sprintf("vlan=%s:%s", vlanDev, baseIface))
			targetIface = vlanDev
		}

		// Deterministic interface name bindings
		for _, iface := range mgmt.Interfaces {
			if iface.Name != "" && iface.HwAddr != "" {
				params = append(params, fmt.Sprintf("ifname=%s:%s", iface.Name, iface.HwAddr))
			}
		}

		// Format single static ip= parameter
		// Format: ip=<client-ip>:[<peer-ip>]:<gateway-ip>:<netmask>:<hostname>:<interface>:<autoconf>[:[<mtu>][:<macaddr>]]
		// autoconf is "none"
		ipParam := fmt.Sprintf("ip=%s::%s:%s:%s:%s:none",
			mgmt.IP,
			mgmt.Gateway,
			mgmt.SubnetMask,
			cfg.OS.Hostname,
			targetIface,
		)
		if mgmt.MTU > 0 {
			ipParam += ":" + strconv.Itoa(mgmt.MTU)
		}
		params = append(params, ipParam)

		// DNS nameservers emitted as separate nameserver=<ip> arguments
		for _, dns := range cfg.OS.DNSNameservers {
			dnsClean := strings.TrimSpace(dns)
			if dnsClean != "" {
				params = append(params, fmt.Sprintf("nameserver=%s", dnsClean))
			}
		}

		return params, nil

	default:
		return nil, fmt.Errorf("unknown management network method '%s'", method)
	}
}
