package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ErrReviewRemoved is returned by SaveCritJSONMerged when the review file was
// deleted (crit cleanup, an approval) while the caller held its copy.
var ErrReviewRemoved = errors.New("review file was removed; not saving")

// SaveCritJSONMerged saves mine, a review copy loaded as base before a long
// operation (such as a forge push), without losing what other writers saved
// in the meantime. Under the review write lock it reads the current file
// (theirs) and merges three ways: comments and replies by ID, comment and
// top-level fields by JSON key, pending remote deletes as a set. A change
// only one side made wins; where both changed the same field, mine wins. It
// returns what it saved, which the caller uses as its next copy and base.
func SaveCritJSONMerged(critPath string, base, mine CritJSON) (CritJSON, error) {
	// Check first, so a removed review does not get a new folder for the lock.
	if _, err := os.Stat(ReviewPathsFor(critPath).Review); os.IsNotExist(err) {
		return CritJSON{}, ErrReviewRemoved
	}
	var merged CritJSON
	err := WithReviewWriteLock(critPath, func() error {
		data, err := ReadFileShared(ReviewPathsFor(critPath).Review)
		if os.IsNotExist(err) {
			return ErrReviewRemoved
		}
		if err != nil {
			return err
		}
		var theirs CritJSON
		if err := json.Unmarshal(data, &theirs); err != nil {
			return fmt.Errorf("invalid review file: %w", err)
		}
		if merged, err = mergeCritJSON3(base, mine, theirs); err != nil {
			return err
		}
		return saveCritJSONToDisk(critPath, merged)
	})
	return merged, err
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
	strip := func(cj CritJSON) CritJSON {
		cj.Files, cj.ReviewComments, cj.PendingRemoteDeletes = nil, nil, nil
		return cj
	}
	var merged CritJSON
	if err := mergeFields3(strip(base), strip(mine), strip(theirs), &merged); err != nil {
		return CritJSON{}, err
	}
	var err error
	if merged.ReviewComments, err = mergeComments3(base.ReviewComments, mine.ReviewComments, theirs.ReviewComments); err != nil {
		return CritJSON{}, err
	}
	if merged.Files, err = mergeFiles3(base.Files, mine.Files, theirs.Files); err != nil {
		return CritJSON{}, err
	}
	merged.PendingRemoteDeletes = mergeRefSet3(base.PendingRemoteDeletes, mine.PendingRemoteDeletes, theirs.PendingRemoteDeletes)
	return merged, nil
}

// mergeFields3 merges three values of one struct type by JSON key into out:
// theirs, with each key mine changed from base taken from mine.
func mergeFields3[T any](base, mine, theirs T, out *T) error {
	var fields [3]map[string]json.RawMessage
	for i, v := range []T{base, mine, theirs} {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &fields[i]); err != nil {
			return err
		}
	}
	b, m, result := fields[0], fields[1], fields[2]
	for key := range unionKeys(b, m) {
		if bytes.Equal(m[key], b[key]) {
			continue
		}
		if v, ok := m[key]; ok {
			result[key] = v
		} else {
			delete(result, key)
		}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
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

func mergeFiles3(base, mine, theirs map[string]CritJSONFile) (map[string]CritJSONFile, error) {
	out := make(map[string]CritJSONFile)
	paths := unionKeys(base, mine)
	for path := range theirs {
		paths[path] = struct{}{}
	}
	for path := range paths {
		b := base[path]
		m, inMine := mine[path]
		t, inTheirs := theirs[path]
		comments, err := mergeComments3(b.Comments, m.Comments, t.Comments)
		if err != nil {
			return nil, err
		}
		if len(comments) == 0 {
			continue
		}
		f := m
		if !inMine {
			f = t
		} else if inTheirs {
			f = t
			if m.Status != b.Status {
				f.Status = m.Status
			}
			if m.FileHash != b.FileHash {
				f.FileHash = m.FileHash
			}
		}
		f.Comments = comments
		out[path] = f
	}
	return out, nil
}

// mergeComments3 merges comments by ID, and each comment present on all
// sides field by field with its replies merged by ID.
func mergeComments3(base, mine, theirs []Comment) ([]Comment, error) {
	var mergeErr error
	out := merge3ByID(base, mine, theirs, func(c Comment) string { return c.ID },
		func(b, m, t Comment) Comment {
			var c Comment
			if err := mergeFields3(b, m, t, &c); err != nil {
				mergeErr = err
				return m
			}
			c.Replies = merge3ByID(b.Replies, m.Replies, t.Replies, func(r Reply) string { return r.ID },
				func(b, m, t Reply) Reply {
					var r Reply
					if err := mergeFields3(b, m, t, &r); err != nil {
						mergeErr = err
						return m
					}
					return r
				})
			return c
		})
	return out, mergeErr
}

// merge3ByID merges two edits of base, matching items by ID. An item only
// one side changed, added or deleted takes that side's version; an item both
// sides changed goes through both. Order is mine's, then items only theirs
// added. Items without an ID (only in old review files; crit assigns IDs)
// cannot be matched: mine's are kept, plus theirs' that neither base nor
// mine has, so an edit theirs made to one shows up as a second copy.
func merge3ByID[T any](base, mine, theirs []T, id func(T) string, both func(b, m, t T) T) []T {
	baseByID := indexByID(base, id)
	theirsByID := indexByID(theirs, id)
	seen := make(map[string]struct{}, len(mine))
	var out []T
	for _, m := range mine {
		key := id(m)
		if _, dup := seen[key]; dup || key == "" {
			out = append(out, m)
			continue
		}
		seen[key] = struct{}{}
		b, inBase := baseByID[key]
		t, inTheirs := theirsByID[key]
		if v, keep := mergeItem3(b, inBase, m, t, inTheirs, both); keep {
			out = append(out, v)
		}
	}
	return append(out, theirsOnly(base, mine, theirs, baseByID, seen, id)...)
}

// mergeItem3 merges one item mine has. keep is false when theirs deleted an
// item mine left unchanged.
func mergeItem3[T any](b T, inBase bool, m, t T, inTheirs bool, both func(b, m, t T) T) (v T, keep bool) {
	switch {
	case !inTheirs:
		return m, !inBase || !jsonEqual(m, b)
	case inBase && jsonEqual(m, b):
		return t, true
	case inBase && jsonEqual(t, b):
		return m, true
	default:
		return both(b, m, t), true
	}
}

// theirsOnly returns the items only theirs has that it added, or edited
// after mine deleted them. seen holds the IDs mine already placed.
func theirsOnly[T any](base, mine, theirs []T, baseByID map[string]T, seen map[string]struct{}, id func(T) string) []T {
	var out []T
	for _, t := range theirs {
		key := id(t)
		if key == "" {
			if !containsJSON(mine, t) && !containsJSON(base, t) {
				out = append(out, t)
			}
			continue
		}
		if _, placed := seen[key]; placed {
			continue
		}
		seen[key] = struct{}{}
		if b, inBase := baseByID[key]; !inBase || !jsonEqual(t, b) {
			out = append(out, t)
		}
	}
	return out
}

// indexByID maps each ID to its first item; later duplicates are ignored.
func indexByID[T any](items []T, id func(T) string) map[string]T {
	out := make(map[string]T, len(items))
	for _, item := range items {
		if _, ok := out[id(item)]; !ok {
			out[id(item)] = item
		}
	}
	return out
}

func containsJSON[T any](items []T, v T) bool {
	for _, item := range items {
		if jsonEqual(item, v) {
			return true
		}
	}
	return false
}

// mergeRefSet3 merges pending remote deletes as a set: theirs, minus the refs
// mine drained since base, plus the refs mine added.
func mergeRefSet3(base, mine, theirs []RemoteRef) []RemoteRef {
	inBase := refSet(base)
	inMine := refSet(mine)
	var out []RemoteRef
	added := make(map[RemoteRef]struct{})
	for _, ref := range theirs {
		_, wasBase := inBase[ref]
		_, stillMine := inMine[ref]
		if wasBase && !stillMine {
			continue
		}
		if _, dup := added[ref]; !dup {
			out = append(out, ref)
			added[ref] = struct{}{}
		}
	}
	for _, ref := range mine {
		if _, wasBase := inBase[ref]; wasBase {
			continue
		}
		if _, dup := added[ref]; !dup {
			out = append(out, ref)
			added[ref] = struct{}{}
		}
	}
	return out
}

func refSet(refs []RemoteRef) map[RemoteRef]struct{} {
	out := make(map[RemoteRef]struct{}, len(refs))
	for _, ref := range refs {
		out[ref] = struct{}{}
	}
	return out
}
