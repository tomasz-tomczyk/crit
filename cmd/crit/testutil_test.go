package main

import (
	"github.com/tomasz-tomczyk/crit/internal/vcs"
)

func resetDefaultBranchOnce() {
	vcs.ResetDefaultBranchOnceForTest()
}
