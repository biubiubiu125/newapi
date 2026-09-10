package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type modelMetaListResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Items []*model.Model `json:"items"`
		Total int64          `json:"total"`
	} `json:"data"`
}

func searchModelsMeta(t *testing.T, query string) (int, modelMetaListResponse) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/models/search?"+query, nil)
	SearchModelsMeta(ctx)
	var payload modelMetaListResponse
	if recorder.Body.Len() > 0 {
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	}
	return recorder.Code, payload
}

func TestSearchModelsMetaSquareStateAndChannelRows(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id:     21,
		Type:   constant.ChannelTypeOpenAI,
		Key:    "listing-key",
		Status: common.ChannelStatusEnabled,
		Name:   "listing-channel",
		Models: "square-visible,square-bare,square-partial-on,square-partial-off",
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "square-visible", ChannelId: 21, Enabled: true},
		{Group: "default", Model: "square-bare", ChannelId: 21, Enabled: true},
		{Group: "default", Model: "square-partial-on", ChannelId: 21, Enabled: true},
	}).Error)
	require.NoError(t, db.Create(&[]model.Model{
		{ModelName: "square-visible", Status: 1, NameRule: model.NameRuleExact, SyncOfficial: 1},
		{ModelName: "square-hidden", Status: 1, NameRule: model.NameRuleExact, SyncOfficial: 1},
		{ModelName: "square-meta-only", Status: 1, NameRule: model.NameRuleExact, SyncOfficial: 1},
		{ModelName: "square-partial-", Status: 1, NameRule: model.NameRulePrefix, SyncOfficial: 1},
		{ModelName: "square-hidden-", Status: 1, NameRule: model.NameRulePrefix, SyncOfficial: 1},
	}).Error)
	require.NoError(t, db.Model(&model.Model{}).Where("model_name IN ?", []string{"square-hidden", "square-hidden-"}).Update("status", 0).Error)
	require.NoError(t, db.Model(&model.Model{}).Where("model_name = ?", "square-hidden-").Update("sync_official", 0).Error)

	code, payload := searchModelsMeta(t, "include_channel_models=true&keyword=square-&page_size=100")
	require.Equal(t, http.StatusOK, code)
	require.True(t, payload.Success)
	got := make(map[string]model.ModelSquareState, len(payload.Data.Items))
	for _, item := range payload.Data.Items {
		got[item.ModelName] = item.SquareState
		if item.ModelName == "square-bare" {
			assert.False(t, item.HasMetadata)
			assert.Equal(t, 1, item.ConfiguredChannelCount)
		}
		if item.ModelName == "square-visible" {
			assert.True(t, item.HasMetadata)
		}
	}
	assert.Equal(t, model.ModelSquareVisible, got["square-visible"])
	assert.Equal(t, model.ModelSquareVisible, got["square-bare"])
	assert.Equal(t, model.ModelSquareHidden, got["square-hidden"])
	assert.Equal(t, model.ModelSquareUnavailable, got["square-meta-only"])
	assert.Equal(t, model.ModelSquarePartial, got["square-partial-"])
	assert.Equal(t, model.ModelSquareHidden, got["square-hidden-"])
	assert.Contains(t, got, "square-partial-on")
	assert.Contains(t, got, "square-partial-off")

	code, payload = searchModelsMeta(t, "include_channel_models=true&keyword=square-&square_state=visible&page_size=2&p=1")
	require.Equal(t, http.StatusOK, code)
	require.True(t, payload.Success)
	assert.GreaterOrEqual(t, payload.Data.Total, int64(2))
	assert.LessOrEqual(t, len(payload.Data.Items), 2)
	for _, item := range payload.Data.Items {
		assert.Equal(t, model.ModelSquareVisible, item.SquareState)
	}

	code, payload = searchModelsMeta(t, "keyword=square-&square_state=visible&page_size=100")
	require.Equal(t, http.StatusOK, code)
	names := make([]string, 0, len(payload.Data.Items))
	for _, item := range payload.Data.Items {
		names = append(names, item.ModelName)
	}
	assert.Contains(t, names, "square-visible")
	assert.NotContains(t, names, "square-bare")

	code, payload = searchModelsMeta(t, "square_state=unknown")
	require.Equal(t, http.StatusBadRequest, code)
	assert.False(t, payload.Success)
	assert.Equal(t, "Invalid model square state", payload.Message)
}
