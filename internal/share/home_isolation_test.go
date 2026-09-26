package share

import (
	"os"
	"testing"

	"github.com/tomasz-tomczyk/crit/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.RunWithTempHome(m)) }
