package api

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashKey hashes a customer API key the same way the node does (SHA-256 hex), so
// the panel stores only the hash while the node independently verifies the raw
// key it was given at provisioning time.
func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func keyPrefix(key string) string {
	if len(key) <= 8 {
		return key
	}
	return key[:8]
}
