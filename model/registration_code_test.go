package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRegistrationCodesIncludesUsedUsername(t *testing.T) {
	truncateTables(t)
	require.NoError(t, DB.AutoMigrate(&RegistrationCode{}))
	const testUserID = 42
	const testUsername = "regcode-test-user"

	require.NoError(t, DB.Exec("DELETE FROM registration_codes").Error)
	require.NoError(t, DB.Unscoped().Where("id = ? OR username = ?", testUserID, testUsername).Delete(&User{}).Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM registration_codes")
		DB.Unscoped().Where("id = ? OR username = ?", testUserID, testUsername).Delete(&User{})
	})

	user := &User{
		Id:       testUserID,
		Username: testUsername,
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(user).Error)
	require.NoError(t, DB.Create(&RegistrationCode{
		Code:       "ABCDEFGHIJKLMNOPQRST",
		Status:     common.RegistrationCodeStatusUsed,
		CreatedAt:  100,
		UsedUserId: user.Id,
		UsedAt:     200,
		BatchId:    "BATCH00000001",
	}).Error)

	codes, total, err := GetRegistrationCodes(
		common.RegistrationCodeStatusUsed,
		0,
		10,
	)

	require.NoError(t, err)
	require.Len(t, codes, 1)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, user.Id, codes[0].UsedUserId)
	assert.Equal(t, user.Username, codes[0].UsedUsername)
}
