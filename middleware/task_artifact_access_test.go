package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactTaskArtifactAccessQueryStripsSignedAccessFromSupportedPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		routePattern string
		requestPath  string
		wantQuery    string
	}{
		{
			name:         "task artifact content",
			routePattern: "/v1/tasks/:task_id/artifacts/:artifact_key/content",
			requestPath:  "/v1/tasks/task_x/artifacts/artifact_x/content?access=signed-token&keep=1",
			wantQuery:    "keep=1",
		},
		{
			name:         "image task result",
			routePattern: "/v1/image-tasks/:task_id/result",
			requestPath:  "/v1/image-tasks/task_x/result?access=signed-token&keep=1",
			wantQuery:    "keep=1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := gin.New()
			var observedURI string
			var observedQuery string
			var rawAccess string
			var present bool
			var invalid bool
			engine.Use(RedactTaskArtifactAccessQuery())
			engine.GET(tt.routePattern, func(c *gin.Context) {
				observedURI = c.Request.RequestURI
				observedQuery = c.Request.URL.RawQuery
				rawAccess, present, invalid = ReadTaskArtifactAccessRequest(c)
				c.Status(http.StatusOK)
			})

			recorder := httptest.NewRecorder()
			engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.requestPath, nil))

			require.Equal(t, http.StatusOK, recorder.Code)
			require.NotEmpty(t, observedURI)
			assert.NotContains(t, observedURI, "access=")
			require.Equal(t, tt.wantQuery, observedQuery)
			require.Equal(t, "signed-token", rawAccess)
			require.True(t, present)
			require.False(t, invalid)
			require.False(t, strings.Contains(observedURI, "access="))
		})
	}
}
