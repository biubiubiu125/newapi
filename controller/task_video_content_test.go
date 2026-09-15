package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupTaskVideoContentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	service.InitHttpClient()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	return db
}

func TestTaskContentUsesLegacyVideoRequestWhenResultURLIsProxyPlaceholder(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	fetchSetting := system_setting.GetFetchSetting()
	oldFetchSetting := *fetchSetting
	t.Cleanup(func() {
		*fetchSetting = oldFetchSetting
	})

	require.NoError(t, db.Create(&model.User{
		Id:       91001,
		Username: "legacy-video-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	upstreamCalled := int32(0)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalled, 1)
		require.Equal(t, "/v1/videos/upstream-video-task/content", r.URL.Path)
		require.Equal(t, "Bearer channel-secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("legacy-video"))
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = []string{upstreamURL.Port()}

	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      91002,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "channel-secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "legacy-video-placeholder",
		Platform:  constant.TaskPlatform("openai"),
		UserId:    91001,
		ChannelId: 91002,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-video-task",
			ResultURL:      taskcommon.BuildProxyURL("legacy-video-placeholder"),
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)
	ctx.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	ctx.Set("role", common.RoleAdminUser)

	TaskArtifactContent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "legacy-video", recorder.Body.String())
	require.Equal(t, int32(1), atomic.LoadInt32(&upstreamCalled))
}

func TestTaskContentUsesPersistedSelectedKeyForOpenAIMultiKey(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	require.NoError(t, db.Create(&model.User{
		Id:       91003,
		Username: "legacy-video-multi-key-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer selected-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("multi-key-video"))
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	fetchSetting := system_setting.GetFetchSetting()
	oldFetchSetting := *fetchSetting
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = []string{upstreamURL.Port()}
	t.Cleanup(func() {
		*fetchSetting = oldFetchSetting
	})

	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      91004,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "key-a\nselected-key",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:   true,
			MultiKeySize: 2,
		},
	}).Error)

	task := &model.Task{
		TaskID:    "legacy-video-multi-key",
		Platform:  constant.TaskPlatform("openai"),
		UserId:    91003,
		ChannelId: 91004,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			Key:            "selected-key",
			UpstreamTaskID: "upstream-multi-key-task",
			ResultURL:      taskcommon.BuildProxyURL("legacy-video-multi-key"),
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)
	ctx.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	ctx.Set("role", common.RoleAdminUser)

	TaskArtifactContent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "multi-key-video", recorder.Body.String())
}

func TestTaskContentUsesGeminiGeneratedVideosWithoutTaskKey(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	fetchSetting := system_setting.GetFetchSetting()
	oldFetchSetting := *fetchSetting
	t.Cleanup(func() {
		*fetchSetting = oldFetchSetting
	})

	require.NoError(t, db.Create(&model.User{
		Id:       91011,
		Username: "gemini-data-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	upstreamCalled := int32(0)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalled, 1)
		require.Equal(t, "/legacy/gemini/video.mp4", r.URL.Path)
		require.Empty(t, r.Header.Get("x-goog-api-key"))
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("gemini-video"))
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = []string{upstreamURL.Port()}

	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      91012,
		Type:    constant.ChannelTypeGemini,
		Key:     "",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "gemini-data-url",
		Platform:  constant.TaskPlatform("gemini"),
		UserId:    91011,
		ChannelId: 91012,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSuccess,
		Data:      []byte(`{"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"` + upstream.URL + `/legacy/gemini/video.mp4"}}]}}}`),
		PrivateData: model.TaskPrivateData{
			ResultURL: taskcommon.BuildProxyURL("gemini-data-url"),
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)
	ctx.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	ctx.Set("role", common.RoleAdminUser)

	TaskArtifactContent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "gemini-video", recorder.Body.String())
	require.Equal(t, int32(1), atomic.LoadInt32(&upstreamCalled))
}

func TestTaskContentUsesGeminiGeneratedVideoBytesBase64Encoded(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	require.NoError(t, db.Create(&model.User{
		Id:       91015,
		Username: "gemini-generated-bytes-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	baseURL := "https://media.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      91016,
		Type:    constant.ChannelTypeGemini,
		Key:     "channel-secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "gemini-generated-bytes",
		Platform:  constant.TaskPlatform("gemini"),
		UserId:    91015,
		ChannelId: 91016,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSuccess,
		Data:      []byte(`{"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"bytesBase64Encoded":"aGVsbG8=","mimeType":"video/mp4"}}]}}}`),
		PrivateData: model.TaskPrivateData{
			ResultURL: taskcommon.BuildProxyURL("gemini-generated-bytes"),
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)
	ctx.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	ctx.Set("role", common.RoleAdminUser)

	TaskArtifactContent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "hello", recorder.Body.String())
}

func TestProxyTaskMediaPersistsInlineVideoArtifact(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	var uploadedBody string
	var uploadedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		uploadedPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		uploadedBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeS3)
	t.Setenv(system_setting.TaskArtifactStoreS3EndpointEnv, upstream.URL)
	t.Setenv(system_setting.TaskArtifactStoreS3BucketEnv, "newapi-artifacts")
	t.Setenv(system_setting.TaskArtifactStoreS3RegionEnv, "us-east-1")
	t.Setenv(system_setting.TaskArtifactStoreS3AccessKeyEnv, "test-access")
	t.Setenv(system_setting.TaskArtifactStoreS3SecretKeyEnv, "test-secret")
	t.Setenv(system_setting.TaskArtifactStoreS3PrefixEnv, "task-artifacts")
	require.NoError(t, service.ConfigureTaskArtifactStore())
	t.Cleanup(func() {
		t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeUpstream)
		_ = service.ConfigureTaskArtifactStore()
	})

	task := &model.Task{
		TaskID: "vertex-inline-persist",
		Status: model.TaskStatusSuccess,
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)

	err := proxyTaskMediaWithArtifact(
		ctx,
		task,
		&relaychannel.TaskContentRequest{
			URL:    "data:video/mp4;base64,aGVsbG8=",
			Method: http.MethodGet,
		},
		&relaychannel.TaskArtifact{Key: "video", Type: "video", MimeType: "video/mp4"},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "hello", recorder.Body.String())
	require.Equal(t, "/newapi-artifacts/task-artifacts/vertex-inline-persist/video", uploadedPath)
	require.Equal(t, "hello", uploadedBody)

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	require.Equal(t, model.TaskArtifactStorageRef{
		Backend:   "s3",
		Bucket:    "newapi-artifacts",
		ObjectKey: "task-artifacts/vertex-inline-persist/video",
		Type:      "video",
		MimeType:  "video/mp4",
		Size:      5,
	}, reloaded.PrivateData.ArtifactRefs["video"])
}

func TestVideoProxyUsesPersistedVideoArtifactWithNonVideoKey(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	var requestedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("stored-clip"))
	}))
	t.Cleanup(upstream.Close)

	t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeS3)
	t.Setenv(system_setting.TaskArtifactStoreS3EndpointEnv, upstream.URL)
	t.Setenv(system_setting.TaskArtifactStoreS3BucketEnv, "newapi-artifacts")
	t.Setenv(system_setting.TaskArtifactStoreS3RegionEnv, "us-east-1")
	t.Setenv(system_setting.TaskArtifactStoreS3AccessKeyEnv, "test-access")
	t.Setenv(system_setting.TaskArtifactStoreS3SecretKeyEnv, "test-secret")
	t.Setenv(system_setting.TaskArtifactStoreS3PrefixEnv, "task-artifacts")
	require.NoError(t, service.ConfigureTaskArtifactStore())
	t.Cleanup(func() {
		t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeUpstream)
		_ = service.ConfigureTaskArtifactStore()
	})

	task := &model.Task{
		TaskID: "stored-clip-task",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ArtifactRefs: map[string]model.TaskArtifactStorageRef{
				"clip": {
					Backend:   "s3",
					Bucket:    "newapi-artifacts",
					ObjectKey: "task-artifacts/stored-clip-task/clip",
					Type:      "video",
					MimeType:  "video/mp4",
					Size:      11,
				},
			},
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)

	require.True(t, resolveStoredVideoArtifact(ctx, task))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "stored-clip", recorder.Body.String())
	require.Equal(t, "/newapi-artifacts/task-artifacts/stored-clip-task/clip", requestedPath)
}

func TestProxyTaskMediaAcceptsCaseInsensitiveBase64DataURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/case-insensitive-data/content", nil)

	err := proxyTaskMediaWithArtifact(
		ctx,
		&model.Task{TaskID: "case-insensitive-data"},
		&relaychannel.TaskContentRequest{
			URL:    "DATA:VIDEO/MP4;BASE64,aGVsbG8=",
			Method: http.MethodGet,
		},
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "hello", recorder.Body.String())
}

func TestProxyTaskMediaRejectsMalformedBase64DataURLMarker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/malformed-data/content", nil)

	err := proxyTaskMediaWithArtifact(
		ctx,
		&model.Task{TaskID: "malformed-data"},
		&relaychannel.TaskContentRequest{
			URL:    "data:video/mp4;base64foo,aGVsbG8=",
			Method: http.MethodGet,
		},
		nil,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported data url")
}

func TestExtractGeminiVideoURLAcceptsCaseInsensitiveDataURL(t *testing.T) {
	require.Equal(t,
		"DATA:VIDEO/MP4;BASE64,aGVsbG8=",
		extractGeminiVideoURLFromResponse(map[string]any{
			"video": "DATA:VIDEO/MP4;BASE64,aGVsbG8=",
		}),
	)
}

func TestProxyTaskMediaDoesNotPersistHeadArtifact(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	var putCount int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			atomic.AddInt32(&putCount, 1)
			w.WriteHeader(http.StatusOK)
			return
		}
		require.Equal(t, http.MethodHead, r.Method)
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(upstream.Close)

	t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeS3)
	t.Setenv(system_setting.TaskArtifactStoreS3EndpointEnv, upstream.URL)
	t.Setenv(system_setting.TaskArtifactStoreS3BucketEnv, "newapi-artifacts")
	t.Setenv(system_setting.TaskArtifactStoreS3RegionEnv, "us-east-1")
	t.Setenv(system_setting.TaskArtifactStoreS3AccessKeyEnv, "test-access")
	t.Setenv(system_setting.TaskArtifactStoreS3SecretKeyEnv, "test-secret")
	t.Setenv(system_setting.TaskArtifactStoreS3PrefixEnv, "task-artifacts")
	require.NoError(t, service.ConfigureTaskArtifactStore())
	t.Cleanup(func() {
		t.Setenv(system_setting.TaskArtifactStoreModeEnv, system_setting.TaskArtifactStoreModeUpstream)
		_ = service.ConfigureTaskArtifactStore()
	})

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemoryCacheEnabled })

	fetchSetting := system_setting.GetFetchSetting()
	oldFetchSetting := *fetchSetting
	t.Cleanup(func() { *fetchSetting = oldFetchSetting })
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = []string{upstreamURL.Port()}

	baseURL := upstream.URL
	const channelID = 91045
	require.NoError(t, db.Create(&model.Channel{
		Id: channelID, Type: constant.ChannelTypeKling, Key: "channel-secret",
		BaseURL: &baseURL, Status: common.ChannelStatusEnabled,
	}).Error)
	task := &model.Task{
		TaskID:    "head-artifact",
		ChannelId: channelID,
		Status:    model.TaskStatusSuccess,
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)

	err = proxyTaskMediaWithArtifact(
		ctx,
		task,
		&relaychannel.TaskContentRequest{
			URL:    upstream.URL + "/media.mp4",
			Method: http.MethodHead,
		},
		&relaychannel.TaskArtifact{Key: "video", Type: "video", MimeType: "video/mp4"},
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Zero(t, atomic.LoadInt32(&putCount))

	var reloaded model.Task
	require.NoError(t, db.First(&reloaded, task.ID).Error)
	assert.Empty(t, reloaded.PrivateData.ArtifactRefs)
}

func TestTaskContentUsesGeminiResultURLWithoutTaskKey(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	fetchSetting := system_setting.GetFetchSetting()
	oldFetchSetting := *fetchSetting
	t.Cleanup(func() {
		*fetchSetting = oldFetchSetting
	})

	require.NoError(t, db.Create(&model.User{
		Id:       91021,
		Username: "gemini-result-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	upstreamCalled := int32(0)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalled, 1)
		require.Equal(t, "/legacy/gemini/result.mp4", r.URL.Path)
		require.Empty(t, r.Header.Get("x-goog-api-key"))
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("gemini-result"))
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = []string{upstreamURL.Port()}

	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      91022,
		Type:    constant.ChannelTypeGemini,
		Key:     "",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "gemini-result-url",
		Platform:  constant.TaskPlatform("gemini"),
		UserId:    91021,
		ChannelId: 91022,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultURL: upstream.URL + "/legacy/gemini/result.mp4",
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)
	ctx.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	ctx.Set("role", common.RoleAdminUser)

	TaskArtifactContent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "gemini-result", recorder.Body.String())
	require.Equal(t, int32(1), atomic.LoadInt32(&upstreamCalled))
}

func TestTaskContentUsesGeminiChannelKeyWhenTaskKeyMissing(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	fetchSetting := system_setting.GetFetchSetting()
	oldFetchSetting := *fetchSetting
	t.Cleanup(func() {
		*fetchSetting = oldFetchSetting
	})

	require.NoError(t, db.Create(&model.User{
		Id:       91031,
		Username: "gemini-channel-key-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	upstreamCalled := int32(0)
	contentURL := ""
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalled, 1)
		switch r.URL.Path {
		case "/v1beta/video-task":
			require.Equal(t, "channel-secret", r.Header.Get("x-goog-api-key"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"name":"operations/video-task","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"` + contentURL + `"}}]}}}`))
		case "/legacy/gemini/channel.mp4":
			require.Equal(t, "channel-secret", r.Header.Get("x-goog-api-key"))
			w.Header().Set("Content-Type", "video/mp4")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("gemini-channel"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	contentURL = upstream.URL + "/legacy/gemini/channel.mp4"
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = []string{upstreamURL.Port()}

	baseURL := upstream.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      91032,
		Type:    constant.ChannelTypeGemini,
		Key:     "channel-secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "gemini-channel-key",
		Platform:  constant.TaskPlatform("gemini"),
		UserId:    91031,
		ChannelId: 91032,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: taskcommon.EncodeLocalTaskID("video-task"),
			ResultURL:      taskcommon.BuildProxyURL("gemini-channel-key"),
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)
	ctx.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	ctx.Set("role", common.RoleAdminUser)

	TaskArtifactContent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "gemini-channel", recorder.Body.String())
	require.Equal(t, int32(2), atomic.LoadInt32(&upstreamCalled))
}

func TestEnsureAPIKeyAppendsWhenOtherParamsContainKeySubstring(t *testing.T) {
	got := ensureAPIKey("https://example.com/legacy/gemini/video.mp4?apikey=existing&foo=1", "secret")
	require.Equal(t, "https://example.com/legacy/gemini/video.mp4?apikey=existing&foo=1&key=secret", got)
}

func TestDoTaskMediaRequestRejectsNilClient(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "https://example.com/video.mp4", nil)
	resp, err := doTaskMediaRequest(nil, req, time.Second)
	require.ErrorIs(t, err, errTaskMediaClientUnavailable)
	require.Nil(t, resp)
}

func TestTaskContentUsesChannelKeyForNestedVideoContentURL(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	fetchSetting := system_setting.GetFetchSetting()
	oldFetchSetting := *fetchSetting
	t.Cleanup(func() {
		*fetchSetting = oldFetchSetting
	})

	require.NoError(t, db.Create(&model.User{
		Id:       93001,
		Username: "nested-video-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	upstreamCalled := int32(0)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&upstreamCalled, 1)
		require.Equal(t, "/v1/videos/nested-upstream/content", r.URL.Path)
		require.Equal(t, "Bearer nested-secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("nested-video"))
	}))
	t.Cleanup(upstream.Close)
	upstreamURL, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = []string{upstreamURL.Port()}

	baseURL := "https://unused.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      93002,
		Type:    constant.ChannelTypeKling,
		Key:     "nested-secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "nested-video-task",
		Platform:  constant.TaskPlatform("kling"),
		UserId:    93001,
		ChannelId: 93002,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "unused-local-upstream",
			ResultURL:      upstream.URL + "/v1/videos/nested-upstream/content",
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)
	ctx.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	ctx.Set("role", common.RoleAdminUser)

	TaskArtifactContent(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "nested-video", recorder.Body.String())
	require.Equal(t, int32(1), atomic.LoadInt32(&upstreamCalled))
}

func TestTaskContentServesStoredDataURLForKling(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	require.NoError(t, db.Create(&model.User{
		Id:       94001,
		Username: "kling-data-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	baseURL := "https://kling.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      94002,
		Type:    constant.ChannelTypeKling,
		Key:     "access|secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "kling-data-task",
		Platform:  constant.TaskPlatform("kling"),
		UserId:    94001,
		ChannelId: 94002,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSubmitted,
	}
	taskcommon.ApplyTaskSuccessResult(task, "data:video/mp4;base64,aGVsbG8=", "", "", time.Now().Unix(), false)
	task.SettlementStatus = model.TaskSettlementStatusSettled
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/videos/"+task.TaskID+"/content",
		nil,
	)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("role", common.RoleAdminUser)

	VideoProxy(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "hello", recorder.Body.String())
}

func TestVideoProxyRejectsPendingSettlement(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	require.NoError(t, db.Create(&model.User{
		Id:       94021,
		Username: "kling-pending-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	baseURL := "https://kling.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      94022,
		Type:    constant.ChannelTypeKling,
		Key:     "access|secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "kling-pending-data-task",
		Platform:  constant.TaskPlatform("kling"),
		UserId:    94021,
		ChannelId: 94022,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSubmitted,
	}
	taskcommon.ApplyTaskSuccessResult(task, "data:video/mp4;base64,aGVsbG8=", "", "", time.Now().Unix(), false)
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskSettlementStatusPending, task.SettlementStatus)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.PublicStatus())
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/videos/"+task.TaskID+"/content",
		nil,
	)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("role", common.RoleAdminUser)

	VideoProxy(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "not completed")
	require.Contains(t, recorder.Body.String(), string(model.TaskStatusInProgress))
	require.NotContains(t, recorder.Body.String(), "hello")
}

func TestVideoProxyRejectsRetryableSettlementReview(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	require.NoError(t, db.Create(&model.User{
		Id:       94101,
		Username: "kling-review-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	baseURL := "https://kling.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      94102,
		Type:    constant.ChannelTypeKling,
		Key:     "access|secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "kling-review-data-task",
		Platform:  constant.TaskPlatform("kling"),
		UserId:    94101,
		ChannelId: 94102,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSubmitted,
	}
	taskcommon.ApplyTaskSuccessResult(task, "data:video/mp4;base64,aGVsbG8=", "", "", time.Now().Unix(), false)
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.NextPollAt = time.Now().Unix() + 60
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.PublicStatus())
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/videos/"+task.TaskID+"/content",
		nil,
	)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("role", common.RoleAdminUser)

	VideoProxy(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "not completed")
	require.Contains(t, recorder.Body.String(), string(model.TaskStatusInProgress))
	require.NotContains(t, recorder.Body.String(), "hello")
}

func TestVideoProxyRejectsUnrecoverableSettlementReview(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	require.NoError(t, db.Create(&model.User{
		Id:       94111,
		Username: "kling-review-parked-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	baseURL := "https://kling.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      94112,
		Type:    constant.ChannelTypeKling,
		Key:     "access|secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "kling-review-parked-data-task",
		Platform:  constant.TaskPlatform("kling"),
		UserId:    94111,
		ChannelId: 94112,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSubmitted,
	}
	taskcommon.ApplyTaskSuccessResult(task, "data:video/mp4;base64,aGVsbG8=", "", "", time.Now().Unix(), false)
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.NextPollAt = 0
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.Equal(t, model.TaskStatus(model.TaskStatusFailure), task.PublicStatus())
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/videos/"+task.TaskID+"/content",
		nil,
	)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("role", common.RoleAdminUser)

	VideoProxy(ctx)

	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "not completed")
	require.Contains(t, recorder.Body.String(), string(model.TaskStatusFailure))
	require.NotContains(t, recorder.Body.String(), "hello")
}

func TestTaskArtifactContentRejectsRetryableSettlementReview(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	require.NoError(t, db.Create(&model.User{
		Id:       94121,
		Username: "kling-artifact-review-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	baseURL := "https://kling.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      94122,
		Type:    constant.ChannelTypeKling,
		Key:     "access|secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "kling-artifact-review-data-task",
		Platform:  constant.TaskPlatform("kling"),
		UserId:    94121,
		ChannelId: 94122,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSubmitted,
	}
	taskcommon.ApplyTaskSuccessResult(task, "data:video/mp4;base64,aGVsbG8=", "", "", time.Now().Unix(), false)
	task.SettlementStatus = model.TaskSettlementStatusReview
	task.NextPollAt = time.Now().Unix() + 60
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.PublicStatus())
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/tasks/"+task.TaskID+"/artifacts/video/content",
		nil,
	)
	ctx.Params = gin.Params{
		{Key: "key", Value: task.TaskID},
		{Key: "artifact_key", Value: "video"},
	}
	ctx.Set("role", common.RoleAdminUser)

	TaskArtifactContent(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	require.Contains(t, recorder.Body.String(), "artifact_not_ready")
	require.NotContains(t, recorder.Body.String(), "hello")
}

func TestLegacyVideoAvailableFalseDuringRetryableSettlementReview(t *testing.T) {
	task := &model.Task{
		TaskID:           "kling-legacy-review",
		Platform:         constant.TaskPlatform("kling"),
		Action:           constant.TaskActionTextToVideo,
		Status:           model.TaskStatusSuccess,
		SettlementStatus: model.TaskSettlementStatusReview,
		NextPollAt:       time.Now().Unix() + 60,
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://cdn.example/video.mp4",
		},
	}

	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), task.PublicStatus())
	require.False(t, legacyVideoAvailable(task))
}

func TestVideoProxyServesInlineVideoWhenChannelMemoryCacheMisses(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	oldMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = oldMemoryCacheEnabled
	})

	oldServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://media.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
	})

	require.NoError(t, db.Create(&model.User{
		Id:       94111,
		Username: "kling-cache-miss-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)

	baseURL := "https://kling.cachemiss.example"
	require.NoError(t, db.Create(&model.Channel{
		Id:      94112,
		Type:    constant.ChannelTypeKling,
		Key:     "access|secret",
		BaseURL: &baseURL,
		Status:  common.ChannelStatusEnabled,
	}).Error)

	task := &model.Task{
		TaskID:    "kling-cache-miss-data-task",
		Platform:  constant.TaskPlatform("kling"),
		UserId:    94111,
		ChannelId: 94112,
		Action:    constant.TaskActionTextToVideo,
		Status:    model.TaskStatusSubmitted,
	}
	taskcommon.ApplyTaskSuccessResult(task, "data:video/mp4;base64,aGVsbG8=", "", "", time.Now().Unix(), false)
	task.SettlementStatus = model.TaskSettlementStatusSettled
	require.Equal(t, model.TaskStatus(model.TaskStatusSuccess), task.Status)
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/v1/videos/"+task.TaskID+"/content",
		nil,
	)
	ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
	ctx.Set("role", common.RoleAdminUser)

	VideoProxy(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "hello", recorder.Body.String())
	require.NotContains(t, recorder.Body.String(), "artifact_plugin_unavailable")
}

func TestGetTaskForArtifactRequestRejectsOtherAPIToken(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, db.Create(&model.User{
		Id:       93011,
		Username: "token-bound-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	task := &model.Task{
		TaskID: "token-bound-task",
		UserId: 93011,
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			TokenId:   80,
			ResultURL: "https://cdn.example/video.mp4",
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
	ctx.Set("id", 93011)
	ctx.Set("token_id", 81)
	ctx.Set("role", common.RoleCommonUser)

	got, exists, err := getTaskForArtifactRequest(ctx, task.TaskID)
	require.NoError(t, err)
	require.False(t, exists)
	require.Nil(t, got)
}

func TestGetTaskForArtifactRequestAllowsMatchingAPIToken(t *testing.T) {
	db := setupTaskVideoContentTestDB(t)
	gin.SetMode(gin.TestMode)

	require.NoError(t, db.Create(&model.User{
		Id:       93012,
		Username: "token-match-owner",
		Password: "password123",
		Status:   common.UserStatusEnabled,
	}).Error)
	task := &model.Task{
		TaskID: "token-match-task",
		UserId: 93012,
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			TokenId:   80,
			ResultURL: "https://cdn.example/video.mp4",
		},
	}
	require.NoError(t, db.Create(task).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
	ctx.Set("id", 93012)
	ctx.Set("token_id", 80)
	ctx.Set("role", common.RoleCommonUser)

	got, exists, err := getTaskForArtifactRequest(ctx, task.TaskID)
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, task.TaskID, got.TaskID)
}

func TestValidateTaskMediaURLRejectsPrivateIPWhenProxyIsConfigured(t *testing.T) {
	fetchSetting := system_setting.GetFetchSetting()
	oldFetchSetting := *fetchSetting
	t.Cleanup(func() {
		*fetchSetting = oldFetchSetting
	})
	fetchSetting.EnableSSRFProtection = true
	fetchSetting.AllowPrivateIp = false
	fetchSetting.ApplyIPFilterForDomain = false
	fetchSetting.AllowedPorts = []string{"80", "443", "8080"}

	err := validateTaskMediaURL("http://127.0.0.1:8080/video.mp4", "http://proxy.example:8080")
	require.Error(t, err)
	require.Contains(t, err.Error(), "private")
}
