package draftpack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ComputeContentHash returns the sha256 hex digest of the pack body with
// ContentHash cleared (so the hash does not cover itself).
func ComputeContentHash(pack *Pack) (string, error) {
	if pack == nil {
		return "", fmt.Errorf("nil pack")
	}
	clone := *pack
	clone.ContentHash = ""
	raw, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// SealContentHash sets pack.ContentHash from the current body.
func SealContentHash(pack *Pack) error {
	h, err := ComputeContentHash(pack)
	if err != nil {
		return err
	}
	pack.ContentHash = h
	return nil
}

// VerifyContentHash checks a pack's ContentHash when present.
// Returns (ok, hasHash, error). ok is true when there is no hash or it matches.
func VerifyContentHash(pack *Pack) (ok bool, hasHash bool, err error) {
	if pack == nil {
		return false, false, fmt.Errorf("nil pack")
	}
	declared := strings.TrimSpace(pack.ContentHash)
	if declared == "" {
		return true, false, nil
	}
	actual, err := ComputeContentHash(pack)
	if err != nil {
		return false, true, err
	}
	return strings.EqualFold(declared, actual), true, nil
}
