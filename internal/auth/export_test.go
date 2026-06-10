package auth

import "github.com/tomasz-tomczyk/crit/internal/config"

type Config = config.Config

var (
	runAuth                = RunAuth
	lazyBackfillAuthUserID = LazyBackfillAuthUserID
)
