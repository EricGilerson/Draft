package draftpack

import (
	"crypto/rand"
	"encoding/hex"
)

func genNodeID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "svc-" + hex.EncodeToString(b[:])
}
