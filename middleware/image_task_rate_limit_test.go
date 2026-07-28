package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var imageTaskAccessRateLimitTestSequence atomic.Int64

func uniqueImageTaskAccessRateLimitIDs(userID int, tokenID int) (int, int) {
	offset := int(imageTaskAccessRateLimitTestSequence.Add(1)) * 10000
	return userID + offset, tokenID + offset
}

func newImageTaskAccessRateLimitEngine(t *testing.T, userID int, tokenID int) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/v1/image-tasks/:task_id", func(c *gin.Context) {
		c.Set("id", userID)
		c.Set("token_id", tokenID)
		ImageTaskAccessRateLimit()(c)
		if c.IsAborted() {
			return
		}
		c.String(http.StatusOK, "ok")
	})
	return engine
}

func imageTaskAccessRequest(engine *gin.Engine, path string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	return recorder
}

func withImageTaskAccessRateLimit(t *testing.T, count int, durationSeconds int) {
	t.Helper()
	oldCount := constant.ImageTaskAccessRateLimitCount
	oldDuration := constant.ImageTaskAccessRateLimitDurationSeconds
	oldRedis := common.RedisEnabled
	constant.ImageTaskAccessRateLimitCount = count
	constant.ImageTaskAccessRateLimitDurationSeconds = durationSeconds
	common.RedisEnabled = false
	t.Cleanup(func() {
		constant.ImageTaskAccessRateLimitCount = oldCount
		constant.ImageTaskAccessRateLimitDurationSeconds = oldDuration
		common.RedisEnabled = oldRedis
	})
}

func TestImageTaskAccessRateLimitBlocksOverBudgetPollingWithPublicEnvelope(t *testing.T) {
	withImageTaskAccessRateLimit(t, 2, 60)
	userID, tokenID := uniqueImageTaskAccessRateLimitIDs(4001, 5001)
	engine := newImageTaskAccessRateLimitEngine(t, userID, tokenID)

	require.Equal(t, http.StatusOK, imageTaskAccessRequest(engine, "/v1/image-tasks/task_a").Code)
	require.Equal(t, http.StatusOK, imageTaskAccessRequest(engine, "/v1/image-tasks/task_a").Code)

	blocked := imageTaskAccessRequest(engine, "/v1/image-tasks/task_a")
	require.Equal(t, http.StatusTooManyRequests, blocked.Code, blocked.Body.String())
	require.Equal(t, "60", blocked.Header().Get("Retry-After"))
	// 必须沿用 /v1/image-tasks/* 的错误信封，而不是 abortTooManyRequests 的 success/message 形态。
	require.Contains(t, blocked.Body.String(), `"code":"rate_limit_exceeded"`)
	require.Contains(t, blocked.Body.String(), `"type":"image_task_error"`)
	require.NotContains(t, blocked.Body.String(), `"success"`)
}

func TestImageTaskAccessRateLimitIsolatesTokensOfSameUser(t *testing.T) {
	withImageTaskAccessRateLimit(t, 1, 60)
	userID, firstTokenID := uniqueImageTaskAccessRateLimitIDs(4002, 5002)
	first := newImageTaskAccessRateLimitEngine(t, userID, firstTokenID)
	second := newImageTaskAccessRateLimitEngine(t, userID, firstTokenID+1)

	require.Equal(t, http.StatusOK, imageTaskAccessRequest(first, "/v1/image-tasks/task_b").Code)
	require.Equal(t, http.StatusTooManyRequests, imageTaskAccessRequest(first, "/v1/image-tasks/task_b").Code)
	// 同一用户的另一个令牌不应被前一个令牌打满的额度牵连。
	require.Equal(t, http.StatusOK, imageTaskAccessRequest(second, "/v1/image-tasks/task_b").Code)
}

func TestImageTaskAccessRateLimitDisabledWhenUnset(t *testing.T) {
	withImageTaskAccessRateLimit(t, 0, 60)
	userID, tokenID := uniqueImageTaskAccessRateLimitIDs(4003, 5004)
	engine := newImageTaskAccessRateLimitEngine(t, userID, tokenID)

	for i := 0; i < 5; i++ {
		require.Equal(t, http.StatusOK, imageTaskAccessRequest(engine, "/v1/image-tasks/task_c").Code)
	}
}

func TestImageTaskAccessRateLimitRejectsUnauthenticatedContext(t *testing.T) {
	withImageTaskAccessRateLimit(t, 5, 60)
	engine := newImageTaskAccessRateLimitEngine(t, 0, 0)

	recorder := imageTaskAccessRequest(engine, "/v1/image-tasks/task_d")
	require.Equal(t, http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), `"type":"image_task_error"`)
}

func TestImageTaskCreateAdmissionSharesRateLimitAcrossRouterInstances(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ImageTaskCreateGuard{},
		&model.ImageTaskCreateRateBucket{},
		&model.ImageTaskCreateReservation{},
	))
	oldDB := model.DB
	oldCount := constant.ImageTaskCreateRateLimitCount
	oldDuration := constant.ImageTaskCreateRateLimitDurationSeconds
	oldMaxInFlight := constant.ImageTaskCreateMaxInFlight
	oldMaxReservedMB := constant.ImageTaskCreateMaxReservedMB
	model.DB = db
	constant.ImageTaskCreateRateLimitCount = 1
	constant.ImageTaskCreateRateLimitDurationSeconds = 60
	constant.ImageTaskCreateMaxInFlight = 8
	constant.ImageTaskCreateMaxReservedMB = 512
	t.Cleanup(func() {
		model.DB = oldDB
		constant.ImageTaskCreateRateLimitCount = oldCount
		constant.ImageTaskCreateRateLimitDurationSeconds = oldDuration
		constant.ImageTaskCreateMaxInFlight = oldMaxInFlight
		constant.ImageTaskCreateMaxReservedMB = oldMaxReservedMB
	})

	newEngine := func() *gin.Engine {
		engine := gin.New()
		engine.POST("/v1/image-tasks/generations", func(c *gin.Context) {
			c.Set("id", 7001)
			c.Set("token_id", 8001)
			ImageTaskCreateAdmission()(c)
			if !c.IsAborted() {
				c.String(http.StatusOK, "ok")
			}
		})
		return engine
	}
	request := func(engine *gin.Engine) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", strings.NewReader(`{"prompt":"test"}`))
		engine.ServeHTTP(recorder, req)
		return recorder
	}

	require.Equal(t, http.StatusOK, request(newEngine()).Code)
	blocked := request(newEngine())
	require.Equal(t, http.StatusTooManyRequests, blocked.Code, blocked.Body.String())
	require.Contains(t, blocked.Body.String(), `"code":"rate_limit_exceeded"`)
}

func TestImageTaskCreateAdmissionRenewsReservationWhileHandlerRuns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.ImageTaskCreateGuard{},
		&model.ImageTaskCreateRateBucket{},
		&model.ImageTaskCreateReservation{},
	))
	oldDB := model.DB
	oldCount := constant.ImageTaskCreateRateLimitCount
	oldDuration := constant.ImageTaskCreateRateLimitDurationSeconds
	oldMaxInFlight := constant.ImageTaskCreateMaxInFlight
	oldMaxReservedMB := constant.ImageTaskCreateMaxReservedMB
	oldTTL := imageTaskCreateAdmissionTTL
	oldRenewInterval := imageTaskCreateAdmissionRenewInterval
	model.DB = db
	constant.ImageTaskCreateRateLimitCount = 0
	constant.ImageTaskCreateRateLimitDurationSeconds = 0
	constant.ImageTaskCreateMaxInFlight = 1
	constant.ImageTaskCreateMaxReservedMB = 1
	imageTaskCreateAdmissionTTL = 2 * time.Second
	imageTaskCreateAdmissionRenewInterval = 100 * time.Millisecond
	t.Cleanup(func() {
		model.DB = oldDB
		constant.ImageTaskCreateRateLimitCount = oldCount
		constant.ImageTaskCreateRateLimitDurationSeconds = oldDuration
		constant.ImageTaskCreateMaxInFlight = oldMaxInFlight
		constant.ImageTaskCreateMaxReservedMB = oldMaxReservedMB
		imageTaskCreateAdmissionTTL = oldTTL
		imageTaskCreateAdmissionRenewInterval = oldRenewInterval
	})

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseRequest)
	engine := gin.New()
	engine.POST("/v1/image-tasks/generations",
		func(c *gin.Context) {
			c.Set("id", 7101)
			c.Set("token_id", 8101)
		},
		ImageTaskCreateAdmission(),
		func(c *gin.Context) {
			startedOnce.Do(func() { close(started) })
			<-release
			c.Status(http.StatusNoContent)
		},
	)

	requestDone := make(chan *httptest.ResponseRecorder, 1)
	requestContext, cancelRequest := context.WithCancel(context.Background())
	t.Cleanup(cancelRequest)
	go func() {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/image-tasks/generations", strings.NewReader(`{"prompt":"slow"}`)).WithContext(requestContext)
		engine.ServeHTTP(recorder, req)
		requestDone <- recorder
	}()
	<-started
	cancelRequest()
	time.Sleep(2500 * time.Millisecond)

	_, err = model.AcquireImageTaskCreateAdmission(7102, 8102, 16, 0, model.ImageTaskCreateAdmissionLimits{
		MaxInFlight:           1,
		MaxReservedBytes:      1 << 20,
		ReservationTTLSeconds: 2,
	})
	require.ErrorIs(t, err, model.ErrImageTaskCreateCapacityExceeded)

	releaseRequest()
	require.Equal(t, http.StatusNoContent, (<-requestDone).Code)
}
