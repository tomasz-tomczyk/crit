package preview

import (
	"os"
	"testing"
)

var (
	connectToPreviewDaemonForTest = connectToPreviewDaemon
	buildPreviewStartArgsForTest  = buildPreviewStartArgs
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
