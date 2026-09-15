package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetVideoRouter(router *gin.Engine) {
	videoSharedRouter := router.Group("/v1")
	videoSharedRouter.Use(middleware.RouteTag("relay"))
	videoSharedRouter.Use(middleware.TokenAuth())
	videoSharedRouter.Use(middleware.SystemPerformanceCheck())
	videoSharedRouter.POST(
		"/video/generations",
		middleware.PinTaskPluginEndpoint(),
		middleware.TaskPluginEndpointOnly(middleware.ModelRequestRateLimit()),
		middleware.PrepareTaskPluginEndpoint(),
		middleware.Distribute(),
		func(c *gin.Context) {
			controller.RelayTaskPluginEndpoint(c, controller.RelayTask)
		},
	)

	videoFetchRouter := router.Group("/v1")
	videoFetchRouter.Use(middleware.RouteTag("relay"))
	videoFetchRouter.Use(middleware.TokenAuthAllowExhausted(), middleware.Distribute())
	{
		videoFetchRouter.GET("/video/generations/:task_id", controller.RelayTaskFetch)
	}

	videoRemixRouter := router.Group("/v1")
	videoRemixRouter.Use(middleware.RouteTag("relay"))
	videoRemixRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		videoRemixRouter.POST("/videos/:video_id/remix", controller.RelayTask)
	}

	klingSubmitRouter := router.Group("/kling/v1")
	klingSubmitRouter.Use(middleware.RouteTag("relay"))
	klingSubmitRouter.Use(middleware.KlingRequestConvert(), middleware.TokenAuth(), middleware.Distribute())
	{
		klingSubmitRouter.POST("/videos/text2video", controller.RelayTask)
		klingSubmitRouter.POST("/videos/image2video", controller.RelayTask)
	}

	klingFetchRouter := router.Group("/kling/v1")
	klingFetchRouter.Use(middleware.RouteTag("relay"))
	klingFetchRouter.Use(middleware.KlingRequestConvert(), middleware.TokenAuthAllowExhausted(), middleware.Distribute())
	{
		klingFetchRouter.GET("/videos/text2video/:task_id", controller.RelayTaskFetch)
		klingFetchRouter.GET("/videos/image2video/:task_id", controller.RelayTaskFetch)
	}

	// Jimeng official API routes - direct mapping to official API format
	jimengOfficialGroup := router.Group("jimeng")
	jimengOfficialGroup.Use(middleware.RouteTag("relay"))
	jimengOfficialGroup.Use(middleware.JimengRequestConvert(), middleware.JimengAuth(), middleware.Distribute())
	{
		// Maps to: /?Action=CVSync2AsyncSubmitTask&Version=2022-08-31 and /?Action=CVSync2AsyncGetResult&Version=2022-08-31
		jimengOfficialGroup.POST("/", controller.RelayTaskOrFetch)
	}
}
