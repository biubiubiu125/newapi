package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetRelayRouterRegistersPublicImageTaskRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetRelayRouter(engine)

	routes := make(map[string]struct{})
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	for _, route := range []string{
		http.MethodPost + " /v1/image-tasks/generations",
		http.MethodPost + " /v1/image-tasks/edits",
		http.MethodGet + " /v1/image-tasks",
		http.MethodGet + " /v1/image-tasks/:task_id",
		http.MethodGet + " /v1/image-tasks/:task_id/result",
		http.MethodPost + " /v1/image-tasks/:task_id/ack",
		http.MethodPost + " /v1/image-tasks/:task_id/cancel",
	} {
		_, exists := routes[route]
		require.Truef(t, exists, "missing route %s", route)
	}
}

// 状态/结果/ACK/取消是以轮询为前提的接口，既不经过 ModelRequestRateLimit（无模型维度），
// 也不在 /api 的 GlobalAPIRateLimit 覆盖范围内，必须挂上专用限流；且必须排在鉴权之后
// 才能按用户和令牌隔离额度。
func TestPublicImageTaskAccessRoutesRateLimitAfterTokenAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tt := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/v1/image-tasks"},
		{method: http.MethodGet, path: "/v1/image-tasks/task_x"},
		{method: http.MethodGet, path: "/v1/image-tasks/task_x/result"},
		{method: http.MethodPost, path: "/v1/image-tasks/task_x/ack"},
		{method: http.MethodPost, path: "/v1/image-tasks/task_x/cancel"},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			engine := gin.New()
			var handlerNames []string
			engine.Use(func(c *gin.Context) {
				handlerNames = c.HandlerNames()
				c.AbortWithStatus(http.StatusTeapot)
			})
			SetRelayRouter(engine)

			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(tt.method, tt.path, nil))
			require.Equal(t, http.StatusTeapot, recorder.Code)

			findHandler := func(fragment string) int {
				for index, name := range handlerNames {
					if strings.Contains(name, fragment) {
						return index
					}
				}
				return -1
			}
			authIndex := findHandler("TokenAuthForTaskAccess")
			rateLimitIndex := findHandler("ImageTaskAccessRateLimit")
			require.NotEqual(t, -1, authIndex, handlerNames)
			require.NotEqual(t, -1, rateLimitIndex, handlerNames)
			require.Less(t, authIndex, rateLimitIndex, handlerNames)
		})
	}
}

func TestPublicImageTaskOpenAPIListsActualErrorResponses(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "docs", "openapi", "relay.json"))
	require.NoError(t, err)
	var spec struct {
		Paths map[string]map[string]struct {
			Responses map[string]any `json:"responses"`
		} `json:"paths"`
	}
	require.NoError(t, common.Unmarshal(document, &spec))

	expected := map[string][]string{
		"post /v1/image-tasks/generations": {"202", "400", "401", "403", "409", "413", "415", "429", "500", "503"},
		"post /v1/image-tasks/edits":       {"202", "400", "401", "403", "409", "413", "415", "429", "500", "503"},
		"get /v1/image-tasks":              {"200", "400", "401", "403", "429", "500", "503"},
		"get /v1/image-tasks/{task_id}":    {"200", "401", "403", "404", "429", "500", "503"},
		"get /v1/image-tasks/{task_id}/result": {
			"200", "401", "403", "404", "409", "410", "429", "500", "503",
		},
		"post /v1/image-tasks/{task_id}/ack": {
			"200", "401", "403", "404", "409", "410", "429", "500", "503",
		},
		"post /v1/image-tasks/{task_id}/cancel": {"200", "401", "403", "404", "409", "429", "500", "503"},
	}
	for operation, statusCodes := range expected {
		parts := strings.SplitN(operation, " ", 2)
		pathItem, exists := spec.Paths[parts[1]]
		require.Truef(t, exists, "missing OpenAPI path %s", parts[1])
		method, exists := pathItem[parts[0]]
		require.Truef(t, exists, "missing OpenAPI operation %s", operation)
		for _, statusCode := range statusCodes {
			require.Containsf(t, method.Responses, statusCode, "%s is missing response %s", operation, statusCode)
		}
	}
}

// TestPublicImageTaskOpenAPIEnumeratesEveryEmittedErrorCode 静态保证 OpenAPI 里的
// error.code 枚举不会和代码实际吐出的码漂移。客户端被要求按 code 分支处理（尤其是
// idempotency_conflict 不可重试 vs idempotency_in_progress 应重试），所以漏一个码
// 就等于给客户端一个未文档化的分支。
func TestPublicImageTaskOpenAPIDocumentsSupportedRequestFields(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "docs", "openapi", "relay.json"))
	require.NoError(t, err)
	type property struct {
		Type      string   `json:"type"`
		Format    string   `json:"format"`
		Minimum   *float64 `json:"minimum"`
		Maximum   *float64 `json:"maximum"`
		MaxLength *int     `json:"maxLength"`
		MinLength *int     `json:"minLength"`
		Pattern   string   `json:"pattern"`
		Default   any      `json:"default"`
	}
	type schema struct {
		Properties map[string]property `json:"properties"`
	}
	var spec struct {
		Components struct {
			Schemas map[string]schema `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, common.Unmarshal(document, &spec))

	commonFields := map[string]string{
		"model":              "string",
		"prompt":             "string",
		"n":                  "integer",
		"size":               "string",
		"quality":            "string",
		"response_format":    "string",
		"style":              "string",
		"user":               "string",
		"background":         "string",
		"moderation":         "string",
		"output_format":      "string",
		"output_compression": "integer",
		"partial_images":     "integer",
		"input_fidelity":     "string",
		"watermark":          "boolean",
		"client_task_id":     "string",
	}
	for _, schemaName := range []string{"PublicImageTaskCreateRequest", "PublicImageTaskEditRequest"} {
		requestSchema, exists := spec.Components.Schemas[schemaName]
		require.Truef(t, exists, "missing OpenAPI schema %s", schemaName)
		for fieldName, fieldType := range commonFields {
			field, exists := requestSchema.Properties[fieldName]
			require.Truef(t, exists, "%s is missing field %s", schemaName, fieldName)
			require.Equalf(t, fieldType, field.Type, "%s.%s has the wrong type", schemaName, fieldName)
		}

		nField := requestSchema.Properties["n"]
		require.NotNil(t, nField.Minimum, schemaName+".n minimum is missing")
		require.NotNil(t, nField.Maximum, schemaName+".n maximum is missing")
		require.Equal(t, float64(1), *nField.Minimum)
		require.Equal(t, float64(128), *nField.Maximum)
	}

	editSchema := spec.Components.Schemas["PublicImageTaskEditRequest"]
	for _, fieldName := range []string{"image", "mask"} {
		field, exists := editSchema.Properties[fieldName]
		require.Truef(t, exists, "PublicImageTaskEditRequest is missing field %s", fieldName)
		require.Equal(t, "string", field.Type)
		require.Equal(t, "binary", field.Format)
	}

	generationSchema := spec.Components.Schemas["PublicImageTaskCreateRequest"]
	require.Equal(t, "dall-e", generationSchema.Properties["model"].Default)
	require.Equal(t, "gpt-image-1", editSchema.Properties["model"].Default)
	for schemaName, requestSchema := range map[string]schema{
		"PublicImageTaskCreateRequest": generationSchema,
		"PublicImageTaskEditRequest":   editSchema,
	} {
		prompt := requestSchema.Properties["prompt"]
		require.NotNil(t, prompt.MinLength, schemaName+".prompt minLength is missing")
		require.Equal(t, 1, *prompt.MinLength)
		require.Equal(t, `\S`, prompt.Pattern)
	}
}

func TestPublicImageTaskOpenAPIRestrictsCreateMediaTypes(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "docs", "openapi", "relay.json"))
	require.NoError(t, err)
	var spec struct {
		Paths map[string]map[string]struct {
			RequestBody struct {
				Content map[string]any `json:"content"`
			} `json:"requestBody"`
		} `json:"paths"`
	}
	require.NoError(t, common.Unmarshal(document, &spec))

	generationContent := spec.Paths["/v1/image-tasks/generations"]["post"].RequestBody.Content
	require.Equal(t, []string{"application/json"}, sortedMapKeys(generationContent))
	editContent := spec.Paths["/v1/image-tasks/edits"]["post"].RequestBody.Content
	require.Equal(t, []string{"multipart/form-data"}, sortedMapKeys(editContent))
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestPublicImageTaskOpenAPIEnumeratesEveryEmittedErrorCode(t *testing.T) {
	document, err := os.ReadFile(filepath.Join("..", "docs", "openapi", "relay.json"))
	require.NoError(t, err)
	var spec struct {
		Components struct {
			Schemas struct {
				PublicImageTaskError struct {
					Properties struct {
						Code struct {
							Enum []string `json:"enum"`
						} `json:"code"`
					} `json:"properties"`
				} `json:"PublicImageTaskError"`
			} `json:"schemas"`
		} `json:"components"`
	}
	require.NoError(t, common.Unmarshal(document, &spec))

	documented := make(map[string]struct{}, len(spec.Components.Schemas.PublicImageTaskError.Properties.Code.Enum))
	for _, code := range spec.Components.Schemas.PublicImageTaskError.Properties.Code.Enum {
		documented[code] = struct{}{}
	}
	require.NotEmpty(t, documented, "PublicImageTaskError.code enum is missing")

	sources := []string{
		filepath.Join("..", "controller", "image_task_public.go"),
		filepath.Join("..", "middleware", "image_task_rate_limit.go"),
		filepath.Join("..", "middleware", "utils.go"),
	}
	patterns := []*regexp.Regexp{
		// publicImageTaskError(c, <status>, "<code>", ...)
		regexp.MustCompile(`publicImageTaskError\([^,]+,[^,]+,\s*"([a-z_]+)"`),
		// respondPublicImageTaskAPIError 的映射表：code = "<code>"
		regexp.MustCompile(`\bcode\s*=\s*"([a-z_]+)"`),
		// 限流中间件的信封："code": "<code>"
		regexp.MustCompile(`"code":\s*"([a-z_]+)"`),
		// PublicImageTask 状态对象中的 Error.Code。
		regexp.MustCompile(`\bCode:\s*"([a-z_]+)"`),
	}

	emitted := map[string]string{}
	for _, source := range sources {
		content, readErr := os.ReadFile(source)
		require.NoError(t, readErr)
		for _, pattern := range patterns {
			for _, match := range pattern.FindAllStringSubmatch(string(content), -1) {
				emitted[match[1]] = source
			}
		}
	}
	require.NotEmpty(t, emitted, "no image task error codes were discovered in source")

	missing := make([]string, 0)
	for code, source := range emitted {
		if _, ok := documented[code]; !ok {
			missing = append(missing, code+" ("+source+")")
		}
	}
	sort.Strings(missing)
	require.Emptyf(t, missing, "PublicImageTaskError.code enum is missing emitted codes: %s", strings.Join(missing, ", "))
}

func TestPublicImageTaskCreateRoutesReuseExistingWorkBeforeNewWorkGuards(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		path         string
		reuseHandler string
	}{
		{path: "/v1/image-tasks/generations", reuseHandler: "ReusePublicImageTaskGenerationIfExists"},
		{path: "/v1/image-tasks/edits", reuseHandler: "ReusePublicImageTaskEditIfExists"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			engine := gin.New()
			var handlerNames []string
			engine.Use(func(c *gin.Context) {
				handlerNames = c.HandlerNames()
				c.AbortWithStatus(http.StatusTeapot)
			})
			SetRelayRouter(engine)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, tt.path, nil)
			engine.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusTeapot, recorder.Code)

			findHandler := func(fragment string) int {
				for index, name := range handlerNames {
					if strings.Contains(name, fragment) {
						return index
					}
				}
				return -1
			}
			authIndex := findHandler("TokenAuthForImageTaskCreation")
			admissionIndex := findHandler("ImageTaskCreateAdmission")
			contentTypeIndex := findHandler("RequirePublicImageTaskContentType")
			modelRateLimitIndex := findHandler("ModelRequestRateLimit")
			reuseIndex := findHandler(tt.reuseHandler)
			exhaustedIndex := findHandler("RejectExhaustedTokenForImageTaskCreation")
			distributeIndex := findHandler("Distribute")
			require.NotEqual(t, -1, authIndex, handlerNames)
			require.NotEqual(t, -1, admissionIndex, handlerNames)
			require.NotEqual(t, -1, contentTypeIndex, handlerNames)
			require.NotEqual(t, -1, modelRateLimitIndex, handlerNames)
			require.NotEqual(t, -1, reuseIndex, handlerNames)
			require.NotEqual(t, -1, exhaustedIndex, handlerNames)
			require.NotEqual(t, -1, distributeIndex, handlerNames)
			require.Less(t, authIndex, contentTypeIndex, handlerNames)
			require.Less(t, contentTypeIndex, reuseIndex, handlerNames)
			require.Less(t, reuseIndex, exhaustedIndex, handlerNames)
	require.Less(t, exhaustedIndex, admissionIndex, handlerNames)
	require.Less(t, admissionIndex, modelRateLimitIndex, handlerNames)
	require.Less(t, modelRateLimitIndex, distributeIndex, handlerNames)
	require.Less(t, reuseIndex, distributeIndex, handlerNames)
	})
	}
}

func TestListModelsSupportsOpenAIAndGeminiAuthentication(t *testing.T) {
	setupRelayRouterTestDB(t)

	user := model.User{
		Username: "models-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100,
	}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&model.Token{
		UserId:         user.Id,
		Key:            "modelstestkey",
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		UnlimitedQuota: true,
	}).Error)

	engine := gin.New()
	SetRelayRouter(engine)

	tests := []struct {
		name           string
		path           string
		headerName     string
		expectedObject string
		expectedField  string
	}{
		{
			name:           "OpenAI bearer token",
			path:           "/v1/models",
			headerName:     "Authorization",
			expectedObject: "list",
			expectedField:  "data",
		},
		{
			name:          "Gemini API key header",
			path:          "/v1/models",
			headerName:    "x-goog-api-key",
			expectedField: "models",
		},
		{
			name:          "Gemini API key query",
			path:          "/v1/models?key=modelstestkey",
			expectedField: "models",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			if test.headerName != "" {
				value := "modelstestkey"
				if test.headerName == "Authorization" {
					value = "Bearer " + value
				}
				request.Header.Set(test.headerName, value)
			}

			engine.ServeHTTP(recorder, request)

			require.Equal(t, http.StatusOK, recorder.Code)
			var payload map[string]any
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
			assert.Contains(t, payload, test.expectedField)
			assert.NotContains(t, payload, "error")
			if test.expectedObject != "" {
				assert.Equal(t, test.expectedObject, payload["object"])
			}
		})
	}
}

func setupRelayRouterTestDB(t *testing.T) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	originalIsMasterNode := common.IsMasterNode
	originalRedisEnabled := common.RedisEnabled
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.RedisEnabled = false
	common.SQLitePath = fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	model.LOG_DB = model.DB
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Token{}, &model.Ability{}))

	t.Cleanup(func() {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
		common.IsMasterNode = originalIsMasterNode
		common.RedisEnabled = originalRedisEnabled
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
}
