package relay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAudioAndWssAccountingErrorsAreHandledAtCallers(t *testing.T) {
	cases := []struct {
		file string
		want string
	}{
		{"audio_handler.go", "if err := service.PostAudioConsumeQuota"},
		{"responses_handler.go", "if err := service.PostAudioConsumeQuota"},
		{"compatible_handler.go", "if err := service.PostAudioConsumeQuota"},
		{"websocket.go", "if err := service.PostWssConsumeQuota"},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			body, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			source := string(body)
			if !strings.Contains(source, tc.want) {
				t.Fatalf("%s must handle returned accounting errors with %q", tc.file, tc.want)
			}
			if !strings.Contains(source, "LogError") {
				t.Fatalf("%s must log returned accounting errors", tc.file)
			}
		})
	}
}

func TestMidjourneyAccountingErrorPersistsAuditAndMarksTaskReview(t *testing.T) {
	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldMainDBType := common.MainDatabaseType()
	oldLogDBType := common.LogDatabaseType()
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldRedisEnabled := common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.Midjourney{}))
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.LogConsumeEnabled = true
	common.RedisEnabled = false
	t.Cleanup(func() {
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.SetDatabaseTypes(oldMainDBType, oldLogDBType)
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.RedisEnabled = oldRedisEnabled
	})

	require.NoError(t, db.Create(&model.User{
		Id:       9401,
		Username: "mj-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	task := &model.Midjourney{
		UserId:     9401,
		MjId:       "mj_accounting_review",
		Action:     "IMAGINE",
		ChannelId:  9403,
		Quota:      100,
		Group:      "default",
		FailReason: "upstream queued",
	}
	require.NoError(t, db.Create(task).Error)

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", nil)
	ctx.Set("token_name", "mj-token")

	recordMidjourneyAccountingError(ctx, &relaycommon.RelayInfo{
		UserId:          9401,
		TokenId:         9402,
		OriginModelName: "midjourney",
		UsingGroup:      "default",
		StartTime:       time.Now(),
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 9403},
	}, task, "record midjourney consume log", errors.New("log sink down"))

	var log model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeError).First(&log).Error)
	require.Equal(t, 9401, log.UserId)
	require.Equal(t, "mj-owner", log.Username)
	require.Equal(t, 9402, log.TokenId)
	require.Contains(t, log.Content, "record midjourney consume log failed")
	require.Contains(t, log.Other, `"accounting_error":true`)

	var reloaded model.Midjourney
	require.NoError(t, db.First(&reloaded, task.Id).Error)
	require.Contains(t, reloaded.FailReason, service.TaskSettlementReviewFailReason)
	require.Contains(t, reloaded.FailReason, "record midjourney consume log failed")
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
}

func TestMidjourneyBillingRollbackClearsTaskQuotaAndKeepsReview(t *testing.T) {
	oldDB := model.DB
	oldMainDBType := common.MainDatabaseType()
	oldLogDBType := common.LogDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = oldDB
		common.SetDatabaseTypes(oldMainDBType, oldLogDBType)
	})
	task := &model.Midjourney{
		UserId:    9404,
		MjId:      "mj_billing_rolled_back",
		Action:    "IMAGINE",
		ChannelId: 9405,
		Quota:     100,
		Group:     "default",
		Status:    "SUCCESS",
		Progress:  "100%",
	}
	require.NoError(t, db.Create(task).Error)

	service.ClearMidjourneyQuotaAfterBillingRollback(context.Background(), task, errors.New("record consume log failed"))

	var reloaded model.Midjourney
	require.NoError(t, db.First(&reloaded, task.Id).Error)
	require.Zero(t, reloaded.Quota)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	require.Contains(t, reloaded.FailReason, service.TaskSettlementReviewFailReason)
}

func TestMidjourneyAccountingRollbackCallersClearTaskQuota(t *testing.T) {
	body, err := os.ReadFile("mjproxy_handler.go")
	require.NoError(t, err)
	source := string(body)

	require.GreaterOrEqual(t, strings.Count(source, "ClearMidjourneyQuotaAfterBillingRollback"), 2)
}

func TestMidjourneySettlementErrorsClearTaskQuotaForReview(t *testing.T) {
	body, err := os.ReadFile("mjproxy_handler.go")
	require.NoError(t, err)
	source := string(body)

	require.GreaterOrEqual(t, strings.Count(source, "ClearMidjourneyQuotaAfterBillingRollback(c, midjourneyTask, settlementErr)"), 2)
}
