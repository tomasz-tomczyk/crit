package main

//go:generate go run gen_integration_hashes.go

import (
	"crypto/sha256"
	"fmt"
)

// computeFileHash returns the hex-encoded SHA256 hash of data.
func computeFileHash(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h)
}
