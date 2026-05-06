package main

import (
	"fmt"
	"net/url"
	"os"
)

// looksLikeDesignArgs returns true when args is exactly one element
// that parses as an http:// or https:// URL.
func looksLikeDesignArgs(args []string) bool {
	if len(args) != 1 {
		return false
	}
	u, err := url.Parse(args[0])
	if err != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// runDesign is the entry point for `crit design <url>`.
func runDesign(args []string) {
	rawURL := ""
	for _, a := range args {
		if len(a) > 0 && a[0] != '-' {
			rawURL = a
			break
		}
	}
	if rawURL == "" {
		fmt.Fprintln(os.Stderr, "Usage: crit design <url>")
		os.Exit(1)
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		fmt.Fprintf(os.Stderr, "crit design: %q is not a valid http/https URL\n", rawURL)
		os.Exit(1)
	}
	origin := u.Scheme + "://" + u.Host
	fmt.Fprintf(os.Stderr, "[crit] design mode — origin: %s (Phase A stub)\n", origin)
	_ = origin
}
