package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type providerPriceOverridesUpdateRequest struct {
	Items []service.ProviderPriceOverride `json:"items"`
}

func hasProviderPriceValue(item service.ProviderPriceOverride) bool {
	for _, value := range []*float64{
		item.InputPrice,
		item.OutputPrice,
		item.CacheInputPrice,
		item.CacheCreatePrice,
		item.CacheCreatePrice1h,
		item.ImageOutputPrice,
	} {
		if value != nil && *value > 0 {
			return true
		}
	}
	return false
}

func GetProviderPriceOverrides(c *gin.Context) {
	items, err := service.GetProviderPriceOverrides()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if items == nil {
		items = []service.ProviderPriceOverride{}
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

func UpdateProviderPriceOverrides(c *gin.Context) {
	var req providerPriceOverridesUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	for _, item := range req.Items {
		if strings.TrimSpace(item.GroupName) == "" {
			common.ApiErrorMsg(c, "group_name is required")
			return
		}
		if strings.TrimSpace(item.ModelName) == "" {
			common.ApiErrorMsg(c, "model_name is required")
			return
		}
		if !hasProviderPriceValue(item) {
			common.ApiErrorMsg(c, "at least one price field must be greater than 0")
			return
		}
	}
	items, err := service.UpdateProviderPriceOverrides(req.Items)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if items == nil {
		items = []service.ProviderPriceOverride{}
	}
	common.ApiSuccess(c, gin.H{"items": items})
}

func GetPublicProviderPricing(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "GET, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Accept")
	if c.Request.Method == "OPTIONS" {
		c.Status(204)
		return
	}
	data, err := service.GetPublicProviderPriceExport()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(200, gin.H{
		"schema_version": service.ProviderPriceExportSchemaVersion,
		"success":        true,
		"message":        "",
		"data":           data,
	})
}
