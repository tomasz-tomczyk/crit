package daemon

import "os"

func FindSessionForCWDBranch(cwd, branch string) (SessionEntry, string, int) {
	return findSessionForCWDBranch(cwd, branch)
}

func StopAllDaemonsForCWD(cwd string) {
	stopAllDaemonsForCWD(cwd)
}

func SessionsDir() (string, error) {
	return sessionsDir()
}

func CleanOrphanedSessions() {
	cleanOrphanedSessions()
}

func TerminationSignals() []os.Signal {
	return terminationSignals()
}

func TerminateProcess(proc *os.Process) error {
	return terminateProcess(proc)
}

func IsDaemonAlive(s SessionEntry) bool {
	return isDaemonAlive(s)
}
