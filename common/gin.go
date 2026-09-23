package common

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/pkg/errors"

	"github.com/gin-gonic/gin"
)

const KeyRequestBody = "key_request_body"
const KeyBodyStorage = "key_body_storage"

var ErrRequestBodyTooLarge = errors.New("request body too large")

func IsRequestBodyTooLargeError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRequestBodyTooLarge) {
		return true
	}
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func GetRequestBody(c *gin.Context) (io.Seeker, error) {
	// 首先检查是否有 BodyStorage 缓存
	if storage, exists := c.Get(KeyBodyStorage); exists && storage != nil {
		if bs, ok := storage.(BodyStorage); ok {
			if _, err := bs.Seek(0, io.SeekStart); err != nil {
				return nil, fmt.Errorf("failed to seek body storage: %w", err)
			}
			return bs, nil
		}
	}

	// 检查旧的缓存方式
	cached, exists := c.Get(KeyRequestBody)
	if exists && cached != nil {
		if b, ok := cached.([]byte); ok {
			bs, err := CreateBodyStorage(b)
			if err != nil {
				return nil, err
			}
			c.Set(KeyBodyStorage, bs)
			return bs, nil
		}
	}

	maxMB := constant.MaxRequestBodyMB
	if maxMB <= 0 {
		maxMB = 128 // 默认 128MB
	}
	maxBytes := int64(maxMB) << 20

	contentLength := c.Request.ContentLength

	// 使用新的存储系统
	storage, err := CreateBodyStorageFromReader(c.Request.Body, contentLength, maxBytes)
	_ = c.Request.Body.Close()

	if err != nil {
		if errors.Is(err, ErrDiskCacheCapacityUnavailable) {
			return nil, err
		}
		if IsRequestBodyTooLargeError(err) {
			return nil, errors.Wrap(ErrRequestBodyTooLarge, fmt.Sprintf("request body exceeds %d MB", maxMB))
		}
		return nil, err
	}

	// 缓存存储对象
	c.Set(KeyBodyStorage, storage)

	return storage, nil
}

// GetBodyStorage 获取请求体存储对象（用于需要多次读取的场景）
func GetBodyStorage(c *gin.Context) (BodyStorage, error) {
	seeker, err := GetRequestBody(c)
	if err != nil {
		return nil, err
	}
	bs, ok := seeker.(BodyStorage)
	if !ok {
		return nil, errors.New("unexpected body storage type")
	}
	return bs, nil
}

// CleanupBodyStorage 清理请求体存储（应在请求结束时调用）
func CleanupBodyStorage(c *gin.Context) {
	if storage, exists := c.Get(KeyBodyStorage); exists && storage != nil {
		if bs, ok := storage.(BodyStorage); ok {
			bs.Close()
		}
		c.Set(KeyBodyStorage, nil)
	}
}

func UnmarshalBodyReusable(c *gin.Context, v any) error {
	storage, err := GetBodyStorage(c)
	if err != nil {
		return err
	}
	contentType := c.Request.Header.Get("Content-Type")

	// disk-backed JSON: stream-decode directly from the file to avoid
	// materializing the entire payload back into a transient []byte
	// (diskStorage.Bytes() would ReadFull the whole file into the heap).
	if storage.IsDisk() && strings.HasPrefix(contentType, "application/json") {
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
			return seekErr
		}
		if err := DecodeJson(storage, v); err != nil {
			return err
		}
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
			return seekErr
		}
		c.Request.Body = io.NopCloser(storage)
		return nil
	}

	if strings.Contains(contentType, gin.MIMEMultipartPOSTForm) {
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
			return seekErr
		}
		if err := parseMultipartFormDataFromReader(c, storage, v); err != nil {
			return err
		}
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
			return seekErr
		}
		c.Request.Body = io.NopCloser(storage)
		return nil
	}

	requestBody, err := storage.Bytes()
	if err != nil {
		return err
	}
	if strings.HasPrefix(contentType, "application/json") {
		err = Unmarshal(requestBody, v)
	} else if strings.Contains(contentType, gin.MIMEPOSTForm) {
		err = parseFormData(requestBody, v)
	} else if strings.Contains(contentType, gin.MIMEMultipartPOSTForm) {
		err = parseMultipartFormData(c, requestBody, v)
	} else {
		// skip for now
		// TODO: someday non json request have variant model, we will need to implementation this
	}
	if err != nil {
		return err
	}
	// Reset request body
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return seekErr
	}
	c.Request.Body = io.NopCloser(storage)
	return nil
}

func SetContextKey(c *gin.Context, key constant.ContextKey, value any) {
	c.Set(string(key), value)
}

func GetContextKey(c *gin.Context, key constant.ContextKey) (any, bool) {
	return c.Get(string(key))
}

func GetContextKeyString(c *gin.Context, key constant.ContextKey) string {
	return c.GetString(string(key))
}

func GetContextKeyInt(c *gin.Context, key constant.ContextKey) int {
	if value, ok := c.Get(string(key)); ok {
		switch v := value.(type) {
		case int:
			return v
		case int8:
			return int(v)
		case int16:
			return int(v)
		case int32:
			return int(v)
		case int64:
			return int(v)
		case uint:
			return int(v)
		case uint8:
			return int(v)
		case uint16:
			return int(v)
		case uint32:
			return int(v)
		case uint64:
			return int(v)
		}
	}
	return c.GetInt(string(key))
}

func GetContextKeyInt64(c *gin.Context, key constant.ContextKey) int64 {
	if value, ok := c.Get(string(key)); ok {
		switch v := value.(type) {
		case int:
			return int64(v)
		case int8:
			return int64(v)
		case int16:
			return int64(v)
		case int32:
			return int64(v)
		case int64:
			return v
		case uint:
			return int64(v)
		case uint8:
			return int64(v)
		case uint16:
			return int64(v)
		case uint32:
			return int64(v)
		case uint64:
			return int64(v)
		}
	}
	return int64(c.GetInt(string(key)))
}

func GetContextInt64(c *gin.Context, key string) int64 {
	if value, ok := c.Get(key); ok {
		switch v := value.(type) {
		case int:
			return int64(v)
		case int8:
			return int64(v)
		case int16:
			return int64(v)
		case int32:
			return int64(v)
		case int64:
			return v
		case uint:
			return int64(v)
		case uint8:
			return int64(v)
		case uint16:
			return int64(v)
		case uint32:
			return int64(v)
		case uint64:
			return int64(v)
		}
	}
	return int64(c.GetInt(key))
}

func GetContextKeyBool(c *gin.Context, key constant.ContextKey) bool {
	return c.GetBool(string(key))
}

func GetContextKeyStringSlice(c *gin.Context, key constant.ContextKey) []string {
	return c.GetStringSlice(string(key))
}

func GetContextKeyStringMap(c *gin.Context, key constant.ContextKey) map[string]any {
	return c.GetStringMap(string(key))
}

func GetContextKeyTime(c *gin.Context, key constant.ContextKey) time.Time {
	return c.GetTime(string(key))
}

func GetContextKeyType[T any](c *gin.Context, key constant.ContextKey) (T, bool) {
	if value, ok := c.Get(string(key)); ok {
		if v, ok := value.(T); ok {
			return v, true
		}
	}
	var t T
	return t, false
}

// ApiError translates LocalizedError (and known sentinels inside
// respondLocalizedAPIError). Remaining dashboard errors keep their original
// text; do not globally wrap every ApiError as an i18n key.
func ApiError(c *gin.Context, err error) {
	if respondLocalizedAPIError(c, http.StatusOK, err) {
		return
	}
	writeAPIError(c, http.StatusOK, errorMessage(err))
}

func ApiErrorMsg(c *gin.Context, msg string) {
	writeAPIError(c, http.StatusOK, msg)
}

func ApiErrorWithStatus(c *gin.Context, status int, err error) {
	if respondLocalizedAPIError(c, status, err) {
		return
	}
	writeAPIError(c, status, errorMessage(err))
}

func respondLocalizedAPIError(c *gin.Context, status int, err error) bool {
	loc, ok := AsLocalizedError(err)
	if !ok {
		return false
	}
	msg := TranslateMessage(c, loc.Key, loc.Args...)
	c.JSON(status, gin.H{
		"success": false,
		"message": msg,
	})
	return true
}

// PublicDashboardErrorMessage sanitizes a dashboard/console error for JSON.
func PublicDashboardErrorMessage(c *gin.Context, original string) string {
	return publicAPIErrorMessage(c, original)
}

// PublicRequestErrorMessage sanitizes a public token/API error string.
func PublicRequestErrorMessage(original string) string {
	sanitized := kitutil.SanitizePublicClientError(original)
	switch sanitized {
	case "",
		kitutil.PublicInternalFailReason,
		kitutil.PublicAccountingFailReason,
		kitutil.PublicSettlementFailReason:
		return "invalid request"
	default:
		return sanitized
	}
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func writeAPIError(c *gin.Context, status int, original string) {
	message := publicAPIErrorMessage(c, original)
	if message != original && !isRecordNotFoundMessage(original) {
		SysError("api error: " + original)
	}
	c.JSON(status, gin.H{
		"success": false,
		"message": message,
	})
}

func publicAPIErrorMessage(c *gin.Context, original string) string {
	sanitized := kitutil.SanitizePublicClientError(original)
	switch {
	case sanitized == "":
		return translateAPIError(c, "common.operation_failed")
	case isRecordNotFoundMessage(sanitized):
		return translateAPIError(c, "common.not_found")
	case sanitized == kitutil.PublicInternalFailReason:
		return translateAPIError(c, "common.database_error")
	case sanitized == kitutil.PublicAccountingFailReason, sanitized == kitutil.PublicSettlementFailReason:
		return translateAPIError(c, "common.operation_failed")
	default:
		if key := consoleSentinelMessageKey(sanitized); key != "" {
			return translateAPIError(c, key)
		}
		if detail, ok := dashboardDetailMessage(c, sanitized); ok {
			return detail
		}
		if dashboardLanguageIsChinese(c) {
			if cleaned := SanitizeChineseConsoleText(sanitized); cleaned != "" {
				return cleaned
			}
			return translateAPIError(c, "common.operation_failed")
		}
		if dashboardHidesUntranslated(c, sanitized) {
			return translateAPIError(c, "common.operation_failed")
		}
		return sanitized
	}
}

// DashboardLanguage is the console language for this request.
// i18n.Init points it at GetLangFromContext. A nil hook leaves the original text.
var DashboardLanguage func(c *gin.Context) string

var (
	claudeNegativeMaxTokensPattern = regexp.MustCompile(`^negative Claude default max_tokens (-?\d+) for "(.*)"$`)
	geminiSafetyThresholdPattern   = regexp.MustCompile(`^invalid Gemini safety threshold "(.*)" for "(.*)"$`)
	nonNegativeNumberPattern       = regexp.MustCompile(`^(.+) must be a finite, non-negative number$`)
)

func dashboardLanguage(c *gin.Context) string {
	if DashboardLanguage == nil || c == nil || c.Request == nil {
		return ""
	}
	return DashboardLanguage(c)
}

func dashboardLanguageIsChinese(c *gin.Context) bool {
	switch dashboardLanguage(c) {
	case "zh-CN", "zh-TW":
		return true
	default:
		return false
	}
}

func dashboardHidesUntranslated(c *gin.Context, msg string) bool {
	return !containsHan(msg) && dashboardLanguageIsChinese(c)
}

// dashboardDetailMessage keeps the invalid value in known admin validation errors.
func dashboardDetailMessage(c *gin.Context, msg string) (string, bool) {
	if !dashboardLanguageIsChinese(c) {
		return "", false
	}
	msg = strings.TrimSpace(msg)
	switch msg {
	case "provide either option maps or model pricing changes":
		return translateAPIError(c, "option.pricing_input_conflict"), true
	case "model pricing changed; reload before saving":
		return translateAPIError(c, "model.pricing_conflict"), true
	}
	if match := claudeNegativeMaxTokensPattern.FindStringSubmatch(msg); len(match) == 3 {
		return translateAPIError(c, "option.claude_negative_max_tokens", map[string]any{
			"Value": match[1],
			"Model": match[2],
		}), true
	}
	if match := geminiSafetyThresholdPattern.FindStringSubmatch(msg); len(match) == 3 {
		return translateAPIError(c, "option.gemini_invalid_safety_threshold", map[string]any{
			"Threshold": match[1],
			"Category":  match[2],
		}), true
	}
	if match := nonNegativeNumberPattern.FindStringSubmatch(msg); len(match) == 2 {
		return translateAPIError(c, "option.non_negative_number", map[string]any{
			"Key": match[1],
		}), true
	}
	return "", false
}

func containsHan(msg string) bool {
	for _, r := range msg {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func consoleSentinelMessageKey(msg string) string {
	switch strings.TrimSpace(msg) {
	case "auth flow is invalid":
		return "auth.flow_invalid"
	case "auth flow has expired":
		return "auth.flow_expired"
	case "auth flow has already been consumed":
		return "auth.flow_consumed"
	}
	lower := strings.ToLower(msg)
	switch {
	case strings.HasPrefix(lower, "generate auth flow token:"):
		return "common.operation_failed"
	case strings.Contains(lower, "runtime cache refresh failed"):
		return "channel.runtime_cache_refresh_failed"
	case strings.HasPrefix(lower, "failed to check name availability"):
		return "deployment.name_check_failed"
	default:
		return ""
	}
}

func isRecordNotFoundMessage(msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	return msg == "record not found" || strings.HasSuffix(msg, ": record not found")
}

func translateAPIError(c *gin.Context, key string, args ...map[string]any) string {
	if TranslateMessage == nil {
		return key
	}
	return TranslateMessage(c, key, args...)
}

func ApiSuccess(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}

// ApiErrorI18n returns a translated error message based on the user's language preference
// key is the i18n message key, args is optional template data
func ApiErrorI18n(c *gin.Context, key string, args ...map[string]any) {
	msg := TranslateMessage(c, key, args...)
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": msg,
	})
}

// ApiSuccessI18n returns a translated success message based on the user's language preference
func ApiSuccessI18n(c *gin.Context, key string, data any, args ...map[string]any) {
	msg := TranslateMessage(c, key, args...)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": msg,
		"data":    data,
	})
}

// TranslateMessage is a helper function that calls i18n.T
// This function is defined here to avoid circular imports
// The actual implementation will be set during init
var TranslateMessage func(c *gin.Context, key string, args ...map[string]any) string

func init() {
	// Default implementation that returns the key as-is
	// This will be replaced by i18n.T during i18n initialization
	TranslateMessage = func(c *gin.Context, key string, args ...map[string]any) string {
		return key
	}
}

func ParseMultipartFormReusable(c *gin.Context) (*multipart.Form, error) {
	storage, err := GetBodyStorage(c)
	if err != nil {
		return nil, err
	}

	// Use the original Content-Type saved on first call to avoid boundary
	// mismatch when callers overwrite c.Request.Header after multipart rebuild.
	var contentType string
	if saved, ok := c.Get("_original_multipart_ct"); ok {
		contentType = saved.(string)
	} else {
		contentType = c.Request.Header.Get("Content-Type")
		c.Set("_original_multipart_ct", contentType)
	}
	boundary, err := parseBoundary(contentType)
	if err != nil {
		return nil, err
	}

	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	reader := multipart.NewReader(storage, boundary)
	form, err := reader.ReadForm(multipartMemoryLimit())
	if err != nil {
		return nil, err
	}

	// Reset request body
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	c.Request.Body = io.NopCloser(storage)
	return form, nil
}

func processFormMap(formMap map[string]any, v any) error {
	jsonData, err := Marshal(formMap)
	if err != nil {
		return err
	}

	err = Unmarshal(jsonData, v)
	if err != nil {
		return err
	}

	return nil
}

func parseFormData(data []byte, v any) error {
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return err
	}
	formMap := make(map[string]any)
	for key, vals := range values {
		if len(vals) == 1 {
			formMap[key] = vals[0]
		} else {
			formMap[key] = vals
		}
	}

	return processFormMap(formMap, v)
}

func parseMultipartFormData(c *gin.Context, data []byte, v any) error {
	var contentType string
	if saved, ok := c.Get("_original_multipart_ct"); ok {
		contentType = saved.(string)
	} else {
		contentType = c.Request.Header.Get("Content-Type")
		c.Set("_original_multipart_ct", contentType)
	}
	boundary, err := parseBoundary(contentType)
	if err != nil {
		if errors.Is(err, errBoundaryNotFound) {
			return Unmarshal(data, v) // Fallback to JSON
		}
		return err
	}

	reader := multipart.NewReader(bytes.NewReader(data), boundary)
	form, err := reader.ReadForm(multipartMemoryLimit())
	if err != nil {
		return err
	}
	defer form.RemoveAll()
	formMap := make(map[string]any)
	for key, vals := range form.Value {
		if len(vals) == 1 {
			formMap[key] = vals[0]
		} else {
			formMap[key] = vals
		}
	}

	return processFormMap(formMap, v)
}

func parseMultipartFormDataFromReader(c *gin.Context, reader io.Reader, v any) error {
	var contentType string
	if saved, ok := c.Get("_original_multipart_ct"); ok {
		contentType = saved.(string)
	} else {
		contentType = c.Request.Header.Get("Content-Type")
		c.Set("_original_multipart_ct", contentType)
	}
	boundary, err := parseBoundary(contentType)
	if err != nil {
		if errors.Is(err, errBoundaryNotFound) {
			data, readErr := io.ReadAll(reader)
			if readErr != nil {
				return readErr
			}
			return Unmarshal(data, v)
		}
		return err
	}

	formReader := multipart.NewReader(reader, boundary)
	form, err := formReader.ReadForm(multipartMemoryLimit())
	if err != nil {
		return err
	}
	defer form.RemoveAll()

	formMap := make(map[string]any)
	for key, vals := range form.Value {
		if len(vals) == 1 {
			formMap[key] = vals[0]
		} else {
			formMap[key] = vals
		}
	}
	return processFormMap(formMap, v)
}

var errBoundaryNotFound = errors.New("multipart boundary not found")

// parseBoundary extracts the multipart boundary from the Content-Type header using mime.ParseMediaType
func parseBoundary(contentType string) (string, error) {
	if contentType == "" {
		return "", errBoundaryNotFound
	}
	// Boundary-UUID / boundary-------xxxxxx
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", err
	}
	boundary, ok := params["boundary"]
	if !ok || boundary == "" {
		return "", errBoundaryNotFound
	}
	return boundary, nil
}

// multipartMemoryLimit returns the configured multipart memory limit in bytes
func multipartMemoryLimit() int64 {
	limitMB := constant.MaxFileDownloadMB
	if limitMB <= 0 {
		limitMB = 32
	}
	return int64(limitMB) << 20
}
