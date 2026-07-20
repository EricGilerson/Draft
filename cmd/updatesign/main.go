// updatesign signs an exact update manifest with the release-only Ed25519 key.
// The public half is compiled into Draft; the private half remains a GitHub
// Actions secret named DRAFT_UPDATER_SIGNING_PRIVATE_KEY.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	in := flag.String("in", "", "manifest path")
	out := flag.String("out", "", "signature path")
	flag.Parse()
	if *in == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: updatesign -in manifest -out signature")
		os.Exit(2)
	}
	encoded := strings.TrimSpace(os.Getenv("DRAFT_UPDATER_SIGNING_PRIVATE_KEY"))
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		fmt.Fprintln(os.Stderr, "DRAFT_UPDATER_SIGNING_PRIVATE_KEY must be a base64 Ed25519 private key")
		os.Exit(2)
	}
	data, err := os.ReadFile(*in)
	if err != nil {
		panic(err)
	}
	sig := ed25519.Sign(ed25519.PrivateKey(key), data)
	if err := os.WriteFile(*out, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o600); err != nil {
		panic(err)
	}
}
