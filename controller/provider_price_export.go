package controller

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type providerPriceOverridesUpdateRequest struct {
	Items []service.ProviderPriceOverride `json:"items"`
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
	seenModelGroups := make(map[string]struct{}, len(req.Items))
	for _, item := range req.Items {
		groupName := strings.TrimSpace(item.GroupName)
		modelName := strings.TrimSpace(item.ModelName)
		if groupName == "" {
			common.ApiErrorMsg(c, "group_name is required")
			return
		}
		if modelName == "" {
			common.ApiErrorMsg(c, "model_name is required")
			return
		}
		modelGroupKey := strings.ToLower(modelName) + "\x00" + strings.ToLower(groupName)
		if _, exists := seenModelGroups[modelGroupKey]; exists {
			common.ApiErrorMsg(c, fmt.Sprintf("duplicate model_name and group_name: %s / %s", modelName, groupName))
			return
		}
		seenModelGroups[modelGroupKey] = struct{}{}
		if item.InputPrice == nil || *item.InputPrice < 0 {
			common.ApiErrorMsg(c, "input_price must be a non-negative number")
			return
		}
		for _, value := range []*float64{
			item.OutputPrice,
			item.CacheInputPrice,
			item.CacheCreatePrice,
			item.CacheCreatePrice1h,
		} {
			if value != nil && *value < 0 {
				common.ApiErrorMsg(c, "price fields must not be negative")
				return
			}
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
	c.Header("Access-Control-Allow-Headers", "Content-Type, Accept, X-Hvoy-Ts, X-Hvoy-Sign")
	if c.Request.Method == "OPTIONS" {
		c.Status(204)
		return
	}
	data, err := service.GetPublicProviderPriceExport()
	if err != nil {
		c.JSON(200, gin.H{
			"schema_version": service.ProviderPriceExportSchemaVersion,
			"success":        false,
			"message":        err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"schema_version": service.ProviderPriceExportSchemaVersion,
		"success":        true,
		"message":        "",
		"data":           data,
	})
}
