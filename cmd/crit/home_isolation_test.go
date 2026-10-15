package main

import (
	"os"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestMain(m *testing.M) {
	cleanup := testutil.PrependFailingForgeCLIs()
	code := testutil.RunWithTempHome(m)
	cleanup()
	os.Exit(code)
}
