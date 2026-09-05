package config

import (
	"fmt"
	"os"
)

func RunConfig(args []string) error {
	for _, arg := range args {
		if arg == "--generate" || arg == "-g" {
			fmt.Print(DefaultConfigString())
			return nil
		}
		if arg == "--migrate" {
			if err := MutateShareTargets(func(_ *[]ShareTarget) error { return nil }); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Migrated sharing configuration in %s\n", GlobalConfigPath())
			return nil
		}
	}
	cfg, err := LoadCurrentConfig()
	if err != nil {
		return err
	}
	fmt.Print(cfg.String())
	return nil
}

// PrintConfigHelp is the single help text for `crit config`. The command
// registry wires it as the command's helpFn, so `crit config --help`,
// `crit config help`, and `crit help config` all reach this one copy.
func PrintConfigHelp() {
	fmt.Fprint(os.Stderr, `Usage: crit config [--generate|-g|--migrate]

Show the configuration merged from ~/.crit.config.json and the project
.crit.config.json as JSON, or print a template with every key and its default
using --generate. CLI flags and environment variables are not reflected in the
resolved output.

Use --migrate to atomically convert legacy share_url/auth fields to
share_targets while preserving unknown configuration keys.

Keys worth knowing:
  output <dir>
      Crit data root for reviews. Reviews live in <dir>/reviews/<key>/, keyed
      per working directory and branch, the same layout as the default
      ~/.crit.

  plan_approve_mode <mode>
      Claude Code permission mode to switch to after a plan-hook approval, for
      example "acceptEdits". Global-only, so a repository cannot weaken your
      permission policy. Unset leaves Claude Code's behavior unchanged.

  notify_on_round_ready <bool>
      Send a desktop notification when a review round is ready for you.
      Opt-in: defaults to false.

  close_on_approve_after_ms <int>
      Auto-close the review tab this many milliseconds after an Approve.
      Global-only. Omit it, or use a negative value, to keep the tab open.

Run 'crit config --generate' for the full key list with default values.
`)
}
