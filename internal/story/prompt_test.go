package story

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	obj := `{"prologue":{"summary":"s"},"chapters":[]}`

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare object", obj, obj},
		{"json fence", "```json\n" + obj + "\n```", obj},
		{"bare fence", "```\n" + obj + "\n```", obj},
		{"uppercase fence tag", "```JSON\n" + obj + "\n```", obj},
		{"leading+trailing prose", "Here is the story:\n" + obj + "\nDone.", obj},
		{"fence with prose after close is a fence", "```json\n" + obj + "\n```\nthanks!", obj},
		{"prose then fence: brace substring wins", "Sure!\n" + obj, obj},
		{"whitespace padding", "\n\n  " + obj + "  \n", obj},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractJSON(tt.in)
			if got != tt.want {
				t.Fatalf("ExtractJSON(%q) = %q, want %q", tt.in, got, tt.want)
			}
			// The extracted candidate must be valid JSON.
			var v any
			if err := json.Unmarshal([]byte(got), &v); err != nil {
				t.Fatalf("extracted candidate is not valid JSON: %v", err)
			}
		})
	}
}

// TestExtractJSON_FenceAndStrayProse is the self-review case: a response with
// BOTH a fence and stray prose around it must still extract the inner JSON.
func TestExtractJSON_FenceAndStrayProse(t *testing.T) {
	obj := `{"prologue":{"summary":"ok"}}`
	in := "Let me think...\n\n```json\n" + obj + "\n```\n\nHope that helps!"
	got := ExtractJSON(in)
	if got != obj {
		t.Fatalf("ExtractJSON with fence+prose = %q, want %q", got, obj)
	}
}

func TestBuildStoryPrompt(t *testing.T) {
	guide := "Author a story.\n"
	schema := `{"prologue":{}}`

	first := BuildStoryPrompt(guide, schema, "")
	if !strings.Contains(first, "Author a story.") {
		t.Error("prompt missing guide text")
	}
	if !strings.Contains(first, schema) {
		t.Error("prompt missing schema")
	}
	if strings.Contains(first, "could not be parsed") {
		t.Error("first prompt must not carry retry feedback")
	}

	retry := BuildStoryPrompt(guide, schema, RetryFeedback(errForTest("bad token")))
	if !strings.Contains(retry, "bad token") {
		t.Error("retry prompt missing parse error")
	}
	if !strings.Contains(retry, "Output raw JSON only") {
		t.Error("retry prompt missing strict-output reminder")
	}
}

type errForTest string

func (e errForTest) Error() string { return string(e) }
