package api

import (
	"crypto/sha256"
	"encoding/hex"
)

// hashKey hashes a customer API key the same way the node does (SHA-256 hex).
//
// THREAT MODEL, stated accurately: the api_keys table holds only this hash, but
// the panel is NOT hash-only overall — subscriptions.api_key keeps the raw key
// because the public /sub/{token} page has to show the customer the credential
// their client needs. So a copy of panel.db, or one leaked /sub link, yields a
// working node credential. That is the deliberate cost of a self-service page;
// it is mitigated by high-entropy tokens (122-bit), no-store/no-referrer
// responses, and redacted request logs — not by hashing. Anyone tightening this
// should add key rotation rather than assume the raw key is already unreadable.
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
