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
	positionalRouteChangeReview
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
	if _, ok := changeReviewArgs(args); ok {
		return positionalRouteChangeReview
	}
	if live.LooksLikeLiveArgs(args) {
		return positionalRouteLive
	}
	if preview.LooksLikePreviewArgs(args) {
		return positionalRoutePreview
	}
	return positionalRouteReview
}

// changeReviewArgs rewrites one GitHub PR or GitLab MR URL to explicit review argv.
func changeReviewArgs(args []string) ([]string, bool) {
	if len(args) != 1 {
		return nil, false
	}
	switch {
	case focus.LooksLikePRURL(args[0]):
		return []string{"--pr", args[0]}, true
	case focus.LooksLikeMRURL(args[0]):
		return []string{"--mr", args[0]}, true
	default:
		return nil, false
	}
}

// runPositionalCLI dispatches bare positional arguments from main.
func runPositionalCLI(args []string) {
	switch routePositionalArgs(args) {
	case positionalRouteChangeReview:
		changeArgs, _ := changeReviewArgs(args)
		clicmd.Exit(runReviewForPositionalCLI(changeArgs))
	case positionalRouteLive:
		runLiveForPositionalCLI(args)
	case positionalRoutePreview:
		runPreviewForPositionalCLI(args)
	default:
		clicmd.Exit(runReviewForPositionalCLI(args))
	}
}
