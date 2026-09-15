package router

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProductionPluginQueryRouteUsesExhaustedTokenAuth(t *testing.T) {
	handlers := productionPluginRouteHandlers(&jsplugin.RoutingGeneration{}, jsplugin.RouteBinding{
		Plugin: &jsplugin.LoadedPlugin{Meta: jsplugin.Meta{Key: "doubao"}},
		Route:  jsplugin.Route{Type: jsplugin.RouteTypeQuery, Method: "GET", Path: "/doubao/tasks/:task_id"},
	})
	joined := pluginHandlerNames(handlers)
	require.Contains(t, joined, "TokenAuthAllowExhausted")
}

func TestProductionPluginDynamicRouteUsesExhaustedTokenAuth(t *testing.T) {
	handlers := productionPluginRouteHandlers(&jsplugin.RoutingGeneration{}, jsplugin.RouteBinding{
		Plugin: &jsplugin.LoadedPlugin{Meta: jsplugin.Meta{Key: "sunoapi"}},
		Route:  jsplugin.Route{Type: jsplugin.RouteTypeDynamic, Method: "POST", Path: "/suno/fetch"},
	})
	joined := pluginHandlerNames(handlers)
	require.Contains(t, joined, "TokenAuthAllowExhausted")
}

func TestProductionPluginSubmitRouteKeepsStrictTokenAuth(t *testing.T) {
	handlers := productionPluginRouteHandlers(&jsplugin.RoutingGeneration{}, jsplugin.RouteBinding{
		Plugin: &jsplugin.LoadedPlugin{Meta: jsplugin.Meta{Key: "doubao"}},
		Route:  jsplugin.Route{Type: jsplugin.RouteTypeSubmit, Method: "POST", Path: "/doubao/tasks"},
	})
	joined := pluginHandlerNames(handlers)
	require.Contains(t, joined, ".TokenAuth.")
	require.NotContains(t, joined, "TokenAuthAllowExhausted")
}

func pluginHandlerNames(handlers []gin.HandlerFunc) string {
	names := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		names = append(names, runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name())
	}
	return strings.Join(names, ",")
}
