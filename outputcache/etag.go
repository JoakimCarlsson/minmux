package outputcache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// generateETag generates a strong ETag for the given response body.
func generateETag(body []byte) string {
	hash := sha256.Sum256(body)
	return fmt.Sprintf(`"%s"`, hex.EncodeToString(hash[:16]))
}

// shouldRevalidate reports whether the request's If-None-Match header
// indicates the cached entity is still fresh on the client.
func shouldRevalidate(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" || etag == "" {
		return false
	}
	return ifNoneMatch == etag || ifNoneMatch == "*"
}
