package service

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestS3ArtifactStorePersistsAndServesStoredArtifact(t *testing.T) {
	var (
		mutex       sync.Mutex
		stored      []byte
		lastMethod  string
		lastPath    string
		lastHeaders http.Header
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mutex.Lock()
		lastMethod = r.Method
		lastPath = r.URL.Path
		lastHeaders = r.Header.Clone()
		mutex.Unlock()

		switch r.Method {
		case http.MethodPut:
			var err error
			stored, err = io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read uploaded artifact: %v", err)
				http.Error(w, "failed to read upload", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		case http.MethodHead:
			if len(stored) == 0 {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", "6")
			w.Header().Set("ETag", `"artifact-etag"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if len(stored) == 0 {
				http.NotFound(w, r)
				return
			}
			body := stored
			status := http.StatusOK
			if r.Header.Get("Range") == "bytes=1-" {
				body = stored[1:]
				status = http.StatusPartialContent
				w.Header().Set("Content-Range", "bytes 1-5/6")
			}
			w.Header().Set("Content-Type", "video/mp4")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			w.WriteHeader(status)
			_, err := w.Write(body)
			if err != nil {
				t.Errorf("write artifact response: %v", err)
			}
		case http.MethodDelete:
			stored = nil
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	database, err := gorm.Open(sqlite.Open("file:task-artifact-store?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Task{}))
	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	store := &s3ArtifactStore{
		config:     systemTaskArtifactStoreTestConfig(),
		endpoint:   endpoint,
		httpClient: server.Client(),
	}
	task := &model.Task{
		TaskID:      "task_s3_artifact",
		Status:      model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{},
	}
	require.NoError(t, database.Create(task).Error)

	ref, err := store.Persist(
		t.Context(),
		task,
		types.TaskArtifact{Key: "video", Type: "video", MimeType: "video/mp4"},
		strings.NewReader("abcdef"),
	)
	require.NoError(t, err)
	require.Equal(t, "task-artifacts/task_s3_artifact/video", ref.ObjectKey)
	require.EqualValues(t, 6, ref.Size)
	require.Equal(t, map[string]model.TaskArtifactStorageRef{
		"video": {
			Backend:   taskArtifactBackendS3,
			Bucket:    "newapi-artifacts",
			ObjectKey: "task-artifacts/task_s3_artifact/video",
			Type:      "video",
			MimeType:  "video/mp4",
			Size:      6,
		},
	}, task.PrivateData.ArtifactRefs)

	resolved, err := store.Resolve(task, "video")
	require.NoError(t, err)
	require.Equal(t, ref, resolved)

	gin.SetMode(gin.TestMode)
	getRecorder := httptest.NewRecorder()
	getContext, _ := gin.CreateTestContext(getRecorder)
	getContext.Request = httptest.NewRequest(http.MethodGet, "/artifact", nil)
	require.NoError(t, store.Serve(getContext, task, ref))
	require.Equal(t, http.StatusOK, getRecorder.Code)
	require.Equal(t, "abcdef", getRecorder.Body.String())
	require.Equal(t, "video/mp4", getRecorder.Header().Get("Content-Type"))

	rangeRecorder := httptest.NewRecorder()
	rangeContext, _ := gin.CreateTestContext(rangeRecorder)
	rangeContext.Request = httptest.NewRequest(http.MethodGet, "/artifact", nil)
	rangeContext.Request.Header.Set("Range", "bytes=1-")
	require.NoError(t, store.Serve(rangeContext, task, ref))
	require.Equal(t, http.StatusPartialContent, rangeRecorder.Code)
	require.Equal(t, "bcdef", rangeRecorder.Body.String())
	require.Equal(t, "bytes 1-5/6", rangeRecorder.Header().Get("Content-Range"))

	headRecorder := httptest.NewRecorder()
	headContext, _ := gin.CreateTestContext(headRecorder)
	headContext.Request = httptest.NewRequest(http.MethodHead, "/artifact", nil)
	require.NoError(t, store.Serve(headContext, task, ref))
	require.Equal(t, http.StatusOK, headRecorder.Code)
	require.Empty(t, headRecorder.Body.Bytes())
	require.Equal(t, "6", headRecorder.Header().Get("Content-Length"))

	mutex.Lock()
	require.Equal(t, http.MethodHead, lastMethod)
	require.Equal(t, "/newapi-artifacts/task-artifacts/task_s3_artifact/video", lastPath)
	require.NotEmpty(t, lastHeaders.Get("Authorization"))
	require.Equal(t, "host;x-amz-content-sha256;x-amz-date", signedHeaderList(lastHeaders.Get("Authorization")))
	mutex.Unlock()
}

func TestS3ArtifactStorePreservesFutureTaskUpdatedAt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.NotFound(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	database, err := gorm.Open(sqlite.Open("file:task-artifact-store-updated-at?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Task{}))
	previousDB := model.DB
	model.DB = database
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)
	store := &s3ArtifactStore{
		config:     systemTaskArtifactStoreTestConfig(),
		endpoint:   endpoint,
		httpClient: server.Client(),
	}
	task := &model.Task{
		TaskID: "task_s3_artifact_updated_at",
		Status: model.TaskStatusSuccess,
	}
	require.NoError(t, database.Create(task).Error)

	futureUpdatedAt := time.Now().Unix() + 3600
	require.NoError(t, database.Model(&model.Task{}).
		Where("id = ?", task.ID).
		Update("updated_at", futureUpdatedAt).Error)

	_, err = store.Persist(
		t.Context(),
		task,
		types.TaskArtifact{Key: "video", Type: "video", MimeType: "video/mp4"},
		strings.NewReader("abcdef"),
	)
	require.NoError(t, err)

	var reloaded model.Task
	require.NoError(t, database.First(&reloaded, task.ID).Error)
	require.GreaterOrEqual(t, reloaded.UpdatedAt, futureUpdatedAt)
}

func TestS3ArtifactStoreRejectsUnsafeObjectPathSegments(t *testing.T) {
	store := &s3ArtifactStore{config: systemTaskArtifactStoreTestConfig()}
	for _, testCase := range []struct {
		name        string
		taskID      string
		artifactKey string
	}{
		{name: "task slash", taskID: "task/escape", artifactKey: "video"},
		{name: "task dot segment", taskID: "..", artifactKey: "video"},
		{name: "artifact slash", taskID: "safe-task", artifactKey: "video/other"},
		{name: "artifact dot segment", taskID: "safe-task", artifactKey: ".."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			task := &model.Task{ID: 1, TaskID: testCase.taskID}
			_, err := store.Persist(t.Context(), task, types.TaskArtifact{Key: testCase.artifactKey}, strings.NewReader("content"))
			require.Error(t, err)
		})
	}
}

func TestS3ArtifactStoreListRejectsInvalidStoredArtifactType(t *testing.T) {
	store := &s3ArtifactStore{config: systemTaskArtifactStoreTestConfig()}
	task := &model.Task{
		ID:     1,
		TaskID: "task_invalid_stored_artifact_type",
		PrivateData: model.TaskPrivateData{
			ArtifactRefs: map[string]model.TaskArtifactStorageRef{
				"video": {
					Backend:   taskArtifactBackendS3,
					Bucket:    "newapi-artifacts",
					ObjectKey: "task-artifacts/task_invalid_stored_artifact_type/video",
					Type:      "script",
				},
			},
		},
	}

	require.Empty(t, store.List(task))
}

func TestS3ArtifactStoreRejectsInvalidArtifactTypeBeforeUpload(t *testing.T) {
	store := &s3ArtifactStore{config: systemTaskArtifactStoreTestConfig()}
	task := &model.Task{ID: 1, TaskID: "task_invalid_artifact_type"}

	_, err := store.Persist(t.Context(), task, types.TaskArtifact{
		Key: "video", Type: "",
	}, strings.NewReader("content"))
	require.ErrorContains(t, err, "task artifact type is invalid")
}

func TestS3ArtifactStoreResolveRejectsInvalidStoredArtifactType(t *testing.T) {
	store := &s3ArtifactStore{config: systemTaskArtifactStoreTestConfig()}
	task := &model.Task{
		ID:     1,
		TaskID: "task_invalid_resolve_type",
		PrivateData: model.TaskPrivateData{ArtifactRefs: map[string]model.TaskArtifactStorageRef{
			"video": {
				Backend:   taskArtifactBackendS3,
				Bucket:    "newapi-artifacts",
				ObjectKey: "task-artifacts/task_invalid_resolve_type/video",
				Type:      "script",
			},
		}},
	}

	ref, err := store.Resolve(task, "video")
	require.NoError(t, err)
	require.Nil(t, ref)
}

func TestS3ArtifactStoreListCapsStoredArtifactCount(t *testing.T) {
	store := &s3ArtifactStore{config: systemTaskArtifactStoreTestConfig()}
	refs := make(map[string]model.TaskArtifactStorageRef, 65)
	for index := 0; index < 65; index++ {
		key := fmt.Sprintf("artifact-%02d", index)
		refs[key] = model.TaskArtifactStorageRef{
			Backend:   taskArtifactBackendS3,
			Bucket:    "newapi-artifacts",
			ObjectKey: "task-artifacts/task_stored_artifact_count/" + key,
			Type:      "file",
		}
	}
	task := &model.Task{
		ID: 1, TaskID: "task_stored_artifact_count",
		PrivateData: model.TaskPrivateData{ArtifactRefs: refs},
	}

	require.Len(t, store.List(task), 64)
}

func TestS3ArtifactStoreUsesStoredBucketAndPrefixAfterConfigurationChanges(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("legacy"))
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL)
	require.NoError(t, err)

	store := &s3ArtifactStore{
		config:     systemTaskArtifactStoreTestConfig(),
		endpoint:   endpoint,
		httpClient: server.Client(),
	}
	task := &model.Task{
		ID:     1,
		TaskID: "task_legacy_ref",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ArtifactRefs: map[string]model.TaskArtifactStorageRef{
				"video": {
					Backend:   taskArtifactBackendS3,
					Bucket:    "legacy-artifacts",
					ObjectKey: "legacy-prefix/task_legacy_ref/video",
					Type:      "video",
					MimeType:  "video/mp4",
				},
			},
		},
	}

	require.Len(t, store.List(task), 1)
	ref, err := store.Resolve(task, "video")
	require.NoError(t, err)
	require.NotNil(t, ref)
	require.Equal(t, "legacy-artifacts", ref.Bucket)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/artifact", nil)
	require.NoError(t, store.Serve(context, task, ref))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "legacy", recorder.Body.String())
	require.Equal(t, "/legacy-artifacts/legacy-prefix/task_legacy_ref/video", requestedPath)
}

func systemTaskArtifactStoreTestConfig() system_setting.TaskArtifactStoreConfig {
	return system_setting.TaskArtifactStoreConfig{
		Mode:                system_setting.TaskArtifactStoreModeS3,
		S3Bucket:            "newapi-artifacts",
		S3Region:            "us-east-1",
		S3AccessKey:         "test-access",
		S3SecretKey:         "test-secret",
		S3Prefix:            "task-artifacts",
		S3PresignTTLSeconds: 900,
	}
}

func signedHeaderList(authorization string) string {
	const marker = "SignedHeaders="
	index := strings.Index(authorization, marker)
	if index < 0 {
		return ""
	}
	value := authorization[index+len(marker):]
	if comma := strings.IndexByte(value, ','); comma >= 0 {
		value = value[:comma]
	}
	return strings.TrimSpace(value)
}
