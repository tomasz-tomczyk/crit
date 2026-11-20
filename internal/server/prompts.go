package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/hooks"
	"github.com/tomasz-tomczyk/crit/internal/prompt"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func (s *Server) loadPromptConfig() (global, project map[string]string, trust prompt.TrustState) {
	global, project = config.LoadPromptMaps(s.projectDir)
	_, projectHooks := config.LoadHookMaps(s.projectDir)
	trust, err := prompt.EvaluateTrust(s.projectDir, project, projectHooks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: evaluating project prompt trust: %v\n", err)
	}
	return global, project, trust
}

func finishPromptMode(sess *Session) string {
	mode := prompt.PromptMode(sess.ReviewType, sess.Mode)
	if mode == "diff" && sess.GetStory() != nil {
		return "story"
	}
	return mode
}

func (s *Server) checkFinishPrompts(sess *Session) []prompt.Problem {
	global, project, trust := s.loadPromptConfig()
	return prompt.Check(global, project, s.projectDir, s.homeDir, finishPromptMode(sess), trust.UseProject)
}

// renderFinishPrompt renders the finish prompt for /api/finish. When it
// cannot, it writes the error response and returns ok=false: 403 if project
// prompts became untrusted, or 422 with the prompt problem so the review
// stays open.
func (s *Server) renderFinishPrompt(w http.ResponseWriter, sess *Session, approved bool, stats map[string]any) (prompt.FinishResult, bool) {
	ctx := s.buildPromptContext(sess, approved, stats)
	// Recheck the trust hash after flushing the review in case a project
	// prompt changed while the request was waiting for the write.
	globalPrompts, projectPrompts, trust := s.loadPromptConfig()
	if trust.Untrusted {
		http.Error(w, "project prompt trust required", http.StatusForbidden)
		return prompt.FinishResult{}, false
	}
	finish := prompt.RenderFinish(globalPrompts, projectPrompts, s.projectDir, s.homeDir, trust.UseProject, ctx)
	if finish.Problem != nil {
		writeJSONStatus(w, http.StatusUnprocessableEntity, finish.Problem)
		return prompt.FinishResult{}, false
	}
	return finish, true
}

func (s *Server) buildPromptContext(sess *Session, approved bool, stats map[string]any) prompt.Context {
	reviewPath := sess.CritJSONPath()
	quoted := shellQuoteArg(reviewPath)
	ctx := prompt.Context{
		ReviewPath:          reviewPath,
		CommentsCmd:         "crit comments --json " + quoted,
		CommentsAllCmd:      "crit comments --json --all " + quoted,
		NextRoundCmd:        session.NextRoundCommand(sess),
		SessionKey:          sess.SessionKey,
		Mode:                finishPromptMode(sess),
		UnresolvedCount:     sess.UnresolvedCommentCount(),
		TotalCount:          sess.TotalCommentCount(),
		FilesWithComments:   filesWithUnresolvedComments(sess),
		Approved:            approved,
		InternalSessionMode: sess.Mode,
		PlanHook:            sess.PlanHook(),
	}
	if sess.Mode == "plan" && sess.PlanDir != "" {
		ctx.PlanSlug = filepath.Base(sess.PlanDir)
	}
	unresolved := listUnresolvedComments(sess)
	if len(unresolved) > 0 {
		if b, err := json.Marshal(unresolved); err == nil {
			ctx.CommentsUnresolvedJSON = string(b)
		}
	}
	if all := listAllComments(sess); len(all) > 0 {
		if b, err := json.Marshal(all); err == nil {
			ctx.CommentsJSON = string(b)
		}
	}
	ctx.SessionStats = promptStats(stats)
	return ctx
}

func promptStats(stats map[string]any) *prompt.SessionStats {
	if stats == nil {
		return nil
	}
	return &prompt.SessionStats{
		DurationSeconds:   promptIntStat(stats, "duration_seconds"),
		FilesReviewed:     promptIntStat(stats, "files_reviewed"),
		CommentsSubmitted: promptIntStat(stats, "comments_submitted"),
	}
}

func promptIntStat(stats map[string]any, key string) int {
	switch v := stats[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func filesWithUnresolvedComments(sess *Session) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, f := range sess.Files {
		for _, c := range f.Comments {
			if !c.Resolved {
				if _, ok := seen[f.Path]; !ok {
					seen[f.Path] = struct{}{}
					out = append(out, f.Path)
				}
				break
			}
		}
	}
	return out
}

// runFinishHooks resolves and executes the configured command hook for the
// current finish state (on_finish_unresolved / on_finish_approved, mode-suffix
// resolved like prompt templates). Hooks run synchronously with the finish
// flow so the review file is already on disk when they read it; a hook timeout
// or non-zero exit is logged as a warning and never blocks finish.
func (s *Server) runFinishHooks(sess *Session, approved bool, stats map[string]any) {
	hook := prompt.HookForFinish(approved)
	mode := prompt.PromptMode(sess.ReviewType, sess.Mode)

	globalHooks, projectHooks := config.LoadHookMaps(s.projectDir)
	_, projectPrompts := config.LoadPromptMaps(s.projectDir)
	trust, err := prompt.EvaluateTrust(s.projectDir, projectPrompts, projectHooks)
	if err != nil {
		log.Printf("finish-hook: evaluating project trust: %v", err)
		return
	}
	ec, err := hooks.ResolveFinishCommand(globalHooks, projectHooks, s.projectDir, s.homeDir, hook, mode, trust.UseProject)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: resolving finish hook %s: %v\n", hook, err)
		return
	}
	if ec == nil {
		return
	}

	ctx := s.buildPromptContext(sess, approved, stats)
	in := hooks.Input{
		Stdin:   hooks.JSONPayload(ctx),
		Env:     hooks.EnvMap(ctx),
		Dir:     sess.RepoRoot,
		Timeout: s.hookTimeout(),
	}
	out, err := hooks.Run(s.effectiveCtx(), *ec, in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: finish hook %s failed: %v\n", ec.Hook, err)
		if out != nil && len(out.Stderr) > 0 {
			fmt.Fprintf(os.Stderr, "  hook stderr:\n%s\n", trimLog(out.Stderr))
		}
		return
	}
	log.Printf("finish-hook %s (%s): exit=%d stdout=%dB stderr=%dB",
		ec.Hook, ec.Source, out.ExitCode, len(out.Stdout), len(out.Stderr))
	if len(out.Stderr) > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", trimLog(out.Stderr))
	}
}

func (s *Server) hookTimeout() time.Duration {
	// A hard cap so a hung hook cannot pin the daemon's finish flow. Generous
	// enough for snapshot/dataset-collection scripts, short enough that the
	// user isn't left staring at the finish UI.
	return 60 * time.Second
}

func trimLog(b []byte) string {
	s := strings.TrimRight(string(b), "\n")
	const max = 4 << 10 // 4 KiB
	if len(s) > max {
		return s[:max] + "\n...[truncated]"
	}
	return s
}
