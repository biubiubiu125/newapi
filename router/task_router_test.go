package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetTaskRouterRegistersWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() { SetTaskRouter(engine) })

	routes := engine.Routes()
	require.Len(t, routes, 5)
	actual := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		actual[route.Method+" "+route.Path] = struct{}{}
	}
	assert.Contains(t, actual, http.MethodPost+" /v1/tasks/:key")
	assert.Contains(t, actual, http.MethodGet+" /v1/tasks/:key")
	assert.Contains(t, actual, http.MethodGet+" /v1/tasks/:key/artifacts")
	assert.Contains(t, actual, http.MethodGet+" /v1/tasks/:key/artifacts/:artifact_key/content")
	assert.Contains(t, actual, http.MethodHead+" /v1/tasks/:key/artifacts/:artifact_key/content")
}

func TestTaskContentRouteRedactsArtifactAccessBeforeAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	var handlerNames []string
	engine.Use(func(c *gin.Context) {
		handlerNames = c.HandlerNames()
		c.AbortWithStatus(http.StatusTeapot)
	})
	SetTaskRouter(engine)

	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/tasks/task_x/artifacts/artifact_x/content?access=invalid", nil))
	require.Equal(t, http.StatusTeapot, recorder.Code)

	findHandler := func(fragment string) int {
		for index, name := range handlerNames {
			if strings.Contains(name, fragment) {
				return index
			}
		}
		return -1
	}
	redactIndex := findHandler("RedactTaskArtifactAccessQuery")
	authIndex := findHandler("TokenOrTaskArtifactAccessAuth")
	require.NotEqual(t, -1, redactIndex, handlerNames)
	require.NotEqual(t, -1, authIndex, handlerNames)
	require.Less(t, redactIndex, authIndex, handlerNames)
}
