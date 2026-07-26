package service

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const probePenaltyBillingExpr = `len < 5000 ? tier("测活请求 · $0.2/次", 200000) : tier("normal", p * 0.3 + c * 0.5 + cr * 0.1)`

func TestTryTieredSettle_ProbePenaltyAndNormalPricing(t *testing.T) {
	t.Run("short input pays fixed probe penalty", func(t *testing.T) {
		info := makeRelayInfo(probePenaltyBillingExpr, 1, 100, 20)

		ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{
			P:   100,
			C:   20,
			Len: 100,
		})

		require.True(t, ok)
		require.NotNil(t, result)
		assert.Equal(t, 100_000, quota)
		assert.Equal(t, "测活请求 · $0.2/次", result.MatchedTier)
	})

	t.Run("normal input keeps token pricing", func(t *testing.T) {
		info := makeRelayInfo(probePenaltyBillingExpr, 1, 10_000, 1_000)

		ok, quota, result := TryTieredSettle(info, billingexpr.TokenParams{
			P:   9_000,
			C:   1_000,
			Len: 10_000,
			CR:  1_000,
		})

		require.True(t, ok)
		require.NotNil(t, result)
		assert.Equal(t, 1_650, quota)
		assert.Equal(t, "normal", result.MatchedTier)
	})
}
