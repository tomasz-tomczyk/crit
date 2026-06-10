package share

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSharePayload_DoesNotIncludeRoundSnapshots(t *testing.T) {
	files := []ShareFile{{Path: "plan.md", Content: "current", Status: "modified"}}
	comments := []ShareComment{{File: "plan.md", Body: "x"}}

	payload := BuildSharePayload(files, comments, 2, []string{"plan.md"}, "", "", "")

	if _, ok := payload["round_snapshots"]; ok {
		t.Fatal("share payload contains round_snapshots key")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "round_snapshots") {
		t.Fatalf("share payload JSON contains round_snapshots:\n%s", data)
	}
}
