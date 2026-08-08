package main

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"
)

var publicCommandNames = []string{
	"share", "fetch", "unpublish", "install", "config", "check", "pr", "mr", "pull",
	"push", "comment", "comments", "review", "live", "preview", "plan", "story",
	"auth", "stop", "status", "stats", "cleanup",
}

func TestCommandRegistry_PublicInventory(t *testing.T) {
	got := visibleCommandNames()
	if !slices.Equal(got, publicCommandNames) {
		t.Fatalf("public commands = %v, want %v", got, publicCommandNames)
	}
}

func TestCommandRegistry_Invariants(t *testing.T) {
	seen := make(map[string]bool)
	for _, command := range commandRegistry {
		if seen[command.name] {
			t.Errorf("duplicate command %q", command.name)
		}
		seen[command.name] = true
		if command.handler == nil {
			t.Errorf("command %q has no handler", command.name)
		}
		if command.help == "" && command.helpFn == nil {
			t.Errorf("command %q has no help", command.name)
		}
		for _, subcommand := range command.subcommands {
			if subcommand.help == "" && subcommand.helpFn == nil {
				t.Errorf("subcommand %q %q has no help", command.name, subcommand.name)
			}
		}
	}
}

func TestCommandHelpFlagsDoNotInvokeHandlers(t *testing.T) {
	for _, name := range publicCommandNames {
		for _, flag := range []string{"--help", "-h"} {
			t.Run(name+"/"+flag, func(t *testing.T) {
				registry := append([]commandDescriptor(nil), commandRegistry...)
				invoked := false
				for i := range registry {
					if registry[i].name == name {
						registry[i].handler = func([]string) { invoked = true }
					}
				}

				out := captureStderr(t, func() {
					handled, err := dispatchWithRegistry([]string{name, flag}, registry)
					if err != nil {
						t.Fatal(err)
					}
					if !handled {
						t.Fatal("command was not dispatched")
					}
				})
				if invoked {
					t.Fatal("help invoked the command handler")
				}
				if !strings.Contains(out, "Usage: crit "+name) {
					t.Fatalf("command help missing scoped usage:\n%s", out)
				}
			})
		}
	}
}

func TestHelpCommand(t *testing.T) {
	for _, name := range publicCommandNames {
		t.Run(name, func(t *testing.T) {
			out := captureStderr(t, func() {
				handled, err := dispatchWithRegistry([]string{"help", name}, commandRegistry)
				if err != nil {
					t.Fatal(err)
				}
				if !handled {
					t.Fatal("help command was not dispatched")
				}
			})
			if !strings.Contains(out, "Usage: crit "+name) {
				t.Fatalf("help output missing command usage:\n%s", out)
			}
		})
	}
}

func TestNestedAuthHelpDoesNotInvokeHandler(t *testing.T) {
	for _, subcommand := range []string{"login", "logout", "whoami"} {
		t.Run("help/"+subcommand, func(t *testing.T) {
			out := captureStderr(t, func() {
				if _, err := dispatchWithRegistry([]string{"help", "auth", subcommand}, commandRegistry); err != nil {
					t.Fatal(err)
				}
			})
			if !strings.Contains(out, "Usage: crit auth "+subcommand) {
				t.Fatalf("nested help missing usage:\n%s", out)
			}
		})
		for _, flag := range []string{"--help", "-h"} {
			t.Run(subcommand+"/"+flag, func(t *testing.T) {
				registry := append([]commandDescriptor(nil), commandRegistry...)
				invoked := false
				for i := range registry {
					if registry[i].name == "auth" {
						registry[i].handler = func([]string) { invoked = true }
					}
				}
				out := captureStderr(t, func() {
					if _, err := dispatchWithRegistry([]string{"auth", subcommand, flag}, registry); err != nil {
						t.Fatal(err)
					}
				})
				if invoked {
					t.Fatal("nested auth help invoked the auth handler")
				}
				if !strings.Contains(out, "Usage: crit auth "+subcommand) {
					t.Fatalf("nested auth help missing usage:\n%s", out)
				}
			})
		}
	}
}

func TestRootHelpAliasesAndHiddenOmission(t *testing.T) {
	for _, alias := range []string{"help", "--help", "-h"} {
		t.Run(alias, func(t *testing.T) {
			out := captureStderr(t, func() {
				if _, err := dispatchWithRegistry([]string{alias}, commandRegistry); err != nil {
					t.Fatal(err)
				}
			})
			if !strings.Contains(out, "crit — inline code review") {
				t.Fatalf("root help missing heading:\n%s", out)
			}
			for _, hidden := range []string{"plan-hook", "_serve"} {
				if strings.Contains(out, hidden) {
					t.Fatalf("root help unexpectedly lists hidden command %q:\n%s", hidden, out)
				}
			}
		})
	}
}

func TestHelpInterceptionOnlyUsesLeadingCommandArgument(t *testing.T) {
	for _, args := range [][]string{
		{"comment", "--", "--help"},
		{"comment", "--output", "--help"},
		{"story", "--story-file", "--help"},
	} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			registry := append([]commandDescriptor(nil), commandRegistry...)
			var received []string
			for i := range registry {
				if registry[i].name == args[0] {
					registry[i].handler = func(args []string) {
						received = append([]string(nil), args...)
					}
				}
			}
			if _, err := dispatchWithRegistry(args, registry); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(received, args[1:]) {
				t.Fatalf("handler args = %v, want %v", received, args[1:])
			}
		})
	}
}

func TestLegacyBareHelpUsesRegistryRenderer(t *testing.T) {
	for _, name := range []string{"config", "story"} {
		t.Run(name, func(t *testing.T) {
			flagHelp := captureStderr(t, func() {
				if _, err := dispatchWithRegistry([]string{name, "--help"}, commandRegistry); err != nil {
					t.Fatal(err)
				}
			})
			bareHelp := captureStderr(t, func() {
				if _, err := dispatchWithRegistry([]string{name, "help"}, commandRegistry); err != nil {
					t.Fatal(err)
				}
			})
			if bareHelp != flagHelp {
				t.Fatalf("bare and flag help differ:\n--- bare ---\n%s\n--- flag ---\n%s", bareHelp, flagHelp)
			}
		})
	}
}

func TestCommandHelpDocumentsKeyOptions(t *testing.T) {
	tests := map[string][]string{
		"install": {"--force", "all"},
		"comment": {"--reply-to", "--resolve", "--json", "--scope"},
		"plan":    {"[--name <slug>]"},
		"story":   {"--no-open", "--pr", "--range", "--scope", "--vcs"},
		"stop":    {"[file...]"},
	}
	for name, expected := range tests {
		t.Run(name, func(t *testing.T) {
			out := captureStderr(t, func() {
				if _, err := dispatchWithRegistry([]string{name, "--help"}, commandRegistry); err != nil {
					t.Fatal(err)
				}
			})
			for _, want := range expected {
				if !strings.Contains(out, want) {
					t.Errorf("help missing %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestInvalidHelpTopicReturnsError(t *testing.T) {
	for _, args := range [][]string{
		{"help", "unknown"},
		{"help", "auth", "unknown"},
		{"help", "auth", "login", "extra"},
	} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			handled, err := dispatchWithRegistry(args, commandRegistry)
			if !handled {
				t.Fatal("help command was not dispatched")
			}
			if err == nil || !strings.Contains(err.Error(), strings.Join(args[1:], " ")) {
				t.Fatalf("error = %v, want unknown topic containing %q", err, strings.Join(args[1:], " "))
			}
		})
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	var out strings.Builder
	done := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(&out, r)
		done <- copyErr
	}()
	writerClosed := false
	copyDone := false
	defer func() {
		os.Stderr = old
		if !writerClosed {
			_ = w.Close()
		}
		if !copyDone {
			<-done
		}
		_ = r.Close()
	}()

	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	writerClosed = true
	os.Stderr = old
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	copyDone = true
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
