package main

import (
	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/focus"
	"github.com/tomasz-tomczyk/crit/internal/live"
	"github.com/tomasz-tomczyk/crit/internal/preview"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

type positionalRoute int

const (
	positionalRouteReview positionalRoute = iota
	positionalRoutePRReview
	positionalRouteLive
	positionalRoutePreview
)

var (
	runReviewForPositionalCLI  = session.RunReview
	runLiveForPositionalCLI    = runLive
	runPreviewForPositionalCLI = runPreview
)

// routePositionalArgs classifies bare positional crit arguments (no subcommand).
func routePositionalArgs(args []string) positionalRoute {
	if _, ok := prReviewArgs(args); ok {
		return positionalRoutePRReview
	}
	if live.LooksLikeLiveArgs(args) {
		return positionalRouteLive
	}
	if preview.LooksLikePreviewArgs(args) {
		return positionalRoutePreview
	}
	return positionalRouteReview
}

// prReviewArgs returns --pr argv when args is a single GitHub PR URL.
func prReviewArgs(args []string) ([]string, bool) {
	if len(args) != 1 || !focus.LooksLikePRURL(args[0]) {
		return nil, false
	}
	return []string{"--pr", args[0]}, true
}

// runPositionalCLI dispatches bare positional arguments from main.
func runPositionalCLI(args []string) {
	switch routePositionalArgs(args) {
	case positionalRoutePRReview:
		prArgs, _ := prReviewArgs(args)
		clicmd.Exit(runReviewForPositionalCLI(prArgs))
	case positionalRouteLive:
		runLiveForPositionalCLI(args)
	case positionalRoutePreview:
		runPreviewForPositionalCLI(args)
	default:
		clicmd.Exit(runReviewForPositionalCLI(args))
	}
}
