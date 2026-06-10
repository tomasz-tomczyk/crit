package daemon

import "github.com/tomasz-tomczyk/crit/internal/config"

type commonDaemonFlags = CommonDaemonFlags

var (
	atomicWriteFile         = config.AtomicWriteFile
	appendCommonDaemonFlags = AppendCommonDaemonFlags
)
