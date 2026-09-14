package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/harvester/harvester-installer/pkg/config"
	"github.com/harvester/harvester-installer/pkg/util"
	"github.com/rancher/mapper"
	"github.com/rancher/mapper/convert"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/coulof/harvester-iso-remaster/internal/emit"
	"github.com/coulof/harvester-iso-remaster/internal/validate"
)

var update = flag.Bool("update", false, "update golden .cmdline files")

var (
	schemas = mapper.NewSchemas().Init(func(s *mapper.Schemas) *mapper.Schemas {
		s.DefaultMappers = func() []mapper.Mapper {
			return []mapper.Mapper{
				config.NewToMap(),
				config.NewToSlice(),
				config.NewToBool(),
				config.NewToInt(),
				config.NewToFloat(),
				&config.FuzzyNames{},
			}
		}
		return s
	}).MustImport(config.HarvesterConfig{})
	harvesterSchema = schemas.Schema("harvesterConfig")
)

// readConfigFromMap mirrors the unexported installer function in pkg/config/read.go
func readConfigFromMap(data map[string]any) (*config.HarvesterConfig, error) {
	cfg := config.NewHarvesterConfig()
	if err := harvesterSchema.Mapper.ToInternal(data); err != nil {
		return cfg, err
	}
	if err := convert.ToObj(data, cfg); err != nil {
		return cfg, err
	}
	if err := cfg.ExternalStorage.ParseMultiPathConfig(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// generateHelper parses YAML and generates kernel cmdline
func generateHelper(t *testing.T, yamlBytes []byte, opts emit.Options) (string, *config.HarvesterConfig) {
	rawYAML := make(map[string]any)
	err := yaml.Unmarshal(yamlBytes, &rawYAML)
	require.NoError(t, err)

	cfg, err := config.LoadHarvesterConfig(yamlBytes)
	require.NoError(t, err)

	valOpts := validate.Options{
		ModeOverride:          opts.ModeOverride,
		AllowSecretsOnCmdline: opts.AllowSecretsOnCmdline,
	}
	err = validate.Validate(cfg, valOpts)
	require.NoError(t, err)

	line, err := emit.Emit(cfg, rawYAML, opts)
	require.NoError(t, err)

	return line, cfg
}

// 7.1 Round-trip oracle test
func TestRoundTripOracle(t *testing.T) {
	fixtures := []string{
		"testdata/create-dhcp-single-nic.yaml",
		"testdata/create-static-bonded-vlan-mtu.yaml",
		"testdata/join-server-url-token.yaml",
		"testdata/multiple-ssh-keys.yaml",
		"testdata/interfaces-hwaddr-name-variants.yaml",
		"testdata/data-disk-skipchecks.yaml",
	}

	for _, f := range fixtures {
		t.Run(filepath.Base(f), func(t *testing.T) {
			yamlBytes, err := os.ReadFile(f)
			require.NoError(t, err)

			opts := emit.Options{
				AllowSecretsOnCmdline: true,
				Format:                "raw",
			}
			line, want := generateHelper(t, yamlBytes, opts)

			// Parse generated line using the installer's own ParseCmdLine
			data, err := util.ParseCmdLine(line, "harvester")
			require.NoError(t, err)

			// Roundtrip through readConfigFromMap
			got, err := readConfigFromMap(data)
			require.NoError(t, err)

			// harvester.install.automatic=true is always emitted
			want.Install.Automatic = true

			// Compare EncodeToMap representations
			wantMap, err := convert.EncodeToMap(want)
			require.NoError(t, err)
			gotMap, err := convert.EncodeToMap(got)
			require.NoError(t, err)

			require.Equal(t, wantMap, gotMap)
		})
	}
}

// 7.2 Golden files tests
func TestGoldenFiles(t *testing.T) {
	validFixtures := []string{
		"create-dhcp-single-nic",
		"create-static-bonded-vlan-mtu",
		"join-server-url-token",
		"multiple-ssh-keys",
		"interfaces-hwaddr-name-variants",
		"data-disk-skipchecks",
	}

	for _, name := range validFixtures {
		t.Run(name, func(t *testing.T) {
			yamlPath := filepath.Join("testdata", name+".yaml")
			cmdlinePath := filepath.Join("testdata", name+".cmdline")

			yamlBytes, err := os.ReadFile(yamlPath)
			require.NoError(t, err)

			opts := emit.Options{
				AllowSecretsOnCmdline: true,
				Format:                "raw",
			}
			actual, _ := generateHelper(t, yamlBytes, opts)

			if *update {
				err := os.WriteFile(cmdlinePath, []byte(actual+"\n"), 0644)
				require.NoError(t, err)
			}

			expectedBytes, err := os.ReadFile(cmdlinePath)
			require.NoError(t, err, "golden file %s missing, run with -update to generate", cmdlinePath)
			expected := strings.TrimSpace(string(expectedBytes))

			assert.Equal(t, expected, actual)
		})
	}
}

func TestGoldenFiles_Errors(t *testing.T) {
	// Size guard overflow
	t.Run("size-guard-overflow", func(t *testing.T) {
		yamlBytes, err := os.ReadFile("testdata/size-guard-overflow.yaml")
		require.NoError(t, err)

		rawYAML := make(map[string]any)
		_ = yaml.Unmarshal(yamlBytes, &rawYAML)

		cfg, err := config.LoadHarvesterConfig(yamlBytes)
		require.NoError(t, err)

		_, err = emit.Emit(cfg, rawYAML, emit.Options{
			AllowSecretsOnCmdline: true,
			MaxLen:                2000,
		})
		require.Error(t, err)
		var sizeErr *emit.SizeLimitError
		require.ErrorAs(t, err, &sizeErr)
	})

	// Double quote error
	t.Run("value-with-quote-error", func(t *testing.T) {
		yamlBytes, err := os.ReadFile("testdata/value-with-quote-error.yaml")
		require.NoError(t, err)

		cfg, err := config.LoadHarvesterConfig(yamlBytes)
		require.NoError(t, err)

		err = validate.Validate(cfg, validate.Options{AllowSecretsOnCmdline: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "contains double quote (\")")
	})

	// Join missing server_url error
	t.Run("join-missing-server-url", func(t *testing.T) {
		yamlBytes, err := os.ReadFile("testdata/join-missing-server-url.yaml")
		require.NoError(t, err)

		cfg, err := config.LoadHarvesterConfig(yamlBytes)
		require.NoError(t, err)

		err = validate.Validate(cfg, validate.Options{AllowSecretsOnCmdline: true})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "join mode requires 'server_url'")
	})
}

// 7.3 Regression tests for Python bugs
func TestRegression_PythonBugs(t *testing.T) {
	// Bug 1: No camelCase key ever appears in the output
	t.Run("no_camelCase_keys", func(t *testing.T) {
		fixtures, err := filepath.Glob("testdata/*.yaml")
		require.NoError(t, err)

		for _, f := range fixtures {
			if strings.Contains(f, "error") || strings.Contains(f, "overflow") || strings.Contains(f, "missing") {
				continue
			}
			yamlBytes, err := os.ReadFile(f)
			require.NoError(t, err)

			line, _ := generateHelper(t, yamlBytes, emit.Options{AllowSecretsOnCmdline: true})
			tokens := strings.Fields(line)
			for _, tok := range tokens {
				if strings.HasPrefix(tok, "harvester.") {
					key := strings.SplitN(tok, "=", 2)[0]
					assert.NotRegexp(t, `[a-z][A-Z]`, key, "key '%s' in fixture %s contains camelCase", key, f)
				}
			}
		}
	})

	// Bug 2: iso_url is absent when unset, never "local"
	t.Run("iso_url_absent_when_unset", func(t *testing.T) {
		yamlText := `
scheme_version: 1
token: "tok"
os:
  hostname: "node-01"
  password: "pass"
install:
  mode: create
  device: "/dev/sda"
  vip: "10.0.0.1"
  vip_mode: "static"
`
		line, _ := generateHelper(t, []byte(yamlText), emit.Options{AllowSecretsOnCmdline: true})
		assert.NotContains(t, line, "iso_url")
		assert.NotContains(t, line, "local")
	})

	// Bug 3: No DNS server appears inside an ip= parameter
	t.Run("no_dns_in_ip_param", func(t *testing.T) {
		yamlText := `
scheme_version: 1
token: "tok"
os:
  hostname: "test-node"
  password: "pass"
  dns_nameservers:
    - 1.1.1.1
    - 8.8.4.4
install:
  mode: create
  device: "/dev/sda"
  vip: "10.0.0.1"
  vip_mode: "static"
  management_interface:
    method: static
    ip: "192.168.1.50"
    subnet_mask: "255.255.255.0"
    gateway: "192.168.1.1"
    interfaces:
      - name: eth0
`
		line, _ := generateHelper(t, []byte(yamlText), emit.Options{AllowSecretsOnCmdline: true})
		tokens := strings.Fields(line)
		for _, tok := range tokens {
			if strings.HasPrefix(tok, "ip=") {
				assert.NotContains(t, tok, "1.1.1.1", "DNS server 1.1.1.1 found inside ip= parameter")
				assert.NotContains(t, tok, "8.8.4.4", "DNS server 8.8.4.4 found inside ip= parameter")
				assert.True(t, strings.HasSuffix(tok, ":none"), "static ip= parameter must end in :none")
			}
		}
		assert.Contains(t, tokens, "nameserver=1.1.1.1")
		assert.Contains(t, tokens, "nameserver=8.8.4.4")
	})

	// Bug 4: At most one ip= parameter
	t.Run("at_most_one_ip_param", func(t *testing.T) {
		yamlText := `
scheme_version: 1
token: "tok"
os:
  hostname: "test-node"
  password: "pass"
install:
  mode: create
  device: "/dev/sda"
  vip: "10.0.0.1"
  vip_mode: "static"
  management_interface:
    method: static
    ip: "192.168.1.50"
    subnet_mask: "255.255.255.0"
    gateway: "192.168.1.1"
    interfaces:
      - name: eth0
`
		line, _ := generateHelper(t, []byte(yamlText), emit.Options{AllowSecretsOnCmdline: true})
		tokens := strings.Fields(line)
		ipCount := 0
		for _, tok := range tokens {
			if strings.HasPrefix(tok, "ip=") {
				ipCount++
			}
		}
		assert.Equal(t, 1, ipCount, "expected exactly 1 ip= parameter")
	})

	// Bug 5: A '#' inside a quoted YAML value survives into output intact
	t.Run("hash_inside_quoted_value_survives", func(t *testing.T) {
		yamlText := `
scheme_version: 1
token: "tok#123#xyz"
os:
  hostname: "test-node"
  password: "p@ss#word#hash"
install:
  mode: create
  device: "/dev/sda"
  vip: "10.0.0.1"
  vip_mode: "static"
`
		line, _ := generateHelper(t, []byte(yamlText), emit.Options{AllowSecretsOnCmdline: true})
		assert.Contains(t, line, "harvester.token=tok#123#xyz")
		assert.Contains(t, line, "harvester.os.password=p@ss#word#hash")
	})

	// Bug 6: A token with a leading zero is not coerced to an integer
	t.Run("token_with_leading_zero_not_coerced", func(t *testing.T) {
		yamlText := `
scheme_version: 1
token: "0123456"
os:
  hostname: "test-node"
  password: "pass"
install:
  mode: create
  device: "/dev/sda"
  vip: "10.0.0.1"
  vip_mode: "static"
`
		line, _ := generateHelper(t, []byte(yamlText), emit.Options{AllowSecretsOnCmdline: true})
		assert.Contains(t, line, "harvester.token=0123456")
	})
}
