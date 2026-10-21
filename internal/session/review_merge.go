package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// SaveCritJSONMerged saves mine, a review copy loaded as base before a long
// operation (such as a forge push), without losing what other writers saved
// in the meantime. Under the review write lock it reads the current file and
// merges three ways: comments and replies by ID, other fields by JSON key.
// A change only one side made wins; where both changed the same comment or
// field, mine wins and keeps the other side's new replies.
func SaveCritJSONMerged(critPath string, base, mine CritJSON) error {
	return WithReviewWriteLock(critPath, func() error {
		theirs := CritJSON{}
		data, err := ReadFileShared(ReviewPathsFor(critPath).Review)
		switch {
		case err == nil:
			if err := json.Unmarshal(data, &theirs); err != nil {
				return fmt.Errorf("invalid review file: %w", err)
			}
		case !os.IsNotExist(err):
			return err
		}
		merged, err := mergeCritJSON3(base, mine, theirs)
		if err != nil {
			return err
		}
		return saveCritJSONToDisk(critPath, merged)
	})
}

// CloneCritJSON returns a deep copy of cj through its JSON form, as a base
// for SaveCritJSONMerged.
func CloneCritJSON(cj CritJSON) CritJSON {
	var out CritJSON
	if data, err := json.Marshal(cj); err == nil {
		_ = json.Unmarshal(data, &out)
	}
	return out
}

// mergeCritJSON3 merges mine and theirs, two edits of base.
func mergeCritJSON3(base, mine, theirs CritJSON) (CritJSON, error) {
	fields := [3]map[string]json.RawMessage{}
	for i, cj := range []CritJSON{base, mine, theirs} {
		cj.Files, cj.ReviewComments = nil, nil
		data, err := json.Marshal(cj)
		if err != nil {
			return CritJSON{}, err
		}
		if err := json.Unmarshal(data, &fields[i]); err != nil {
			return CritJSON{}, err
		}
	}
	b, m, out := fields[0], fields[1], fields[2]
	for key := range unionKeys(b, m) {
		if bytes.Equal(m[key], b[key]) {
			continue
		}
		if v, ok := m[key]; ok {
			out[key] = v
		} else {
			delete(out, key)
		}
	}
	data, err := json.Marshal(out)
	if err != nil {
		return CritJSON{}, err
	}
	var merged CritJSON
	if err := json.Unmarshal(data, &merged); err != nil {
		return CritJSON{}, err
	}
	merged.ReviewComments = mergeComments3(base.ReviewComments, mine.ReviewComments, theirs.ReviewComments)
	merged.Files = mergeFiles3(base.Files, mine.Files, theirs.Files)
	return merged, nil
}

func unionKeys[V any](a, b map[string]V) map[string]struct{} {
	keys := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	return keys
}

// jsonEqual compares values by their JSON form, the form all three merge
// inputs share once saved.
func jsonEqual(a, b any) bool {
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(ja, jb)
}

func mergeFiles3(base, mine, theirs map[string]CritJSONFile) map[string]CritJSONFile {
	out := make(map[string]CritJSONFile)
	paths := unionKeys(base, mine)
	for path := range theirs {
		paths[path] = struct{}{}
	}
	for path := range paths {
		b, inBase := base[path]
		m, inMine := mine[path]
		t, inTheirs := theirs[path]
		switch {
		case inMine && inTheirs:
			f := t
			if f.Status == b.Status && f.FileHash == b.FileHash {
				f.Status, f.FileHash = m.Status, m.FileHash
			}
			f.Comments = mergeComments3(b.Comments, m.Comments, t.Comments)
			out[path] = f
		case inMine && (!inBase || !jsonEqual(m, b)):
			out[path] = m // added by mine, or deleted by theirs after mine changed it
		case inTheirs && (!inBase || !jsonEqual(t, b)):
			out[path] = t
		}
	}
	return out
}

// mergeComments3 merges two edits of base by comment ID, in mine's order with
// comments only theirs added appended.
func mergeComments3(base, mine, theirs []Comment) []Comment {
	baseByID := commentsByID(base)
	theirsByID := commentsByID(theirs)
	mineIDs := make(map[string]struct{}, len(mine))
	var out []Comment
	for _, m := range mine {
		mineIDs[m.ID] = struct{}{}
		b, inBase := baseByID[m.ID]
		t, inTheirs := theirsByID[m.ID]
		switch {
		case !inTheirs:
			// Kept unless theirs deleted a comment mine left unchanged.
			if !inBase || !jsonEqual(m, b) {
				out = append(out, m)
			}
		case inBase && jsonEqual(m, b):
			out = append(out, t)
		case inBase && jsonEqual(t, b):
			out = append(out, m)
		default:
			m.Replies = appendNewReplies(m.Replies, t.Replies, b.Replies)
			out = append(out, m)
		}
	}
	for _, t := range theirs {
		if _, inMine := mineIDs[t.ID]; inMine {
			continue
		}
		// Added by theirs, or edited by theirs after mine deleted it.
		if b, inBase := baseByID[t.ID]; !inBase || !jsonEqual(t, b) {
			out = append(out, t)
		}
	}
	return out
}

func commentsByID(comments []Comment) map[string]Comment {
	out := make(map[string]Comment, len(comments))
	for _, c := range comments {
		out[c.ID] = c
	}
	return out
}

// appendNewReplies adds replies theirs has that neither mine nor base has.
func appendNewReplies(mine, theirs, base []Reply) []Reply {
	known := make(map[string]struct{}, len(mine)+len(base))
	for _, r := range mine {
		known[r.ID] = struct{}{}
	}
	for _, r := range base {
		known[r.ID] = struct{}{}
	}
	out := append([]Reply(nil), mine...)
	for _, r := range theirs {
		if _, ok := known[r.ID]; !ok && r.ID != "" {
			out = append(out, r)
		}
	}
	return out
}
