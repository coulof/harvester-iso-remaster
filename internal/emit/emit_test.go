package emit

import (
	"strings"
	"testing"

	"github.com/harvester/harvester-installer/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmit_CreateModeCanonical(t *testing.T) {
	cfg := &config.HarvesterConfig{
		SchemeVersion: 1,
		Token:         "cluster-token-sample",
		OS: config.OS{
			Hostname: "hv-create-node",
			Password: "rancher#sample$password",
			SSHAuthorizedKeys: []string{
				"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKey123 user@domain",
			},
			DNSNameservers: []string{
				"10.144.103.254",
				"8.8.8.8",
			},
			NTPServers: []string{
				"0.suse.pool.ntp.org",
			},
		},
		Install: config.Install{
			Mode:       "create",
			Device:     "/dev/sda",
			DataDisk:   "/dev/sdb",
			Vip:        "10.144.98.240",
			VipMode:    "static",
			SkipChecks: true,
			ISOURL:     "https://releases.rancher.com/harvester/v1.8.2/harvester-v1.8.2-amd64.iso",
			ManagementInterface: config.Network{
				Method:     "static",
				IP:         "10.144.98.241",
				SubnetMask: "255.255.248.0",
				Gateway:    "10.144.103.254",
				Interfaces: []config.NetworkInterface{
					{Name: "eth0"},
				},
			},
		},
	}

	opts := Options{
		AllowSecretsOnCmdline: true,
		Format:                "raw",
	}

	line, err := Emit(cfg, nil, opts)
	require.NoError(t, err)

	// Canonical keys
	assert.Contains(t, line, "harvester.install.automatic=true")
	assert.Contains(t, line, "harvester.scheme_version=1")
	assert.Contains(t, line, "harvester.token=cluster-token-sample")
	assert.Contains(t, line, "harvester.os.hostname=hv-create-node")
	assert.Contains(t, line, "harvester.os.password=rancher#sample$password")
	assert.Contains(t, line, `harvester.os.ssh_authorized_keys="ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExampleKey123 user@domain"`)
	assert.Contains(t, line, "harvester.os.dns_nameservers=10.144.103.254")
	assert.Contains(t, line, "harvester.os.dns_nameservers=8.8.8.8")
	assert.Contains(t, line, "harvester.os.ntp_servers=0.suse.pool.ntp.org")
	assert.Contains(t, line, "harvester.install.device=/dev/sda")
	assert.Contains(t, line, "harvester.install.data_disk=/dev/sdb")
	assert.Contains(t, line, "harvester.install.vip=10.144.98.240")
	assert.Contains(t, line, "harvester.install.vip_mode=static")
	assert.Contains(t, line, "harvester.install.skipchecks=true")
	assert.Contains(t, line, "harvester.install.management_interface.interfaces=name:eth0")
	assert.Contains(t, line, "ip=10.144.98.241::10.144.103.254:255.255.248.0:hv-create-node:eth0:none")
	assert.Contains(t, line, "nameserver=10.144.103.254")
	assert.Contains(t, line, "nameserver=8.8.8.8")

	// Verify no camelCase harvester.* key was emitted
	tokens := strings.Fields(line)
	for _, tok := range tokens {
		if strings.HasPrefix(tok, "harvester.") {
			key := strings.SplitN(tok, "=", 2)[0]
			assert.NotRegexp(t, `[a-z][A-Z]`, key, "key %s contains camelCase", key)
		}
	}
}

func TestEmit_BondingAndVlan(t *testing.T) {
	cfg := &config.HarvesterConfig{
		SchemeVersion: 1,
		Token:         "tok",
		OS: config.OS{
			Hostname: "bonded-node",
			Password: "pass",
		},
		Install: config.Install{
			Mode:    "create",
			Device:  "/dev/vda",
			Vip:     "10.0.0.10",
			VipMode: "static",
			ManagementInterface: config.Network{
				Method:     "static",
				IP:         "10.100.0.20",
				SubnetMask: "255.255.255.0",
				Gateway:    "10.100.0.1",
				VlanID:     150,
				Interfaces: []config.NetworkInterface{
					{Name: "eth0", HwAddr: "52:54:00:11:22:33"},
					{Name: "eth1", HwAddr: "52:54:00:44:55:66"},
				},
				BondOptions: map[string]string{
					"mode":   "802.3ad",
					"miimon": "100",
				},
			},
		},
	}

	opts := Options{
		AllowSecretsOnCmdline: true,
	}

	line, err := Emit(cfg, nil, opts)
	require.NoError(t, err)

	parts := strings.Split(line, " ")

	assert.Contains(t, parts, "bond=mgmt-bo:eth0,eth1:miimon=100,mode=802.3ad")
	assert.Contains(t, parts, "vlan=mgmt-bo.150:mgmt-bo")
	assert.Contains(t, parts, "ifname=eth0:52:54:00:11:22:33")
	assert.Contains(t, parts, "ifname=eth1:52:54:00:44:55:66")
	assert.Contains(t, parts, "ip=10.100.0.20::10.100.0.1:255.255.255.0:bonded-node:mgmt-bo.150:none")
}

func TestEmit_NoDracut(t *testing.T) {
	cfg := &config.HarvesterConfig{
		SchemeVersion: 1,
		Token:         "tok",
		OS:            config.OS{Hostname: "node-01"},
		Install: config.Install{
			Mode:   "create",
			Device: "/dev/sda",
			ManagementInterface: config.Network{
				Method: "dhcp",
			},
		},
	}

	opts := Options{
		NoDracut: true,
	}

	line, err := Emit(cfg, nil, opts)
	require.NoError(t, err)

	assert.NotContains(t, line, "ip=")
	assert.NotContains(t, line, "nameserver=")
}

func TestEmit_SizeLimitExceeded(t *testing.T) {
	cfg := &config.HarvesterConfig{
		SchemeVersion: 1,
		OS: config.OS{
			Hostname: "node-01",
			SSHAuthorizedKeys: []string{
				"ssh-ed25519 " + strings.Repeat("A", 2500),
			},
		},
		Install: config.Install{
			Mode:   "create",
			Device: "/dev/sda",
		},
	}

	opts := Options{
		MaxLen: 2000,
	}

	_, err := Emit(cfg, nil, opts)
	require.Error(t, err)

	var sizeErr *SizeLimitError
	require.True(t, errorsAs(err, &sizeErr))
	assert.Greater(t, sizeErr.TotalLength, 2000)
	assert.Equal(t, 2000, sizeErr.Limit)
	assert.NotEmpty(t, sizeErr.Contributors)
	assert.Contains(t, err.Error(), "exceeds limit of 2000 bytes")
}

func TestEmit_DoubleQuoteRejected(t *testing.T) {
	cfg := &config.HarvesterConfig{
		OS: config.OS{
			Hostname: `bad"host`,
		},
		Install: config.Install{
			Mode:   "create",
			Device: "/dev/sda",
		},
	}

	opts := Options{}
	_, err := Emit(cfg, nil, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contains double quote (\")")
}

func TestEmit_Formats(t *testing.T) {
	cfg := &config.HarvesterConfig{
		SchemeVersion: 1,
		OS:            config.OS{Hostname: "node-01"},
		Install: config.Install{
			Mode:   "create",
			Device: "/dev/sda",
		},
	}

	raw, err := Emit(cfg, nil, Options{Format: "raw"})
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(raw, "harvester."))

	grub, err := Emit(cfg, nil, Options{Format: "grub"})
	require.NoError(t, err)
	assert.Equal(t, "set extra_iso_cmdline=\""+raw+"\"", grub)

	ipxe, err := Emit(cfg, nil, Options{Format: "ipxe"})
	require.NoError(t, err)
	assert.Equal(t, "imgargs "+raw, ipxe)
}

func TestEmit_StaticNetworkFailsWhenNoInterfaceName(t *testing.T) {
	cfg := &config.HarvesterConfig{
		OS: config.OS{Hostname: "node-01"},
		Install: config.Install{
			Mode:   "create",
			Device: "/dev/sda",
			ManagementInterface: config.Network{
				Method:     "static",
				IP:         "10.0.0.2",
				SubnetMask: "255.255.255.0",
				Gateway:    "10.0.0.1",
				Interfaces: []config.NetworkInterface{
					{HwAddr: "52:54:00:12:34:56"}, // No name!
				},
			},
		},
	}

	_, err := Emit(cfg, nil, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot determine boot interface")
}

func errorsAs(err error, target any) bool {
	switch t := target.(type) {
	case **SizeLimitError:
		if e, ok := err.(*SizeLimitError); ok {
			*t = e
			return true
		}
	}
	return false
}
