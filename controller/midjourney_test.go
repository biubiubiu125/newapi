package controller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaypkg "github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMidjourneyPollingTest(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.Token{},
		&model.TokenUsageDaily{},
		&model.Midjourney{},
		&model.MidjourneySettlementRecord{},
		&model.Log{},
		&model.QuotaData{},
		&model.UserSubscription{},
		&model.SubscriptionPlan{},
		&model.SubscriptionPreConsumeRecord{},
	))

	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = false
	service.InitHttpClient()
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})

	return db
}

func newMidjourneyPollingServer(t *testing.T, response dto.MidjourneyDto) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/mj/task/list-by-condition", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode([]dto.MidjourneyDto{response}))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGetAllMidjourneyReturnsCurrentUsername(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}))
	require.NoError(t, db.Create(&model.User{
		Id:       5201,
		Username: "mj-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:     5201,
		Action:     constant.MjActionImagine,
		MjId:       "mj-username",
		SubmitTime: time.Now().UnixMilli(),
		ChannelId:  4102,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/mj/?p=1&page_size=10", nil)

	GetAllMidjourney(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Items []struct {
				UserID   int    `json:"user_id"`
				Username string `json:"username"`
				MjID     string `json:"mj_id"`
			} `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	require.Len(t, response.Data.Items, 1)
	require.Equal(t, 5201, response.Data.Items[0].UserID)
	require.Equal(t, "mj-owner", response.Data.Items[0].Username)
	require.Equal(t, "mj-username", response.Data.Items[0].MjID)
}

func seedMidjourneyPollingChannel(t *testing.T, db *gorm.DB, id int, baseURL string) {
	t.Helper()
	require.NoError(t, db.Create(&model.Channel{
		Id:      id,
		Name:    "midjourney-test",
		Key:     "mj-secret",
		Type:    constant.ChannelTypeMidjourney,
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
	}).Error)
}

func TestRunMidjourneyTaskUpdateOnceRollsBackRefundWhenRefundLogFails(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	server := newMidjourneyPollingServer(t, dto.MidjourneyDto{
		MjId:       "mj-refund-log-fails",
		Status:     "FAILURE",
		Progress:   "100%",
		FailReason: "upstream failed",
		FinishTime: time.Now().UnixMilli(),
	})
	seedMidjourneyPollingChannel(t, db, 4101, server.URL)
	require.NoError(t, db.Create(&model.User{
		Id:       5101,
		Username: "legacy-mj-owner",
		Password: "password123",
		Quota:    100,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:     5101,
		Action:     constant.MjActionImagine,
		MjId:       "mj-refund-log-fails",
		Status:     "IN_PROGRESS",
		Progress:   "50%",
		ChannelId:  4101,
		Quota:      25,
		SubmitTime: time.Now().UnixMilli(),
	}).Error)

	oldLogDB := model.LOG_DB
	brokenLogDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.LOG_DB = brokenLogDB
	t.Cleanup(func() {
		model.LOG_DB = oldLogDB
		if sqlDB, err := brokenLogDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var user model.User
	require.NoError(t, db.Select("quota").First(&user, 5101).Error)
	require.Equal(t, 100, user.Quota)
}

func TestRunMidjourneyTaskUpdateOnceDoesNotWriteRefundLogWhenRefundCreditFails(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	server := newMidjourneyPollingServer(t, dto.MidjourneyDto{
		MjId:       "mj-refund-credit-fails",
		Status:     "FAILURE",
		Progress:   "100%",
		FailReason: "upstream failed",
		FinishTime: time.Now().UnixMilli(),
	})
	seedMidjourneyPollingChannel(t, db, 4102, server.URL)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:     5999,
		Action:     constant.MjActionImagine,
		MjId:       "mj-refund-credit-fails",
		Status:     "IN_PROGRESS",
		Progress:   "50%",
		ChannelId:  4102,
		Quota:      25,
		SubmitTime: time.Now().UnixMilli(),
	}).Error)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ? AND type = ?", 5999, model.LogTypeRefund).Count(&count).Error)
	require.Zero(t, count)
}

func TestRunMidjourneyTaskUpdateOnceRefundsTokenAndUsageCounters(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenUsageDaily{}, &model.Channel{}))
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	submitAt := time.Now().AddDate(0, 0, -1).Unix()
	submitDate := time.Unix(submitAt, 0).In(loc).Format("2006-01-02")
	todayDate := time.Now().In(loc).Format("2006-01-02")
	server := newMidjourneyPollingServer(t, dto.MidjourneyDto{
		MjId:       "mj-refund-accounting",
		Status:     "FAILURE",
		Progress:   "100%",
		FailReason: "upstream failed",
		FinishTime: time.Now().UnixMilli(),
	})
	seedMidjourneyPollingChannel(t, db, 4103, server.URL)
	require.NoError(t, db.Create(&model.User{
		Id:        5103,
		Username:  "legacy-mj-owner",
		Password:  "password123",
		Quota:     75,
		UsedQuota: 25,
		Status:    common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          6103,
		UserId:      5103,
		Key:         "mj-token",
		Name:        "mj-token",
		RemainQuota: 75,
		UsedQuota:   25,
		Status:      common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 4103).Update("used_quota", 25).Error)
	model.RecordTokenUsage(6103, 5103, 25, submitAt)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:     5103,
		Action:     constant.MjActionImagine,
		MjId:       "mj-refund-accounting",
		Status:     "IN_PROGRESS",
		Progress:   "50%",
		ChannelId:  4103,
		Quota:      25,
		TokenId:    6103,
		Group:      "default",
		SubmitTime: submitAt * 1000,
	}).Error)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var user model.User
	require.NoError(t, db.First(&user, 5103).Error)
	require.Equal(t, 100, user.Quota)
	require.Zero(t, user.UsedQuota)
	var token model.Token
	require.NoError(t, db.First(&token, 6103).Error)
	require.Equal(t, 100, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
	var channel model.Channel
	require.NoError(t, db.First(&channel, 4103).Error)
	require.Zero(t, channel.UsedQuota)
	var usage model.TokenUsageDaily
	require.NoError(t, db.Where("token_id = ? AND date = ?", 6103, submitDate).First(&usage).Error)
	require.Zero(t, usage.Quota)
	require.Equal(t, 1, usage.RequestCount)
	var todayUsageCount int64
	require.NoError(t, db.Model(&model.TokenUsageDaily{}).Where("token_id = ? AND date = ?", 6103, todayDate).Count(&todayUsageCount).Error)
	require.Zero(t, todayUsageCount)
	var refundLog model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 5103, model.LogTypeRefund).First(&refundLog).Error)
	require.Equal(t, 6103, refundLog.TokenId)
	require.Equal(t, "default", refundLog.Group)
	var refundedTask model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "mj-refund-accounting").First(&refundedTask).Error)
	require.Zero(t, refundedTask.Quota)
}

func TestRunMidjourneyTaskUpdateOnceRefundsWhenChannelCacheMissing(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenUsageDaily{}, &model.Channel{}))
	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})
	submitAt := time.Now().AddDate(0, 0, -1).Unix()
	require.NoError(t, db.Create(&model.User{
		Id:        5106,
		Username:  "cache-missing-owner",
		Password:  "password123",
		Quota:     75,
		UsedQuota: 25,
		Status:    common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          6106,
		UserId:      5106,
		Key:         "mj-cache-token",
		Name:        "mj-cache-token",
		RemainQuota: 75,
		UsedQuota:   25,
		Status:      common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:        4106,
		Name:      "mj-channel-cache-missing",
		Type:      constant.ChannelTypeMidjourney,
		Status:    common.ChannelStatusEnabled,
		UsedQuota: 25,
	}).Error)
	model.RecordTokenUsage(6106, 5106, 25, submitAt)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:     5106,
		Action:     constant.MjActionImagine,
		MjId:       "mj-cache-missing",
		Status:     "IN_PROGRESS",
		Progress:   "50%",
		ChannelId:  4106,
		Quota:      25,
		TokenId:    6106,
		Group:      "default",
		SubmitTime: submitAt * 1000,
	}).Error)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var user model.User
	require.NoError(t, db.First(&user, 5106).Error)
	require.Equal(t, 100, user.Quota)
	require.Zero(t, user.UsedQuota)
	var token model.Token
	require.NoError(t, db.First(&token, 6106).Error)
	require.Equal(t, 100, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
	var channel model.Channel
	require.NoError(t, db.First(&channel, 4106).Error)
	require.Zero(t, channel.UsedQuota)
	var task model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "mj-cache-missing").First(&task).Error)
	require.Equal(t, "FAILURE", task.Status)
	require.Equal(t, "100%", task.Progress)
	require.Zero(t, task.Quota)
	var refundLog model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 5106, model.LogTypeRefund).First(&refundLog).Error)
	require.Equal(t, 6106, refundLog.TokenId)
}

func TestRunMidjourneyTaskUpdateOnceRefundsNullMidjourneyIdTask(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenUsageDaily{}, &model.Channel{}))
	submitAt := time.Now().AddDate(0, 0, -1).Unix()
	require.NoError(t, db.Create(&model.User{
		Id:        5108,
		Username:  "null-mj-owner",
		Password:  "password123",
		Quota:     75,
		UsedQuota: 25,
		Status:    common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          6108,
		UserId:      5108,
		Key:         "mj-null-token",
		Name:        "mj-null-token",
		RemainQuota: 75,
		UsedQuota:   25,
		Status:      common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:        4108,
		Name:      "mj-null-channel",
		Type:      constant.ChannelTypeMidjourney,
		Status:    common.ChannelStatusEnabled,
		UsedQuota: 25,
	}).Error)
	model.RecordTokenUsage(6108, 5108, 25, submitAt)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:     5108,
		Action:     constant.MjActionImagine,
		MjId:       "",
		Status:     "IN_PROGRESS",
		Progress:   "50%",
		ChannelId:  4108,
		Quota:      25,
		TokenId:    6108,
		Group:      "default",
		SubmitTime: submitAt * 1000,
	}).Error)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var user model.User
	require.NoError(t, db.First(&user, 5108).Error)
	require.Equal(t, 100, user.Quota)
	require.Zero(t, user.UsedQuota)
	var task model.Midjourney
	require.NoError(t, db.Where("user_id = ?", 5108).First(&task).Error)
	require.Equal(t, "FAILURE", task.Status)
	require.Equal(t, "100%", task.Progress)
	require.Zero(t, task.Quota)
	var refundLog model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 5108, model.LogTypeRefund).First(&refundLog).Error)
	require.Equal(t, 6108, refundLog.TokenId)
}

func TestMarkMidjourneyBillingReviewPersistsSettlementStatus(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:    5107,
		Action:    constant.MjActionImagine,
		MjId:      "mj-review-status",
		Status:    "SUCCESS",
		Progress:  "100%",
		ChannelId: 4107,
		Quota:     25,
	}).Error)
	var task model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "mj-review-status").First(&task).Error)

	service.MarkMidjourneyBillingReview(context.Background(), &task, errors.New("record consume log failed"))

	require.True(t, db.Migrator().HasColumn(&model.Midjourney{}, "settlement_status"))
	var settlementStatus string
	require.NoError(t, db.Model(&model.Midjourney{}).
		Select("settlement_status").
		Where("mj_id = ?", "mj-review-status").
		Scan(&settlementStatus).Error)
	require.Equal(t, model.TaskSettlementStatusReview, settlementStatus)
}

func TestRunMidjourneyTaskUpdateOnceMarksStaleApplyingRefundForTerminalFailureReview(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:           5109,
		Action:           constant.MjActionImagine,
		MjId:             "mj-terminal-stale-applying",
		Status:           "FAILURE",
		Progress:         "100%",
		ChannelId:        4109,
		Quota:            25,
		TokenId:          6109,
		Group:            "default",
		SubmitTime:       now * 1000,
		SettlementStatus: "",
	}).Error)
	var task model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "mj-terminal-stale-applying").First(&task).Error)
	require.NoError(t, db.Create(&model.MidjourneySettlementRecord{
		MidjourneyID: task.Id,
		PublicTaskID: task.MjId,
		Status:       model.TaskSettlementRecordStatusApplying,
		Operation:    "refund",
		CreatedAt:    now - 1200,
		UpdatedAt:    now - 1200,
	}).Error)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var reloaded model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "mj-terminal-stale-applying").First(&reloaded).Error)
	require.Equal(t, "FAILURE", reloaded.Status)
	require.Equal(t, "100%", reloaded.Progress)
	require.Equal(t, 25, reloaded.Quota)
	require.Equal(t, model.TaskSettlementStatusReview, reloaded.SettlementStatus)
	var record model.MidjourneySettlementRecord
	require.NoError(t, db.Where("midjourney_id = ?", task.Id).First(&record).Error)
	require.Equal(t, model.TaskSettlementRecordStatusReview, record.Status)
	require.Contains(t, record.Error, "interrupted")
}

func TestRunMidjourneyTaskUpdateOnceRollsBackFullRefundWhenRefundLogFails(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenUsageDaily{}, &model.Channel{}))
	submitAt := time.Now().AddDate(0, 0, -1).Unix()
	server := newMidjourneyPollingServer(t, dto.MidjourneyDto{
		MjId:       "mj-refund-full-log-fails",
		Status:     "FAILURE",
		Progress:   "100%",
		FailReason: "upstream failed",
		FinishTime: time.Now().UnixMilli(),
	})
	seedMidjourneyPollingChannel(t, db, 4104, server.URL)
	require.NoError(t, db.Create(&model.User{
		Id:        5104,
		Username:  "legacy-mj-owner",
		Password:  "password123",
		Quota:     75,
		UsedQuota: 25,
		Status:    common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          6104,
		UserId:      5104,
		Key:         "mj-token",
		Name:        "mj-token",
		RemainQuota: 75,
		UsedQuota:   25,
		Status:      common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 4104).Update("used_quota", 25).Error)
	model.RecordTokenUsage(6104, 5104, 25, submitAt)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:     5104,
		Action:     constant.MjActionImagine,
		MjId:       "mj-refund-full-log-fails",
		Status:     "IN_PROGRESS",
		Progress:   "50%",
		ChannelId:  4104,
		Quota:      25,
		TokenId:    6104,
		Group:      "default",
		SubmitTime: submitAt * 1000,
	}).Error)
	oldLogDB := model.LOG_DB
	brokenLogDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.LOG_DB = brokenLogDB
	t.Cleanup(func() {
		model.LOG_DB = oldLogDB
		if sqlDB, err := brokenLogDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var user model.User
	require.NoError(t, db.First(&user, 5104).Error)
	require.Equal(t, 75, user.Quota)
	require.Equal(t, 25, user.UsedQuota)
	var token model.Token
	require.NoError(t, db.First(&token, 6104).Error)
	require.Equal(t, 75, token.RemainQuota)
	require.Equal(t, 25, token.UsedQuota)
	var channel model.Channel
	require.NoError(t, db.First(&channel, 4104).Error)
	require.Equal(t, int64(25), channel.UsedQuota)
	var usage model.TokenUsageDaily
	require.NoError(t, db.Where("token_id = ?", 6104).First(&usage).Error)
	require.Equal(t, 25, usage.Quota)
}

func TestRunMidjourneyTaskUpdateOnceRefundsSubscriptionTask(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	server := newMidjourneyPollingServer(t, dto.MidjourneyDto{
		MjId:       "mj-refund-subscription",
		Status:     "FAILURE",
		Progress:   "100%",
		FailReason: "upstream failed",
		FinishTime: time.Now().UnixMilli(),
	})
	seedMidjourneyPollingChannel(t, db, 4105, server.URL)
	require.NoError(t, db.Create(&model.User{Id: 5105, Username: "sub-owner", Password: "password123", Status: common.UserStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 7105, Title: "plan"}).Error)
	require.NoError(t, db.Create(&model.UserSubscription{
		Id:          8105,
		UserId:      5105,
		PlanId:      7105,
		Status:      "active",
		AmountTotal: 100,
		AmountUsed:  25,
	}).Error)
	require.NoError(t, db.Create(&model.Midjourney{
		UserId:         5105,
		Action:         constant.MjActionImagine,
		MjId:           "mj-refund-subscription",
		Status:         "IN_PROGRESS",
		Progress:       "50%",
		ChannelId:      4105,
		Quota:          25,
		BillingSource:  service.BillingSourceSubscription,
		SubscriptionId: 8105,
		Group:          "default",
		SubmitTime:     time.Now().UnixMilli(),
	}).Error)

	runMidjourneyTaskUpdateOnce(context.Background(), nil)

	var sub model.UserSubscription
	require.NoError(t, db.First(&sub, 8105).Error)
	require.Zero(t, sub.AmountUsed)
	var user model.User
	require.NoError(t, db.First(&user, 5105).Error)
	require.Zero(t, user.Quota)
}

func TestGetUserQuotaDataByUserIdFillsCurrentUsername(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	require.NoError(t, db.Create(&model.User{
		Id:       5301,
		Username: "current-name",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.QuotaData{
		UserID:    5301,
		Username:  "old-name",
		ModelName: "gpt-4o",
		CreatedAt: 3600,
		Count:     1,
		Quota:     25,
	}).Error)

	rows, err := model.GetQuotaDataByUserId(5301, 0, 7200)

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "current-name", rows[0].Username)
}

func TestMidjourneyInsertFailureDoesNotConsumeQuota(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenUsageDaily{}, &model.Channel{}))
	oldModelPrices := ratio_setting.ModelPrice2JSONString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldModelPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0.00005}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, db.Create(&model.User{
		Id:       5401,
		Username: "mj-owner",
		Password: "password123",
		Quota:    100,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          6401,
		UserId:      5401,
		Key:         "mj-token",
		Name:        "mj-token",
		RemainQuota: 100,
		Status:      common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     4401,
		Name:   "mj-channel",
		Type:   constant.ChannelTypeMidjourney,
		Status: common.ChannelStatusEnabled,
	}).Error)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"description":"submitted","result":"mj-local-insert-fails"}`))
	}))
	t.Cleanup(upstream.Close)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"test"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("group", "default")
	ctx.Set("token_name", "mj-token")
	ctx.Set("channel_id", 4401)
	ctx.Set("base_url", upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 4401)
	info := &relaycommon.RelayInfo{
		UserId:          5401,
		TokenId:         6401,
		TokenKey:        "mj-token",
		OriginModelName: "mj_imagine",
		UserGroup:       "default",
		UsingGroup:      "default",
		RelayMode:       relayconstant.RelayModeMidjourneyImagine,
		StartTime:       time.Now(),
	}
	require.NoError(t, db.Migrator().DropTable(&model.Midjourney{}))

	resp := relaypkg.RelayMidjourneySubmit(ctx, info)

	require.NotNil(t, resp)
	require.Equal(t, "insert_midjourney_task_failed", resp.Description)
	var user model.User
	require.NoError(t, db.First(&user, 5401).Error)
	require.Equal(t, 100, user.Quota)
	require.Zero(t, user.UsedQuota)
	var token model.Token
	require.NoError(t, db.First(&token, 6401).Error)
	require.Equal(t, 100, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ?", 5401).Count(&logCount).Error)
	require.Zero(t, logCount)
}

func TestMidjourneySubmitUsesSubscriptionBillingWhenWalletIsEmpty(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	oldModelPrices := ratio_setting.ModelPrice2JSONString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldModelPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0.00005}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, db.Create(&model.User{
		Id:       5403,
		Username: "mj-sub-owner",
		Password: "password123",
		Quota:    0,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          6403,
		UserId:      5403,
		Key:         "mj-sub-token",
		Name:        "mj-sub-token",
		RemainQuota: 100,
		Status:      common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.SubscriptionPlan{Id: 7403, Title: "mj-sub-plan"}).Error)
	require.NoError(t, db.Create(&model.UserSubscription{
		Id:          8403,
		UserId:      5403,
		PlanId:      7403,
		Status:      "active",
		AmountTotal: 100,
		StartTime:   time.Now().Add(-time.Hour).Unix(),
		EndTime:     time.Now().Add(time.Hour).Unix(),
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     4403,
		Name:   "mj-sub-channel",
		Type:   constant.ChannelTypeMidjourney,
		Status: common.ChannelStatusEnabled,
	}).Error)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"description":"submitted","result":"mj-sub-task"}`))
	}))
	t.Cleanup(upstream.Close)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"test"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("group", "default")
	ctx.Set("token_name", "mj-sub-token")
	ctx.Set("channel_id", 4403)
	ctx.Set("base_url", upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 4403)
	info := &relaycommon.RelayInfo{
		UserId:          5403,
		TokenId:         6403,
		TokenKey:        "mj-sub-token",
		OriginModelName: "mj_imagine",
		UserGroup:       "default",
		UsingGroup:      "default",
		RelayMode:       relayconstant.RelayModeMidjourneyImagine,
		StartTime:       time.Now(),
	}

	resp := relaypkg.RelayMidjourneySubmit(ctx, info)

	require.Nil(t, resp)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":1`)
	var task model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "mj-sub-task").First(&task).Error)
	require.Equal(t, service.BillingSourceSubscription, task.BillingSource)
	require.Equal(t, 8403, task.SubscriptionId)
	var sub model.UserSubscription
	require.NoError(t, db.First(&sub, 8403).Error)
	require.EqualValues(t, 25, sub.AmountUsed)
	var user model.User
	require.NoError(t, db.First(&user, 5403).Error)
	require.Zero(t, user.Quota)
}

func TestMidjourneySubmitConsumeLogIncludesTaskID(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	oldModelPrices := ratio_setting.ModelPrice2JSONString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldModelPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0.00005}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, db.Create(&model.User{
		Id:       5404,
		Username: "mj-log-owner",
		Password: "password123",
		Quota:    100,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          6404,
		UserId:      5404,
		Key:         "mj-log-token",
		Name:        "mj-log-token",
		RemainQuota: 100,
		Status:      common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     4404,
		Name:   "mj-log-channel",
		Type:   constant.ChannelTypeMidjourney,
		Status: common.ChannelStatusEnabled,
	}).Error)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"description":"submitted","result":"mj-log-task"}`))
	}))
	t.Cleanup(upstream.Close)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"test"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("group", "default")
	ctx.Set("token_name", "mj-log-token")
	ctx.Set("channel_id", 4404)
	ctx.Set("base_url", upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 4404)
	info := &relaycommon.RelayInfo{
		UserId:          5404,
		TokenId:         6404,
		TokenKey:        "mj-log-token",
		OriginModelName: "mj_imagine",
		UserGroup:       "default",
		UsingGroup:      "default",
		RelayMode:       relayconstant.RelayModeMidjourneyImagine,
		StartTime:       time.Now(),
	}

	resp := relaypkg.RelayMidjourneySubmit(ctx, info)

	require.Nil(t, resp)
	require.Equal(t, http.StatusOK, recorder.Code)
	var task model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "mj-log-task").First(&task).Error)
	var log model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 5404, model.LogTypeConsume).First(&log).Error)
	var other map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(log.Other), &other))
	require.Equal(t, task.MjId, other["task_id"])
}

func TestSwapFaceInsertFailureDoesNotConsumeQuota(t *testing.T) {
	db := setupMidjourneyPollingTest(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.TokenUsageDaily{}, &model.Channel{}))
	oldModelPrices := ratio_setting.ModelPrice2JSONString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldModelPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"swap_face":0.00005}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, db.Create(&model.User{
		Id:       5402,
		Username: "swap-owner",
		Password: "password123",
		Quota:    100,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          6402,
		UserId:      5402,
		Key:         "swap-token",
		Name:        "swap-token",
		RemainQuota: 100,
		Status:      common.TokenStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     4402,
		Name:   "swap-channel",
		Type:   constant.ChannelTypeMidjourney,
		Status: common.ChannelStatusEnabled,
	}).Error)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"description":"submitted","result":"swap-local-insert-fails"}`))
	}))
	t.Cleanup(upstream.Close)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/insight-face/swap", strings.NewReader(`{"sourceBase64":"a","targetBase64":"b"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("group", "default")
	ctx.Set("token_name", "swap-token")
	ctx.Set("channel_id", 4402)
	ctx.Set("base_url", upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelBaseUrl, upstream.URL)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 4402)
	info := &relaycommon.RelayInfo{
		UserId:          5402,
		TokenId:         6402,
		TokenKey:        "swap-token",
		OriginModelName: "swap_face",
		UserGroup:       "default",
		UsingGroup:      "default",
		RelayMode:       relayconstant.RelayModeSwapFace,
		StartTime:       time.Now(),
	}
	require.NoError(t, db.Migrator().DropTable(&model.Midjourney{}))

	resp := relaypkg.RelaySwapFace(ctx, info)

	require.NotNil(t, resp)
	require.Equal(t, "insert_midjourney_task_failed", resp.Description)
	var user model.User
	require.NoError(t, db.First(&user, 5402).Error)
	require.Equal(t, 100, user.Quota)
	require.Zero(t, user.UsedQuota)
	var token model.Token
	require.NoError(t, db.First(&token, 6402).Error)
	require.Equal(t, 100, token.RemainQuota)
	require.Zero(t, token.UsedQuota)
	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("user_id = ?", 5402).Count(&logCount).Error)
	require.Zero(t, logCount)
}
