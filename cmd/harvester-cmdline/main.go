package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/harvester/harvester-installer/pkg/config"
	"gopkg.in/yaml.v3"

	"github.com/coulof/harvester-iso-remaster/internal/emit"
	"github.com/coulof/harvester-iso-remaster/internal/validate"
)

// Version constants
const (
	ToolVersion        = "v0.1.0"
	PinnedInstallerTag = "v1.8.2"
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [flags] <config.yaml>\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Flags:\n")
	fmt.Fprintf(os.Stderr, "  --mode create|join            override install.mode\n")
	fmt.Fprintf(os.Stderr, "  --format raw|grub|ipxe        output form (default raw: one space-joined line)\n")
	fmt.Fprintf(os.Stderr, "  --no-dracut                   omit ip=/nameserver=/vlan=/ifname=\n")
	fmt.Fprintf(os.Stderr, "  --max-len N                   size guard, default 2000\n")
	fmt.Fprintf(os.Stderr, "  --allow-secrets-on-cmdline    permit os.password / token (off by default)\n")
	fmt.Fprintf(os.Stderr, "  --validate-only               validate and exit, emit nothing\n")
	fmt.Fprintf(os.Stderr, "  --version                     print tool version and the pinned installer tag\n")
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("harvester-cmdline", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = usage

	var (
		mode         string
		format       string
		noDracut     bool
		maxLen       int
		allowSecrets bool
		validateOnly bool
		showVersion  bool
	)

	fs.StringVar(&mode, "mode", "", "override install.mode (create or join)")
	fs.StringVar(&format, "format", "raw", "output form (raw, grub, or ipxe)")
	fs.BoolVar(&noDracut, "no-dracut", false, "omit ip=/nameserver=/vlan=/ifname=")
	fs.IntVar(&maxLen, "max-len", emit.DefaultMaxCmdlineLen, "size guard, default 2000")
	fs.BoolVar(&allowSecrets, "allow-secrets-on-cmdline", false, "permit os.password / token (off by default)")
	fs.BoolVar(&validateOnly, "validate-only", false, "validate and exit, emit nothing")
	fs.BoolVar(&showVersion, "version", false, "print tool version and the pinned installer tag")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if showVersion {
		fmt.Printf("harvester-cmdline %s (pinned installer tag: %s)\n", ToolVersion, PinnedInstallerTag)
		return 0
	}

	// Positional arguments
	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "[-] Error: Missing required <config.yaml> argument.\n\n")
		usage()
		return 2
	}
	configFile := fs.Arg(0)

	// Validate mode option if passed
	if mode != "" && mode != config.ModeCreate && mode != config.ModeJoin {
		fmt.Fprintf(os.Stderr, "[-] Error: Invalid --mode '%s'. Must be 'create' or 'join'.\n", mode)
		return 2
	}

	// Validate format option
	switch strings.ToLower(format) {
	case "raw", "grub", "ipxe":
	default:
		fmt.Fprintf(os.Stderr, "[-] Error: Invalid --format '%s'. Must be 'raw', 'grub', or 'ipxe'.\n", format)
		return 2
	}

	// Read file
	yamlBytes, err := os.ReadFile(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Error reading configuration file '%s': %v\n", configFile, err)
		return 2
	}

	// Parse raw YAML into map to check for present fields
	rawYAML := make(map[string]any)
	if err := yaml.Unmarshal(yamlBytes, &rawYAML); err != nil {
		fmt.Fprintf(os.Stderr, "[-] Error parsing YAML in '%s': %v\n", configFile, err)
		return 1
	}

	// Load configuration using installer's own loader
	cfg, err := config.LoadHarvesterConfig(yamlBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Error loading Harvester configuration: %v\n", err)
		return 1
	}

	// Validate configuration
	valOpts := validate.Options{
		ModeOverride:          mode,
		AllowSecretsOnCmdline: allowSecrets,
	}
	if err := validate.Validate(cfg, valOpts); err != nil {
		fmt.Fprintf(os.Stderr, "[-] Validation failed: %v\n", err)
		return 1
	}

	if validateOnly {
		return 0
	}

	// Warn if emitting secrets
	if allowSecrets {
		hasSecrets := cfg.OS.Password != "" || cfg.Token != ""
		if hasSecrets {
			fmt.Fprintf(os.Stderr, "[!] Warning: Emitting plaintext secrets on kernel command line.\n"+
				"    /proc/cmdline is world-readable and captured in systemd/dmesg logs.\n"+
				"    For production, prefer config_url or an embedded ISO config file.\n")
		}
	}

	// Emit parameters
	emitOpts := emit.Options{
		ModeOverride:          mode,
		AllowSecretsOnCmdline: allowSecrets,
		NoDracut:              noDracut,
		MaxLen:                maxLen,
		Format:                format,
	}
	cmdline, err := emit.Emit(cfg, rawYAML, emitOpts)
	if err != nil {
		var sizeErr *emit.SizeLimitError
		if errors.As(err, &sizeErr) {
			fmt.Fprintf(os.Stderr, "[-] Error: %v", sizeErr)
			return 3
		}
		fmt.Fprintf(os.Stderr, "[-] Emission failed: %v\n", err)
		return 1
	}

	fmt.Println(cmdline)
	return 0
}
