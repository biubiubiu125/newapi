package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
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
		return "zh-CN", true
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
	attempts := common.GetEnvOrDefault("SYNC_HTTP_RETRY", 3)
	if attempts < 1 {
		attempts = 1
	}
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
				limited := io.LimitReader(resp.Body, maxBytes)
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
	var req syncRequest
	// 允许空体
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "invalid request body: " + err.Error()})
		return
	}
	source, ok := normalizeSyncSource(req.Source)
	if !ok {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "unsupported sync source: " + strings.TrimSpace(req.Source)})
		return
	}
	// 1) 获取未配置模型列表
	missing, err := model.GetMissingModels()
	if err != nil {
		common.SysError("failed to get missing models: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取模型列表失败，请稍后重试"})
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
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取上游模型失败: " + fetchErr.Error(), "locale": req.Locale, "source_urls": gin.H{"models_url": modelsURL, "vendors_url": vendorsURL}})
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
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
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

// SyncUpstreamPreview 预览上游与本地的差异（仅用于弹窗选择）
func SyncUpstreamPreview(c *gin.Context) {
	// 1) 拉取上游数据
	timeoutSec := common.GetEnvOrDefault("SYNC_HTTP_TIMEOUT_SECONDS", 15)
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	locale := c.Query("locale")
	source, ok := normalizeSyncSource(c.Query("source"))
	if !ok {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "unsupported sync source: " + strings.TrimSpace(c.Query("source"))})
		return
	}
	modelsURL, vendorsURL := getUpstreamURLs(locale)

	var modelsEnv upstreamEnvelope[upstreamModel]
	if err := fetchJSON(ctx, modelsURL, &modelsEnv); err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "获取上游模型失败: " + err.Error(), "locale": locale, "source_urls": gin.H{"models_url": modelsURL, "vendors_url": vendorsURL}})
		return
	}

	modelByName := make(map[string]upstreamModel)
	upstreamNames := make([]string, 0, len(modelsEnv.Data))
	for _, m := range modelsEnv.Data {
		if m.ModelName != "" {
			modelByName[m.ModelName] = m
			upstreamNames = append(upstreamNames, m.ModelName)
		}
	}

	// 2) 本地已有模型
	var locals []model.Model
	if len(upstreamNames) > 0 {
		if err := model.DB.Where("model_name IN ? AND sync_official <> 0", upstreamNames).Find(&locals).Error; err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "get local models failed: " + err.Error()})
			return
		}
	}

	// 本地 vendor 名称映射
	vendorIdSet := make(map[int]struct{})
	for _, m := range locals {
		if m.VendorID != 0 {
			vendorIdSet[m.VendorID] = struct{}{}
		}
	}
	vendorIDs := make([]int, 0, len(vendorIdSet))
	for id := range vendorIdSet {
		vendorIDs = append(vendorIDs, id)
	}
	idToVendorName := make(map[int]string)
	if len(vendorIDs) > 0 {
		var dbVendors []model.Vendor
		if err := model.DB.Where("id IN ?", vendorIDs).Find(&dbVendors).Error; err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": "get local vendors failed: " + err.Error()})
			return
		}
		for _, v := range dbVendors {
			idToVendorName[v.Id] = v.Name
		}
	}

	// 3) 缺失且上游存在的模型
	missingList, err := model.GetMissingModels()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "get missing models failed: " + err.Error()})
		return
	}
	var missing []string
	for _, name := range missingList {
		if _, ok := modelByName[name]; ok {
			missing = append(missing, name)
		}
	}

	// 4) 计算冲突字段
	type conflictField struct {
		Field    string      `json:"field"`
		Local    interface{} `json:"local"`
		Upstream interface{} `json:"upstream"`
	}
	type conflictItem struct {
		ModelName string          `json:"model_name"`
		Fields    []conflictField `json:"fields"`
	}

	var conflicts []conflictItem
	for _, local := range locals {
		up, ok := modelByName[local.ModelName]
		if !ok {
			continue
		}
		fields := make([]conflictField, 0, 7)
		if strings.TrimSpace(local.Description) != strings.TrimSpace(up.Description) {
			fields = append(fields, conflictField{Field: "description", Local: local.Description, Upstream: up.Description})
		}
		if strings.TrimSpace(local.Icon) != strings.TrimSpace(up.Icon) {
			fields = append(fields, conflictField{Field: "icon", Local: local.Icon, Upstream: up.Icon})
		}
		if strings.TrimSpace(local.Tags) != strings.TrimSpace(up.Tags) {
			fields = append(fields, conflictField{Field: "tags", Local: local.Tags, Upstream: up.Tags})
		}
		upstreamEndpoints, err := canonicalUpstreamEndpoints(up.Endpoints)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"success": false, "message": fmt.Sprintf("invalid upstream endpoints for %s: %v", local.ModelName, err)})
			return
		}
		localEndpoints, err := canonicalEndpointsString(local.Endpoints)
		if err != nil {
			localEndpoints = strings.TrimSpace(local.Endpoints)
		}
		if localEndpoints != upstreamEndpoints {
			fields = append(fields, conflictField{Field: "endpoints", Local: localEndpoints, Upstream: upstreamEndpoints})
		}
		// vendor 对比使用名称
		localVendor := idToVendorName[local.VendorID]
		if strings.TrimSpace(localVendor) != strings.TrimSpace(up.VendorName) {
			fields = append(fields, conflictField{Field: "vendor", Local: localVendor, Upstream: up.VendorName})
		}
		if local.NameRule != up.NameRule {
			fields = append(fields, conflictField{Field: "name_rule", Local: local.NameRule, Upstream: up.NameRule})
		}
		if local.Status != chooseStatus(up.Status, local.Status) {
			fields = append(fields, conflictField{Field: "status", Local: local.Status, Upstream: up.Status})
		}
		if len(fields) > 0 {
			conflicts = append(conflicts, conflictItem{ModelName: local.ModelName, Fields: fields})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"missing":   missing,
			"conflicts": conflicts,
			"source": gin.H{
				"type":        source,
				"locale":      locale,
				"models_url":  modelsURL,
				"vendors_url": vendorsURL,
			},
		},
	})
}
