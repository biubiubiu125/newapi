package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

const systemLogoPublicPrefix = "/system-assets/"
const walletNoticeMaxLength = 1000

func normalizeSystemLogoURL(value string) string {
	logo := strings.TrimSpace(value)
	if logo == "" {
		return ""
	}
	lower := strings.ToLower(logo)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		if parsed, err := url.Parse(logo); err == nil && strings.HasPrefix(parsed.Path, systemLogoPublicPrefix) {
			return parsed.Path
		}
		return logo
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "data:") {
		return logo
	}
	return logo
}

var completionRatioMetaOptionKeys = []string{
	"ModelPrice",
	"ModelRatio",
	"CompletionRatio",
	"CacheRatio",
	"CreateCacheRatio",
	"ImageRatio",
	"AudioRatio",
	"AudioCompletionRatio",
}

func isPaymentComplianceOptionKey(key string) bool {
	return strings.HasPrefix(key, "payment_setting.compliance_")
}

func isPositiveOptionValue(value string) bool {
	intValue, err := strconv.Atoi(strings.TrimSpace(value))
	if err == nil {
		return intValue > 0
	}
	floatValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return err == nil && floatValue > 0
}

func collectModelNamesFromOptionValue(raw string, modelNames map[string]struct{}) {
	if strings.TrimSpace(raw) == "" {
		return
	}

	var parsed map[string]any
	if err := common.UnmarshalJsonStr(raw, &parsed); err != nil {
		return
	}

	for modelName := range parsed {
		modelNames[modelName] = struct{}{}
	}
}

func buildCompletionRatioMetaValue(optionValues map[string]string) string {
	modelNames := make(map[string]struct{})
	for _, key := range completionRatioMetaOptionKeys {
		collectModelNamesFromOptionValue(optionValues[key], modelNames)
	}

	meta := make(map[string]ratio_setting.CompletionRatioInfo, len(modelNames))
	for modelName := range modelNames {
		meta[modelName] = ratio_setting.GetCompletionRatioInfo(modelName)
	}

	jsonBytes, err := common.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(jsonBytes)
}

func UploadSystemLogo(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgLogoFileRequired)
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, 5*1024*1024+1))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(data) > 5*1024*1024 {
		common.ApiErrorI18n(c, i18n.MsgLogoFileTooLarge)
		return
	}
	contentType := http.DetectContentType(data)
	ext, ok := systemAssetExtensionFromContentType(contentType)
	if !ok {
		common.ApiErrorI18n(c, i18n.MsgLogoFileType)
		return
	}
	dir, err := ensureSystemAssetDir()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	name := fmt.Sprintf("logo-%d-%s%s", time.Now().UnixMilli(), strings.ToLower(common.GetRandomString(8)), ext)
	fullPath := filepath.Join(dir, name)
	if err := os.WriteFile(fullPath, data, 0o644); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"url": normalizeSystemLogoURL(systemLogoPublicPrefix + name),
	})
}

func GetSystemAsset(c *gin.Context) {
	name := filepath.Base(strings.TrimSpace(c.Param("name")))
	if name == "" || name == "." || strings.Contains(name, string(filepath.Separator)) {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	dir, err := ensureSystemAssetDir()
	if err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	fullPath := filepath.Join(dir, name)
	if _, err := os.Stat(fullPath); err != nil {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.File(fullPath)
}

func ensureSystemAssetDir() (string, error) {
	dir := filepath.Join(".", "uploads", "system-assets")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func systemAssetExtensionFromContentType(contentType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/webp":
		return ".webp", true
	case "image/gif":
		return ".gif", true
	case "image/x-icon", "image/vnd.microsoft.icon":
		return ".ico", true
	default:
		return "", false
	}
}

func GetOptions(c *gin.Context) {
	var options []*model.Option
	optionValues := make(map[string]string)
	common.OptionMapRWMutex.Lock()
	for k, v := range common.OptionMap {
		if model.IsDeprecatedOptionKey(k) ||
			k == "theme.frontend" ||
			k == "billing_setting.billing_mode" ||
			k == "billing_setting.billing_expr" {
			continue
		}
		value := common.Interface2String(v)
		isSensitiveKey := strings.HasSuffix(k, "Token") ||
			strings.HasSuffix(k, "Secret") ||
			strings.HasSuffix(k, "Key") ||
			strings.HasSuffix(k, "secret") ||
			strings.HasSuffix(k, "api_key")
		if isSensitiveKey {
			continue
		}
		options = append(options, &model.Option{
			Key:   k,
			Value: value,
		})
		for _, optionKey := range completionRatioMetaOptionKeys {
			if optionKey == k {
				optionValues[k] = value
				break
			}
		}
	}
	common.OptionMapRWMutex.Unlock()
	// Display the effective pricing maps, including built-in defaults that are
	// intentionally not persisted in the administrator option table.
	for key, values := range map[string]map[string]string{
		"billing_setting.billing_mode": billing_setting.GetBillingModeCopy(),
		"billing_setting.billing_expr": billing_setting.GetBillingExprCopy(),
	} {
		encoded, err := common.Marshal(values)
		if err != nil {
			common.ApiErrorWithStatus(c, http.StatusInternalServerError, err)
			return
		}
		options = append(options, &model.Option{Key: key, Value: string(encoded)})
	}
	options = append(options, &model.Option{
		Key:   "CompletionRatioMeta",
		Value: buildCompletionRatioMetaValue(optionValues),
	})
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    options,
	})
}

type OptionUpdateRequest struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func UpdateOption(c *gin.Context) {
	var option OptionUpdateRequest
	err := common.DecodeJson(c.Request.Body, &option)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.T(c, i18n.MsgInvalidParams),
		})
		return
	}
	if model.IsDeprecatedOptionKey(option.Key) {
		common.ApiErrorI18n(c, i18n.MsgOptionDeprecated)
		return
	}
	switch option.Value.(type) {
	case bool:
		option.Value = common.Interface2String(option.Value.(bool))
	case float64:
		option.Value = common.Interface2String(option.Value.(float64))
	case int:
		option.Value = common.Interface2String(option.Value.(int))
	default:
		option.Value = fmt.Sprintf("%v", option.Value)
	}
	if option.Key == "payment_setting.wallet_notice" {
		walletNotice := strings.TrimSpace(option.Value.(string))
		if len([]rune(walletNotice)) > walletNoticeMaxLength {
			common.ApiErrorI18n(c, i18n.MsgOptionWalletNoticeTooLong)
			return
		}
		option.Value = walletNotice
	}
	previousAnnouncements := ""
	if option.Key == "console_setting.announcements" {
		previousAnnouncements = console_setting.GetConsoleSetting().Announcements
	}
	switch option.Key {
	default:
		if isPaymentComplianceOptionKey(option.Key) {
			common.ApiErrorI18n(c, i18n.MsgOptionComplianceFieldReadonly)
			return
		}
	}
	if option.Key == "TaskPublicAddress" && option.Value.(string) != "" {
		if err := service.ValidateTaskArtifactBaseURL(option.Value.(string)); err != nil {
			common.ApiErrorI18n(c, mapTaskArtifactBaseURLError(err))
			return
		}
	}
	switch option.Key {
	case "GitHubOAuthEnabled":
		if option.Value == "true" && common.GitHubClientId == "" {
			common.ApiErrorI18n(c, i18n.MsgOptionOAuthGitHubMissing)
			return
		}
	case "discord.enabled":
		if option.Value == "true" && system_setting.GetDiscordSettings().ClientId == "" {
			common.ApiErrorI18n(c, i18n.MsgOptionOAuthDiscordMissing)
			return
		}
	case "oidc.enabled":
		if option.Value == "true" && system_setting.GetOIDCSettings().ClientId == "" {
			common.ApiErrorI18n(c, i18n.MsgOptionOAuthOIDCMissing)
			return
		}
	case "LinuxDOOAuthEnabled":
		if option.Value == "true" && common.LinuxDOClientId == "" {
			common.ApiErrorI18n(c, i18n.MsgOptionOAuthLinuxDOMissing)
			return
		}
	case "EmailDomainRestrictionEnabled":
		if option.Value == "true" && len(common.EmailDomainWhitelist) == 0 {
			common.ApiErrorI18n(c, i18n.MsgOptionEmailDomainRestrictionEmpty)
			return
		}
	case "WeChatAuthEnabled":
		if option.Value == "true" && common.WeChatServerAddress == "" {
			common.ApiErrorI18n(c, i18n.MsgOptionWeChatMissing)
			return
		}
	case "TurnstileCheckEnabled":
		if option.Value == "true" && common.TurnstileSiteKey == "" {
			common.ApiErrorI18n(c, i18n.MsgOptionTurnstileMissing)
			return
		}
	case "TelegramOAuthEnabled":
		if option.Value == "true" && common.TelegramBotToken == "" {
			common.ApiErrorI18n(c, i18n.MsgOptionTelegramMissing)
			return
		}
	case "theme.frontend":
		if option.Value != "default" {
			common.ApiErrorI18n(c, i18n.MsgOptionThemeClassicRemoved)
			return
		}
	case "claude.default_max_tokens":
		if err := model_setting.ValidateClaudeDefaultMaxTokens(option.Value.(string)); err != nil {
			common.ApiError(c, err)
			return
		}
	case "gemini.safety_settings":
		if err := model_setting.ValidateGeminiSafetySettings(option.Value.(string)); err != nil {
			common.ApiError(c, err)
			return
		}
	case "GroupRatio":
		err = ratio_setting.CheckGroupRatio(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "ModelRequestRateLimitGroup":
		err = setting.CheckModelRequestRateLimitGroup(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "AutomaticDisableStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "AutomaticRetryStatusCodes":
		_, err = operation_setting.ParseHTTPStatusCodeRanges(option.Value.(string))
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "console_setting.api_info":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "ApiInfo")
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "console_setting.announcements":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "Announcements")
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "console_setting.faq":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "FAQ")
		if err != nil {
			common.ApiError(c, err)
			return
		}
	case "console_setting.uptime_kuma_groups":
		err = console_setting.ValidateConsoleSettings(option.Value.(string), "UptimeKumaGroups")
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	err = model.UpdateOption(option.Key, option.Value.(string))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if option.Key == "console_setting.announcements" {
		if _, err := service.AutoPushChangedAnnouncements(previousAnnouncements, option.Value.(string)); err != nil {
			common.SysLog("failed to create automatic telegram announcement push: " + err.Error())
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func mapTaskArtifactBaseURLError(err error) string {
	switch {
	case errors.Is(err, service.ErrTaskArtifactURLEmpty):
		return i18n.MsgOptionTaskArtifactURLEmpty
	case errors.Is(err, service.ErrTaskArtifactURLWhitespace):
		return i18n.MsgOptionTaskArtifactURLWhitespace
	case errors.Is(err, service.ErrTaskArtifactURLInvalid):
		return i18n.MsgOptionTaskArtifactURLInvalid
	case errors.Is(err, service.ErrTaskArtifactURLScheme):
		return i18n.MsgOptionTaskArtifactURLScheme
	case errors.Is(err, service.ErrTaskArtifactURLHost):
		return i18n.MsgOptionTaskArtifactURLHost
	case errors.Is(err, service.ErrTaskArtifactURLQuery):
		return i18n.MsgOptionTaskArtifactURLQuery
	default:
		return i18n.MsgInvalidParams
	}
}
