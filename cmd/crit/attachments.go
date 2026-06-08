package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxAttachmentBytes is the upper bound on a single pasted image after decode.
// 5 MiB comfortably covers full-screen screenshots while keeping the share
// payload (base64-inlined attachments + JSON) within crit-web's request limits.
const maxAttachmentBytes = 5 << 20

// attachmentExtByMIME maps the MIME types we accept to the file extension
// used on disk and in the URL. Anything outside this map is rejected at
// upload time.
var attachmentExtByMIME = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

// attachmentMIMEByExt is the reverse lookup used when serving and when
// inlining data URIs. Built from attachmentExtByMIME so the two stay in sync.
var attachmentMIMEByExt = func() map[string]string {
	m := make(map[string]string, len(attachmentExtByMIME))
	for mime, ext := range attachmentExtByMIME {
		m[ext] = mime
	}
	// Both jpg and jpeg are accepted on the URL side; map jpeg explicitly.
	m["jpeg"] = "image/jpeg"
	return m
}()

// attachmentFilenameRE matches our on-disk filenames: a UUIDv4 plus an
// allowed extension. UUIDs guarantee no collisions across the lifetime of
// a review folder; the strict pattern doubles as path-traversal protection
// (no "..", no "/" can pass).
var attachmentFilenameRE = regexp.MustCompile(
	`^([0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12})\.(png|jpe?g|gif|webp)$`,
)

// attachmentMarkdownRE finds markdown image references that point at our
// attachments dir using the canonical relative path stored in review.json.
// The filename group is validated separately via attachmentFilenameRE so
// the regex itself can stay permissive on the suffix.
//
// Matches: ![alt text](attachments/<file>)
//
//	![alt text](attachments/<file> "title")
//
// Not matched: ![](https://...), ![](/api/...), ![](./...) — anything that
// isn't a bare relative path under attachments/ is left alone so external
// images and historical absolute URLs survive the rewriters intact.
var attachmentMarkdownRE = regexp.MustCompile(
	`!\[([^\]]*)\]\(attachments/([A-Za-z0-9._-]+)(?:\s+"[^"]*")?\)`,
)

// randomUUID returns a UUIDv4 in canonical 8-4-4-4-12 hex form. crypto/rand
// is the only randomness source — no fallback to time-based IDs even on
// rand failure (which only happens if the kernel can't seed, in which case
// we want to refuse to write anything).
func randomUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("read random bytes for uuid: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// saveAttachment writes data into the review's attachments dir under a
// fresh UUIDv4 name. It detects MIME via http.DetectContentType and rejects
// anything outside attachmentExtByMIME. Returns the bare filename (no path)
// that the frontend should use in the markdown image URL.
//
// Each successful call writes a new file even for byte-identical pastes —
// the reviewer's feedback explicitly chose UUIDs over content-addressing
// to avoid worrying about hash collisions and to keep the original-name
// alt text decoupled from on-disk identity.
func saveAttachment(reviewPath string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("empty attachment payload")
	}
	if len(data) > maxAttachmentBytes {
		return "", fmt.Errorf("attachment too large: %d bytes (max %d)", len(data), maxAttachmentBytes)
	}

	mime := http.DetectContentType(data)
	// DetectContentType may return "image/jpeg; charset=utf-8" — strip params.
	if idx := strings.Index(mime, ";"); idx >= 0 {
		mime = strings.TrimSpace(mime[:idx])
	}
	ext, ok := attachmentExtByMIME[mime]
	if !ok {
		return "", fmt.Errorf("unsupported image type: %s", mime)
	}

	if reviewPath == "" {
		return "", errors.New("no review path; cannot store attachment")
	}
	dir := reviewPathsFor(reviewPath).Attachments

	uuid, err := randomUUID()
	if err != nil {
		return "", err
	}
	filename := uuid + "." + ext
	target := filepath.Join(dir, filename)

	if err := atomicWriteFile(target, data, 0o600); err != nil {
		return "", fmt.Errorf("write attachment: %w", err)
	}
	return filename, nil
}

// attachmentPathFor resolves a filename to an absolute path inside the
// attachments dir, returning the path and detected MIME on success.
// Filename is validated against attachmentFilenameRE so traversal ("..",
// "/") is impossible.
func attachmentPathFor(reviewPath, filename string) (path, mime string, err error) {
	m := attachmentFilenameRE.FindStringSubmatch(filename)
	if m == nil {
		return "", "", errors.New("invalid attachment filename")
	}
	if reviewPath == "" {
		return "", "", errors.New("no review path")
	}
	mime, ok := attachmentMIMEByExt[strings.ToLower(m[2])]
	if !ok {
		return "", "", errors.New("unknown attachment extension")
	}
	return filepath.Join(reviewPathsFor(reviewPath).Attachments, filename), mime, nil
}

// inlineAttachmentsAsDataURIs rewrites local attachments/<file> markdown
// references in body to data: URIs by reading and base64-encoding each
// referenced file. External and absolute URLs are untouched. Missing or
// oversized files are left as-is (and will simply 404 on the shared viewer
// rather than blowing up the share request).
//
// Called from the share path so that shared reviews carry the actual image
// bytes — crit-web has no asset endpoint of its own.
func inlineAttachmentsAsDataURIs(reviewPath, body string) string {
	if reviewPath == "" || !strings.Contains(body, "attachments/") {
		return body
	}
	return attachmentMarkdownRE.ReplaceAllStringFunc(body, func(match string) string {
		sub := attachmentMarkdownRE.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		alt, filename := sub[1], sub[2]
		path, mime, err := attachmentPathFor(reviewPath, filename)
		if err != nil {
			return match
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return match
		}
		// Cap inlined size defensively — a misbehaving caller couldn't
		// have got past saveAttachment's check, but belt + braces.
		if len(data) > maxAttachmentBytes {
			return match
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		return fmt.Sprintf("![%s](data:%s;base64,%s)", alt, mime, encoded)
	})
}

// stripAttachmentReferences removes local attachments/<file> markdown
// image refs from body and returns the rewritten body plus the number
// stripped. External and absolute URLs are left intact. Used on the GitHub
// push path: pasted images live only inside the local crit review folder,
// so the GitHub-rendered comment substitutes a "view in Crit" notice.
func stripAttachmentReferences(body string) (string, int) {
	if !strings.Contains(body, "attachments/") {
		return body, 0
	}
	count := 0
	out := attachmentMarkdownRE.ReplaceAllStringFunc(body, func(match string) string {
		count++
		return ""
	})
	if count == 0 {
		return body, 0
	}
	// Collapse the blank line(s) the deletion may have left behind so the
	// remaining text reads naturally on GitHub.
	out = strings.TrimRight(out, " \n")
	noun := "image"
	if count != 1 {
		noun = "images"
	}
	out += fmt.Sprintf("\n\n_[%d %s removed — view in Crit]_", count, noun)
	return out, count
}

// bodyRewriter processes a comment body before it leaves crit en route to
// GitHub. nil is interpreted as "leave the body alone"; this is convenient
// for unit tests that don't care about attachment handling.
type bodyRewriter func(body string) string

// stripBodyRewriter removes local attachment refs from a body and appends
// a "view in Crit" notice. Pasted images live only inside the local review
// folder; the push path is the only outbound boundary that doesn't carry
// the bytes (share-to-crit-web inlines as data URI; the GitHub comment
// gets a notice instead).
func stripBodyRewriter(body string) string {
	out, _ := stripAttachmentReferences(body)
	return out
}

// sanitizeAttachmentAltText turns an arbitrary upload filename into safe
// markdown alt text. Strips control characters, collapses whitespace, drops
// brackets and parentheses (which would terminate the markdown image
// syntax), and caps length. Empty result is allowed — callers fall back to
// a default when this returns "".
func sanitizeAttachmentAltText(name string) string {
	const maxLen = 120
	var b strings.Builder
	b.Grow(len(name))
	prevSpace := false
	for _, r := range name {
		switch {
		case r < 0x20, r == 0x7f:
			continue
		case r == '[', r == ']', r == '(', r == ')':
			continue
		case r == ' ', r == '\t':
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
		default:
			b.WriteRune(r)
			prevSpace = false
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > maxLen {
		out = strings.TrimSpace(out[:maxLen])
	}
	return out
}
