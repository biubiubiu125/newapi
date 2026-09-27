package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 上游地址
const (
	upstreamModelsURL  = "https://basellm.github.io/llm-metadata/api/newapi/models.json"
	upstreamVendorsURL = "https://basellm.github.io/llm-metadata/api/newapi/vendors.json"
)

func normalizeLocale(locale string) (string, bool) {
	l := strings.ToLower(strings.TrimSpace(locale))
	switch l {
	case "en", "ja":
		return l, true
	case "zh", "zh-cn", "zh_cn":
		return "zh", true
	case "zh-tw", "zh_tw":
		return "zh-TW", true
	default:
		return "", false
	}
}

func normalizeSyncSource(source string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(source))
	switch s {
	case "", "official":
		return "official", true
	default:
		return "", false
	}
}

func restrictMissingModels(current []string, requested []string) []string {
	if requested == nil {
		return nil
	}

	allowed := make(map[string]struct{}, len(requested))
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[name] = struct{}{}
		}
	}

	filtered := make([]string, 0, len(current))
	for _, name := range current {
		if _, ok := allowed[name]; ok {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func getUpstreamBase() string {
	return common.GetEnvOrDefaultString("SYNC_UPSTREAM_BASE", "https://basellm.github.io/llm-metadata")
}

func getUpstreamURLs(locale string) (modelsURL, vendorsURL string) {
	base := strings.TrimRight(getUpstreamBase(), "/")
	if l, ok := normalizeLocale(locale); ok && l != "" {
		return fmt.Sprintf("%s/api/i18n/%s/newapi/models.json", base, l),
			fmt.Sprintf("%s/api/i18n/%s/newapi/vendors.json", base, l)
	}
	return fmt.Sprintf("%s/api/newapi/models.json", base), fmt.Sprintf("%s/api/newapi/vendors.json", base)
}

type upstreamEnvelope[T any] struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    []T    `json:"data"`
}

type upstreamModel struct {
	Description string          `json:"description"`
	Endpoints   json.RawMessage `json:"endpoints"`
	Icon        string          `json:"icon"`
	ModelName   string          `json:"model_name"`
	NameRule    int             `json:"name_rule"`
	Status      *int            `json:"status"`
	Tags        string          `json:"tags"`
	VendorName  string          `json:"vendor_name"`
}

type upstreamVendor struct {
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Name        string `json:"name"`
	Status      *int   `json:"status"`
}

var (
	etagCache  = make(map[string]string)
	bodyCache  = make(map[string][]byte)
	cacheMutex sync.RWMutex
)

type overwriteField struct {
	ModelName string   `json:"model_name"`
	Fields    []string `json:"fields"`
}

type syncRequest struct {
	Overwrite   []overwriteField `json:"overwrite"`
	Locale      string           `json:"locale"`
	Source      string           `json:"source"`
	Missing     []string         `json:"missing"`
	SkipMissing bool             `json:"skip_missing"`
}

func newHTTPClient() *http.Client {
	timeoutSec := common.GetEnvOrDefault("SYNC_HTTP_TIMEOUT_SECONDS", 10)
	dialer := &net.Dialer{Timeout: time.Duration(timeoutSec) * time.Second}
	transport := &http.Transport{
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   time.Duration(timeoutSec) * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: time.Duration(timeoutSec) * time.Second,
	}
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		if strings.HasSuffix(host, "github.io") {
			if conn, err := dialer.DialContext(ctx, "tcp4", addr); err == nil {
				return conn, nil
			}
			return dialer.DialContext(ctx, "tcp6", addr)
		}
		return dialer.DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: transport}
}

var (
	httpClientOnce sync.Once
	httpClient     *http.Client
)

func getHTTPClient() *http.Client {
	httpClientOnce.Do(func() {
		httpClient = newHTTPClient()
	})
	return httpClient
}

func decodeUpstreamJSON[T any](buf []byte, out *upstreamEnvelope[T]) error {
	trimmed := strings.TrimSpace(string(buf))
	if trimmed == "" {
		return errors.New("invalid upstream response: empty body")
	}
	if strings.HasPrefix(trimmed, "[") {
		var arr []T
		if err := common.UnmarshalJsonStr(trimmed, &arr); err != nil {
			return err
		}
		*out = upstreamEnvelope[T]{Success: true, Data: arr}
		return nil
	}

	var env struct {
		Success *bool           `json:"success"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := common.UnmarshalJsonStr(trimmed, &env); err != nil {
		return err
	}
	if env.Success != nil && !*env.Success {
		message := strings.TrimSpace(env.Message)
		if message == "" {
			message = "upstream returned success=false"
		}
		return errors.New(message)
	}
	if len(env.Data) == 0 {
		return errors.New("invalid upstream response: missing data")
	}
	dataText := strings.TrimSpace(string(env.Data))
	if !strings.HasPrefix(dataText, "[") {
		return errors.New("invalid upstream response: data must be array")
	}
	var data []T
	if err := common.UnmarshalJsonStr(dataText, &data); err != nil {
		return fmt.Errorf("invalid upstream response data: %w", err)
	}
	*out = upstreamEnvelope[T]{
		Success: true,
		Message: env.Message,
		Data:    data,
	}
	return nil
}

func fetchJSON[T any](ctx context.Context, url string, out *upstreamEnvelope[T]) error {
	var lastErr error
	attempts := max(common.GetEnvOrDefault("SYNC_HTTP_RETRY", 3), 1)
	baseDelay := 200 * time.Millisecond
	maxMB := common.GetEnvOrDefault("SYNC_HTTP_MAX_MB", 10)
	maxBytes := int64(maxMB) << 20
	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		// ETag conditional request
		cacheMutex.RLock()
		if et := etagCache[url]; et != "" {
			req.Header.Set("If-None-Match", et)
		}
		cacheMutex.RUnlock()

		resp, err := getHTTPClient().Do(req)
		if err != nil {
			lastErr = err
			// backoff with jitter
			sleep := baseDelay * time.Duration(1<<attempt)
			jitter := time.Duration(rand.Intn(150)) * time.Millisecond
			time.Sleep(sleep + jitter)
			continue
		}
		func() {
			defer resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusOK:
				// read body into buffer for caching and flexible decode
				limited := io.LimitReader(resp.Body, maxBytes+1)
				buf, err := io.ReadAll(limited)
				if err != nil {
					lastErr = err
					return
				}
				var decoded upstreamEnvelope[T]
				if err := decodeUpstreamJSON(buf, &decoded); err != nil {
					lastErr = err
					return
				}

				// cache only successfully decoded upstream payloads
				cacheMutex.Lock()
				if et := resp.Header.Get("ETag"); et != "" {
					etagCache[url] = et
				}
				bodyCache[url] = buf
				cacheMutex.Unlock()

				*out = decoded
				lastErr = nil
			case http.StatusNotModified:
				// use cache
				cacheMutex.RLock()
				buf := bodyCache[url]
				cacheMutex.RUnlock()
				if len(buf) == 0 {
					lastErr = errors.New("cache miss for 304 response")
					return
				}
				if err := decodeUpstreamJSON(buf, out); err != nil {
					lastErr = err
					return
				}
				lastErr = nil
			default:
				lastErr = errors.New(resp.Status)
			}
		}()
		if lastErr == nil {
			return nil
		}
		sleep := baseDelay * time.Duration(1<<attempt)
		jitter := time.Duration(rand.Intn(150)) * time.Millisecond
		time.Sleep(sleep + jitter)
	}
	return lastErr
}

func ensureVendorID(db *gorm.DB, vendorName string, vendorByName map[string]upstreamVendor, vendorFetchErr error, vendorIDCache map[string]int, createdVendors *int) (int, error) {
	vendorName = strings.TrimSpace(vendorName)
	if vendorName == "" {
		return 0, nil
	}
	if id, ok := vendorIDCache[vendorName]; ok {
		return id, nil
	}
	var existing model.Vendor
	if err := db.Where("name = ?", vendorName).First(&existing).Error; err == nil {
		vendorIDCache[vendorName] = existing.Id
		return existing.Id, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	if vendorFetchErr != nil {
		return 0, fmt.Errorf("fetch upstream vendor metadata failed: %w", vendorFetchErr)
	}
	uv := vendorByName[vendorName]
	v := &model.Vendor{
		Name:        vendorName,
		Description: uv.Description,
		Icon:        coalesce(uv.Icon, ""),
		Status:      chooseStatus(uv.Status, 1),
	}
	if err := v.InsertWithDB(db); err != nil {
		return 0, err
	}
	*createdVendors++
	vendorIDCache[vendorName] = v.Id
	return v.Id, nil
}

func canonicalEndpointsString(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "null" {
		return "", nil
	}
	var endpoints map[string]interface{}
	if err := common.UnmarshalJsonStr(value, &endpoints); err != nil {
		return "", fmt.Errorf("invalid endpoints json: %w", err)
	}
	if endpoints == nil {
		return "", nil
	}
	if len(endpoints) == 0 {
		return "", nil
	}
	for endpoint, config := range endpoints {
		if strings.TrimSpace(endpoint) == "" {
			return "", errors.New("invalid endpoints: empty endpoint name")
		}
		switch config.(type) {
		case string, map[string]interface{}:
		default:
			return "", fmt.Errorf("invalid endpoints value for %q", endpoint)
		}
	}
	canonical, err := common.Marshal(endpoints)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

func canonicalUpstreamEndpoints(raw json.RawMessage) (string, error) {
	return canonicalEndpointsString(string(raw))
}

func shouldFetchUpstreamVendors(missing []string, overwrite []overwriteField, modelByName map[string]upstreamModel) bool {
	for _, name := range missing {
		up, ok := modelByName[name]
		if ok && strings.TrimSpace(up.VendorName) != "" {
			return true
		}
	}
	for _, ow := range overwrite {
		if !containsField(ow.Fields, "vendor") {
			continue
		}
		up, ok := modelByName[ow.ModelName]
		if ok && strings.TrimSpace(up.VendorName) != "" {
			return true
		}
	}
	return false
}

// SyncUpstreamModels 同步上游模型与供应商：
// - 仅创建请求中显式列出的「未配置模型」
// - 可通过 overwrite 选择性覆盖更新本地已有模型的字段（前提：sync_official <> 0）
func SyncUpstreamModels(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgModelSyncBodyInvalid, map[string]any{"Error": err.Error()})
		return
	}
	if metadataSelectionRequest(body) {
		applyMetadataSelection(c, body)
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	var req syncRequest
	// 旧前端仍按 locale/source/missing/overwrite 同步；完全空体走上面的选择接口并返回 400。
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		common.ApiErrorI18n(c, i18n.MsgModelSyncBodyInvalid, map[string]any{"Error": err.Error()})
		return
	}
	source, ok := normalizeSyncSource(req.Source)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": i18n.T(c, i18n.MsgModelSyncSourceUnsupported, map[string]any{"Source": strings.TrimSpace(req.Source)})})
		return
	}
	// 1) 获取未配置模型列表
	missing, err := model.GetMissingModels()
	if err != nil {
		common.SysError("failed to get missing models: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": i18n.T(c, i18n.MsgModelSyncListFailed)})
		return
	}
	if req.SkipMissing {
		missing = nil
	} else {
		missing = restrictMissingModels(missing, req.Missing)
	}

	// 若既无缺失模型需要创建，也未指定覆盖更新字段，则无需请求上游数据，直接返回
	if len(missing) == 0 && len(req.Overwrite) == 0 {
		modelsURL, vendorsURL := getUpstreamURLs(req.Locale)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"created_models":  0,
				"created_vendors": 0,
				"updated_models":  0,
				"skipped_models":  []string{},
				"created_list":    []string{},
				"updated_list":    []string{},
				"source": gin.H{
					"type":        source,
					"locale":      req.Locale,
					"models_url":  modelsURL,
					"vendors_url": vendorsURL,
				},
			},
		})
		return
	}

	// 2) 拉取上游 models，vendors 仅在需要创建或覆盖 vendor 时按需拉取
	timeoutSec := common.GetEnvOrDefault("SYNC_HTTP_TIMEOUT_SECONDS", 15)
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	modelsURL, vendorsURL := getUpstreamURLs(req.Locale)
	var vendorsEnv upstreamEnvelope[upstreamVendor]
	var modelsEnv upstreamEnvelope[upstreamModel]
	var vendorFetchErr error
	fetchErr := fetchJSON(ctx, modelsURL, &modelsEnv)
	if fetchErr != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": i18n.T(c, i18n.MsgModelSyncUpstreamFailed, map[string]any{"Error": fetchErr.Error()}), "locale": req.Locale, "source_urls": gin.H{"models_url": modelsURL, "vendors_url": vendorsURL}})
		return
	}

	// 建立映射
	vendorByName := make(map[string]upstreamVendor)
	modelByName := make(map[string]upstreamModel)
	for _, m := range modelsEnv.Data {
		if m.ModelName != "" {
			modelByName[m.ModelName] = m
		}
	}
	if shouldFetchUpstreamVendors(missing, req.Overwrite, modelByName) {
		if err := fetchJSON(ctx, vendorsURL, &vendorsEnv); err != nil {
			vendorFetchErr = err
		}
		for _, v := range vendorsEnv.Data {
			if v.Name != "" {
				vendorByName[v.Name] = v
			}
		}
	}
	// 3) 执行同步：仅创建缺失模型；若上游缺失该模型则跳过
	createdModels := 0
	createdVendors := 0
	updatedModels := 0
	skipped := make([]string, 0)
	createdList := make([]string, 0)
	updatedList := make([]string, 0)
	// 本地缓存：vendorName -> id
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		vendorIDCache := make(map[string]int)

		for _, name := range missing {
			up, ok := modelByName[name]
			if !ok {
				skipped = append(skipped, name)
				continue
			}

			// 若本地已存在且设置为不同步，则跳过（极端情况：缺失列表与本地状态不同步时）
			var existing model.Model
			if err := tx.Where("model_name = ?", name).First(&existing).Error; err == nil {
				if existing.SyncOfficial == 0 {
					skipped = append(skipped, name)
					continue
				}
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("get local model failed: %w", err)
			}

			// 确保 vendor 存在
			vendorID, err := ensureVendorID(tx, up.VendorName, vendorByName, vendorFetchErr, vendorIDCache, &createdVendors)
			if err != nil {
				return fmt.Errorf("ensure vendor failed: %w", err)
			}
			endpoints, err := canonicalUpstreamEndpoints(up.Endpoints)
			if err != nil {
				return fmt.Errorf("invalid endpoints for %s: %w", name, err)
			}

			// 创建模型
			mi := &model.Model{
				ModelName:    name,
				Description:  up.Description,
				Icon:         up.Icon,
				Tags:         up.Tags,
				VendorID:     vendorID,
				Endpoints:    endpoints,
				Status:       chooseStatus(up.Status, 1),
				SyncOfficial: 1,
				NameRule:     up.NameRule,
			}
			if err := mi.InsertWithDB(tx); err != nil {
				return fmt.Errorf("create model failed: %w", err)
			}
			createdModels++
			createdList = append(createdList, name)
		}

		// 4) 处理可选覆盖（更新本地已有模型的差异字段）
		if len(req.Overwrite) > 0 {
			// vendorIDCache 已用于创建阶段，可复用
			for _, ow := range req.Overwrite {
				up, ok := modelByName[ow.ModelName]
				if !ok {
					continue
				}
				var local model.Model
				if err := tx.Where("model_name = ?", ow.ModelName).First(&local).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						continue
					}
					return fmt.Errorf("get local model failed: %w", err)
				}

				// 跳过被禁用官方同步的模型
				if local.SyncOfficial == 0 {
					continue
				}

				// 映射 vendor
				newVendorID := 0
				if containsField(ow.Fields, "vendor") {
					var err error
					newVendorID, err = ensureVendorID(tx, up.VendorName, vendorByName, vendorFetchErr, vendorIDCache, &createdVendors)
					if err != nil {
						return fmt.Errorf("ensure vendor failed: %w", err)
					}
				}

				// 应用字段覆盖（事务）
				if err := tx.Transaction(func(tx *gorm.DB) error {
					needUpdate := false
					if containsField(ow.Fields, "description") {
						local.Description = up.Description
						needUpdate = true
					}
					if containsField(ow.Fields, "icon") {
						local.Icon = up.Icon
						needUpdate = true
					}
					if containsField(ow.Fields, "tags") {
						local.Tags = up.Tags
						needUpdate = true
					}
					if containsField(ow.Fields, "vendor") {
						local.VendorID = newVendorID
						needUpdate = true
					}
					if containsField(ow.Fields, "endpoints") {
						endpoints, err := canonicalUpstreamEndpoints(up.Endpoints)
						if err != nil {
							return fmt.Errorf("invalid endpoints for %s: %w", ow.ModelName, err)
						}
						local.Endpoints = endpoints
						needUpdate = true
					}
					if containsField(ow.Fields, "name_rule") {
						local.NameRule = up.NameRule
						needUpdate = true
					}
					if containsField(ow.Fields, "status") {
						local.Status = chooseStatus(up.Status, local.Status)
						needUpdate = true
					}
					if !needUpdate {
						return nil
					}
					local.UpdatedTime = common.GetTimestamp()
					if err := tx.Save(&local).Error; err != nil {
						return err
					}
					updatedModels++
					updatedList = append(updatedList, ow.ModelName)
					return nil
				}); err != nil {
					return fmt.Errorf("update model failed: %w", err)
				}
			}
		}

		return nil
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	if createdModels > 0 || updatedModels > 0 || createdVendors > 0 {
		model.InvalidatePricingCache()
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"created_models":  createdModels,
			"created_vendors": createdVendors,
			"updated_models":  updatedModels,
			"skipped_models":  skipped,
			"created_list":    createdList,
			"updated_list":    updatedList,
			"source": gin.H{
				"type":        source,
				"locale":      req.Locale,
				"models_url":  modelsURL,
				"vendors_url": vendorsURL,
			},
		},
	})
}

func containsField(fields []string, key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, f := range fields {
		if strings.ToLower(strings.TrimSpace(f)) == key {
			return true
		}
	}
	return false
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func chooseStatus(primary *int, fallback int) int {
	if primary != nil {
		return *primary
	}
	return fallback
}

type metadataSyncSource struct {
	Locale     string `json:"locale"`
	ModelsURL  string `json:"models_url"`
	VendorsURL string `json:"vendors_url"`
	Version    string `json:"version"`
	Type       string `json:"type,omitempty"`
}

type metadataSyncField struct {
	Field    string `json:"field"`
	Local    any    `json:"local"`
	Upstream any    `json:"upstream"`
}

type metadataSyncCandidate struct {
	ModelName      string                `json:"model_name"`
	Kind           string                `json:"kind"`
	Scope          string                `json:"scope"`
	RecordVersion  string                `json:"record_version"`
	Fields         []metadataSyncField   `json:"fields"`
	Upstream       *model.MetadataValues `json:"upstream,omitempty"`
	VendorToCreate string                `json:"vendor_to_create,omitempty"`
}

func metadataSelectionRequest(body []byte) bool {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return true
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &probe); err != nil {
		return false
	}
	if _, ok := probe["selections"]; ok {
		return true
	}
	if _, ok := probe["source_version"]; ok {
		return true
	}
	return len(probe) == 0
}

func metadataValuesFromUpstream(item upstreamModel) (model.MetadataValues, error) {
	endpoints := ""
	if len(item.Endpoints) > 0 && string(item.Endpoints) != "null" {
		if err := common.Unmarshal(item.Endpoints, &endpoints); err != nil {
			endpoints = string(item.Endpoints)
		}
	}
	values := model.MetadataValues{
		Description: item.Description, Icon: item.Icon, Tags: item.Tags,
		Vendor: strings.TrimSpace(item.VendorName), Endpoints: endpoints,
		NameRule: item.NameRule, Status: chooseStatus(item.Status, 0),
	}
	if err := model.ValidateMetadataValues(values); err != nil {
		return model.MetadataValues{}, err
	}
	return values, nil
}

func fetchMetadataCatalog(c *gin.Context, locale string) (metadataSyncSource, map[string]model.MetadataValues, map[string]model.Vendor, error) {
	resolved := strings.TrimSpace(locale)
	if resolved != "" {
		var valid bool
		resolved, valid = normalizeLocale(locale)
		if !valid {
			return metadataSyncSource{}, nil, nil, errors.New("unsupported metadata language")
		}
	}
	modelsURL, vendorsURL := getUpstreamURLs(resolved)
	source := metadataSyncSource{Locale: resolved, ModelsURL: modelsURL, VendorsURL: vendorsURL}
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(common.GetEnvOrDefault("SYNC_HTTP_TIMEOUT_SECONDS", 15))*time.Second)
	defer cancel()
	var modelsEnv upstreamEnvelope[upstreamModel]
	var vendorsEnv upstreamEnvelope[upstreamVendor]
	var modelsErr, vendorsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); modelsErr = fetchJSON(ctx, modelsURL, &modelsEnv) }()
	go func() { defer wg.Done(); vendorsErr = fetchJSON(ctx, vendorsURL, &vendorsEnv) }()
	wg.Wait()
	if modelsErr != nil {
		return source, nil, nil, fmt.Errorf("fetch models (%s, %s): %w", resolved, modelsURL, modelsErr)
	}
	if vendorsErr != nil {
		return source, nil, nil, fmt.Errorf("fetch vendors (%s, %s): %w", resolved, vendorsURL, vendorsErr)
	}
	if !modelsEnv.Success || !vendorsEnv.Success {
		return source, nil, nil, errors.New("upstream metadata source reported failure")
	}
	models := make(map[string]model.MetadataValues)
	vendors := make(map[string]model.Vendor)
	for _, vendor := range vendorsEnv.Data {
		vendor.Name = strings.TrimSpace(vendor.Name)
		if vendor.Name == "" {
			continue
		}
		vendors[vendor.Name] = model.Vendor{Name: vendor.Name, Description: vendor.Description, Icon: vendor.Icon, Status: chooseStatus(vendor.Status, 1)}
	}
	for _, item := range modelsEnv.Data {
		if strings.TrimSpace(item.ModelName) == "" {
			continue
		}
		values, err := metadataValuesFromUpstream(item)
		if err != nil {
			return source, nil, nil, fmt.Errorf("model %s: %w", item.ModelName, err)
		}
		if _, duplicate := models[item.ModelName]; duplicate {
			return source, nil, nil, fmt.Errorf("duplicate upstream model: %s", item.ModelName)
		}
		models[item.ModelName] = values
	}
	encoded, err := common.Marshal([]any{source.Locale, models, vendors})
	if err != nil {
		return source, nil, nil, err
	}
	source.Version = fmt.Sprintf("%x", sha256.Sum256(encoded))
	return source, models, vendors, nil
}

func metadataSyncCandidates(locals map[string]*model.Model, vendors map[string]*model.Vendor, missing []string, upstream map[string]model.MetadataValues, upstreamVendors map[string]model.Vendor) []metadataSyncCandidate {
	siteNames := make(map[string]bool)
	allNames := make(map[string]bool)
	for name := range locals {
		siteNames[name] = true
		allNames[name] = true
	}
	for _, name := range missing {
		siteNames[name] = true
		allNames[name] = true
	}
	for name := range upstream {
		allNames[name] = true
	}
	names := make([]string, 0, len(allNames))
	for name := range allNames {
		names = append(names, name)
	}
	sort.Strings(names)
	vendorByID := make(map[int]*model.Vendor, len(vendors))
	for _, vendor := range vendors {
		vendorByID[vendor.Id] = vendor
	}
	candidates := make([]metadataSyncCandidate, 0, len(names))
	for _, name := range names {
		candidate := metadataSyncCandidate{ModelName: name, Scope: "catalog", Kind: "create", Fields: []metadataSyncField{}}
		if siteNames[name] {
			candidate.Scope = "site"
		}
		local := locals[name]
		up, found := upstream[name]
		if !found {
			candidate.Kind = "missing_upstream"
			candidates = append(candidates, candidate)
			continue
		}
		candidate.Upstream = &up
		var localVendor *model.Vendor
		if local != nil {
			localVendor = vendorByID[local.VendorID]
		}
		candidate.RecordVersion = model.MetadataRecordVersion(local, localVendor, model.FindMetadataVendor(vendors, up.Vendor))
		if local != nil && local.SyncOfficial == 0 {
			candidate.Kind = "blocked"
			candidates = append(candidates, candidate)
			continue
		}
		if up.Vendor != "" && model.FindMetadataVendor(vendors, up.Vendor) == nil {
			if _, exists := upstreamVendors[up.Vendor]; !exists {
				candidate.Kind = "missing_vendor"
				candidates = append(candidates, candidate)
				continue
			}
			candidate.VendorToCreate = up.Vendor
		}
		localValues := model.MetadataValues{}
		if local != nil {
			candidate.Kind = "update"
			localValues = model.MetadataValues{Description: local.Description, Icon: local.Icon, Tags: local.Tags, Endpoints: local.Endpoints, NameRule: local.NameRule, Status: local.Status}
			if localVendor != nil {
				localValues.Vendor = localVendor.Name
			}
		}
		localRaw, _ := common.Marshal(localValues)
		upRaw, _ := common.Marshal(up)
		var localFields, upFields map[string]any
		_ = common.Unmarshal(localRaw, &localFields)
		_ = common.Unmarshal(upRaw, &upFields)
		for _, field := range model.MetadataSyncFields {
			if local == nil || localFields[field] != upFields[field] {
				candidate.Fields = append(candidate.Fields, metadataSyncField{Field: field, Local: localFields[field], Upstream: upFields[field]})
			}
		}
		if local != nil && len(candidate.Fields) == 0 {
			candidate.Kind = "unchanged"
		}
		candidates = append(candidates, candidate)
	}
	return candidates
}

func applyMetadataSelection(c *gin.Context, body []byte) {
	var request struct {
		Locale        string                        `json:"locale"`
		SourceVersion string                        `json:"source_version"`
		Selections    []model.MetadataSyncSelection `json:"selections"`
	}
	if err := common.Unmarshal(body, &request); err != nil || len(request.Selections) == 0 || request.SourceVersion == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Preview and select metadata changes before applying"})
		return
	}
	source, upstream, vendors, err := fetchMetadataCatalog(c, request.Locale)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if source.Version != request.SourceVersion {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "Upstream metadata changed; preview again"})
		return
	}
	updates := make([]model.MetadataSyncUpdate, 0, len(request.Selections))
	for _, selection := range request.Selections {
		values, exists := upstream[selection.ModelName]
		if !exists {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": "Selected upstream model is no longer available"})
			return
		}
		updates = append(updates, model.MetadataSyncUpdate{MetadataSyncSelection: selection, Values: values})
	}
	result, err := model.ApplyMetadataSync(updates, vendors)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrMetadataSyncConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	recordManageAudit(c, "model.metadata.sync", map[string]any{"created_models": result.CreatedModels, "updated_models": result.UpdatedModels, "created_vendors": result.CreatedVendors})
	common.ApiSuccess(c, result)
}

func SyncUpstreamPreview(c *gin.Context) {
	locale := c.Query("locale")
	sourceType, ok := normalizeSyncSource(c.Query("source"))
	if !ok {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": i18n.T(c, i18n.MsgModelSyncSourceUnsupported, map[string]any{"Source": strings.TrimSpace(c.Query("source"))})})
		return
	}
	source, upstreamValues, upstreamVendors, err := fetchMetadataCatalog(c, locale)
	if err != nil {
		original := i18n.T(c, i18n.MsgModelSyncUpstreamFailed, map[string]any{"Error": err.Error()})
		message := common.PublicDashboardErrorMessage(c, original)
		if message != original {
			common.SysError("api error: " + original)
		}
		c.JSON(http.StatusOK, gin.H{"success": false, "message": message, "locale": locale, "source_urls": gin.H{"models_url": source.ModelsURL, "vendors_url": source.VendorsURL}})
		return
	}
	source.Type = sourceType
	localModels, localVendors, err := model.GetMetadataSyncState(model.DB)
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgModelSyncLocalModelsFailed, map[string]any{"Error": err.Error()})
		return
	}
	missingList, err := model.GetMissingModels()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgModelSyncMissingFailed, map[string]any{"Error": err.Error()})
		return
	}
	var missing []string
	for _, name := range missingList {
		if _, ok := upstreamValues[name]; ok {
			missing = append(missing, name)
		}
	}
	candidates := metadataSyncCandidates(localModels, localVendors, missingList, upstreamValues, upstreamVendors)

	type conflictField struct {
		Field    string `json:"field"`
		Local    any    `json:"local"`
		Upstream any    `json:"upstream"`
	}
	type conflictItem struct {
		ModelName string          `json:"model_name"`
		Fields    []conflictField `json:"fields"`
	}
	var conflicts []conflictItem
	for _, candidate := range candidates {
		if candidate.Kind != "update" || len(candidate.Fields) == 0 {
			continue
		}
		fields := make([]conflictField, 0, len(candidate.Fields))
		for _, field := range candidate.Fields {
			fields = append(fields, conflictField{Field: field.Field, Local: field.Local, Upstream: field.Upstream})
		}
		conflicts = append(conflicts, conflictItem{ModelName: candidate.ModelName, Fields: fields})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"missing":    missing,
			"conflicts":  conflicts,
			"candidates": candidates,
			"source":     source,
		},
	})
}
