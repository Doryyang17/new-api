package model

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	RegistrationCodeLength       = 20
	registrationCodeCharset      = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	registrationCodeBatchIdChars = 12
)

var ErrRegistrationCodeInvalid = errors.New("registration code invalid or used")

type RegistrationCode struct {
	Id           int    `json:"id"`
	Code         string `json:"code" gorm:"type:varchar(20);uniqueIndex"`
	Status       int    `json:"status" gorm:"type:int;default:1;index"`
	CreatedBy    int    `json:"created_by" gorm:"index"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint"`
	UsedUserId   int    `json:"used_user_id" gorm:"index"`
	UsedAt       int64  `json:"used_at" gorm:"bigint"`
	BatchId      string `json:"batch_id" gorm:"type:varchar(32);index"`
	Note         string `json:"note,omitempty" gorm:"type:varchar(255)"`
	UsedUsername string `json:"used_username,omitempty" gorm:"column:used_username;->;-:migration"`
}

func (RegistrationCode) TableName() string {
	return "registration_codes"
}

func NormalizeRegistrationCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func IsRegistrationCodeFormatValid(code string) bool {
	if len(code) != RegistrationCodeLength {
		return false
	}
	for _, ch := range code {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			continue
		}
		return false
	}
	return true
}

func randomRegistrationCode(length int) (string, error) {
	if length <= 0 {
		return "", nil
	}
	max := big.NewInt(int64(len(registrationCodeCharset)))
	code := make([]byte, length)
	for i := range code {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		code[i] = registrationCodeCharset[n.Int64()]
	}
	return string(code), nil
}

func GenerateRegistrationCodes(count int, createdBy int, note string) ([]string, error) {
	if count <= 0 {
		return nil, errors.New("registration code count must be positive")
	}
	note = strings.TrimSpace(note)
	if len(note) > 255 {
		return nil, errors.New("registration code note is too long")
	}

	batchId, err := randomRegistrationCode(registrationCodeBatchIdChars)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, count)
	now := common.GetTimestamp()
	err = DB.Transaction(func(tx *gorm.DB) error {
		for i := 0; i < count; i++ {
			code, err := randomRegistrationCode(RegistrationCodeLength)
			if err != nil {
				return err
			}
			registrationCode := RegistrationCode{
				Code:      code,
				Status:    common.RegistrationCodeStatusEnabled,
				CreatedBy: createdBy,
				CreatedAt: now,
				BatchId:   batchId,
				Note:      note,
			}
			if err := tx.Create(&registrationCode).Error; err != nil {
				return err
			}
			keys = append(keys, code)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func GetRegistrationCodes(status int, startIdx int, num int) (registrationCodes []*RegistrationCode, total int64, err error) {
	countQuery := DB.Model(&RegistrationCode{}).Where("status = ?", status)
	if err = countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	listQuery := DB.Model(&RegistrationCode{}).Where("registration_codes.status = ?", status)
	if status == common.RegistrationCodeStatusUsed {
		listQuery = listQuery.
			Select("registration_codes.*, users.username AS used_username").
			Joins("LEFT JOIN users ON users.id = registration_codes.used_user_id")
	}
	err = listQuery.
		Order("registration_codes.id desc").
		Limit(num).
		Offset(startIdx).
		Find(&registrationCodes).Error
	return registrationCodes, total, err
}

func IsRegistrationCodeAvailable(code string) (bool, error) {
	code = NormalizeRegistrationCode(code)
	if !IsRegistrationCodeFormatValid(code) {
		return false, nil
	}
	var count int64
	err := DB.Model(&RegistrationCode{}).
		Where("code = ? AND status = ?", code, common.RegistrationCodeStatusEnabled).
		Count(&count).Error
	return count > 0, err
}

func UseRegistrationCodeWithTx(tx *gorm.DB, code string, userId int) error {
	code = NormalizeRegistrationCode(code)
	if userId == 0 || !IsRegistrationCodeFormatValid(code) {
		return ErrRegistrationCodeInvalid
	}
	result := tx.Model(&RegistrationCode{}).
		Where("code = ? AND status = ?", code, common.RegistrationCodeStatusEnabled).
		Updates(map[string]any{
			"status":       common.RegistrationCodeStatusUsed,
			"used_user_id": userId,
			"used_at":      common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRegistrationCodeInvalid
	}
	return nil
}
