package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	taskArtifactBackendS3 = "s3"
	maxTaskArtifactBytes  = 512 << 20
)

var taskArtifactBucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// StoredArtifactRef describes a persisted artifact object.
type StoredArtifactRef struct {
	Backend   string
	Bucket    string
	ObjectKey string
	Type      string
	MimeType  string
	Size      int64
}

// TaskArtifactStore is the persistence boundary for generated artifact bytes.
type TaskArtifactStore interface {
	Enabled() bool
	List(task *model.Task) []types.TaskArtifact
	Resolve(task *model.Task, artifactKey string) (*StoredArtifactRef, error)
	Persist(ctx context.Context, task *model.Task, artifact types.TaskArtifact, content io.Reader) (*StoredArtifactRef, error)
	Serve(c *gin.Context, task *model.Task, ref *StoredArtifactRef) error
}

var (
	ErrTaskArtifactStoreDisabled = errors.New("task artifact store is disabled")
	ErrTaskArtifactNotFound      = errors.New("task artifact is not stored")
)

type disabledArtifactStore struct{}

func (disabledArtifactStore) Enabled() bool { return false }

func (disabledArtifactStore) List(*model.Task) []types.TaskArtifact { return nil }

func (disabledArtifactStore) Resolve(*model.Task, string) (*StoredArtifactRef, error) {
	return nil, nil
}

func (disabledArtifactStore) Persist(context.Context, *model.Task, types.TaskArtifact, io.Reader) (*StoredArtifactRef, error) {
	return nil, ErrTaskArtifactStoreDisabled
}

func (disabledArtifactStore) Serve(*gin.Context, *model.Task, *StoredArtifactRef) error {
	return ErrTaskArtifactStoreDisabled
}

type s3ArtifactStore struct {
	config     system_setting.TaskArtifactStoreConfig
	endpoint   *url.URL
	httpClient *http.Client
}

var (
	taskArtifactStoreMu   sync.RWMutex
	taskArtifactStore     TaskArtifactStore = &disabledArtifactStore{}
	taskArtifactStoreOnce sync.Once
)

// ConfigureTaskArtifactStore initializes the configured backend. Upstream mode
// intentionally keeps the provider-proxy compatibility behavior.
func ConfigureTaskArtifactStore() error {
	config := system_setting.LoadTaskArtifactStoreConfig()
	var store TaskArtifactStore = &disabledArtifactStore{}
	if config.Mode == system_setting.TaskArtifactStoreModeS3 {
		parsed, err := url.Parse(config.S3Endpoint)
		if err != nil {
			return fmt.Errorf("parse task artifact S3 endpoint: %w", err)
		}
		store = &s3ArtifactStore{
			config:     config,
			endpoint:   parsed,
			httpClient: &http.Client{Timeout: 2 * time.Minute},
		}
	}
	taskArtifactStoreMu.Lock()
	taskArtifactStore = store
	taskArtifactStoreMu.Unlock()
	return nil
}

func init() {
	taskArtifactStoreOnce.Do(func() {
		if err := ConfigureTaskArtifactStore(); err != nil {
			taskArtifactStoreMu.Lock()
			taskArtifactStore = &disabledArtifactStore{}
			taskArtifactStoreMu.Unlock()
		}
	})
}

// GetTaskArtifactStore returns the process-wide configured artifact backend.
func GetTaskArtifactStore() TaskArtifactStore {
	taskArtifactStoreMu.RLock()
	store := taskArtifactStore
	taskArtifactStoreMu.RUnlock()
	return store
}

func (s *s3ArtifactStore) Enabled() bool { return true }

func (s *s3ArtifactStore) List(task *model.Task) []types.TaskArtifact {
	if task == nil || task.ID <= 0 || !validTaskArtifactPathSegment(task.TaskID) {
		return nil
	}
	keys := make([]string, 0, len(task.PrivateData.ArtifactRefs))
	for key := range task.PrivateData.ArtifactRefs {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	artifacts := make([]types.TaskArtifact, 0, len(keys))
	for _, key := range keys {
		ref := task.PrivateData.ArtifactRefs[key]
		if !validStoredTaskArtifactRef(task.TaskID, key, ref) {
			continue
		}
		artifactType := normalizeStoredTaskArtifactType(key, ref.Type)
		if artifactType == "" {
			continue
		}
		mimeType := strings.TrimSpace(ref.MimeType)
		if len(mimeType) > 255 || strings.ContainsAny(mimeType, "\r\n") {
			continue
		}
		artifacts = append(artifacts, types.TaskArtifact{
			Key: key, Type: artifactType, MimeType: mimeType,
		})
		if len(artifacts) == 64 {
			break
		}
	}
	return artifacts
}

func (s *s3ArtifactStore) objectKey(taskID, artifactKey string) string {
	prefix := strings.Trim(s.config.S3Prefix, "/")
	parts := []string{taskID, artifactKey}
	if prefix != "" {
		parts = append([]string{prefix}, parts...)
	}
	return strings.Join(parts, "/")
}

func (s *s3ArtifactStore) Resolve(task *model.Task, artifactKey string) (*StoredArtifactRef, error) {
	if task == nil || task.ID <= 0 || !validTaskArtifactPathSegment(task.TaskID) ||
		!validTaskArtifactPathSegment(artifactKey) {
		return nil, ErrTaskArtifactNotFound
	}
	ref, ok := task.PrivateData.ArtifactRefs[artifactKey]
	if !ok || !validStoredTaskArtifactRef(task.TaskID, artifactKey, ref) ||
		normalizeStoredTaskArtifactType(artifactKey, ref.Type) == "" {
		return nil, nil
	}
	return &StoredArtifactRef{
		Backend: ref.Backend, Bucket: ref.Bucket, ObjectKey: ref.ObjectKey,
		Type: ref.Type, MimeType: ref.MimeType, Size: ref.Size,
	}, nil
}

func (s *s3ArtifactStore) Persist(ctx context.Context, task *model.Task, artifact types.TaskArtifact, content io.Reader) (*StoredArtifactRef, error) {
	if task == nil || task.ID <= 0 || !validTaskArtifactPathSegment(task.TaskID) {
		return nil, errors.New("task artifact persistence requires a persisted task")
	}
	if content == nil {
		return nil, errors.New("task artifact content is nil")
	}
	if !validTaskArtifactPathSegment(artifact.Key) {
		return nil, errors.New("task artifact key is empty")
	}
	if !validTaskArtifactType(artifact.Type) {
		return nil, errors.New("task artifact type is invalid")
	}

	mimeType := strings.TrimSpace(artifact.MimeType)
	if len(mimeType) > 255 || strings.ContainsAny(mimeType, "\r\n") {
		return nil, errors.New("task artifact mime type is invalid")
	}
	if existing, err := s.Resolve(task, artifact.Key); err == nil && existing != nil {
		return existing, nil
	}

	objectKey := s.objectKey(task.TaskID, artifact.Key)
	counting := &countingReader{
		reader: io.LimitReader(content, maxTaskArtifactBytes+1),
	}
	if err := s.doObjectRequest(ctx, http.MethodPut, s.config.S3Bucket, objectKey, mimeType, counting); err != nil {
		return nil, err
	}
	if counting.count > maxTaskArtifactBytes {
		_ = s.deleteObject(context.Background(), s.config.S3Bucket, objectKey)
		return nil, fmt.Errorf("task artifact exceeds %d bytes", maxTaskArtifactBytes)
	}

	ref := &StoredArtifactRef{
		Backend:   taskArtifactBackendS3,
		Bucket:    s.config.S3Bucket,
		ObjectKey: objectKey,
		Type:      artifact.Type,
		MimeType:  mimeType,
		Size:      counting.count,
	}
	if err := s.saveTaskRef(ref, task, artifact.Key); err != nil {
		_ = s.deleteObject(context.Background(), s.config.S3Bucket, objectKey)
		return nil, err
	}
	return ref, nil
}

func (s *s3ArtifactStore) saveTaskRef(ref *StoredArtifactRef, task *model.Task, artifactKey string) error {
	if model.DB == nil {
		return errors.New("task artifact persistence database is unavailable")
	}
	return model.DB.Transaction(func(tx *gorm.DB) error {
		var current model.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id", "task_id", "private_data", "updated_at").
			Where("id = ?", task.ID).First(&current).Error; err != nil {
			return err
		}
		if current.PrivateData.ArtifactRefs == nil {
			current.PrivateData.ArtifactRefs = make(map[string]model.TaskArtifactStorageRef)
		}
		current.PrivateData.ArtifactRefs[artifactKey] = model.TaskArtifactStorageRef{
			Backend: ref.Backend, Bucket: ref.Bucket, ObjectKey: ref.ObjectKey,
			Type: ref.Type, MimeType: ref.MimeType, Size: ref.Size,
		}
		updatedAt := time.Now().Unix()
		if updatedAt <= current.UpdatedAt {
			updatedAt = current.UpdatedAt + 1
		}
		if err := tx.Model(&model.Task{}).Where("id = ?", task.ID).
			Updates(map[string]any{
				"private_data": current.PrivateData,
				"updated_at":   updatedAt,
			}).Error; err != nil {
			return err
		}
		task.PrivateData = current.PrivateData
		task.UpdatedAt = updatedAt
		return nil
	})
}

func (s *s3ArtifactStore) Serve(c *gin.Context, task *model.Task, ref *StoredArtifactRef) error {
	if c == nil || task == nil || ref == nil || ref.Backend != taskArtifactBackendS3 ||
		!validTaskArtifactPathSegment(task.TaskID) {
		return ErrTaskArtifactNotFound
	}
	matched := false
	for artifactKey, storedRef := range task.PrivateData.ArtifactRefs {
		if storedRef.Backend == ref.Backend && storedRef.Bucket == ref.Bucket &&
			storedRef.ObjectKey == ref.ObjectKey && validStoredTaskArtifactRef(task.TaskID, artifactKey, storedRef) {
			matched = true
			break
		}
	}
	if !matched {
		return ErrTaskArtifactNotFound
	}
	request := c.Request
	if request == nil {
		return errors.New("task artifact request is unavailable")
	}
	objectMethod := http.MethodGet
	if request.Method == http.MethodHead {
		objectMethod = http.MethodHead
	}
	objectRequest, err := s.newObjectRequest(request.Context(), objectMethod, ref.Bucket, ref.ObjectKey, "", nil)
	if err != nil {
		return err
	}
	for _, headerName := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since"} {
		if value := strings.TrimSpace(request.Header.Get(headerName)); value != "" {
			objectRequest.Header.Set(headerName, value)
		}
	}
	response, err := s.httpClient.Do(objectRequest)
	if err != nil {
		return fmt.Errorf("S3 artifact GET request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return ErrTaskArtifactNotFound
	}
	if (response.StatusCode < 200 || response.StatusCode >= 300) &&
		response.StatusCode != http.StatusNotModified &&
		response.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return fmt.Errorf("S3 artifact GET returned status %d", response.StatusCode)
	}
	if ref.MimeType != "" {
		c.Header("Content-Type", ref.MimeType)
	} else if contentType := response.Header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	for _, header := range []string{"Content-Length", "Content-Range", "ETag", "Last-Modified"} {
		if value := response.Header.Get(header); value != "" {
			c.Header(header, value)
		}
	}
	c.Header("Cache-Control", "private, no-store")
	c.Status(response.StatusCode)
	if request.Method == http.MethodHead || response.StatusCode == http.StatusNotModified ||
		response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return nil
	}
	_, err = io.Copy(c.Writer, response.Body)
	return err
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count += int64(n)
	return n, err
}

func (s *s3ArtifactStore) deleteObject(ctx context.Context, bucket, objectKey string) error {
	if err := s.doObjectRequest(ctx, http.MethodDelete, bucket, objectKey, "", nil); err != nil {
		return err
	}
	return nil
}

func (s *s3ArtifactStore) doObjectRequest(ctx context.Context, method, bucket, objectKey, contentType string, body io.Reader) error {
	request, err := s.newObjectRequest(ctx, method, bucket, objectKey, contentType, body)
	if err != nil {
		return err
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("S3 artifact %s request failed: %w", method, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("S3 artifact %s returned status %d", method, response.StatusCode)
	}
	return nil
}

func (s *s3ArtifactStore) newObjectRequest(ctx context.Context, method, bucket, objectKey, contentType string, body io.Reader) (*http.Request, error) {
	if s.endpoint == nil {
		return nil, errors.New("S3 endpoint is unavailable")
	}
	if !validTaskArtifactBucket(bucket) || !validStoredTaskArtifactObjectKey(objectKey) {
		return nil, ErrTaskArtifactNotFound
	}
	objectURL := *s.endpoint
	objectURL.Path = strings.TrimRight(s.endpoint.Path, "/") + "/" +
		bucket + "/" + objectKey
	objectURL.RawPath = strings.TrimRight(s.endpoint.EscapedPath(), "/") + "/" +
		url.PathEscape(bucket) + "/" + escapeS3Key(objectKey)
	request, err := http.NewRequestWithContext(ctx, method, objectURL.String(), body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	signS3Request(request, s.config.S3Region, s.config.S3AccessKey, s.config.S3SecretKey)
	return request, nil
}

func validStoredTaskArtifactRef(taskID, artifactKey string, ref model.TaskArtifactStorageRef) bool {
	if !validTaskArtifactPathSegment(taskID) || !validTaskArtifactPathSegment(artifactKey) ||
		ref.Backend != taskArtifactBackendS3 ||
		!validTaskArtifactBucket(ref.Bucket) ||
		!validStoredTaskArtifactObjectKey(ref.ObjectKey) {
		return false
	}
	suffix := "/" + taskID + "/" + artifactKey
	return strings.HasSuffix(ref.ObjectKey, suffix) || ref.ObjectKey == strings.TrimPrefix(suffix, "/")
}

func validTaskArtifactBucket(bucket string) bool {
	return bucket == strings.TrimSpace(bucket) &&
		taskArtifactBucketPattern.MatchString(bucket) &&
		!strings.Contains(bucket, "..")
}

func validStoredTaskArtifactObjectKey(objectKey string) bool {
	if objectKey == "" || objectKey != strings.TrimSpace(objectKey) ||
		strings.HasPrefix(objectKey, "/") || strings.HasSuffix(objectKey, "/") ||
		strings.Contains(objectKey, "\\") || len(objectKey) > 1024 {
		return false
	}
	for _, part := range strings.Split(objectKey, "/") {
		if !validTaskArtifactPathSegment(part) {
			return false
		}
	}
	return true
}

func validTaskArtifactPathSegment(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." ||
		strings.ContainsAny(value, "/\\") {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func normalizeStoredTaskArtifactType(key, artifactType string) string {
	artifactType = strings.TrimSpace(artifactType)
	if artifactType == "" {
		switch key {
		case "video":
			return "video"
		case "audio":
			return "audio"
		case "image", "image-result":
			return "image"
		default:
			return "file"
		}
	}
	switch artifactType {
	case "video", "audio", "image", "file":
		return artifactType
	}
	return ""
}

func validTaskArtifactType(artifactType string) bool {
	switch strings.TrimSpace(artifactType) {
	case "video", "audio", "image", "file":
		return strings.TrimSpace(artifactType) == artifactType
	default:
		return false
	}
}

func escapeS3Key(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func signS3Request(request *http.Request, region, accessKey, secretKey string) {
	now := time.Now().UTC()
	payloadHash := "UNSIGNED-PAYLOAD"
	if request.Body == nil {
		payloadHash = sha256Hex(nil)
	}
	request.Header.Set("x-amz-content-sha256", payloadHash)
	request.Header.Set("x-amz-date", now.Format("20060102T150405Z"))
	canonicalHeaders := "host:" + canonicalHeaderValue(request.URL.Host) + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + request.Header.Get("x-amz-date") + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	if contentType := request.Header.Get("Content-Type"); contentType != "" {
		canonicalHeaders = "content-type:" + canonicalHeaderValue(contentType) + "\n" + canonicalHeaders
		signedHeaders = "content-type;" + signedHeaders
	}
	canonicalRequest := strings.Join([]string{
		request.Method,
		canonicalURI(request.URL),
		canonicalQuery(request.URL),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	date := now.Format("20060102")
	scope := date + "/" + region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + request.Header.Get("x-amz-date") + "\n" +
		scope + "\n" + sha256Hex([]byte(canonicalRequest))
	signingKey := hmacSHA256(
		hmacSHA256(
			hmacSHA256(
				hmacSHA256([]byte("AWS4"+secretKey), []byte(date)),
				[]byte(region),
			),
			[]byte("s3"),
		),
		[]byte("aws4_request"),
	)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+accessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func canonicalURI(requestURL *url.URL) string {
	if requestURL.EscapedPath() == "" {
		return "/"
	}
	return requestURL.EscapedPath()
}

func canonicalQuery(requestURL *url.URL) string {
	return requestURL.Query().Encode()
}

func canonicalHeaderValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, value []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(value)
	return mac.Sum(nil)
}

var _ TaskArtifactStore = (*s3ArtifactStore)(nil)
