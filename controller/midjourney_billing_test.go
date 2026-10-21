package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMidjourneyFailureRefundDatabaseMatrix(t *testing.T) {
	service.InitHttpClient()
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			dsn := os.Getenv("AUDIT_MYSQL_DSN")
			dialect := common.DatabaseTypeMySQL
			if kind == "postgres" {
				dsn, dialect = os.Getenv("AUDIT_POSTGRES_DSN"), common.DatabaseTypePostgreSQL
			} else if kind == "sqlite" {
				dsn, dialect = "", common.DatabaseTypeSQLite
			}
			if kind != "sqlite" && dsn == "" {
				t.Skip("disposable database DSN is not configured")
			}
			db, _ := newAuditTestDatabase(t, kind, dsn)
			useLogExportDatabases(t, db, db, dialect, dialect)
			previousBatch, previousExport := common.BatchUpdateEnabled, common.DataExportEnabled
			common.BatchUpdateEnabled, common.DataExportEnabled = false, false
			t.Cleanup(func() { common.BatchUpdateEnabled, common.DataExportEnabled = previousBatch, previousExport })
			require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}, &model.Midjourney{}, &model.CheckinBonus{}, &model.CheckinBonusUsage{}))

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `[{"id":"refund-task","status":"FAILURE","progress":"100%","failReason":"provider rejected task"}]`)
			}))
			t.Cleanup(server.Close)
			now := time.Now()
			user := model.User{Id: 1, Username: "mj-owner", AffCode: "mj-owner", Quota: 80, UsedQuota: 20, RequestCount: 7}
			token := model.Token{Id: 1, UserId: 1, Key: "mj-refund-fixture", RemainQuota: 80, UsedQuota: 20}
			bonus := model.CheckinBonus{UserId: 1, CheckinId: 1, Amount: 5, ExpireAt: now.Add(time.Hour).Unix(), Status: model.CheckinBonusStatusActive}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, db.Create(&token).Error)
			require.NoError(t, db.Create(&bonus).Error)
			require.NoError(t, db.Create(&[]model.Channel{{Id: 1, Name: "billed", UsedQuota: 20}, {Id: 2, Name: "origin", UsedQuota: 40, BaseURL: &server.URL}}).Error)
			require.NoError(t, db.Create(&model.CheckinBonusUsage{
				RequestId: "mj-refund", UserId: 1, BonusId: bonus.Id, ReservedAmount: 5, ConsumedAmount: 5,
				OriginalFundingSource: model.CheckinBonusOriginalFundingWallet, OriginalReservedAmount: 15, OriginalConsumedAmount: 15,
				TokenId: 1, TokenReservedAmount: 20, TokenConsumedAmount: 20, Status: model.CheckinBonusUsageStatusSettled,
			}).Error)
			task := model.Midjourney{UserId: 1, MjId: "refund-task", Action: "IMAGINE", ChannelId: 2, BillingChannelId: 1, Quota: 20,
				SubmitTime: now.UnixMilli(), Progress: "0%", Status: "SUBMITTED", BillingStatus: model.MidjourneyBillingStatusCharged,
				BillingRequestId: "mj-refund", BillingSource: service.BillingSourceWallet, CheckinBonusConsumed: 5, TokenId: 1}
			require.NoError(t, db.Create(&task).Error)

			assert.Equal(t, 1, runMidjourneyTaskUpdateOnce(context.Background(), nil).UnfinishedTasks)
			assert.Zero(t, runMidjourneyTaskUpdateOnce(context.Background(), nil).UnfinishedTasks)
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&token, token.Id).Error)
			require.NoError(t, db.First(&bonus, bonus.Id).Error)
			require.NoError(t, db.First(&task, task.Id).Error)
			assert.Equal(t, 95, user.Quota)
			assert.Zero(t, user.UsedQuota)
			assert.Equal(t, 7, user.RequestCount)
			assert.Equal(t, 100, token.RemainQuota)
			assert.Zero(t, token.UsedQuota)
			assert.Equal(t, 5, bonus.RemainingAmount)
			assert.Equal(t, model.MidjourneyBillingStatusRefunded, task.BillingStatus)
			var channels []model.Channel
			require.NoError(t, db.Order("id").Find(&channels).Error)
			require.Len(t, channels, 2)
			assert.Zero(t, channels[0].UsedQuota)
			assert.EqualValues(t, 40, channels[1].UsedQuota)
			var refunds int64
			require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&refunds).Error)
			assert.EqualValues(t, 1, refunds)
		})
	}
}
