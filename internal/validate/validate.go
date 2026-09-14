package validate

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/harvester/harvester-installer/pkg/config"
	"github.com/harvester/harvester-installer/pkg/util"
	"github.com/rancher/mapper/convert"
)

// Options specifies configuration validation flags.
type Options struct {
	ModeOverride          string
	AllowSecretsOnCmdline bool
}

// Validate checks the HarvesterConfig against required constraints.
// It returns an error if validation fails.
func Validate(cfg *config.HarvesterConfig, opts Options) error {
	if cfg == nil {
		return errors.New("nil configuration provided")
	}

	var errs []string

	// Determine effective mode
	mode := cfg.Install.Mode
	if opts.ModeOverride != "" {
		mode = opts.ModeOverride
	}

	// Mode validation
	switch mode {
	case config.ModeCreate:
		if cfg.Install.Vip == "" {
			errs = append(errs, "create mode requires 'install.vip'")
		}
		if cfg.Install.VipMode == "" {
			errs = append(errs, "create mode requires 'install.vip_mode' ('static' or 'dhcp')")
		} else {
			switch cfg.Install.VipMode {
			case "dhcp":
				if cfg.Install.VipHwAddr != "" {
					errs = append(errs, "install.vip_hw_addr must not be set when install.vip_mode is 'dhcp'")
				}
			case "static":
				if cfg.Install.Vip != "" && net.ParseIP(cfg.Install.Vip) == nil {
					errs = append(errs, fmt.Sprintf("invalid static install.vip '%s': must be a valid IP address", cfg.Install.Vip))
				}
			default:
				errs = append(errs, fmt.Sprintf("invalid install.vip_mode '%s': must be 'static' or 'dhcp'", cfg.Install.VipMode))
			}
		}
	case config.ModeJoin:
		if cfg.ServerURL == "" {
			errs = append(errs, "join mode requires 'server_url'")
		}
		if opts.AllowSecretsOnCmdline && cfg.Token == "" {
			errs = append(errs, "join mode requires 'token'")
		}
	default:
		errs = append(errs, fmt.Sprintf("invalid install.mode '%s': must be 'create' or 'join'", mode))
	}

	// Device is required in both modes
	if cfg.Install.Device == "" {
		errs = append(errs, "missing required field: 'install.device'")
	}

	// Hostname check
	if cfg.OS.Hostname == "" {
		errs = append(errs, "missing required field: 'os.hostname'")
	}

	// Secrets check
	hasSecrets := cfg.OS.Password != "" || cfg.Token != ""
	if hasSecrets && !opts.AllowSecretsOnCmdline {
		errs = append(errs, "refusing to emit plaintext secrets (os.password and/or token) on kernel command line. "+
			"/proc/cmdline is world-readable and captured in systemd/dmesg/bootloader logs. "+
			"Use --allow-secrets-on-cmdline to override, or serve configuration via config_url / embedded ISO config file.")
	}

	// ISO URL validation
	if cfg.Install.ISOURL != "" {
		u, err := url.Parse(cfg.Install.ISOURL)
		if err != nil || !u.IsAbs() || u.Scheme == "" || (u.Scheme != "http" && u.Scheme != "https") {
			errs = append(errs, fmt.Sprintf("invalid install.iso_url '%s': must be an absolute HTTP or HTTPS URL", cfg.Install.ISOURL))
		}
	}

	// Management network validation
	mgmt := cfg.Install.ManagementInterface
	if mgmt.Method == "static" {
		if mgmt.IP == "" {
			errs = append(errs, "static management interface requires 'install.management_interface.ip'")
		} else if net.ParseIP(mgmt.IP) == nil {
			errs = append(errs, fmt.Sprintf("invalid install.management_interface.ip '%s': must be a valid IP address", mgmt.IP))
		}

		if mgmt.SubnetMask == "" {
			errs = append(errs, "static management interface requires 'install.management_interface.subnet_mask'")
		} else if net.ParseIP(mgmt.SubnetMask) == nil {
			errs = append(errs, fmt.Sprintf("invalid install.management_interface.subnet_mask '%s': must be a valid netmask", mgmt.SubnetMask))
		}

		if mgmt.Gateway == "" {
			errs = append(errs, "static management interface requires 'install.management_interface.gateway'")
		} else if net.ParseIP(mgmt.Gateway) == nil {
			errs = append(errs, fmt.Sprintf("invalid install.management_interface.gateway '%s': must be a valid IP address", mgmt.Gateway))
		}

		// Ensure interface name can be determined
		if len(mgmt.Interfaces) == 0 {
			errs = append(errs, "static management interface requires at least one interface in 'install.management_interface.interfaces'")
		} else {
			for i, iface := range mgmt.Interfaces {
				if iface.Name == "" {
					errs = append(errs, fmt.Sprintf("interface at index %d has no name specified; boot interface cannot be guessed", i))
				}
				if iface.HwAddr != "" {
					if _, err := util.IsMACAddress(iface.HwAddr); err != nil {
						errs = append(errs, fmt.Sprintf("interface '%s' has invalid hwAddr '%s': %v", iface.Name, iface.HwAddr, err))
					}
				}
			}
		}
	} else if mgmt.Method != "" && mgmt.Method != "dhcp" {
		errs = append(errs, fmt.Sprintf("invalid install.management_interface.method '%s': must be 'dhcp' or 'static'", mgmt.Method))
	}

	// Reject any value containing double quotes (") anywhere
	m, err := convert.EncodeToMap(cfg)
	if err == nil {
		if quoteErrs := checkDoubleQuotes(m, ""); len(quoteErrs) > 0 {
			errs = append(errs, quoteErrs...)
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}

func checkDoubleQuotes(v any, path string) []string {
	var errs []string
	switch val := v.(type) {
	case string:
		if strings.Contains(val, "\"") {
			name := path
			if name == "" {
				name = "value"
			}
			errs = append(errs, fmt.Sprintf("field '%s' contains double quote (\"), which is forbidden on kernel command line", name))
		}
	case map[string]any:
		for k, item := range val {
			subPath := k
			if path != "" {
				subPath = path + "." + k
			}
			errs = append(errs, checkDoubleQuotes(item, subPath)...)
		}
	case []any:
		for i, item := range val {
			subPath := fmt.Sprintf("%s[%d]", path, i)
			errs = append(errs, checkDoubleQuotes(item, subPath)...)
		}
	}
	return errs
}
