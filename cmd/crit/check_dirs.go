package main

import (
	"fmt"
	"io"

	"github.com/tomasz-tomczyk/crit/internal/userdirs"
)

// printUserDirs tells `crit check` users where crit reads its config and keeps
// its state, and names any other state folder that still holds data: one left
// by a partial move to the XDG layout, or written by a process (an agent hook,
// say) that did not see the same XDG_STATE_HOME.
func printUserDirs(w io.Writer, home string) {
	fmt.Fprintln(w, "Files:")
	fmt.Fprintf(w, "  config: %s\n", userdirs.ConfigFile(home))
	dirs := userdirs.StateDirs(home)
	fmt.Fprintf(w, "  state:  %s\n", dirs[0])
	for _, dir := range dirs[1:] {
		fmt.Fprintf(w, "  also has crit data: %s\n", dir)
	}
	if len(dirs) > 1 {
		fmt.Fprintln(w, "    → crit resume and crit cleanup read every folder above; new reviews go to state.")
		fmt.Fprintln(w, "      If a hook or agent writes to the other folder, give it the same XDG_STATE_HOME as your shell.")
	}
	if userdirs.IsLegacyInstall(home) && userdirs.ConfigDir(home) != "" && userdirs.StateBaseDir(home) != "" {
		fmt.Fprintln(w, "  → run crit migrate-xdg to move the old ~/.crit files to the XDG folders.")
	}
}
