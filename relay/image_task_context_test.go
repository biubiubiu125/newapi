package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSetupImageTaskBaseGinContextWritesUsername(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}))

	oldDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = oldDB
	})

	require.NoError(t, model.DB.Create(&model.User{
		Id:       9201,
		Username: "image-owner",
		Password: "password123",
		Group:    "image-group",
		Quota:    1000,
		Status:   common.UserStatusEnabled,
	}).Error)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	err = setupImageTaskBaseGinContext(ctx, &model.Task{
		UserId: 9201,
		Group:  "default",
	})

	require.NoError(t, err)
	require.Equal(t, "image-owner", ctx.GetString("username"))
	require.Equal(t, "image-owner", common.GetContextKeyString(ctx, constant.ContextKeyUserName))
}
