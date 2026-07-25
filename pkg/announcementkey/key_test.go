package announcementkey

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLegacyIDPreservesExistingReadKey(t *testing.T) {
	publishDate := "2026-07-25T08:00:00Z"
	content := "维护公告"
	legacyKey := Legacy(publishDate, content)

	assert.Equal(t, legacyKey, FromID(LegacyID(publishDate, content)))
	assert.Equal(t, FromID("notice-1"), FromID(" notice-1 "))
}
