package relay

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRunSyncWrapperImageTaskTransientChannelLookupDoesNotRefund(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.User{}, &model.Token{}, &model.Log{}, &model.QuotaData{}))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedis := common.RedisEnabled
	oldBatch := common.BatchUpdateEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	previousLookup := cacheGetChannel
	cacheGetChannel = func(int) (*model.Channel, error) {
		return nil, errors.New("SSL connection has been closed unexpectedly")
	}
	t.Cleanup(func() {
		cacheGetChannel = previousLookup
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedis
		common.BatchUpdateEnabled = oldBatch
		_ = sqlDB.Close()
	})

	const userQuota int64 = 4000
	const preConsumed = 900
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "image-channel-blip", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: userQuota,
	}).Error)
	withTempImageTaskCache(t)
	body := []byte(`{"model":"gpt-image-1","prompt":"cat","n":1}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })

	task := &model.Task{
		TaskID:     "image-channel-blip",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  77,
		Quota:      preConsumed,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			BillingSource:      service.BillingSourceWallet,
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	err = runSyncWrapperImageTask(context.Background(), task)
	require.Error(t, err)

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), updated.Status)
	require.Equal(t, preConsumed, updated.Quota)
	require.False(t, updated.RefundPending)
	require.Empty(t, updated.FailReason)
	require.Greater(t, updated.NextPollAt, time.Now().Unix())

	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	require.EqualValues(t, userQuota, user.Quota)
}

func TestRunSyncWrapperImageTaskMissingChannelStillRefunds(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(
		&model.Task{}, &model.TaskSettlementRecord{}, &model.User{}, &model.Token{},
		&model.Channel{}, &model.Log{}, &model.QuotaData{}, &model.TokenUsageDaily{},
	))

	oldDB := model.DB
	oldLogDB := model.LOG_DB
	oldUsingSQLite := common.UsingSQLite
	oldRedis := common.RedisEnabled
	oldBatch := common.BatchUpdateEnabled
	model.DB = db
	model.LOG_DB = db
	common.UsingSQLite = true
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	previousLookup := cacheGetChannel
	cacheGetChannel = func(int) (*model.Channel, error) {
		return nil, errors.New("channel #77 no longer exists")
	}
	t.Cleanup(func() {
		cacheGetChannel = previousLookup
		model.DB = oldDB
		model.LOG_DB = oldLogDB
		common.UsingSQLite = oldUsingSQLite
		common.RedisEnabled = oldRedis
		common.BatchUpdateEnabled = oldBatch
		_ = sqlDB.Close()
	})

	const userQuota int64 = 4000
	const preConsumed = 900
	require.NoError(t, db.Create(&model.User{
		Id: 1, Username: "image-channel-gone", Password: "password123",
		Status: common.UserStatusEnabled, Group: "default", Quota: userQuota,
	}).Error)
	withTempImageTaskCache(t)
	body := []byte(`{"model":"gpt-image-1","prompt":"cat","n":1}`)
	bodyPath, err := common.WriteImageTaskBodyCacheFile(body)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(bodyPath) })

	task := &model.Task{
		TaskID:     "image-channel-gone",
		Platform:   constant.TaskPlatformImage,
		UserId:     1,
		Group:      "default",
		ChannelId:  77,
		Quota:      preConsumed,
		Action:     constant.TaskActionImageGeneration,
		Status:     model.TaskStatusQueued,
		Progress:   "0%",
		SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "gpt-image-1"},
		PrivateData: model.TaskPrivateData{
			BillingSource:      service.BillingSourceWallet,
			ImageTaskMode:      dto.ImageTaskModeSyncWrapper,
			RequestPath:        "/v1/images/generations",
			RequestMethod:      http.MethodPost,
			RequestContentType: "application/json",
			RequestBodyPath:    bodyPath,
			RequestBodySize:    int64(len(body)),
			Key:                "upstream-key",
		},
	}
	require.NoError(t, db.Create(task).Error)

	require.NoError(t, runSyncWrapperImageTask(context.Background(), task))

	var updated model.Task
	require.NoError(t, db.First(&updated, task.ID).Error)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), updated.Status)
	require.Zero(t, updated.Quota)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	require.EqualValues(t, userQuota+preConsumed, user.Quota)
}
