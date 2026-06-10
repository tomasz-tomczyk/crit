package github

import (
	"errors"
	"fmt"
	"os"

	"github.com/tomasz-tomczyk/crit/internal/clicmd"
	"github.com/tomasz-tomczyk/crit/internal/session"
)

func RunPR(args []string) error {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: crit pr <num|url>")
		return clicmd.ExitError{Code: 1, Err: errors.New("exit")}
	}
	return session.RunReview([]string{"--pr", args[0]})
}
