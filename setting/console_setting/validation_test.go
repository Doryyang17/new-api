package console_setting

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/announcementkey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func announcementValidationPayload(title string, extra any) string {
	return fmt.Sprintf(
		`[{"title":%q,"content":"公告正文","publishDate":"2026-07-25T08:00:00+08:00","level":"normal","forceRead":false,"immediate":true,"extra":%v}]`,
		title,
		extra,
	)
}

func TestValidateAnnouncementsCountsUnicodeCharacters(t *testing.T) {
	validTitle := strings.Repeat("公", 120)
	require.NoError(t, ValidateConsoleSettings(
		announcementValidationPayload(validTitle, `"摘要"`),
		"Announcements",
	))

	tooLongTitle := strings.Repeat("告", 121)
	err := ValidateConsoleSettings(
		announcementValidationPayload(tooLongTitle, `"摘要"`),
		"Announcements",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "标题长度不能超过120字符")
}

func TestValidateAnnouncementsRejectsNonStringSummary(t *testing.T) {
	err := ValidateConsoleSettings(
		announcementValidationPayload("系统公告", "123"),
		"Announcements",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "摘要长度不能超过300字符")
}

func TestValidateAnnouncementsRejectsDuplicateReadKeys(t *testing.T) {
	legacyDate := "2026-07-25T08:00:00+08:00"
	legacyContent := "旧公告"
	tests := []struct {
		name    string
		payload string
	}{
		{
			name: "explicit id",
			payload: `[
				{"id":"notice-1","content":"第一条","publishDate":"2026-07-25T08:00:00+08:00"},
				{"id":"notice-1","content":"第二条","publishDate":"2026-07-25T09:00:00+08:00"}
			]`,
		},
		{
			name: "legacy signature",
			payload: `[
				{"content":"相同正文","publishDate":"2026-07-25T08:00:00+08:00"},
				{"content":"相同正文","publishDate":"2026-07-25T08:00:00+08:00"}
			]`,
		},
		{
			name: "legacy migration marker",
			payload: fmt.Sprintf(`[
				{"content":%q,"publishDate":%q},
				{"id":%q,"content":"另一条公告","publishDate":"2026-07-25T09:00:00+08:00"}
			]`, legacyContent, legacyDate, announcementkey.LegacyID(legacyDate, legacyContent)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateConsoleSettings(test.payload, "Announcements")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "重复的唯一标识")
		})
	}
}

func TestValidateAnnouncementsRejectsTypedFieldMismatches(t *testing.T) {
	tests := []struct {
		name        string
		field       string
		value       string
		wantMessage string
	}{
		{name: "type", field: "type", value: `123`, wantMessage: "类型值不合法"},
		{name: "category", field: "category", value: `123`, wantMessage: "分类配置不合法"},
		{name: "pinned", field: "pinned", value: `"true"`, wantMessage: "置顶配置不合法"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := fmt.Sprintf(
				`[{"content":"公告正文","publishDate":"2026-07-25T08:00:00+08:00",%q:%s}]`,
				test.field,
				test.value,
			)
			err := ValidateConsoleSettings(payload, "Announcements")
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantMessage)
		})
	}
}
