package session

import "fmt"

// A sub-thread is a Comment with a ParentID: it comments on a passage of
// another message — a comment or a reply — and lives in the same list as its
// root thread (a file's comments or the review-level comments), so replies,
// resolution, rounds and the review-file merge treat it as any comment.

// ThreadMessageIDs returns the IDs of the messages a thread shows: the
// comment itself and each of its replies. A sub-thread can comment on any of
// them.
func ThreadMessageIDs(c Comment) []string {
	ids := make([]string, 0, len(c.Replies)+1)
	ids = append(ids, c.ID)
	for _, r := range c.Replies {
		ids = append(ids, r.ID)
	}
	return ids
}

// SubThreadIDs returns the IDs of every sub-thread of comments that descends
// from one of messageIDs, at any depth: the sub-threads on those messages,
// then the sub-threads on them or on their replies, and so on. comments is
// one list, from the session or from a loaded review file alike, since a
// sub-thread lives in the list of its root thread.
func SubThreadIDs(comments []Comment, messageIDs []string) map[string]struct{} {
	byParent := make(map[string][]int)
	for i, c := range comments {
		if c.ParentID != "" {
			byParent[c.ParentID] = append(byParent[c.ParentID], i)
		}
	}
	found := make(map[string]struct{})
	queue := append([]string(nil), messageIDs...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, i := range byParent[id] {
			sub := comments[i]
			if _, seen := found[sub.ID]; seen {
				continue
			}
			found[sub.ID] = struct{}{}
			queue = append(queue, ThreadMessageIDs(sub)...)
		}
	}
	return found
}

// findMessage locates the message id in comments: the index of the comment
// that is the message or holds it as a reply, with the message's author and
// body.
func findMessage(comments []Comment, id string) (owner int, author, body string, ok bool) {
	for i, c := range comments {
		if c.ID == id {
			return i, c.Author, c.Body, true
		}
		for _, r := range c.Replies {
			if r.ID == id {
				return i, r.Author, r.Body, true
			}
		}
	}
	return -1, "", "", false
}

// ThreadRoot returns the root thread of the message id: the comment holding
// it, then up the ParentID chain to a comment without a parent. A sub-thread
// on that message takes the root's scope, side and view tags (see
// AddSubThread). False when the chain leaves comments (or loops, in a
// hand-edited review file).
func ThreadRoot(comments []Comment, id string) (Comment, bool) {
	for range comments {
		i, _, _, ok := findMessage(comments, id)
		if !ok {
			return Comment{}, false
		}
		if comments[i].ParentID == "" {
			return comments[i], true
		}
		id = comments[i].ParentID
	}
	return Comment{}, false
}

// SubThreadsStayNotice returns the line, newline included, that `crit push`
// prints about the sub-threads of cj, which no forge can hold, or "" when cj
// has none.
func SubThreadsStayNotice(cj CritJSON) string {
	n := 0
	for _, f := range cj.Files {
		for _, c := range f.Comments {
			if c.ParentID != "" {
				n++
			}
		}
	}
	for _, c := range cj.ReviewComments {
		if c.ParentID != "" {
			n++
		}
	}
	switch n {
	case 0:
		return ""
	case 1:
		return "1 sub-thread stays in crit.\n"
	default:
		return fmt.Sprintf("%d sub-threads stay in crit.\n", n)
	}
}
