package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type tokenUsageBatchRequest struct {
	Ids []int `json:"ids"`
}

func GetTokenUsageStats(c *gin.Context) {
	tokenId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "API 密钥 ID 不正确")
		return
	}
	token, err := model.GetTokenByIds(tokenId, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	resetAt := int64(0)
	resetQuota := 0
	if reset, err := model.GetTokenUsageReset(token.Id); err == nil {
		resetAt = reset.ResetAt
		resetQuota = reset.ResetQuota
	} else if err != gorm.ErrRecordNotFound {
		common.ApiError(c, err)
		return
	}
	stats, err := model.GetTokenUsageStats(token.Id, resetAt, resetQuota)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, stats)
}

func ResetTokenUsageStats(c *gin.Context) {
	tokenId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "API 密钥 ID 不正确")
		return
	}
	token, err := model.GetTokenByIds(tokenId, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	resetAt := common.GetTimestamp()
	resetQuota, err := model.SumTokenUsageQuota(token.Id, 0, 0)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpsertTokenUsageReset(token.Id, token.UserId, resetAt, resetQuota); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"reset_at": resetAt})
}

func GetTokenUsageStatsBatch(c *gin.Context) {
	var req tokenUsageBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "请求参数不正确")
		return
	}
	if len(req.Ids) == 0 {
		common.ApiSuccess(c, gin.H{})
		return
	}
	if len(req.Ids) > 100 {
		common.ApiErrorMsg(c, "单次最多查询 100 个 API Key")
		return
	}

	uniqueIds := make([]int, 0, len(req.Ids))
	seen := make(map[int]struct{}, len(req.Ids))
	for _, id := range req.Ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniqueIds = append(uniqueIds, id)
	}
	if len(uniqueIds) == 0 {
		common.ApiSuccess(c, gin.H{})
		return
	}

	var tokens []model.Token
	if err := model.DB.Where("id IN ? AND user_id = ?", uniqueIds, c.GetInt("id")).Find(&tokens).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	result := make(map[int]model.TokenUsageStats, len(tokens))
	for _, token := range tokens {
		resetAt := int64(0)
		resetQuota := 0
		if reset, err := model.GetTokenUsageReset(token.Id); err == nil {
			resetAt = reset.ResetAt
			resetQuota = reset.ResetQuota
		} else if err != gorm.ErrRecordNotFound {
			common.ApiError(c, err)
			return
		}
		stats, err := model.GetTokenUsageStats(token.Id, resetAt, resetQuota)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		result[token.Id] = stats
	}
	common.ApiSuccess(c, result)
}
