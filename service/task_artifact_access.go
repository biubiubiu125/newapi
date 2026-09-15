package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

const (
	TaskArtifactAccessQueryParameter = "access"
	TaskArtifactResultArtifactKey    = "image-result"
	MidjourneyImageArtifactKey       = "mj-image"
	taskArtifactAccessVersion        = "v2"
	maxTaskArtifactTaskIDLength      = 191
	maxTaskArtifactKeyLength         = 128
)

var ErrTaskArtifactAccessInvalid = errors.New("task artifact access is invalid")

func taskArtifactAccessMessage(taskID, artifactKey string, expiresAt int64) []byte {
	return []byte(taskArtifactAccessVersion + "\x00" + taskID + "\x00" + artifactKey + "\x00" + strconv.FormatInt(expiresAt, 10))
}

func taskArtifactAccessTTL() time.Duration {
	seconds := system_setting.LoadTaskArtifactStoreConfig().S3PresignTTLSeconds
	if seconds <= 0 || seconds > system_setting.MaxTaskArtifactStorePresignTTLSeconds {
		seconds = system_setting.DefaultTaskArtifactStorePresignTTLSeconds
	}
	return time.Duration(seconds) * time.Second
}

// IssueTaskArtifactAccess creates a time-limited capability bound to exactly
// one public task ID and artifact key. It contains no user or upstream data.
func IssueTaskArtifactAccess(taskID, artifactKey string) (string, error) {
	return issueTaskArtifactAccessAt(taskID, artifactKey, time.Now())
}

func issueTaskArtifactAccessAt(taskID, artifactKey string, now time.Time) (string, error) {
	taskID = strings.TrimSpace(taskID)
	artifactKey = strings.TrimSpace(artifactKey)
	if !validTaskArtifactPathSegment(taskID) || len(taskID) > maxTaskArtifactTaskIDLength ||
		!validTaskArtifactPathSegment(artifactKey) || len(artifactKey) > maxTaskArtifactKeyLength ||
		common.CryptoSecret == "" {
		return "", ErrTaskArtifactAccessInvalid
	}

	expiresAt := now.Add(taskArtifactAccessTTL()).Unix()
	mac := hmac.New(sha256.New, []byte(common.CryptoSecret))
	_, _ = mac.Write(taskArtifactAccessMessage(taskID, artifactKey, expiresAt))
	return strconv.FormatInt(expiresAt, 10) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// VerifyTaskArtifactAccess verifies the route binding and expiry without
// reading task, user, or token state. Signature comparison is constant-time.
func VerifyTaskArtifactAccess(access, taskID, artifactKey string) bool {
	return verifyTaskArtifactAccessAt(access, taskID, artifactKey, time.Now())
}

func verifyTaskArtifactAccessAt(access, taskID, artifactKey string, now time.Time) bool {
	taskID = strings.TrimSpace(taskID)
	artifactKey = strings.TrimSpace(artifactKey)
	parts := strings.Split(access, ".")
	if len(parts) != 2 ||
		!validTaskArtifactPathSegment(taskID) || len(taskID) > maxTaskArtifactTaskIDLength ||
		!validTaskArtifactPathSegment(artifactKey) || len(artifactKey) > maxTaskArtifactKeyLength ||
		common.CryptoSecret == "" {
		return false
	}

	expiresAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || expiresAt < now.Unix() {
		return false
	}
	actualSignature, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(actualSignature) != sha256.Size {
		return false
	}

	mac := hmac.New(sha256.New, []byte(common.CryptoSecret))
	_, _ = mac.Write(taskArtifactAccessMessage(taskID, artifactKey, expiresAt))
	return hmac.Equal(actualSignature, mac.Sum(nil))
}

// ValidateTaskArtifactBaseURL validates configuration syntax only. It
// deliberately performs no DNS lookup or reachability probe.
func ValidateTaskArtifactBaseURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("task artifact base URL is empty")
	}
	if raw != trimmed {
		return errors.New("task artifact base URL must not contain surrounding whitespace")
	}
	raw = trimmed
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return errors.New("task artifact base URL is invalid")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return errors.New("task artifact base URL must use http or https")
	}
	if parsed.Host == "" || parsed.User != nil || parsed.Opaque != "" {
		return errors.New("task artifact base URL must contain a host and no userinfo")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(raw, "#") {
		return errors.New("task artifact base URL must not contain a query or fragment")
	}
	return nil
}

// BuildTaskArtifactContentURL returns an absolute capability URL for a
// generic task artifact. The legacy image-result URL below remains separate
// for compatibility with existing clients and routers.
func BuildTaskArtifactContentURL(taskID, artifactKey string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	artifactKey = strings.TrimSpace(artifactKey)
	if !validTaskArtifactPathSegment(taskID) || len(taskID) > maxTaskArtifactTaskIDLength ||
		!validTaskArtifactPathSegment(artifactKey) || len(artifactKey) > maxTaskArtifactKeyLength {
		return "", ErrTaskArtifactAccessInvalid
	}
	baseAddress := strings.TrimSpace(system_setting.TaskPublicAddress)
	if baseAddress == "" {
		baseAddress = strings.TrimSpace(system_setting.ServerAddress)
	}
	if err := ValidateTaskArtifactBaseURL(baseAddress); err != nil {
		return "", err
	}
	baseURL, err := url.Parse(baseAddress)
	if err != nil {
		return "", err
	}
	access, err := IssueTaskArtifactAccess(taskID, artifactKey)
	if err != nil {
		return "", err
	}
	basePath := strings.TrimRight(baseURL.Path, "/")
	escapedBasePath := strings.TrimRight(baseURL.EscapedPath(), "/")
	suffixPath := fmt.Sprintf("/v1/tasks/%s/artifacts/%s/content", taskID, artifactKey)
	escapedSuffixPath := fmt.Sprintf(
		"/v1/tasks/%s/artifacts/%s/content",
		url.PathEscape(taskID),
		url.PathEscape(artifactKey),
	)
	baseURL.Path = basePath + suffixPath
	baseURL.RawPath = escapedBasePath + escapedSuffixPath
	query := baseURL.Query()
	query.Set(TaskArtifactAccessQueryParameter, access)
	baseURL.RawQuery = query.Encode()
	return baseURL.String(), nil
}

// BuildTaskArtifactResultURL returns the signed URL for the existing public
// image-task result endpoint. TaskPublicAddress overrides ServerAddress when
// configured; the latter keeps the optional setting useful on single-domain
// deployments. This path is registered by the image-task router.
func BuildTaskArtifactResultURL(taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if !validTaskArtifactPathSegment(taskID) || len(taskID) > maxTaskArtifactTaskIDLength {
		return "", ErrTaskArtifactAccessInvalid
	}
	baseAddress := strings.TrimSpace(system_setting.TaskPublicAddress)
	if baseAddress == "" {
		baseAddress = strings.TrimSpace(system_setting.ServerAddress)
	}
	if err := ValidateTaskArtifactBaseURL(baseAddress); err != nil {
		return "", err
	}
	baseURL, err := url.Parse(baseAddress)
	if err != nil {
		return "", err
	}
	access, err := IssueTaskArtifactAccess(taskID, TaskArtifactResultArtifactKey)
	if err != nil {
		return "", err
	}

	basePath := strings.TrimRight(baseURL.Path, "/")
	escapedBasePath := strings.TrimRight(baseURL.EscapedPath(), "/")
	suffixPath := fmt.Sprintf("/v1/image-tasks/%s/result", taskID)
	escapedSuffixPath := fmt.Sprintf("/v1/image-tasks/%s/result", url.PathEscape(taskID))
	baseURL.Path = basePath + suffixPath
	baseURL.RawPath = escapedBasePath + escapedSuffixPath
	query := baseURL.Query()
	query.Set(TaskArtifactAccessQueryParameter, access)
	baseURL.RawQuery = query.Encode()
	return baseURL.String(), nil
}

func MidjourneyForwardImageURL(mjID string) string {
	mjID = strings.TrimSpace(mjID)
	baseAddress := strings.TrimRight(strings.TrimSpace(system_setting.ServerAddress), "/")
	imageURL := baseAddress + "/mj/image/" + mjID
	access, err := IssueTaskArtifactAccess(mjID, MidjourneyImageArtifactKey)
	if err != nil {
		return imageURL
	}
	query := url.Values{}
	query.Set(TaskArtifactAccessQueryParameter, access)
	return imageURL + "?" + query.Encode()
}
