package github

import "github.com/tomasz-tomczyk/crit/internal/review"

func init() {
	review.FetchPRHeadInfoFn = func(prNumber int) (*review.PRHeadInfo, error) {
		info, err := FetchPRByNumber(prNumber)
		if err != nil || info == nil {
			return nil, err
		}
		return &review.PRHeadInfo{HeadRefName: info.HeadRefName}, nil
	}
}
