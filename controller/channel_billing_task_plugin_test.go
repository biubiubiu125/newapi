package controller

import (
	"fmt"
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

func TestUpdateChannelBalanceRejectsTaskPluginChannels(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	channel := &model.Channel{
		Name:   "task plugin",
		Type:   constant.ChannelTypeTaskPlugin,
		Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, db.Create(channel).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", channel.Id)}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/update_balance", nil)

	UpdateChannelBalance(ctx)

	assert.Contains(t, recorder.Body.String(), "任务插件渠道不支持余额查询")
}
