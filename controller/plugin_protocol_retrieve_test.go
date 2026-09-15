package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRetrieveTaskPluginResponseRejectsOtherToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	resolved := false
	deps := pluginProtocolBridgeDeps{
		getByTaskId: func(userID int, taskID string) (*model.Task, bool, error) {
			task := &model.Task{TaskID: taskID, UserId: userID}
			task.PrivateData.TokenId = 80
			return task, true, nil
		},
		resolvePlugin: func(*model.Task) (*pluginruntime.LoadedPlugin, *pluginruntime.RoutingGeneration, bool) {
			resolved = true
			return nil, nil, false
		},
	}

	engine := gin.New()
	engine.GET("/v1/responses/:response_id", func(c *gin.Context) {
		c.Set("id", 7)
		c.Set("token_id", 81)
		retrieveTaskPluginResponse(c, deps)
	})

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/responses/resp_abc123", nil))

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.False(t, resolved)
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, "not_found", payload.Error.Code)
	require.Contains(t, payload.Error.Message, "resp_abc123")
}

func TestRetrieveTaskPluginResponseAllowsMatchingAndSessionTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		tokenID int
	}{
		{name: "matching token", tokenID: 80},
		{name: "session token", tokenID: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved := false
			deps := pluginProtocolBridgeDeps{
				getByTaskId: func(userID int, taskID string) (*model.Task, bool, error) {
					task := &model.Task{TaskID: taskID, UserId: userID}
					task.PrivateData.TokenId = 80
					return task, true, nil
				},
				resolvePlugin: func(*model.Task) (*pluginruntime.LoadedPlugin, *pluginruntime.RoutingGeneration, bool) {
					resolved = true
					return nil, nil, false
				},
			}
			engine := gin.New()
			engine.GET("/v1/responses/:response_id", func(c *gin.Context) {
				c.Set("id", 7)
				c.Set("token_id", tt.tokenID)
				retrieveTaskPluginResponse(c, deps)
			})
			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/responses/resp_abc123", nil))
			require.True(t, resolved)
			require.Equal(t, http.StatusNotFound, recorder.Code)
		})
	}
}
