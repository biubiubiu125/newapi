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

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func useOptionPricingTestDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousIsMasterNode := common.IsMasterNode
	previousSQLitePath := common.SQLitePath
	previousMain := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	previousSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")

	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.ReplaceAll(t.Name(), "/", "_"),
	)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))
	require.NoError(t, model.InitDB())
	require.NoError(t, model.DB.AutoMigrate(&model.Option{}))
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}

	t.Cleanup(func() {
		if model.DB != nil {
			sqlDB, err := model.DB.DB()
			if err == nil {
				_ = sqlDB.Close()
			}
		}
		model.DB = previousDB
		common.IsMasterNode = previousIsMasterNode
		common.SQLitePath = previousSQLitePath
		common.SetDatabaseTypes(previousMain, previousLog)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", previousSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	})
}

func TestUpdateOptionRejectsNegativeImageRatioWithoutMutatingRuntime(t *testing.T) {
	// SQLite harness is a non-production path. Production persistence is PostgreSQL.
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	useOptionPricingTestDB(t)

	previous := ratio_setting.ImageRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(previous))
	})
	require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(`{"keep":2}`))

	body, err := json.Marshal(map[string]any{
		"key":   "ImageRatio",
		"value": `{"keep":-1}`,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Accept-Language", "zh-CN")

	UpdateOption(ctx)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, false, payload["success"])
	require.Contains(t, payload["message"], "ImageRatio")
	require.Contains(t, payload["message"], "必须是有限的非负数")

	copy := ratio_setting.GetImageRatioCopy()
	require.Equal(t, 2.0, copy["keep"])
}

func TestUpdateOptionAcceptsPerRequestBillingModeAlias(t *testing.T) {
	// SQLite harness is a non-production path. Production persistence is PostgreSQL.
	gin.SetMode(gin.TestMode)
	useOptionPricingTestDB(t)

	body, err := json.Marshal(map[string]any{
		"key":   "billing_setting.billing_mode",
		"value": `{"keep":"per-request"}`,
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	UpdateOption(ctx)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, true, payload["success"], payload["message"])
	require.Equal(t, billing_setting.BillingModeRatio, billing_setting.GetBillingMode("keep"))
}

func patchModelPricing(t *testing.T, body []byte) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/option/model_pricing", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("Accept-Language", "zh-CN")
	ctx.Set("id", 1)
	UpdateModelPricingConfig(ctx)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	return recorder.Code, payload
}

func TestUpdateModelPricingConfigAppliesOptionMaps(t *testing.T) {
	// SQLite harness is a non-production path. Production persistence is PostgreSQL.
	gin.SetMode(gin.TestMode)
	useOptionPricingTestDB(t)

	previousPrice := ratio_setting.ModelPrice2JSONString()
	previousImage := ratio_setting.ImageRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrice))
		require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(previousImage))
	})

	require.NoError(t, model.UpdateModelPricingOptions(map[string]string{
		"ModelPrice": `{"keep":0.1}`,
		"ImageRatio": `{"keep":1}`,
	}))

	body, err := json.Marshal(map[string]any{
		"options": map[string]string{
			"ModelPrice": `{"keep":0.25}`,
			"ImageRatio": `{"keep":2}`,
		},
		"expected_options": map[string]string{
			"ModelPrice": `{"keep":0.1}`,
			"ImageRatio": `{"keep":1}`,
		},
	})
	require.NoError(t, err)

	status, payload := patchModelPricing(t, body)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, true, payload["success"], payload["message"])
	require.Equal(t, 0.25, ratio_setting.GetModelPriceCopy()["keep"])
	require.Equal(t, 2.0, ratio_setting.GetImageRatioCopy()["keep"])
}

func TestUpdateModelPricingConfigRejectsPartialOptionMaps(t *testing.T) {
	// SQLite harness is a non-production path. Production persistence is PostgreSQL.
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	useOptionPricingTestDB(t)

	previousPrice := ratio_setting.ModelPrice2JSONString()
	previousImage := ratio_setting.ImageRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrice))
		require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(previousImage))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"keep":0.1}`))
	require.NoError(t, ratio_setting.UpdateImageRatioByJSONString(`{"keep":2}`))
	require.NoError(t, model.UpdateModelPricingOptions(map[string]string{
		"ModelPrice": `{"keep":0.1}`,
		"ImageRatio": `{"keep":2}`,
	}))

	body, err := json.Marshal(map[string]any{
		"options": map[string]string{
			"ModelPrice": `{"keep":0.25}`,
			"ImageRatio": `{"keep":-1}`,
		},
		"expected_options": map[string]string{
			"ModelPrice": `{"keep":0.1}`,
			"ImageRatio": `{"keep":2}`,
		},
	})
	require.NoError(t, err)

	_, payload := patchModelPricing(t, body)
	require.Equal(t, false, payload["success"])
	require.Contains(t, payload["message"], "ImageRatio")
	require.Contains(t, payload["message"], "必须是有限的非负数")
	require.Equal(t, 0.1, ratio_setting.GetModelPriceCopy()["keep"])
	require.Equal(t, 2.0, ratio_setting.GetImageRatioCopy()["keep"])
}

func TestUpdateModelPricingConfigRejectsMixedChangesAndOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	useOptionPricingTestDB(t)

	body, err := json.Marshal(map[string]any{
		"changes": []map[string]any{{
			"model_name":       "keep",
			"expected_version": "v1",
			"pricing":          map[string]any{"ModelPrice": 0.25},
		}},
		"options": map[string]string{
			"ModelPrice": `{"keep":0.25}`,
		},
	})
	require.NoError(t, err)

	status, payload := patchModelPricing(t, body)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, false, payload["success"])
	require.Contains(t, payload["message"], "请只提交选项映射或模型价格变更")
}

func TestUpdateModelPricingConfigConflictsWhenExpectedOptionsMissing(t *testing.T) {
	// SQLite harness is a non-production path. Production persistence is PostgreSQL.
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	useOptionPricingTestDB(t)

	body, err := json.Marshal(map[string]any{
		"options": map[string]string{
			"ModelPrice": `{"keep":0.25}`,
		},
	})
	require.NoError(t, err)

	status, payload := patchModelPricing(t, body)
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, false, payload["success"])
	require.Contains(t, payload["message"], "重新加载")
}

func TestUpdateModelPricingConfigConflictsWhenExpectedOptionsStale(t *testing.T) {
	// SQLite harness is a non-production path. Production persistence is PostgreSQL.
	gin.SetMode(gin.TestMode)
	require.NoError(t, i18n.Init())
	useOptionPricingTestDB(t)

	previousPrice := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrice))
	})
	require.NoError(t, model.UpdateModelPricingOptions(map[string]string{
		"ModelPrice": `{"keep":0.1}`,
	}))

	body, err := json.Marshal(map[string]any{
		"options": map[string]string{
			"ModelPrice": `{"keep":0.25}`,
		},
		"expected_options": map[string]string{
			"ModelPrice": `{"keep":0.99}`,
		},
	})
	require.NoError(t, err)

	status, payload := patchModelPricing(t, body)
	require.Equal(t, http.StatusConflict, status)
	require.Equal(t, false, payload["success"])
	require.Contains(t, payload["message"], "重新加载")
	require.Equal(t, 0.1, ratio_setting.GetModelPriceCopy()["keep"])
}
