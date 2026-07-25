package announcementkey

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const legacyIDPrefix = "legacy-"

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// Legacy returns the read key used by announcements that predate stable IDs.
func Legacy(publishDate string, content string) string {
	return digest("legacy:" + publishDate + "\x00" + content)
}

// LegacyID gives an ID-less announcement a durable identity without changing
// its existing read key.
func LegacyID(publishDate string, content string) string {
	return legacyIDPrefix + Legacy(publishDate, content)
}

// FromID returns the read key for an explicitly identified announcement.
func FromID(id string) string {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, legacyIDPrefix) {
		legacyKey := strings.TrimPrefix(id, legacyIDPrefix)
		if len(legacyKey) == sha256.Size*2 {
			if _, err := hex.DecodeString(legacyKey); err == nil {
				return strings.ToLower(legacyKey)
			}
		}
	}
	return digest("id:" + id)
}
