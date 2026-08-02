package controller

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoTaskSettlementTestAdaptor struct {
	totalTokens int
	model       string
}

func (videoTaskSettlementTestAdaptor) Init(info *relaycommon.RelayInfo) {}

func (videoTaskSettlementTestAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return nil
}

func (videoTaskSettlementTestAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

func (videoTaskSettlementTestAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	return nil
}

func (videoTaskSettlementTestAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

func (videoTaskSettlementTestAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return "", nil
}

func (videoTaskSettlementTestAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	return nil
}

func (videoTaskSettlementTestAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	return nil, nil
}

func (videoTaskSettlementTestAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return nil, nil
}

func (videoTaskSettlementTestAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	return "", nil, nil
}

func (videoTaskSettlementTestAdaptor) GetModelList() []string {
	return nil
}

func (videoTaskSettlementTestAdaptor) GetChannelName() string {
	return "settlement-test"
}

func (a videoTaskSettlementTestAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	modelName := a.model
	if modelName == "" {
		modelName = "video-settlement-test"
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"model":"` + modelName + `"}`)),
	}, nil
}

func (a videoTaskSettlementTestAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	totalTokens := a.totalTokens
	if totalTokens == 0 {
		totalTokens = 1000
	}
	return &relaycommon.TaskInfo{
		Status:      string(model.TaskStatusSuccess),
		TotalTokens: totalTokens,
	}, nil
}

func TestUpdateVideoSingleTaskDoesNotSettleWhenStatusUpdateFails(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.Log{}, &model.QuotaData{}, &model.TokenUsageDaily{}))
	require.NoError(t, db.Exec(`CREATE TABLE tasks (id integer primary key, task_id text, status text, quota integer, updated_at integer)`).Error)

	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldModelRatios := ratio_setting.ModelRatio2JSONString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	oldGroupGroupRatios := ratio_setting.GroupGroupRatio2JSONString()
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"video-settlement-test":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModelRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldGroupGroupRatios))
	})

	require.NoError(t, db.Create(&model.User{
		Id:       9310,
		Username: "video-wallet-owner",
		Password: "password123",
		Group:    "default",
		Quota:    10_000,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          9410,
		UserId:      9310,
		Key:         "video-token",
		Name:        "video-token",
		RemainQuota: 10_000,
		Status:      common.TokenStatusEnabled,
	}).Error)

	channel := &model.Channel{
		Id:     9510,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "upstream-key",
		Name:   "video-channel",
		Status: common.ChannelStatusEnabled,
	}
	task := &model.Task{
		ID:        1,
		TaskID:    "video-update-fail",
		Platform:  constant.TaskPlatform("kling"),
		UserId:    9310,
		ChannelId: channel.Id,
		Group:     "default",
		Action:    constant.TaskActionGenerate,
		Status:    model.TaskStatusInProgress,
		Progress:  "30%",
		Quota:     100,
		PrivateData: model.TaskPrivateData{
			TokenId:       9410,
			BillingSource: service.BillingSourceWallet,
		},
	}
	require.NoError(t, db.Exec(
		`INSERT INTO tasks (id, task_id, status, quota, updated_at) VALUES (?, ?, ?, ?, 0)`,
		task.ID,
		task.TaskID,
		task.Status,
		task.Quota,
	).Error)

	err := updateVideoSingleTask(context.Background(), videoTaskSettlementTestAdaptor{}, channel, task.TaskID, map[string]*model.Task{
		task.TaskID: task,
	})

	require.NoError(t, err)
	var user model.User
	require.NoError(t, db.Select("quota").First(&user, 9310).Error)
	require.Equal(t, 10_000, user.Quota)
	var token model.Token
	require.NoError(t, db.Select("remain_quota", "used_quota").First(&token, 9410).Error)
	require.Equal(t, 10_000, token.RemainQuota)
	require.Equal(t, 0, token.UsedQuota)
	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&logCount).Error)
	require.Equal(t, int64(0), logCount)
}

func TestUpdateVideoSingleTaskFloorsPositiveTokenSettlementToOne(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskSettlementRecord{}, &model.Token{}, &model.Log{}, &model.QuotaData{}, &model.TokenUsageDaily{}))

	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldModelRatios := ratio_setting.ModelRatio2JSONString()
	oldGroupRatios := ratio_setting.GroupRatio2JSONString()
	oldGroupGroupRatios := ratio_setting.GroupGroupRatio2JSONString()
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tiny-video-model":0.4}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{}`))
	t.Cleanup(func() {
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
		common.LogConsumeEnabled = oldLogConsumeEnabled
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldModelRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldGroupRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldGroupGroupRatios))
	})

	const userID, tokenID, channelID = 9320, 9420, 9520
	const initQuota, preConsumed = 10_000, 10
	const tokenRemain = 5_000

	require.NoError(t, db.Create(&model.User{
		Id:           userID,
		Username:     "video-floor-owner",
		Password:     "password123",
		Group:        "default",
		Quota:        initQuota,
		UsedQuota:    preConsumed,
		RequestCount: 1,
		Status:       common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Token{
		Id:          tokenID,
		UserId:      userID,
		Key:         "video-floor-token",
		Name:        "video-floor-token",
		RemainQuota: tokenRemain,
		UsedQuota:   preConsumed,
		Status:      common.TokenStatusEnabled,
	}).Error)
	channel := &model.Channel{
		Id:        channelID,
		Type:      constant.ChannelTypeOpenAI,
		Key:       "upstream-key",
		Name:      "video-channel",
		Status:    common.ChannelStatusEnabled,
		UsedQuota: preConsumed,
	}
	require.NoError(t, db.Create(channel).Error)

	task := &model.Task{
		TaskID:    "video-token-floor",
		Platform:  constant.TaskPlatform("kling"),
		UserId:    userID,
		ChannelId: channel.Id,
		Group:     "default",
		Action:    constant.TaskActionGenerate,
		Status:    model.TaskStatusInProgress,
		Progress:  "30%",
		Quota:     preConsumed,
		Data:      json.RawMessage(`{"model":"tiny-video-model"}`),
		Properties: model.Properties{
			OriginModelName: "tiny-video-model",
		},
		PrivateData: model.TaskPrivateData{
			TokenId:       tokenID,
			BillingSource: service.BillingSourceWallet,
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "tiny-video-model",
			},
		},
	}
	require.NoError(t, db.Create(task).Error)

	err := updateVideoSingleTask(context.Background(), videoTaskSettlementTestAdaptor{
		totalTokens: 1,
		model:       "tiny-video-model",
	}, channel, task.TaskID, map[string]*model.Task{
		task.TaskID: task,
	})

	require.NoError(t, err)

	var reloadedTask model.Task
	require.NoError(t, db.First(&reloadedTask, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloadedTask.Status)
	require.Equal(t, 1, reloadedTask.Quota)
	require.Empty(t, reloadedTask.SettlementStatus)

	var user model.User
	require.NoError(t, db.Select("quota", "used_quota", "request_count").First(&user, userID).Error)
	require.Equal(t, initQuota+preConsumed-1, user.Quota)
	require.Equal(t, 1, user.UsedQuota)
	require.Equal(t, 1, user.RequestCount)

	var token model.Token
	require.NoError(t, db.Select("remain_quota", "used_quota").First(&token, tokenID).Error)
	require.Equal(t, tokenRemain+preConsumed-1, token.RemainQuota)
	require.Equal(t, 1, token.UsedQuota)

	var reloadedChannel model.Channel
	require.NoError(t, db.Select("used_quota").First(&reloadedChannel, channelID).Error)
	require.EqualValues(t, 1, reloadedChannel.UsedQuota)

	var log model.Log
	require.NoError(t, model.LOG_DB.Order("id desc").First(&log).Error)
	require.Equal(t, model.LogTypeRefund, log.Type)
	require.Equal(t, preConsumed-1, log.Quota)
	require.Equal(t, "video-floor-owner", log.Username)
	require.Equal(t, "video-floor-token", log.TokenName)
}
