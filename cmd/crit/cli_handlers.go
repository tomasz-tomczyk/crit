package main

import (
	"github.com/tomasz-tomczyk/crit/internal/auth"
	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/comment"
	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/github"
	"github.com/tomasz-tomczyk/crit/internal/live"
	"github.com/tomasz-tomczyk/crit/internal/preview"
	"github.com/tomasz-tomczyk/crit/internal/review"
	"github.com/tomasz-tomczyk/crit/internal/session"
	"github.com/tomasz-tomczyk/crit/internal/share"
)

func runShare(args []string)     { clicmd.Exit(share.RunShare(args)) }
func runFetch(args []string)     { clicmd.Exit(share.RunFetch(args)) }
func runUnpublish(args []string) { clicmd.Exit(share.RunUnpublish(args)) }
func runConfig(args []string)    { clicmd.Exit(config.RunConfig(args)) }
func runPull(args []string)      { clicmd.Exit(github.RunPull(args)) }
func runPush(args []string)      { clicmd.Exit(github.RunPush(args)) }
func runComment(args []string)   { clicmd.Exit(comment.RunComment(args)) }
func runReview(args []string)    { clicmd.Exit(session.RunReview(args)) }
func runPlan(args []string)      { clicmd.Exit(session.RunPlan(args)) }
func runStop(args []string)      { clicmd.Exit(session.RunStop(args)) }
func runStatus(args []string)    { clicmd.Exit(session.RunStatus(args)) }
func runCleanup(args []string)   { clicmd.Exit(review.RunCleanup(args)) }
func runPR(args []string)        { clicmd.Exit(github.RunPR(args)) }

func runLive(args []string)    { live.RunLive(args) }
func runPreview(args []string) { preview.RunPreview(args) }
func runAuth(args []string)    { auth.RunAuth(args) }
func runStats(args []string)   { session.RunStats(args) }

func runPlanHook()      { clicmd.Exit(session.RunPlanHook()) }
func runCodexPlanHook() { clicmd.Exit(session.RunCodexPlanHook()) }
