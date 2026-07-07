package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/hooks"
	"github.com/tomasz-tomczyk/crit/internal/prompt"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func (s *Server) promptTrustState() (prompt.TrustState, error) {
	_, projectPrompts := config.LoadPromptMaps(s.projectDir)
	_, projectHooks := config.LoadHookMaps(s.projectDir)
	return prompt.EvaluateTrust(s.projectDir, projectPrompts, projectHooks)
}

func (s *Server) buildPromptContext(sess *Session, approved bool, stats map[string]any) prompt.Context {
	mode := prompt.PromptMode(sess.ReviewType, sess.Mode)
	reviewPath := sess.CritJSONPath()
	quoted := shellQuoteArg(reviewPath)
	ctx := prompt.Context{
		ReviewPath:          reviewPath,
		CommentsCmd:         fmt.Sprintf("crit comments --json %s", quoted),
		CommentsAllCmd:      fmt.Sprintf("crit comments --json --all %s", quoted),
		NextRoundCmd:        session.NextRoundCommand(sess),
		SessionKey:          sess.SessionKey,
		Mode:                mode,
		UnresolvedCount:     sess.UnresolvedCommentCount(),
		TotalCount:          sess.TotalCommentCount(),
		FilesWithComments:   filesWithUnresolvedComments(sess),
		Approved:            approved,
		InternalSessionMode: sess.Mode,
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
	if stats != nil {
		ctx.SessionStats = &prompt.SessionStats{}
		if v, ok := stats["duration_seconds"].(int); ok {
			ctx.SessionStats.DurationSeconds = v
		} else if v, ok := stats["duration_seconds"].(float64); ok {
			ctx.SessionStats.DurationSeconds = int(v)
		}
		if v, ok := stats["files_reviewed"].(int); ok {
			ctx.SessionStats.FilesReviewed = v
		} else if v, ok := stats["files_reviewed"].(float64); ok {
			ctx.SessionStats.FilesReviewed = int(v)
		}
		if v, ok := stats["comments_submitted"].(int); ok {
			ctx.SessionStats.CommentsSubmitted = v
		} else if v, ok := stats["comments_submitted"].(float64); ok {
			ctx.SessionStats.CommentsSubmitted = int(v)
		}
	}
	return ctx
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

func (s *Server) renderFinishPrompts(sess *Session, approved bool, stats map[string]any) (promptStr string, meta *prompt.Meta) {
	globalPrompts, projectPrompts := config.LoadPromptMaps(s.projectDir)
	_, projectHooks := config.LoadHookMaps(s.projectDir)
	trust, err := prompt.EvaluateTrust(s.projectDir, projectPrompts, projectHooks)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: evaluating project prompt trust: %v\n", err)
	}
	ctx := s.buildPromptContext(sess, approved, stats)
	result := prompt.RenderFinish(globalPrompts, projectPrompts, s.projectDir, s.homeDir, trust.UseProject, ctx)
	return result.Prompt, result.Meta
}

func (s *Server) projectPromptsUntrusted() bool {
	trust, err := s.promptTrustState()
	if err != nil {
		return false
	}
	return trust.Untrusted
}

func (s *Server) projectPromptTrustPayload() map[string]any {
	trust, err := s.promptTrustState()
	if err != nil {
		return map[string]any{"project_prompts_untrusted": false}
	}
	out := map[string]any{
		"project_prompts_untrusted": trust.Untrusted,
	}
	if trust.HasProjectPrompts {
		out["project_prompt_sources"] = trust.Sources
		out["project_prompt_content_hash"] = trust.ContentHash
	}
	return out
}

func (s *Server) renderProjectPromptPreview(sess *Session) string {
	_, projectPrompts := config.LoadPromptMaps(s.projectDir)
	_, projectHooks := config.LoadHookMaps(s.projectDir)
	trust, err := prompt.EvaluateTrust(s.projectDir, projectPrompts, projectHooks)
	if err != nil || !trust.HasProjectPrompts {
		return ""
	}
	var sections []string
	for _, spec := range []struct {
		hook     string
		approved bool
	}{
		{prompt.HookFinishUnresolved, false},
		{prompt.HookFinishApproved, true},
	} {
		ctx := s.buildPromptContext(sess, spec.approved, nil)
		if !spec.approved && ctx.UnresolvedCount == 0 {
			ctx.UnresolvedCount = 1
		}
		result := prompt.RenderFinish(nil, projectPrompts, s.projectDir, s.homeDir, true, ctx)
		if result.Prompt == "" || result.Prompt == "Review finished." {
			continue
		}
		if result.Meta == nil || !strings.HasPrefix(result.Meta.TemplateSource, "project:") {
			continue
		}
		label := spec.hook
		if result.Meta.Hook != "" {
			label = result.Meta.Hook
		}
		sections = append(sections, "=== "+label+" ===\n"+result.Prompt)
	}
	return strings.Join(sections, "\n\n")
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
	trust, err := prompt.EvaluateTrust(s.projectDir, nil, projectHooks)
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
