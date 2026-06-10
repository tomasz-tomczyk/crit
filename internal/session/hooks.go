package session

import "fmt"

// CLIReviewConfig holds parsed review CLI flags for RunReview.
type CLIReviewConfig struct {
	Files              []string
	Focus              *Focus
	PlanDir            string
	NoOpen             bool
	NoIntegrationCheck bool
	VCSOverride        string
	BaseBranch         string
	IgnorePatterns     []string
}

var (
	ResolveServerConfigFn func(args []string) (*CLIReviewConfig, error)
	PreflightCheckFn      func(sc *CLIReviewConfig) string
)

// FocusKeyArgs returns daemon session key file args for PR/range focus.
func FocusKeyArgs(sc *CLIReviewConfig) []string {
	if sc == nil || sc.Focus == nil || sc.Focus.Kind != FocusRange {
		if sc == nil {
			return nil
		}
		return sc.Files
	}
	if sc.Focus.PRNumber > 0 {
		return []string{fmt.Sprintf("pr:%d", sc.Focus.PRNumber)}
	}
	return []string{fmt.Sprintf("range:%s..%s", sc.Focus.BaseSHA, sc.Focus.HeadSHA)}
}
