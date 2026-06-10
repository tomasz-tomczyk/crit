package session

import "path/filepath"

// ReviewPaths derives the v4 folder layout from a review identity path.
type ReviewPaths struct {
	Folder      string
	Review      string
	Snapshots   string
	Attachments string
}

// ReviewPathsFor returns the v4 folder-form paths for a review identity.
func ReviewPathsFor(identity string) ReviewPaths {
	return ReviewPaths{
		Folder:      identity,
		Review:      filepath.Join(identity, "review.json"),
		Snapshots:   filepath.Join(identity, "snapshots.json"),
		Attachments: filepath.Join(identity, "attachments"),
	}
}
