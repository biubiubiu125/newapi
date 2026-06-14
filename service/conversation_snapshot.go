package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const (
	conversationExportDirName       = "conversation-exports"
	conversationExportRetentionHour = 24
	maxConversationExportRangeDays  = 90

	conversationSnapshotQuotaKey            = "conversation_snapshot_quota"
	conversationSnapshotPromptTokensKey     = "conversation_snapshot_prompt_tokens"
	conversationSnapshotCompletionTokensKey = "conversation_snapshot_completion_tokens"
	conversationSnapshotTotalTokensKey      = "conversation_snapshot_total_tokens"
	conversationSnapshotCacheTokensKey      = "conversation_snapshot_cache_tokens"

	conversationSnapshotMemoryBufferLimit = 1 << 20
)

var shanghaiLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return loc
}()

type ConversationExportFilter struct {
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	UserId    int    `json:"user_id,omitempty"`
	TokenId   int    `json:"token_id,omitempty"`
	ModelName string `json:"model,omitempty"`
	Group     string `json:"group,omitempty"`
}

type ConversationExportRequest struct {
	Filter ConversationExportFilter `json:"filter"`
	Mode   string                   `json:"mode"`
	Fields []string                 `json:"fields"`
}

type ConversationSnapshotCapture struct {
	capture *ResponseCaptureWriter
}

type ResponseCaptureWriter struct {
	gin.ResponseWriter
	mu       sync.Mutex
	buf      bytes.Buffer
	file     *os.File
	filePath string
	writeErr error
}

func (w *ResponseCaptureWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	_ = w.captureLocked(data)
	w.mu.Unlock()
	return w.ResponseWriter.Write(data)
}

func (w *ResponseCaptureWriter) WriteString(data string) (int, error) {
	w.mu.Lock()
	_ = w.captureLocked([]byte(data))
	w.mu.Unlock()
	return w.ResponseWriter.WriteString(data)
}

func (w *ResponseCaptureWriter) captureLocked(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	if w.file == nil && w.buf.Len()+len(data) <= conversationSnapshotMemoryBufferLimit {
		_, w.writeErr = w.buf.Write(data)
		return w.writeErr
	}
	if w.file == nil {
		file, err := os.CreateTemp("", "newapi-conversation-snapshot-*.tmp")
		if err != nil {
			w.writeErr = err
			return err
		}
		w.file = file
		w.filePath = file.Name()
		if w.buf.Len() > 0 {
			if _, err := w.file.Write(w.buf.Bytes()); err != nil {
				w.writeErr = err
				return err
			}
			w.buf.Reset()
		}
	}
	_, w.writeErr = w.file.Write(data)
	return w.writeErr
}

func (w *ResponseCaptureWriter) CapturedString() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.capturedStringLocked()
}

func (w *ResponseCaptureWriter) capturedStringLocked() (string, error) {
	if w.writeErr != nil {
		return w.buf.String(), w.writeErr
	}
	if w.file == nil {
		return w.buf.String(), nil
	}
	if err := w.file.Sync(); err != nil {
		return "", err
	}
	if _, err := w.file.Seek(0, 0); err != nil {
		return "", err
	}
	data, err := os.ReadFile(w.filePath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (w *ResponseCaptureWriter) CapturedResponseText(stream bool) (string, string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writeErr != nil {
		raw := w.buf.String()
		return raw, ExtractConversationResponseText(raw), w.writeErr
	}
	if w.file == nil {
		raw := w.buf.String()
		if stream {
			metadata, text, err := ExtractConversationStreamResponseTextFromReader(strings.NewReader(raw))
			return metadata, text, err
		}
		return raw, ExtractConversationResponseText(raw), nil
	}
	if !stream {
		raw, err := w.capturedStringLocked()
		return raw, ExtractConversationResponseText(raw), err
	}
	if err := w.file.Sync(); err != nil {
		return "", "", err
	}
	if _, err := w.file.Seek(0, 0); err != nil {
		return "", "", err
	}
	raw, text, err := ExtractConversationStreamResponseTextFromReader(w.file)
	if err != nil {
		return raw, text, err
	}
	return raw, text, nil
}

func (w *ResponseCaptureWriter) Cleanup() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		_ = w.file.Close()
		_ = os.Remove(w.filePath)
		w.file = nil
		w.filePath = ""
	}
}

func ShouldSnapshotRelayMode(mode int) bool {
	switch mode {
	case relayconstant.RelayModeChatCompletions,
		relayconstant.RelayModeCompletions,
		relayconstant.RelayModeResponses,
		relayconstant.RelayModeResponsesCompact,
		relayconstant.RelayModeGemini:
		return true
	default:
		return false
	}
}

func BeginConversationSnapshotCapture(c *gin.Context, info *relaycommon.RelayInfo, request dto.Request) *ConversationSnapshotCapture {
	if c == nil || info == nil || request == nil {
		return nil
	}
	isTextRelay := ShouldSnapshotRelayMode(info.RelayMode) ||
		info.RelayFormat == types.RelayFormatClaude ||
		info.GetFinalRequestRelayFormat() == types.RelayFormatClaude
	if !isTextRelay {
		return nil
	}
	captureWriter := &ResponseCaptureWriter{ResponseWriter: c.Writer}
	c.Writer = captureWriter
	return &ConversationSnapshotCapture{capture: captureWriter}
}

func FinishConversationSnapshotCapture(c *gin.Context, info *relaycommon.RelayInfo, capture *ConversationSnapshotCapture, apiErr *types.NewAPIError) {
	if c == nil || info == nil || capture == nil || capture.capture == nil {
		return
	}
	rawResponse, responseText, captureErr := capture.capture.CapturedResponseText(common.GetContextKeyBool(c, constant.ContextKeyIsStream))
	defer capture.capture.Cleanup()
	if captureErr != nil {
		common.SysLog("failed to capture conversation response: " + captureErr.Error())
	}
	requestText := ExtractConversationRequestText(info.Request)
	statusCode := 0
	errorSummary := ""
	if apiErr != nil {
		statusCode = apiErr.StatusCode
		errorSummary = apiErr.ErrorWithStatusCode()
		if responseText == "" {
			responseText = apiErr.Error()
		}
	} else if capture.capture.Status() > 0 {
		statusCode = capture.capture.Status()
	}
	if strings.TrimSpace(requestText) == "" && strings.TrimSpace(responseText) == "" {
		return
	}

	snapshot := &model.ConversationSnapshot{
		RequestId:        c.GetString(common.RequestIdKey),
		UserId:           info.UserId,
		Username:         c.GetString("username"),
		TokenId:          info.TokenId,
		TokenName:        c.GetString("token_name"),
		TokenKey:         MaskAPIKeyForDisplay(info.TokenKey),
		ModelName:        info.OriginModelName,
		Group:            common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		RequestText:      requestText,
		ResponseText:     responseText,
		PromptTokens:     firstPositiveInt(c.GetInt(conversationSnapshotPromptTokensKey), extractPromptTokens(rawResponse)),
		CompletionTokens: firstPositiveInt(c.GetInt(conversationSnapshotCompletionTokensKey), extractCompletionTokens(rawResponse)),
		TotalTokens:      firstPositiveInt(c.GetInt(conversationSnapshotTotalTokensKey), extractTotalTokens(rawResponse)),
		CacheTokens:      firstPositiveInt(c.GetInt(conversationSnapshotCacheTokensKey), extractCacheTokens(rawResponse)),
		Quota:            c.GetInt(conversationSnapshotQuotaKey),
		ChannelId:        common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		ChannelName:      common.GetContextKeyString(c, constant.ContextKeyChannelName),
		StatusCode:       statusCode,
		ErrorSummary:     errorSummary,
		CreatedAt:        common.GetTimestamp(),
	}
	if snapshot.TotalTokens == 0 {
		snapshot.TotalTokens = snapshot.PromptTokens + snapshot.CompletionTokens
	}
	if snapshot.PromptTokens == 0 {
		snapshot.PromptTokens = info.GetEstimatePromptTokens()
	}
	gopool.Go(func() {
		if err := model.InsertConversationSnapshot(snapshot); err != nil {
			common.SysLog("failed to insert conversation snapshot: " + err.Error())
		}
	})
}

func ExtractConversationRequestText(request dto.Request) string {
	if request == nil {
		return ""
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return extractOpenAIRequestText(r)
	case *dto.OpenAIResponsesRequest:
		return extractResponsesRequestText(r)
	case *dto.ClaudeRequest:
		return extractClaudeRequestText(r)
	case *dto.GeminiChatRequest:
		return extractGeminiRequestText(r)
	default:
		meta := request.GetTokenCountMeta()
		if meta == nil {
			return ""
		}
		return strings.TrimSpace(meta.CombineText)
	}
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func MaskAPIKeyForDisplay(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	masked := model.MaskTokenKey(strings.TrimPrefix(key, "sk-"))
	if masked == "" {
		return ""
	}
	return "sk-" + masked
}

func appendNamedLine(lines []string, name string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return lines
	}
	return append(lines, name+": "+value)
}

func hasCacheControl(raw []byte) bool {
	return len(raw) > 0 && strings.TrimSpace(string(raw)) != ""
}

func stringifyPromptValue(value any) string {
	return strings.TrimSpace(strings.Join(collectPromptTextValues(value, true), "\n"))
}

func stringifyRawJSONText(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := common.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Join(collectPromptTextValues(value, true), "\n"))
}

func collectPromptTextValues(value any, collectFreeStrings bool) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		if collectFreeStrings {
			return []string{v}
		}
	case []string:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				parts = append(parts, item)
			}
		}
		return parts
	case []any:
		var parts []string
		for _, item := range v {
			parts = append(parts, collectPromptTextValues(item, collectFreeStrings)...)
		}
		return parts
	case map[string]string:
		m := make(map[string]any, len(v))
		for key, item := range v {
			m[key] = item
		}
		return collectPromptMapTextValues(m, collectFreeStrings)
	case map[string]any:
		return collectPromptMapTextValues(v, collectFreeStrings)
	}
	return nil
}

func collectPromptMapTextValues(m map[string]any, collectFreeStrings bool) []string {
	var parts []string
	for key, value := range m {
		lowerKey := strings.ToLower(strings.TrimSpace(key))
		if shouldSkipPromptSnapshotKey(lowerKey) {
			continue
		}
		switch lowerKey {
		case "text", "content", "input", "prompt", "instructions", "value", "variables":
			parts = append(parts, collectPromptTextValues(value, true)...)
		default:
			parts = append(parts, collectPromptTextValues(value, collectFreeStrings && isLikelyUserVariableKey(lowerKey))...)
		}
	}
	return parts
}

func shouldSkipPromptSnapshotKey(key string) bool {
	switch key {
	case "id", "version", "name", "type", "role", "metadata", "cache_control",
		"prompt_cache_key", "prompt_cache_retention", "image_url", "file_url", "url":
		return true
	default:
		return false
	}
}

func isLikelyUserVariableKey(key string) bool {
	return key != "" && !shouldSkipPromptSnapshotKey(key)
}

func extractOpenAIRequestText(req *dto.GeneralOpenAIRequest) string {
	var lines []string
	if req.Instruction != "" {
		lines = appendNamedLine(lines, "instruction", req.Instruction)
	}
	if prompt := stringifyPromptValue(req.Prompt); prompt != "" {
		lines = appendNamedLine(lines, "prompt", prompt)
	}
	for _, input := range req.ParseInput() {
		lines = appendNamedLine(lines, "input", input)
	}
	for _, message := range req.Messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "message"
		}
		var contentParts []string
		for _, part := range message.ParseContent() {
			switch part.Type {
			case dto.ContentTypeText:
				if hasCacheControl(part.CacheControl) {
					contentParts = append(contentParts, "[缓存正文未保存]")
				} else {
					contentParts = append(contentParts, part.Text)
				}
			case dto.ContentTypeImageURL:
				contentParts = append(contentParts, "[图片内容未保存]")
			case dto.ContentTypeInputAudio:
				contentParts = append(contentParts, "[音频内容未保存]")
			case dto.ContentTypeVideoUrl:
				contentParts = append(contentParts, "[视频内容未保存]")
			case dto.ContentTypeFile:
				contentParts = append(contentParts, "[文件内容未保存]")
			}
		}
		if len(contentParts) > 0 {
			lines = append(lines, role+": "+strings.Join(contentParts, "\n"))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractResponsesRequestText(req *dto.OpenAIResponsesRequest) string {
	var lines []string
	if len(req.Instructions) > 0 {
		lines = appendNamedLine(lines, "instructions", string(req.Instructions))
	}
	for _, input := range req.ParseInput() {
		switch input.Type {
		case "input_text", "message", "":
			if hasCacheControl(input.CacheControl) {
				lines = appendNamedLine(lines, "input", "[缓存正文未保存]")
			} else {
				lines = appendNamedLine(lines, "input", input.Text)
			}
		case "input_image":
			lines = append(lines, "[图片内容未保存]")
		case "input_file":
			lines = append(lines, "[文件内容未保存]")
		default:
			if input.Text != "" {
				lines = appendNamedLine(lines, input.Type, input.Text)
			}
		}
	}
	if len(req.Prompt) > 0 {
		lines = appendNamedLine(lines, "prompt", stringifyRawJSONText(req.Prompt))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractClaudeRequestText(req *dto.ClaudeRequest) string {
	var lines []string
	if req.Prompt != "" {
		lines = appendNamedLine(lines, "prompt", req.Prompt)
	}
	if req.System != nil {
		if req.IsStringSystem() {
			lines = appendNamedLine(lines, "system", req.GetStringSystem())
		} else {
			for _, media := range req.ParseSystem() {
				if media.Type == "text" {
					if hasCacheControl(media.CacheControl) {
						lines = append(lines, "system: [缓存正文未保存]")
					} else {
						lines = appendNamedLine(lines, "system", media.GetText())
					}
				} else {
					lines = append(lines, "[多模态内容未保存]")
				}
			}
		}
	}
	for _, message := range req.Messages {
		role := strings.TrimSpace(message.Role)
		if role == "" {
			role = "message"
		}
		if message.IsStringContent() {
			lines = appendNamedLine(lines, role, message.GetStringContent())
			continue
		}
		content, _ := message.ParseContent()
		var parts []string
		for _, media := range content {
			switch media.Type {
			case "text":
				if hasCacheControl(media.CacheControl) {
					parts = append(parts, "[缓存正文未保存]")
				} else {
					parts = append(parts, media.GetText())
				}
			case "image":
				parts = append(parts, "[图片内容未保存]")
			default:
				parts = append(parts, "[多模态内容未保存]")
			}
		}
		if len(parts) > 0 {
			lines = append(lines, role+": "+strings.Join(parts, "\n"))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractGeminiRequestText(req *dto.GeminiChatRequest) string {
	var lines []string
	if req.SystemInstructions != nil {
		for _, part := range req.SystemInstructions.Parts {
			if part.Text != "" {
				lines = appendNamedLine(lines, "system", part.Text)
			}
		}
	}
	for _, content := range req.Contents {
		role := strings.TrimSpace(content.Role)
		if role == "" {
			role = "message"
		}
		var parts []string
		for _, part := range content.Parts {
			if part.Text != "" {
				parts = append(parts, part.Text)
			}
			if part.InlineData != nil {
				if strings.HasPrefix(strings.ToLower(part.InlineData.MimeType), "image/") {
					parts = append(parts, "[图片内容未保存]")
				} else {
					parts = append(parts, "[多模态内容未保存]")
				}
			}
			if part.FileData != nil {
				parts = append(parts, "[文件内容未保存]")
			}
		}
		if len(parts) > 0 {
			lines = append(lines, role+": "+strings.Join(parts, "\n"))
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func ExtractConversationResponseText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "\ndata:") || strings.HasPrefix(raw, "data:") {
		return extractSSEText(raw)
	}
	if gjson.Valid(raw) {
		return extractJSONResponseText(raw)
	}
	return raw
}

func ExtractConversationStreamResponseTextFromReader(r io.Reader) (string, string, error) {
	reader := bufio.NewReaderSize(r, 64*1024)
	var (
		parts    []string
		metadata strings.Builder
	)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			appendStreamResponseLine(line, &parts, &metadata)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return metadata.String(), strings.TrimSpace(strings.Join(parts, "")), err
		}
	}
	return metadata.String(), strings.TrimSpace(strings.Join(parts, "")), nil
}

func appendStreamResponseLine(line string, parts *[]string, metadata *strings.Builder) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "data:") {
		return
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "" || data == "[DONE]" {
		return
	}
	if text := extractSSEEventText(data); text != "" {
		*parts = append(*parts, text)
	}
	if strings.Contains(data, `"usage"`) || strings.Contains(data, `"usageMetadata"`) {
		metadata.WriteString("data: ")
		metadata.WriteString(data)
		metadata.WriteByte('\n')
	}
}

func extractSSEText(raw string) string {
	var parts []string
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		text := extractSSEEventText(data)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, ""))
}

func extractSSEEventText(raw string) string {
	if !gjson.Valid(raw) {
		return ""
	}
	eventType := gjson.Get(raw, "type").String()
	switch eventType {
	case "response.output_text.delta":
		return gjson.Get(raw, "delta").String()
	case "response.output_text.done":
		return ""
	}
	return extractJSONResponseText(raw)
}

func extractJSONResponseText(raw string) string {
	var values []string
	paths := []string{
		"choices.#.message.content",
		"choices.#.delta.content",
		"choices.#.text",
		"text",
		"output_text",
		"output.#.content.#.text",
		"output.#.content.#.text.value",
		"output.#.content.#.text.annotations.#.text",
		"content.#.text",
		"content.#.text.value",
		"candidates.#.content.parts.#.text",
		"completion",
		"message.content",
		"error.message",
	}
	for _, path := range paths {
		result := gjson.Get(raw, path)
		if !result.Exists() {
			continue
		}
		result.ForEach(func(_, value gjson.Result) bool {
			if value.IsArray() {
				value.ForEach(func(_, item gjson.Result) bool {
					appendJSONText(&values, item)
					return true
				})
				return true
			}
			appendJSONText(&values, value)
			return true
		})
		appendJSONText(&values, result)
	}
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(values, ""))
}

func appendJSONText(values *[]string, value gjson.Result) {
	if !value.Exists() {
		return
	}
	if value.IsArray() || value.IsObject() {
		return
	}
	text := value.String()
	if text != "" {
		*values = append(*values, text)
	}
}

func extractPromptTokens(raw string) int {
	return firstJSONInt(raw, "usage.prompt_tokens", "usage.input_tokens", "usageMetadata.promptTokenCount")
}

func extractCompletionTokens(raw string) int {
	return firstJSONInt(raw, "usage.completion_tokens", "usage.output_tokens", "usageMetadata.candidatesTokenCount")
}

func extractTotalTokens(raw string) int {
	return firstJSONInt(raw, "usage.total_tokens", "usageMetadata.totalTokenCount")
}

func extractCacheTokens(raw string) int {
	return firstJSONInt(raw, "usage.prompt_tokens_details.cached_tokens", "usage.input_tokens_details.cached_tokens")
}

func firstJSONInt(raw string, paths ...string) int {
	if raw == "" {
		return 0
	}
	for _, path := range paths {
		var found int
		ok := false
		if strings.Contains(raw, "\ndata:") || strings.HasPrefix(raw, "data:") {
			for _, line := range strings.Split(raw, "\n") {
				line = strings.TrimSpace(line)
				if !strings.HasPrefix(line, "data:") {
					continue
				}
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if data == "" || data == "[DONE]" || !gjson.Valid(data) {
					continue
				}
				v := gjson.Get(data, path)
				if v.Exists() && v.Int() > 0 {
					found = int(v.Int())
					ok = true
				}
			}
		} else if gjson.Valid(raw) {
			v := gjson.Get(raw, path)
			if v.Exists() && v.Int() > 0 {
				found = int(v.Int())
				ok = true
			}
		}
		if ok {
			return found
		}
	}
	return 0
}

func ValidateConversationExportRequest(req ConversationExportRequest) error {
	if req.Filter.StartTime <= 0 || req.Filter.EndTime <= 0 || req.Filter.StartTime > req.Filter.EndTime {
		return fmt.Errorf("时间范围不能为空")
	}
	if req.Filter.EndTime-req.Filter.StartTime > int64(maxConversationExportRangeDays*24*time.Hour/time.Second) {
		return fmt.Errorf("单次最多导出 %d 天", maxConversationExportRangeDays)
	}
	if len(req.Fields) == 0 {
		return fmt.Errorf("请至少选择一个导出字段")
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = model.ConversationExportModePlain
	}
	if mode != model.ConversationExportModePlain && mode != model.ConversationExportModeStrict {
		return fmt.Errorf("导出模式不正确")
	}
	for _, field := range req.Fields {
		if _, ok := conversationExportFieldLabels[field]; !ok {
			return fmt.Errorf("不支持的导出字段: %s", field)
		}
	}
	return nil
}

func CreateConversationExportTask(req ConversationExportRequest, createdBy int) (*model.ConversationExportTask, error) {
	if err := ValidateConversationExportRequest(req); err != nil {
		return nil, err
	}
	filterJSON, err := common.Marshal(req.Filter)
	if err != nil {
		return nil, err
	}
	fieldsJSON, err := common.Marshal(req.Fields)
	if err != nil {
		return nil, err
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = model.ConversationExportModePlain
	}
	now := common.GetTimestamp()
	task := &model.ConversationExportTask{
		Status:    model.ConversationExportStatusPending,
		Mode:      mode,
		Filters:   string(filterJSON),
		Fields:    string(fieldsJSON),
		CreatedBy: createdBy,
		CreatedAt: now,
		ExpiresAt: now + int64(conversationExportRetentionHour*time.Hour/time.Second),
	}
	if err := model.InsertConversationExportTask(task); err != nil {
		return nil, err
	}
	gopool.Go(func() {
		RunConversationExportTask(task.Id)
	})
	return task, nil
}

func RunConversationExportTask(taskId int) {
	task, err := model.GetConversationExportTask(taskId)
	if err != nil {
		common.SysLog("failed to get conversation export task: " + err.Error())
		return
	}
	now := common.GetTimestamp()
	_ = model.UpdateConversationExportTask(task.Id, map[string]interface{}{
		"status":     model.ConversationExportStatusRunning,
		"started_at": now,
	})
	var filter ConversationExportFilter
	var fields []string
	if err := common.Unmarshal([]byte(task.Filters), &filter); err != nil {
		failConversationExportTask(task.Id, "解析筛选条件失败: "+err.Error())
		return
	}
	if err := common.Unmarshal([]byte(task.Fields), &fields); err != nil {
		failConversationExportTask(task.Id, "解析字段失败: "+err.Error())
		return
	}

	dir, err := ensureConversationExportDir()
	if err != nil {
		failConversationExportTask(task.Id, "创建导出目录失败: "+err.Error())
		return
	}
	fileName := fmt.Sprintf("conversation-export-%d-%s.csv", task.Id, time.Now().In(shanghaiLocation).Format("20060102150405"))
	filePath := filepath.Join(dir, fileName)
	file, err := os.Create(filePath)
	if err != nil {
		failConversationExportTask(task.Id, "创建 CSV 文件失败: "+err.Error())
		return
	}
	defer func() { _ = file.Close() }()
	_, _ = file.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(file)
	headers := make([]string, 0, len(fields))
	for _, field := range fields {
		headers = append(headers, conversationExportFieldLabels[field])
	}
	if err := writer.Write(headers); err != nil {
		failConversationExportTaskWithOpenFile(task.Id, filePath, file, "写入 CSV 失败: "+err.Error())
		return
	}

	tx := applyConversationExportFilter(model.ConversationSnapshotQuery(), filter)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		failConversationExportTaskWithOpenFile(task.Id, filePath, file, "统计快照失败: "+err.Error())
		return
	}
	const batchSize = 500
	var exported int64
	var lastId int
	var lastCreatedAt int64
	for {
		var snapshots []*model.ConversationSnapshot
		batchTx := applyConversationExportFilter(model.ConversationSnapshotQuery(), filter)
		if lastCreatedAt > 0 {
			batchTx = batchTx.Where("(created_at > ? OR (created_at = ? AND id > ?))", lastCreatedAt, lastCreatedAt, lastId)
		}
		batchTx = batchTx.Order("created_at asc, id asc").Limit(batchSize)
		if err := batchTx.Find(&snapshots).Error; err != nil {
			failConversationExportTaskWithOpenFile(task.Id, filePath, file, "读取快照失败: "+err.Error())
			return
		}
		if len(snapshots) == 0 {
			break
		}
		for _, snapshot := range snapshots {
			lastId = snapshot.Id
			lastCreatedAt = snapshot.CreatedAt
			row := make([]string, 0, len(fields))
			for _, field := range fields {
				value := conversationSnapshotFieldValue(snapshot, field)
				if task.Mode == model.ConversationExportModeStrict && conversationExportTextFields[field] {
					value = StrictRedactExportText(value)
				}
				value = SafeCSVCell(value)
				row = append(row, value)
			}
			if err := writer.Write(row); err != nil {
				failConversationExportTaskWithOpenFile(task.Id, filePath, file, "写入 CSV 行失败: "+err.Error())
				return
			}
			exported++
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			failConversationExportTaskWithOpenFile(task.Id, filePath, file, "刷新 CSV 失败: "+err.Error())
			return
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		failConversationExportTaskWithOpenFile(task.Id, filePath, file, "保存 CSV 失败: "+err.Error())
		return
	}
	stat, err := file.Stat()
	if err != nil {
		failConversationExportTaskWithOpenFile(task.Id, filePath, file, "读取 CSV 文件信息失败: "+err.Error())
		return
	}
	finishedAt := common.GetTimestamp()
	_ = model.UpdateConversationExportTask(task.Id, map[string]interface{}{
		"status":      model.ConversationExportStatusSucceeded,
		"finished_at": finishedAt,
		"expires_at":  finishedAt + int64(conversationExportRetentionHour*time.Hour/time.Second),
		"file_name":   fileName,
		"file_path":   filePath,
		"file_size":   stat.Size(),
		"total_rows":  exported,
	})
}

func failConversationExportTask(taskId int, reason string) {
	_ = model.UpdateConversationExportTask(taskId, map[string]interface{}{
		"status":         model.ConversationExportStatusFailed,
		"finished_at":    common.GetTimestamp(),
		"failure_reason": reason,
	})
}

func failConversationExportTaskWithFile(taskId int, filePath string, reason string) {
	if strings.TrimSpace(filePath) != "" {
		if dir, err := ensureConversationExportDir(); err == nil && isConversationExportPath(dir, filePath) {
			_ = os.Remove(filePath)
		}
	}
	failConversationExportTask(taskId, reason)
}

func failConversationExportTaskWithOpenFile(taskId int, filePath string, file *os.File, reason string) {
	if file != nil {
		_ = file.Close()
	}
	failConversationExportTaskWithFile(taskId, filePath, reason)
}

func applyConversationExportFilter(tx *gorm.DB, filter ConversationExportFilter) *gorm.DB {
	tx = tx.Where("created_at >= ? AND created_at <= ?", filter.StartTime, filter.EndTime)
	if filter.UserId > 0 {
		tx = tx.Where("user_id = ?", filter.UserId)
	}
	if filter.TokenId > 0 {
		tx = tx.Where("token_id = ?", filter.TokenId)
	}
	if strings.TrimSpace(filter.ModelName) != "" {
		tx = tx.Where("model_name = ?", strings.TrimSpace(filter.ModelName))
	}
	if strings.TrimSpace(filter.Group) != "" {
		tx = tx.Where("group_name = ?", strings.TrimSpace(filter.Group))
	}
	return tx
}

var conversationExportFieldLabels = map[string]string{
	"time":              "时间",
	"username":          "用户名",
	"api_key":           "API Key",
	"model":             "模型",
	"request_content":   "请求内容",
	"response_content":  "响应内容",
	"user_id":           "用户 ID",
	"token_id":          "密钥 ID",
	"token_name":        "密钥名称",
	"group":             "分组",
	"prompt_tokens":     "输入 Tokens",
	"completion_tokens": "输出 Tokens",
	"total_tokens":      "总 Tokens",
	"cache_tokens":      "缓存 Tokens",
	"channel_id":        "渠道 ID",
	"channel_name":      "渠道名称",
}

var conversationExportTextFields = map[string]bool{
	"username":         true,
	"api_key":          true,
	"model":            true,
	"request_content":  true,
	"response_content": true,
	"token_name":       true,
	"group":            true,
	"channel_name":     true,
}

func conversationSnapshotFieldValue(snapshot *model.ConversationSnapshot, field string) string {
	if snapshot == nil {
		return ""
	}
	switch field {
	case "time":
		return time.Unix(snapshot.CreatedAt, 0).In(shanghaiLocation).Format("2006-01-02 15:04:05")
	case "username":
		return snapshot.Username
	case "api_key":
		return snapshot.TokenKey
	case "model":
		return snapshot.ModelName
	case "request_content":
		return snapshot.RequestText
	case "response_content":
		return snapshot.ResponseText
	case "user_id":
		return strconv.Itoa(snapshot.UserId)
	case "token_id":
		return strconv.Itoa(snapshot.TokenId)
	case "token_name":
		return snapshot.TokenName
	case "group":
		return snapshot.Group
	case "prompt_tokens":
		return strconv.Itoa(snapshot.PromptTokens)
	case "completion_tokens":
		return strconv.Itoa(snapshot.CompletionTokens)
	case "total_tokens":
		return strconv.Itoa(snapshot.TotalTokens)
	case "cache_tokens":
		return strconv.Itoa(snapshot.CacheTokens)
	case "channel_id":
		return strconv.Itoa(snapshot.ChannelId)
	case "channel_name":
		return snapshot.ChannelName
	default:
		return ""
	}
}

func ensureConversationExportDir() (string, error) {
	dir := filepath.Join(".", "data", conversationExportDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func ConversationExportFilePath(task *model.ConversationExportTask) (string, error) {
	if task == nil {
		return "", fmt.Errorf("导出任务不存在")
	}
	if task.Status != model.ConversationExportStatusSucceeded {
		return "", fmt.Errorf("导出任务尚未完成")
	}
	if task.ExpiresAt > 0 && task.ExpiresAt < common.GetTimestamp() {
		_ = model.UpdateConversationExportTask(task.Id, map[string]interface{}{"status": model.ConversationExportStatusExpired})
		return "", fmt.Errorf("导出文件已过期")
	}
	if strings.TrimSpace(task.FilePath) == "" {
		return "", fmt.Errorf("导出文件不存在")
	}
	return safeConversationExportFilePath(task.FilePath)
}

func ConversationExportStoredFilePath(task *model.ConversationExportTask) (string, error) {
	if task == nil {
		return "", fmt.Errorf("导出任务不存在")
	}
	if strings.TrimSpace(task.FilePath) == "" {
		return "", fmt.Errorf("导出文件不存在")
	}
	return safeConversationExportFilePath(task.FilePath)
}

func safeConversationExportFilePath(filePath string) (string, error) {
	dir, err := ensureConversationExportDir()
	if err != nil {
		return "", err
	}
	if !isConversationExportPath(dir, filePath) {
		return "", fmt.Errorf("导出文件路径不合法")
	}
	if _, err := os.Stat(filePath); err != nil {
		return "", fmt.Errorf("导出文件不存在")
	}
	return filePath, nil
}

func isConversationExportPath(baseDir string, target string) bool {
	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func CleanupExpiredConversationExports() {
	now := time.Now()
	_ = model.MarkExpiredConversationExportTasks(now)
	dir, err := ensureConversationExportDir()
	if err != nil {
		common.SysLog("failed to ensure conversation export dir: " + err.Error())
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		common.SysLog("failed to read conversation export dir: " + err.Error())
		return
	}
	cutoff := now.Add(-conversationExportRetentionHour * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
		}
	}
}

func CleanupOldConversationSnapshots(ctx context.Context) (int64, error) {
	days := common.ConversationSnapshotRetentionDays
	if days <= 0 {
		return 0, nil
	}
	target := time.Now().In(shanghaiLocation).AddDate(0, 0, -days).Unix()
	return model.DeleteOldConversationSnapshots(ctx, target, 1000)
}

func StartConversationSnapshotMaintenanceTask() {
	if !common.IsMasterNode {
		return
	}
	gopool.Go(func() {
		runConversationSnapshotMaintenance()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			runConversationSnapshotMaintenance()
		}
	})
}

func runConversationSnapshotMaintenance() {
	CleanupExpiredConversationExports()
	deleted, err := CleanupOldConversationSnapshots(context.Background())
	if err != nil {
		common.SysLog("conversation snapshot cleanup failed: " + err.Error())
		return
	}
	if deleted > 0 {
		common.SysLog(fmt.Sprintf("conversation snapshot cleanup deleted %d rows", deleted))
	}
}

func DeleteConversationSnapshotsInRange(ctx context.Context, startTimestamp int64, endTimestamp int64) (int64, error) {
	if startTimestamp <= 0 || endTimestamp <= 0 || startTimestamp > endTimestamp {
		return 0, fmt.Errorf("时间范围不能为空")
	}
	return model.DeleteConversationSnapshotsByTimeRange(ctx, startTimestamp, endTimestamp, 1000)
}

var strictRedactionRules = []struct {
	re          *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile(`(?i)\b(bearer|authorization|api[-_ ]?key|secret[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|session[-_ ]?id|cookie)\b\s*[:=]\s*["']?[^"'\s,;]+`), "$1=[密钥已脱敏]"},
	{regexp.MustCompile(`(?i)\b(sk-[A-Za-z0-9_\-]{12,}|pk-[A-Za-z0-9_\-]{12,}|ak-[A-Za-z0-9_\-]{12,})\b`), "[密钥已脱敏]"},
	{regexp.MustCompile(`(?i)\b(password|passwd|pwd|client_secret|private_key|secret)\b\s*[:=]\s*["']?[^"'\s,;]+`), "$1=[密钥已脱敏]"},
	{regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`), "[邮箱已脱敏]"},
	{regexp.MustCompile(`(?i)(\+?\d{1,4}[-\s]?)?1[3-9]\d{9}\b`), "[电话已脱敏]"},
	{regexp.MustCompile(`(?i)\b(?:\+?\d[\d\-\s]{7,}\d)\b`), "[电话已脱敏]"},
	{regexp.MustCompile(`(?i)\b[1-9]\d{5}(?:18|19|20)\d{2}(?:0[1-9]|1[0-2])(?:0[1-9]|[12]\d|3[01])\d{3}[\dXx]\b`), "[证件号已脱敏]"},
	{regexp.MustCompile(`(?i)\b[A-Z]\d{7,8}\b`), "[证件号已脱敏]"},
	{regexp.MustCompile(`\b(?:\d[ -]*?){13,19}\b`), "[银行卡已脱敏]"},
	{regexp.MustCompile(`(?i)\b(cvv|cvc|security code)\b\s*[:=]?\s*\d{3,4}\b`), "[银行卡安全码已脱敏]"},
	{regexp.MustCompile(`(?i)\b(alipay|paypal)\b\s*[:=]\s*[^"'\s,;]+`), "$1=[支付账号已脱敏]"},
	{regexp.MustCompile(`(?i)\b(bc1|[13])[a-zA-HJ-NP-Z0-9]{25,62}\b`), "[加密货币地址已脱敏]"},
	{regexp.MustCompile(`\b0x[a-fA-F0-9]{40}\b`), "[加密货币地址已脱敏]"},
	{regexp.MustCompile(`\bT[A-Za-z1-9]{33}\b`), "[加密货币地址已脱敏]"},
	{regexp.MustCompile(`(?i)\b(?:mnemonic|seed phrase|助记词)\b\s*[:=]\s*[^。；;\n]+`), "[助记词已脱敏]"},
	{regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), "[私钥已脱敏]"},
	{regexp.MustCompile(`(?i)\b[0-9A-HJ-NPQRTUWXY]{18}\b`), "[公司信息已脱敏]"},
	{regexp.MustCompile(`(?i)(公司名称|发票抬头|企业名称)\s*[:：]\s*[^，。；;\n]+`), "$1：[公司名称已脱敏]"},
	{regexp.MustCompile(`(?i)(对公账户|公司账户|银行账号)\s*[:：]\s*[\d -]{8,}`), "$1：[公司账户已脱敏]"},
	{regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`), "[IP 已脱敏]"},
	{regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){2,7}[0-9a-f]{1,4}\b`), "[IP 已脱敏]"},
	{regexp.MustCompile(`(?i)([?&](?:token|key|secret|password|code)=)[^&#\s]+`), "$1[密钥已脱敏]"},
	{regexp.MustCompile(`[\p{Han}]{2,}(省|市|自治区|区|县|镇|乡|街道|路|街|巷|号楼|单元|室)[\p{Han}\dA-Za-z\-号楼单元室幢栋]+`), "[地址已脱敏]"},
}

func StrictRedactExportText(input string) string {
	if input == "" {
		return ""
	}
	output := input
	for _, rule := range strictRedactionRules {
		output = rule.re.ReplaceAllString(output, rule.replacement)
	}
	return output
}

func SafeCSVCell(input string) string {
	if input == "" {
		return ""
	}
	switch input[0] {
	case '=', '+', '-', '@', '\t', '\r', '\n':
		return "'" + input
	default:
		return input
	}
}
