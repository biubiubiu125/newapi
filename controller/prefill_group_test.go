package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func usePrefillControllerPostgreSQL(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	dsn := strings.TrimSpace(os.Getenv("TEST_POSTGRES_DSN"))
	if dsn == "" {
		dsn = "host=/var/run/postgresql user=biubiubiu dbname=newapi_ci_review sslmode=disable"
	}
	schemaName := fmt.Sprintf("prefill_ctl_%d", time.Now().UnixNano())
	admin, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	require.NoError(t, admin.Exec("CREATE SCHEMA "+schemaName).Error)
	t.Cleanup(func() {
		_ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error
		_ = adminSQL.Close()
	})

	isolated, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  dsn + " options=-csearch_path=" + schemaName,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	require.NoError(t, err)
	isolatedSQL, err := isolated.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = isolatedSQL.Close() })
	require.NoError(t, isolated.AutoMigrate(&model.PrefillGroup{}))

	previous := model.DB
	model.DB = isolated
	t.Cleanup(func() { model.DB = previous })
}

func performPrefill(t *testing.T, handler gin.HandlerFunc, method string, body any) map[string]any {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, "/api/prefill_group", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = req
	handler(context)
	require.Equal(t, http.StatusOK, recorder.Code)
	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestUpdatePrefillGroupEchoesOriginalCreatedTime(t *testing.T) {
	usePrefillControllerPostgreSQL(t)
	created := performPrefill(t, CreatePrefillGroup, http.MethodPost, map[string]any{
		"name":        "  chat  ",
		"type":        "model",
		"description": "  keep  ",
		"items":       []string{"gpt-4o"},
	})
	require.Equal(t, true, created["success"])
	createdData := created["data"].(map[string]any)
	require.Equal(t, "chat", createdData["name"])
	require.Equal(t, "keep", createdData["description"])
	createdTime := createdData["created_time"].(float64)
	require.NotZero(t, createdTime)

	updated := performPrefill(t, UpdatePrefillGroup, http.MethodPut, map[string]any{
		"id":          createdData["id"],
		"name":        "chat-edit",
		"type":        "endpoint",
		"description": "",
		"items":       []string{"https://example.test"},
	})
	require.Equal(t, true, updated["success"])
	updatedData := updated["data"].(map[string]any)
	require.Equal(t, createdTime, updatedData["created_time"])

	var stored model.PrefillGroup
	require.NoError(t, model.DB.First(&stored, int(createdData["id"].(float64))).Error)
	require.Equal(t, int64(createdTime), stored.CreatedTime)
	require.Equal(t, "chat-edit", stored.Name)
	require.Equal(t, "", stored.Description)
}

func TestUpdatePrefillGroupRejectsBlankAndOversizedFields(t *testing.T) {
	usePrefillControllerPostgreSQL(t)
	created := performPrefill(t, CreatePrefillGroup, http.MethodPost, map[string]any{
		"name":  "kept",
		"type":  "model",
		"items": []string{"a"},
	})
	require.Equal(t, true, created["success"])
	id := created["data"].(map[string]any)["id"]

	blank := performPrefill(t, UpdatePrefillGroup, http.MethodPut, map[string]any{
		"id": id, "name": "   ", "type": "model", "items": []string{"a"},
	})
	require.Equal(t, false, blank["success"])
	require.Equal(t, "组名称和类型不能为空", blank["message"])

	longName := performPrefill(t, UpdatePrefillGroup, http.MethodPut, map[string]any{
		"id": id, "name": strings.Repeat("名", 65), "type": "model", "items": []string{"a"},
	})
	require.Equal(t, false, longName["success"])
	require.Equal(t, "组名称最多 64 个字符", longName["message"])

	longDescription := performPrefill(t, UpdatePrefillGroup, http.MethodPut, map[string]any{
		"id": id, "name": "kept", "type": "model", "description": strings.Repeat("述", 256), "items": []string{"a"},
	})
	require.Equal(t, false, longDescription["success"])
	require.Equal(t, "组描述最多 255 个字符", longDescription["message"])

	missing := performPrefill(t, UpdatePrefillGroup, http.MethodPut, map[string]any{
		"id": 999999, "name": "missing", "type": "model", "items": []string{"a"},
	})
	require.Equal(t, false, missing["success"])
	require.Equal(t, "预填组不存在", missing["message"])

	var stored model.PrefillGroup
	require.NoError(t, model.DB.First(&stored, int(id.(float64))).Error)
	require.Equal(t, "kept", stored.Name)
	require.NotZero(t, stored.CreatedTime)
}
