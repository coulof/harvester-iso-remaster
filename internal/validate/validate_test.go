package validate

import (
	"testing"

	"github.com/harvester/harvester-installer/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_ValidCreateMode(t *testing.T) {
	cfg := &config.HarvesterConfig{
		SchemeVersion: 1,
		Token:         "cluster-token",
		OS: config.OS{
			Hostname: "node-01",
			Password: "rancher-password",
		},
		Install: config.Install{
			Mode:     "create",
			Device:   "/dev/sda",
			Vip:      "10.0.0.100",
			VipMode:  "static",
			ISOURL:   "http://example.com/harvester.iso",
			ManagementInterface: config.Network{
				Method:     "static",
				IP:         "10.0.0.101",
				SubnetMask: "255.255.255.0",
				Gateway:    "10.0.0.1",
				Interfaces: []config.NetworkInterface{
					{Name: "eth0", HwAddr: "52:54:00:12:34:56"},
				},
			},
		},
	}

	opts := Options{
		AllowSecretsOnCmdline: true,
	}

	err := Validate(cfg, opts)
	require.NoError(t, err)
}

func TestValidate_ValidJoinMode(t *testing.T) {
	cfg := &config.HarvesterConfig{
		SchemeVersion: 1,
		ServerURL:     "https://10.0.0.100:443",
		Token:         "cluster-token",
		OS: config.OS{
			Hostname: "node-02",
			Password: "rancher-password",
		},
		Install: config.Install{
			Mode:   "join",
			Device: "/dev/sda",
			ManagementInterface: config.Network{
				Method: "dhcp",
			},
		},
	}

	opts := Options{
		AllowSecretsOnCmdline: true,
	}

	err := Validate(cfg, opts)
	require.NoError(t, err)
}

func TestValidate_JoinMissingServerURL(t *testing.T) {
	cfg := &config.HarvesterConfig{
		Token: "cluster-token",
		OS:    config.OS{Hostname: "node-02", Password: "pass"},
		Install: config.Install{
			Mode:   "join",
			Device: "/dev/sda",
		},
	}

	opts := Options{AllowSecretsOnCmdline: true}
	err := Validate(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "join mode requires 'server_url'")
}

func TestValidate_CreateMissingVip(t *testing.T) {
	cfg := &config.HarvesterConfig{
		Token: "cluster-token",
		OS:    config.OS{Hostname: "node-01", Password: "pass"},
		Install: config.Install{
			Mode:    "create",
			Device:  "/dev/sda",
			VipMode: "static",
		},
	}

	opts := Options{AllowSecretsOnCmdline: true}
	err := Validate(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create mode requires 'install.vip'")
}

func TestValidate_CreateVipModeDHCPWithHwAddr(t *testing.T) {
	cfg := &config.HarvesterConfig{
		Token: "cluster-token",
		OS:    config.OS{Hostname: "node-01", Password: "pass"},
		Install: config.Install{
			Mode:      "create",
			Device:    "/dev/sda",
			Vip:       "10.0.0.1",
			VipMode:   "dhcp",
			VipHwAddr: "52:54:00:12:34:56",
		},
	}

	opts := Options{AllowSecretsOnCmdline: true}
	err := Validate(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install.vip_hw_addr must not be set when install.vip_mode is 'dhcp'")
}

func TestValidate_CreateVipModeStaticInvalidIP(t *testing.T) {
	cfg := &config.HarvesterConfig{
		Token: "cluster-token",
		OS:    config.OS{Hostname: "node-01", Password: "pass"},
		Install: config.Install{
			Mode:    "create",
			Device:  "/dev/sda",
			Vip:     "not-an-ip",
			VipMode: "static",
		},
	}

	opts := Options{AllowSecretsOnCmdline: true}
	err := Validate(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a valid IP address")
}

func TestValidate_MissingDevice(t *testing.T) {
	cfg := &config.HarvesterConfig{
		Token: "cluster-token",
		OS:    config.OS{Hostname: "node-01", Password: "pass"},
		Install: config.Install{
			Mode:    "create",
			Vip:     "10.0.0.1",
			VipMode: "static",
		},
	}

	opts := Options{AllowSecretsOnCmdline: true}
	err := Validate(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required field: 'install.device'")
}

func TestValidate_SecretsRefusedByDefault(t *testing.T) {
	cfg := &config.HarvesterConfig{
		Token: "cluster-token",
		OS:    config.OS{Hostname: "node-01", Password: "secret-password"},
		Install: config.Install{
			Mode:    "create",
			Device:  "/dev/sda",
			Vip:     "10.0.0.1",
			VipMode: "static",
		},
	}

	opts := Options{AllowSecretsOnCmdline: false}
	err := Validate(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to emit plaintext secrets")
}

func TestValidate_InvalidISOURL(t *testing.T) {
	cfg := &config.HarvesterConfig{
		OS: config.OS{Hostname: "node-01"},
		Install: config.Install{
			Mode:    "create",
			Device:  "/dev/sda",
			Vip:     "10.0.0.1",
			VipMode: "static",
			ISOURL:  "local", // The python script used "local", which is invalid!
		},
	}

	opts := Options{}
	err := Validate(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid install.iso_url 'local'")
}

func TestValidate_StaticMgmtMissingGateway(t *testing.T) {
	cfg := &config.HarvesterConfig{
		OS: config.OS{Hostname: "node-01"},
		Install: config.Install{
			Mode:    "create",
			Device:  "/dev/sda",
			Vip:     "10.0.0.1",
			VipMode: "static",
			ManagementInterface: config.Network{
				Method:     "static",
				IP:         "10.0.0.10",
				SubnetMask: "255.255.255.0",
				Interfaces: []config.NetworkInterface{{Name: "eth0"}},
			},
		},
	}

	opts := Options{}
	err := Validate(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "install.management_interface.gateway")
}

func TestValidate_DoubleQuoteRejected(t *testing.T) {
	cfg := &config.HarvesterConfig{
		OS: config.OS{
			Hostname: `bad"hostname`,
		},
		Install: config.Install{
			Mode:    "create",
			Device:  "/dev/sda",
			Vip:     "10.0.0.1",
			VipMode: "static",
		},
	}

	opts := Options{}
	err := Validate(cfg, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contains double quote (\")")
}

func TestValidate_ModeOverride(t *testing.T) {
	cfg := &config.HarvesterConfig{
		ServerURL: "https://10.0.0.1:443",
		Token:     "tok",
		OS:        config.OS{Hostname: "node-02", Password: "pass"},
		Install: config.Install{
			Mode:   "create", // config says create, but override will say join
			Device: "/dev/sda",
		},
	}

	// With override to join, it should validate as join
	opts := Options{
		ModeOverride:          "join",
		AllowSecretsOnCmdline: true,
	}
	err := Validate(cfg, opts)
	require.NoError(t, err)
}
