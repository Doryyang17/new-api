package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateCheckinBonusOptionKeepsRangeValid(t *testing.T) {
	original := checkinBonusSetting
	t.Cleanup(func() { checkinBonusSetting = original })
	checkinBonusSetting.MinAmount = 10
	checkinBonusSetting.MaxAmount = 20

	require.Error(t, ValidateCheckinBonusOption("checkin_bonus_setting.min_amount", "21"))
	require.Error(t, ValidateCheckinBonusOption("checkin_bonus_setting.max_amount", "9"))
	require.NoError(t, ValidateCheckinBonusOption("checkin_bonus_setting.min_amount", "20"))
	require.NoError(t, ValidateCheckinBonusOption("checkin_bonus_setting.max_amount", "10"))
}
