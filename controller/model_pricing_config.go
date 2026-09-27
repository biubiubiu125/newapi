package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetModelPricingConfig(c *gin.Context) {
	snapshot, err := model.GetModelPricingSnapshot(c.QueryArray("model"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, snapshot)
}

func PreviewModelPricingConversion(c *gin.Context) {
	var request struct {
		ModelName string              `json:"model_name"`
		Pricing   model.PricingValues `json:"pricing"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	preview, err := model.PreviewModelPricingConversion(request.ModelName, request.Pricing)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, preview)
}

func PreviewModelPricing(c *gin.Context) {
	var request struct {
		ModelName string              `json:"model_name"`
		Pricing   model.PricingValues `json:"pricing"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	preview, err := model.PreviewModelPricing(request.ModelName, request.Pricing)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, model.ModelPricingDescription{
		Effective:      preview,
		CacheWriteMode: model.ResolveCacheWriteMode(request.ModelName, request.Pricing),
		BillingDetails: model.ResolveLegacyBillingDetails(request.ModelName, preview, request.Pricing),
	})
}

func UpdateModelPricingConfig(c *gin.Context) {
	var request struct {
		Changes         []model.ModelPricingChange `json:"changes"`
		Options         map[string]string          `json:"options"`
		ExpectedOptions map[string]string          `json:"expected_options"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorWithStatus(c, http.StatusBadRequest, err)
		return
	}
	if len(request.Changes) > 0 && len(request.Options) > 0 {
		common.ApiErrorWithStatus(
			c,
			http.StatusBadRequest,
			errors.New("provide either option maps or model pricing changes"),
		)
		return
	}
	if len(request.Options) > 0 {
		if request.ExpectedOptions == nil {
			common.ApiErrorWithStatus(c, http.StatusConflict, model.ErrModelPricingConflict)
			return
		}
		if err := model.UpdateModelPricingOptionsChecked(request.Options, request.ExpectedOptions); err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, model.ErrModelPricingConflict) {
				status = http.StatusConflict
			}
			common.ApiErrorWithStatus(c, status, err)
			return
		}
		keys := make([]string, 0, len(request.Options))
		for key := range request.Options {
			keys = append(keys, key)
		}
		recordManageAudit(c, "model.pricing.update", map[string]interface{}{
			"options": keys,
		})
		common.ApiSuccess(c, gin.H{"updated_options": keys})
		return
	}
	if err := model.UpdateModelPricing(request.Changes); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrModelPricingConflict) {
			status = http.StatusConflict
		}
		common.ApiErrorWithStatus(c, status, err)
		return
	}
	names := make([]string, 0, len(request.Changes))
	for _, change := range request.Changes {
		names = append(names, change.ModelName)
	}
	recordManageAudit(c, "model.pricing.update", map[string]interface{}{"models": names})
	common.ApiSuccess(c, gin.H{"updated_models": names})
}
