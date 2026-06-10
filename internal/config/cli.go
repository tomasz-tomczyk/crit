package config

import (
	"fmt"
	"os"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func RunConfig(args []string) error {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			printConfigHelp()
			return nil
		}
		if arg == "--generate" || arg == "-g" {
			fmt.Print(DefaultConfigString())
			return nil
		}
	}
	configDir := ""
	if vc := vcs.DetectVCS(""); vc != nil {
		configDir, _ = vc.RepoRoot()
	}
	if configDir == "" {
		var err error
		configDir, err = mustGetwd()
		if err != nil {
			return err
		}
	}
	cfg := LoadConfig(configDir)
	fmt.Print(cfg.String())
	return nil
}

func printConfigHelp() {
	fmt.Fprintf(os.Stderr, `crit config — show resolved configuration

Prints the merged configuration from global and project config files as JSON.
CLI flags and environment variables are not reflected in this output.

Usage:
  crit config              Show resolved config
  crit config --generate   Print a config template with all keys

`)
}

func mustGetwd() (string, error) {
	return clicmd.MustGetwd()
}
