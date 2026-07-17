package operation_setting

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// CheckinBonusSetting controls the independent, same-day check-in bonus.
// MinAmount and MaxAmount use the same integer quota unit as the billing
// system, but the bonus is never added to users.quota.
type CheckinBonusSetting struct {
	Enabled   bool `json:"enabled"`
	MinAmount int  `json:"min_amount"`
	MaxAmount int  `json:"max_amount"`
}

var checkinBonusSetting = CheckinBonusSetting{
	Enabled:   false,
	MinAmount: 50000,  // 0.10 USD with the default quota scale
	MaxAmount: 500000, // 1.00 USD with the default quota scale
}

func init() {
	config.GlobalConfig.Register("checkin_bonus_setting", &checkinBonusSetting)
}

func GetCheckinBonusSetting() *CheckinBonusSetting {
	return &checkinBonusSetting
}

func ValidateCheckinBonusOption(key string, value string) error {
	prospective := checkinBonusSetting
	switch key {
	case "checkin_bonus_setting.enabled":
		enabled, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("签到赠金开关必须是布尔值")
		}
		prospective.Enabled = enabled
	case "checkin_bonus_setting.min_amount", "checkin_bonus_setting.max_amount":
		amount, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || amount < 0 || amount > common.MaxQuota {
			return fmt.Errorf("签到赠金额度必须是 0 到 %d 之间的整数", common.MaxQuota)
		}
		if key == "checkin_bonus_setting.min_amount" {
			prospective.MinAmount = amount
		} else {
			prospective.MaxAmount = amount
		}
	default:
		return nil
	}
	return ValidateCheckinBonusRange(prospective.MinAmount, prospective.MaxAmount)
}

func ValidateCheckinBonusRange(minAmount int, maxAmount int) error {
	if minAmount < 0 || maxAmount < minAmount || maxAmount > common.MaxQuota {
		return fmt.Errorf("最低签到赠金不能高于最高签到赠金，且金额必须在 0 到 %d 之间", common.MaxQuota)
	}
	return nil
}
