package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionMapNormalizesLegacyZeroRateLimitDuration(t *testing.T) {
	const key = "ModelRequestRateLimitDurationMinutes"
	previousDuration := setting.ModelRequestRateLimitDurationMinutes
	common.OptionMapRWMutex.Lock()
	previousMapWasNil := common.OptionMap == nil
	if previousMapWasNil {
		common.OptionMap = make(map[string]string)
	}
	previousValue, previousValueFound := common.OptionMap[key]
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		setting.ModelRequestRateLimitDurationMinutes = previousDuration
		if previousValueFound {
			common.OptionMap[key] = previousValue
		} else {
			delete(common.OptionMap, key)
		}
		if previousMapWasNil {
			common.OptionMap = nil
		}
		common.OptionMapRWMutex.Unlock()
	})

	require.NoError(t, updateOptionMap(key, "0"))
	assert.Equal(t, 1, setting.ModelRequestRateLimitDurationMinutes)
	common.OptionMapRWMutex.RLock()
	assert.Equal(t, "1", common.OptionMap[key])
	common.OptionMapRWMutex.RUnlock()
}
