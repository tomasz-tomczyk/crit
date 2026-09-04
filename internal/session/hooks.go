package session

import "fmt"

// CLIReviewConfig holds parsed review CLI flags for RunReview.
type CLIReviewConfig struct {
	Files              []string
	Focus              *Focus
	OutputDir          string
	PlanDir            string
	NoOpen             bool
	OpenCmd            string
	Quiet              bool
	NoIntegrationCheck bool
	VCSOverride        string
	BaseBranch         string
	IgnorePatterns     []string
	SessionID          string
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
	if sc.Focus.ChangeNumber > 0 {
		if sc.Focus.Forge == "gitlab" {
			return []string{fmt.Sprintf("mr:%d", sc.Focus.ChangeNumber)}
		}
		if sc.Focus.RemoteBaseProject != "" {
			return []string{fmt.Sprintf("pr:%s#%d", sc.Focus.RemoteBaseProject, sc.Focus.ChangeNumber)}
		}
		return []string{fmt.Sprintf("pr:%d", sc.Focus.ChangeNumber)}
	}
	return []string{fmt.Sprintf("range:%s..%s", sc.Focus.BaseSHA, sc.Focus.HeadSHA)}
}
