package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tomasz-tomczyk/crit/internal/config"
	"github.com/tomasz-tomczyk/crit/internal/prompt"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func (s *Server) promptTrustState() (prompt.TrustState, error) {
	_, projectPrompts := config.LoadPromptMaps(s.projectDir)
	return prompt.EvaluateTrust(s.projectDir, projectPrompts)
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
	trust, err := prompt.EvaluateTrust(s.projectDir, projectPrompts)
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
	if len(projectPrompts) == 0 {
		return ""
	}
	mode := prompt.PromptMode(sess.ReviewType, sess.Mode)
	var sections []string
	for _, spec := range []struct {
		hook     string
		approved bool
	}{
		{prompt.HookFinishUnresolved, false},
		{prompt.HookFinishApproved, true},
	} {
		value, key := prompt.LookupPrompt(projectPrompts, spec.hook, mode)
		if value == "" || key == "" {
			continue
		}
		ctx := s.buildPromptContext(sess, spec.approved, nil)
		if !spec.approved && ctx.UnresolvedCount == 0 {
			ctx.UnresolvedCount = 1
		}
		result := prompt.RenderFinish(nil, projectPrompts, s.projectDir, s.homeDir, true, ctx)
		sections = append(sections, "=== "+key+" ===\n"+result.Prompt)
	}
	return strings.Join(sections, "\n\n")
}
