package router

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetApiRouterDoesNotPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() {
		SetApiRouter(engine)
	})
}

func TestReferralAdminAndUserRoutesAreDistinct(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	paths := map[string]bool{}
	for _, route := range engine.Routes() {
		paths[route.Method+" "+route.Path] = true
	}

	require.True(t, paths["GET /api/user/referral/commissions"])
	require.True(t, paths["GET /api/user/admin/referral/commissions"])
	require.True(t, paths["POST /api/user/admin/referral/upload"])
	require.True(t, paths["POST /api/user/referral/upload"])
}
