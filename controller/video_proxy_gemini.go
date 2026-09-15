package controller

import (
	"fmt"
	neturl "net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	"github.com/QuantumNous/new-api/service"
)

var acquireVertexAccessToken = vertexcore.AcquireAccessToken

func decorateGeminiMediaURL(videoURL, apiKey string, extraHosts ...string) (string, map[string]string, error) {
	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		return "", nil, fmt.Errorf("gemini video url not found")
	}
	if taskcommon.IsDataURL(videoURL) {
		return videoURL, nil, nil
	}
	if taskcommon.IsGCSURI(videoURL) || taskcommon.IsUnsignedGCSHTTPS(videoURL) {
		return "", nil, fmt.Errorf("gemini gcs uri not retrievable with api key")
	}
	if !geminiMediaURLNeedsAPIKey(videoURL, extraHosts...) {
		return videoURL, nil, nil
	}
	headers := map[string]string{}
	if strings.TrimSpace(apiKey) != "" {
		headers["x-goog-api-key"] = apiKey
	}
	return ensureAPIKey(videoURL, apiKey), headers, nil
}

func geminiMediaURLNeedsAPIKey(videoURL string, extraHosts ...string) bool {
	hostname := model.MediaHostFromBaseURL(videoURL)
	switch hostname {
	case "generativelanguage.googleapis.com", "ai.google.dev", "files.googleapis.com":
		return true
	}
	for _, extra := range extraHosts {
		if extraHost := model.MediaHostFromBaseURL(extra); extraHost != "" && extraHost == hostname {
			return true
		}
	}
	return false
}

func geminiChannelMediaHosts(channel *model.Channel) []string {
	if channel == nil {
		return nil
	}
	baseURL := strings.TrimSpace(channel.GetBaseURL())
	if baseURL == "" {
		baseURL = constant.GetChannelBaseURL(channel.Type)
	}
	host := model.MediaHostFromBaseURL(baseURL)
	if host == "" {
		return nil
	}
	return []string{host}
}

func getGeminiVideoURL(channel *model.Channel, task *model.Task, apiKey string) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("invalid channel or task")
	}

	if url := storedRetrievableTaskMediaURL(task); url != "" {
		return url, nil
	}
	if url := extractGeminiVideoURLFromTaskData(task); url != "" {
		return url, nil
	}

	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	adaptor := relay.GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channel.Type)))
	if adaptor == nil {
		return "", fmt.Errorf("gemini task adaptor not found")
	}

	if apiKey == "" {
		return "", fmt.Errorf("api key not available for task")
	}

	proxy := channel.GetSetting().Proxy
	resp, err := adaptor.FetchTask(baseURL, apiKey, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil {
		return "", fmt.Errorf("fetch task failed: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return "", fmt.Errorf("fetch task returned empty response")
	}
	defer resp.Body.Close()

	body, err := service.ReadResponseBodyLimited(resp, service.MaxResponseBodyBytes)
	if err != nil {
		return "", fmt.Errorf("read task response failed: %w", err)
	}

	taskInfo, parseErr := adaptor.ParseTaskResult(body)
	if parseErr == nil && taskInfo != nil && taskInfo.RemoteUrl != "" {
		return taskInfo.RemoteUrl, nil
	}

	if url := extractGeminiVideoURLFromPayload(body); url != "" {
		return url, nil
	}

	if parseErr != nil {
		return "", fmt.Errorf("parse task result failed: %w", parseErr)
	}

	return "", fmt.Errorf("gemini video url not found")
}

func extractGeminiVideoURLFromTaskData(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	var payload map[string]any
	if err := common.Unmarshal(task.Data, &payload); err != nil {
		return ""
	}
	return extractGeminiVideoURLFromMap(payload)
}

func extractGeminiVideoURLFromPayload(body []byte) string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return extractGeminiVideoURLFromMap(payload)
}

func extractGeminiVideoURLFromMap(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if uri, ok := payload["uri"].(string); ok && uri != "" {
		return uri
	}
	if resp, ok := payload["response"].(map[string]any); ok {
		if uri := extractGeminiVideoURLFromResponse(resp); uri != "" {
			return uri
		}
	}
	return ""
}

func extractGeminiVideoURLFromResponse(resp map[string]any) string {
	if resp == nil {
		return ""
	}
	if gvr, ok := resp["generateVideoResponse"].(map[string]any); ok {
		if uri := extractGeminiVideoURLFromGeneratedVideos(gvr); uri != "" {
			return uri
		}
		if uri := extractGeminiVideoURLFromGeneratedSamples(gvr); uri != "" {
			return uri
		}
	}
	if videos, ok := resp["videos"].([]any); ok {
		for _, video := range videos {
			if vm, ok := video.(map[string]any); ok {
				if uri, ok := vm["uri"].(string); ok && uri != "" {
					return uri
				}
				if b64, _ := vm["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
					mime, _ := vm["mimeType"].(string)
					enc, _ := vm["encoding"].(string)
					return buildVideoDataURL(mime, enc, b64)
				}
			}
		}
	}
	if b64, _ := resp["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, b64)
	}
	if uri, ok := resp["video"].(string); ok && uri != "" {
		lowerURI := strings.ToLower(uri)
		if taskcommon.IsDataURL(uri) || strings.HasPrefix(lowerURI, "http://") || strings.HasPrefix(lowerURI, "https://") {
			return uri
		}
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, uri)
	}
	if uri, ok := resp["uri"].(string); ok && uri != "" {
		return uri
	}
	return ""
}

func extractGeminiVideoURLFromGeneratedVideos(gvr map[string]any) string {
	if gvr == nil {
		return ""
	}
	if videos, ok := gvr["generatedVideos"].([]any); ok {
		for _, video := range videos {
			if vm, ok := video.(map[string]any); ok {
				if nested, ok := vm["video"].(map[string]any); ok {
					if uri, ok := nested["uri"].(string); ok && uri != "" {
						return uri
					}
					if b64, _ := nested["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
						mime, _ := nested["mimeType"].(string)
						enc, _ := nested["encoding"].(string)
						return buildVideoDataURL(mime, enc, b64)
					}
				}
			}
		}
	}
	return ""
}

func extractGeminiVideoURLFromGeneratedSamples(gvr map[string]any) string {
	if gvr == nil {
		return ""
	}
	if samples, ok := gvr["generatedSamples"].([]any); ok {
		for _, sample := range samples {
			if sm, ok := sample.(map[string]any); ok {
				if video, ok := sm["video"].(map[string]any); ok {
					if uri, ok := video["uri"].(string); ok && uri != "" {
						return uri
					}
					if b64, _ := video["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
						mime, _ := video["mimeType"].(string)
						enc, _ := video["encoding"].(string)
						return buildVideoDataURL(mime, enc, b64)
					}
				}
			}
		}
	}
	return ""
}

func getVertexVideoURL(channel *model.Channel, task *model.Task) (string, error) {
	if channel == nil || task == nil {
		return "", fmt.Errorf("invalid channel or task")
	}
	if url := storedRetrievableTaskMediaURL(task); url != "" {
		return url, nil
	}
	if url := strings.TrimSpace(task.GetResultURL()); url != "" && !isTaskProxyContentURL(url, task.TaskID) {
		return url, nil
	}
	if url := extractVertexVideoURLFromTaskData(task); url != "" {
		return url, nil
	}

	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	adaptor := relay.GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channel.Type)))
	if adaptor == nil {
		return "", fmt.Errorf("vertex task adaptor not found")
	}

	key := getVertexTaskKey(channel, task)
	if key == "" {
		return "", fmt.Errorf("vertex key not available for task")
	}

	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, channel.GetSetting().Proxy)
	if err != nil {
		return "", fmt.Errorf("fetch task failed: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return "", fmt.Errorf("fetch task returned empty response")
	}
	defer resp.Body.Close()

	body, err := service.ReadResponseBodyLimited(resp, service.MaxResponseBodyBytes)
	if err != nil {
		return "", fmt.Errorf("read task response failed: %w", err)
	}

	taskInfo, parseErr := adaptor.ParseTaskResult(body)
	if parseErr == nil && taskInfo != nil {
		if url := firstNonEmptyTrimmed(taskInfo.Url, taskInfo.RemoteUrl); url != "" {
			return url, nil
		}
	}
	if url := extractVertexVideoURLFromPayload(body); url != "" {
		return url, nil
	}
	if parseErr != nil {
		return "", fmt.Errorf("parse task result failed: %w", parseErr)
	}
	return "", fmt.Errorf("vertex video url not found")
}

func decorateVertexMediaURL(channel *model.Channel, task *model.Task, videoURL string) (string, map[string]string, error) {
	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		return "", nil, fmt.Errorf("vertex video url not found")
	}
	if taskcommon.IsGCSURI(videoURL) {
		httpsURL, ok := taskcommon.GCSURIToHTTPS(videoURL)
		if !ok {
			return "", nil, fmt.Errorf("invalid vertex gcs uri")
		}
		token, err := vertexAccessTokenForChannel(channel, task)
		if err != nil {
			return "", nil, err
		}
		return httpsURL, map[string]string{"Authorization": "Bearer " + token}, nil
	}
	if vertexStorageHTTPSNeedsAuth(videoURL) {
		token, err := vertexAccessTokenForChannel(channel, task)
		if err != nil {
			return "", nil, err
		}
		return videoURL, map[string]string{"Authorization": "Bearer " + token}, nil
	}
	return videoURL, nil, nil
}

func vertexStorageHTTPSNeedsAuth(rawURL string) bool {
	parsed, err := neturl.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed == nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "storage.googleapis.com" && host != "storage.cloud.google.com" {
		return false
	}
	query := parsed.Query()
	return strings.TrimSpace(query.Get("X-Goog-Signature")) == "" &&
		strings.TrimSpace(query.Get("X-Goog-Algorithm")) == "" &&
		strings.TrimSpace(query.Get("GoogleAccessId")) == "" &&
		strings.TrimSpace(query.Get("X-Goog-Credential")) == ""
}

func vertexAccessTokenForChannel(channel *model.Channel, task *model.Task) (string, error) {
	key := getVertexTaskKey(channel, task)
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("vertex key not available for task")
	}
	var creds vertexcore.Credentials
	if err := common.Unmarshal([]byte(key), &creds); err != nil {
		return "", fmt.Errorf("failed to decode vertex credentials: %w", err)
	}
	if strings.TrimSpace(creds.ClientEmail) == "" || strings.TrimSpace(creds.PrivateKey) == "" {
		return "", fmt.Errorf("vertex credentials missing client_email or private_key")
	}
	proxy := ""
	if channel != nil {
		proxy = strings.TrimSpace(channel.GetSetting().Proxy)
	}
	token, err := acquireVertexAccessToken(creds, proxy)
	if err != nil {
		return "", fmt.Errorf("failed to acquire vertex access token: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return "", fmt.Errorf("vertex access token is empty")
	}
	return token, nil
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func isTaskProxyContentURL(url string, taskID string) bool {
	if strings.TrimSpace(url) == "" || strings.TrimSpace(taskID) == "" {
		return false
	}
	return strings.Contains(url, "/v1/videos/"+taskID+"/content")
}

func getVertexTaskKey(channel *model.Channel, task *model.Task) string {
	return getTaskChannelKey(channel, task)
}

func getGeminiTaskKey(channel *model.Channel, task *model.Task) string {
	return getTaskChannelKey(channel, task)
}

func getTaskChannelKey(channel *model.Channel, task *model.Task) string {
	if task != nil {
		if key := strings.TrimSpace(task.PrivateData.Key); key != "" {
			return key
		}
	}
	if channel == nil {
		return ""
	}
	keys := channel.GetKeys()
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key != "" {
			return key
		}
	}
	return strings.TrimSpace(channel.Key)
}

func extractVertexVideoURLFromTaskData(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	return extractVertexVideoURLFromPayload(task.Data)
}

func extractVertexVideoURLFromPayload(body []byte) string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return ""
	}
	resp, ok := payload["response"].(map[string]any)
	if !ok || resp == nil {
		return ""
	}

	var httpURL, gcsURL string
	collectURI := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		switch {
		case strings.HasPrefix(strings.ToLower(value), "http://"), strings.HasPrefix(strings.ToLower(value), "https://"):
			if httpURL == "" {
				httpURL = value
			}
		case taskcommon.IsGCSURI(value):
			if gcsURL == "" {
				gcsURL = value
			}
		}
	}

	if videos, ok := resp["videos"].([]any); ok && len(videos) > 0 {
		if video, ok := videos[0].(map[string]any); ok && video != nil {
			collectURI(asJSONString(video["uri"]))
			collectURI(asJSONString(video["gcsUri"]))
		}
	}
	if gvr, ok := resp["generateVideoResponse"].(map[string]any); ok && gvr != nil {
		if generated, ok := gvr["generatedVideos"].([]any); ok && len(generated) > 0 {
			if item, ok := generated[0].(map[string]any); ok && item != nil {
				if video, ok := item["video"].(map[string]any); ok && video != nil {
					collectURI(asJSONString(video["uri"]))
					collectURI(asJSONString(video["gcsUri"]))
				}
			}
		}
	}
	if httpURL != "" {
		return httpURL
	}
	if gcsURL != "" {
		return gcsURL
	}

	if videos, ok := resp["videos"].([]any); ok && len(videos) > 0 {
		if video, ok := videos[0].(map[string]any); ok && video != nil {
			if b64, _ := video["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
				mime, _ := video["mimeType"].(string)
				enc, _ := video["encoding"].(string)
				return buildVideoDataURL(mime, enc, b64)
			}
		}
	}
	if b64, _ := resp["bytesBase64Encoded"].(string); strings.TrimSpace(b64) != "" {
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, b64)
	}
	if video, _ := resp["video"].(string); strings.TrimSpace(video) != "" {
		if taskcommon.IsDataURL(video) || strings.HasPrefix(strings.ToLower(video), "http://") || strings.HasPrefix(strings.ToLower(video), "https://") {
			return video
		}
		enc, _ := resp["encoding"].(string)
		return buildVideoDataURL("", enc, video)
	}
	return ""
}

func asJSONString(value any) string {
	s, _ := value.(string)
	return s
}

func buildVideoDataURL(mimeType string, encoding string, base64Data string) string {
	mime := strings.TrimSpace(mimeType)
	if mime == "" {
		enc := strings.TrimSpace(encoding)
		if enc == "" {
			enc = "mp4"
		}
		if strings.Contains(enc, "/") {
			mime = enc
		} else {
			mime = "video/" + enc
		}
	}
	return "data:" + mime + ";base64," + base64Data
}

func ensureAPIKey(uri, key string) string {
	uri = strings.TrimSpace(uri)
	key = strings.TrimSpace(key)
	if key == "" || uri == "" {
		return uri
	}
	parsed, err := neturl.Parse(uri)
	if err != nil || parsed == nil {
		if strings.Contains(uri, "?") {
			return fmt.Sprintf("%s&key=%s", uri, neturl.QueryEscape(key))
		}
		return fmt.Sprintf("%s?key=%s", uri, neturl.QueryEscape(key))
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "", "http", "https":
	default:
		return uri
	}
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		query := parsed.Query()
		if query.Has("key") {
			return uri
		}
		query.Set("key", key)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	if strings.Contains(uri, "?") {
		return fmt.Sprintf("%s&key=%s", uri, neturl.QueryEscape(key))
	}
	return fmt.Sprintf("%s?key=%s", uri, neturl.QueryEscape(key))
}
