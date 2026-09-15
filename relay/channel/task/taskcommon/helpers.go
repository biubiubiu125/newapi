package taskcommon

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// UnmarshalMetadata converts a map[string]any metadata to a typed struct via JSON round-trip.
// This replaces the repeated pattern: json.Marshal(metadata) → json.Unmarshal(bytes, &target).
func UnmarshalMetadata(metadata map[string]any, target any) error {
	if metadata == nil {
		return nil
	}
	// Prevent metadata from overriding model fields to avoid billing bypass.
	delete(metadata, "model")
	metaBytes, err := common.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata failed: %w", err)
	}
	if err := common.Unmarshal(metaBytes, target); err != nil {
		return fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	return nil
}

// DefaultString returns val if non-empty, otherwise fallback.
func DefaultString(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

// DefaultInt returns val if non-zero, otherwise fallback.
func DefaultInt(val, fallback int) int {
	if val == 0 {
		return fallback
	}
	return val
}

// EncodeLocalTaskID encodes an upstream operation name to a URL-safe base64 string.
// Used by Gemini/Vertex to store upstream names as task IDs.
func EncodeLocalTaskID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// DecodeLocalTaskID decodes a base64-encoded upstream operation name.
func DecodeLocalTaskID(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// BuildProxyURL constructs the video proxy URL using the public task ID.
// e.g., "https://your-server.com/v1/videos/task_xxxx/content"
func BuildProxyURL(taskID string) string {
	return fmt.Sprintf("%s/v1/videos/%s/content", system_setting.ServerAddress, taskID)
}

const (
	MissingResultURLReason     = "video result url missing"
	InlineResultTooLargeReason = "video inline result too large"
	MaxInlineResultURLBytes    = 64 << 20
)

// AllowsEmptyProxyResult reports whether a successful adaptor result may persist
// a task content proxy URL when the provider omitted a direct or data: URL.
// Only Sora-like OpenAI video channels (and Suno) fetch media by public task ID.
// Plugin tasks store a non-numeric platform key, so channelType is the authority
// when the platform string is not a channel-type integer.
func AllowsEmptyProxyResult(platform constant.TaskPlatform, channelType int) bool {
	if platform == constant.TaskPlatformSuno {
		return true
	}
	switch channelType {
	case constant.ChannelTypeSora, constant.ChannelTypeOpenAI, constant.ChannelTypeNewAPI:
		return true
	}
	parsed, err := strconv.Atoi(string(platform))
	if err != nil {
		switch strings.ToLower(strings.TrimSpace(string(platform))) {
		case "sora", "openai", string(constant.TaskPlatformSuno):
			return true
		default:
			return false
		}
	}
	switch parsed {
	case constant.ChannelTypeSora, constant.ChannelTypeOpenAI, constant.ChannelTypeNewAPI:
		return true
	default:
		return false
	}
}

// IsGCSURI reports whether value uses the GCS object scheme.
// Clients cannot fetch gs:// URLs, so they stay behind the task content proxy.
func IsGCSURI(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "gs://")
}

// IsUnsignedGCSHTTPS reports whether value is a GCS HTTPS object URL without a
// signature. Gemini API keys cannot fetch these objects.
func IsUnsignedGCSHTTPS(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https":
	default:
		return false
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Hostname())) {
	case "storage.googleapis.com", "storage.cloud.google.com":
		return !gcsHTTPSIsSigned(parsed)
	default:
		return false
	}
}

func gcsHTTPSIsSigned(parsed *url.URL) bool {
	if parsed == nil {
		return false
	}
	query := parsed.Query()
	return strings.TrimSpace(query.Get("X-Goog-Signature")) != "" ||
		strings.TrimSpace(query.Get("X-Goog-Algorithm")) != "" ||
		strings.TrimSpace(query.Get("GoogleAccessId")) != "" ||
		strings.TrimSpace(query.Get("X-Goog-Credential")) != ""
}

// GCSURIToHTTPS converts gs://bucket/object to the GCS HTTPS object URL.
func GCSURIToHTTPS(uri string) (string, bool) {
	trimmed := strings.TrimSpace(uri)
	if !IsGCSURI(trimmed) {
		return "", false
	}
	rest := strings.TrimSpace(trimmed[len("gs://"):])
	bucket, object, ok := strings.Cut(rest, "/")
	if !ok || bucket == "" || strings.TrimSpace(object) == "" {
		return "", false
	}
	return "https://storage.googleapis.com/" + bucket + "/" + object, true
}

// ResolvePersistedResultURL maps adaptor URLs onto the stored ResultURL.
// Direct http(s), data:, and gs:// locators are stored as-is so content proxy
// can retrieve them. GetResultURL rewrites data:/gs:// to the public proxy path.
// allowEmptyProxy is for platforms like Sora that complete without a URL and
// expect VideoProxy to fetch by public task ID. Realtime/plugin success without
// a URL must not invent a proxy link.
func ResolvePersistedResultURL(taskID, url, remoteURL string, allowEmptyProxy bool) (string, bool) {
	resultURL := strings.TrimSpace(url)
	if resultURL == "" {
		resultURL = strings.TrimSpace(remoteURL)
	}
	switch {
	case resultURL != "":
		return resultURL, true
	case allowEmptyProxy:
		return BuildProxyURL(taskID), true
	default:
		return "", false
	}
}

// ApplyTaskSuccessResult writes a terminal success result, or converts the task
// to failure when no media URL exists and empty proxy URLs are not allowed.
func ApplyTaskSuccessResult(task *model.Task, url, remoteURL, reason string, now int64, allowEmptyProxy bool) {
	if task == nil {
		return
	}
	task.Progress = ProgressComplete
	if task.FinishTime == 0 {
		task.FinishTime = now
	}
	if resultURL, ok := ResolvePersistedResultURL(task.TaskID, url, remoteURL, allowEmptyProxy); ok {
		if IsDataURL(resultURL) && len(resultURL) > MaxInlineResultURLBytes {
			task.Status = model.TaskStatusFailure
			task.PrivateData.ResultURL = ""
			task.FailReason = InlineResultTooLargeReason
			return
		}
		task.Status = model.TaskStatusSuccess
		task.FailReason = strings.TrimSpace(reason)
		task.PrivateData.ResultURL = resultURL
		if task.SettlementStatus == "" {
			task.SettlementStatus = model.TaskSettlementStatusPending
		}
		return
	}
	task.Status = model.TaskStatusFailure
	task.PrivateData.ResultURL = ""
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		task.FailReason = trimmed
	} else {
		task.FailReason = MissingResultURLReason
	}
}

// IsRemoteMediaLocator reports whether value is a fetchable or inline media locator.
func IsRemoteMediaLocator(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if IsDataURL(trimmed) || IsGCSURI(trimmed) {
		return true
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// IsDataURL reports whether value uses the case-insensitive data URL scheme.
// Result producers are external to the task lifecycle, so scheme casing must
// not decide whether inline media is kept behind the task content proxy.
func IsDataURL(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "data:")
}

// IsBase64DataURL reports whether value is a data URL whose final metadata
// token is the exact case-insensitive base64 flag.
func IsBase64DataURL(value string) bool {
	header, _, ok := strings.Cut(value, ",")
	if !ok || !strings.HasPrefix(strings.ToLower(header), "data:") {
		return false
	}
	metadata := header[len("data:"):]
	tokens := strings.Split(metadata, ";")
	return len(tokens) > 1 && strings.EqualFold(tokens[len(tokens)-1], "base64")
}

// Status-to-progress mapping constants for polling updates.
const (
	ProgressSubmitted  = "10%"
	ProgressQueued     = "20%"
	ProgressInProgress = "30%"
	ProgressComplete   = "100%"
)

// ---------------------------------------------------------------------------
// BaseBilling — embeddable no-op implementations for TaskAdaptor billing methods.
// Adaptors that do not need custom billing can embed this struct directly.
// ---------------------------------------------------------------------------

type BaseBilling struct{}

// EstimateBilling returns nil (no extra ratios; use base model price).
func (BaseBilling) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

// AdjustBillingOnSubmit returns nil (no submit-time adjustment).
func (BaseBilling) AdjustBillingOnSubmit(_ *relaycommon.RelayInfo, _ []byte) map[string]float64 {
	return nil
}

// AdjustBillingOnComplete returns 0 (keep pre-charged amount).
func (BaseBilling) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

// ApplyPublicOpenAIVideoProjection overwrites OpenAI video status, progress,
// completion time, error, and result URL with the public settlement-aware projection.
func ApplyPublicOpenAIVideoProjection(task *model.Task, video *dto.OpenAIVideo) {
	if task == nil || video == nil {
		return
	}
	publicStatus := task.PublicStatus()
	video.Status = publicStatus.ToVideoStatus()
	if progress := strings.TrimSpace(task.PublicProgress()); progress != "" {
		video.SetProgressStr(progress)
	}
	switch publicStatus {
	case model.TaskStatusFailure:
		reason := strings.TrimSpace(task.PublicFailReason())
		if reason == "" && video.Error != nil {
			reason = model.SanitizePublicTaskFailReason(video.Error.Message)
		}
		if reason != "" {
			video.Error = &dto.OpenAIVideoError{
				Message: reason,
				Code:    "task_failed",
			}
		} else {
			video.Error = nil
		}
		video.CompletedAt = 0
		video.Metadata = nil
		return
	default:
		video.Error = nil
	}
	if publicStatus != model.TaskStatusSuccess {
		video.CompletedAt = 0
		video.Metadata = nil
		return
	}
	if resultURL := strings.TrimSpace(task.PublicResultURL()); resultURL != "" {
		video.Metadata = map[string]any{"url": resultURL}
	} else {
		video.Metadata = nil
	}
	if video.CompletedAt == 0 {
		if task.FinishTime > 0 {
			video.CompletedAt = task.FinishTime
		} else {
			video.CompletedAt = task.UpdatedAt
		}
	}
}
