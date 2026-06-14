package controller

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type deleteConversationSnapshotsRequest struct {
	StartTime int64 `json:"start_time"`
	EndTime   int64 `json:"end_time"`
}

func CreateConversationExport(c *gin.Context) {
	var req service.ConversationExportRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的导出参数")
		return
	}
	task, err := service.CreateConversationExportTask(req, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, task)
}

func ListConversationExports(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	tasks, total, err := model.ListConversationExportTasks(pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(tasks)
	common.ApiSuccess(c, pageInfo)
}

func DownloadConversationExport(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "导出任务 ID 不正确")
		return
	}
	task, err := model.GetConversationExportTask(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	path, err := service.ConversationExportFilePath(task)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+strconv.Quote(task.FileName))
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.File(path)
}

func DeleteConversationExport(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "导出任务 ID 不正确")
		return
	}
	task, err := model.GetConversationExportTask(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if strings.TrimSpace(task.FilePath) != "" {
		if path, err := service.ConversationExportStoredFilePath(task); err == nil {
			_ = os.Remove(path)
		}
	}
	if err := model.DeleteConversationExportTask(task); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}

func DeleteConversationSnapshots(c *gin.Context) {
	var req deleteConversationSnapshotsRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorMsg(c, "无效的删除参数")
		return
	}
	deleted, err := service.DeleteConversationSnapshotsInRange(context.Background(), req.StartTime, req.EndTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": deleted})
}

func CountConversationSnapshots(c *gin.Context) {
	startTime, _ := strconv.ParseInt(c.Query("start_time"), 10, 64)
	endTime, _ := strconv.ParseInt(c.Query("end_time"), 10, 64)
	if startTime <= 0 || endTime <= 0 || startTime > endTime {
		common.ApiErrorMsg(c, "时间范围不能为空")
		return
	}
	count, err := model.CountConversationSnapshots(startTime, endTime)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"count": count})
}

func SearchConversationExportTokens(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("keyword"))
	if keyword == "" {
		common.ApiSuccess(c, []gin.H{})
		return
	}
	var tokens []model.Token
	tx := model.DB.Model(&model.Token{}).Unscoped()
	if tokenId, err := strconv.Atoi(keyword); err == nil {
		tx = tx.Where("id = ?", tokenId)
	} else {
		pattern := "%" + keyword + "%"
		tx = tx.Where("name LIKE ?", pattern)
	}
	if err := tx.Limit(20).Find(&tokens).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]gin.H, 0, len(tokens))
	for _, token := range tokens {
		maskedKey := service.MaskAPIKeyForDisplay(token.Key)
		items = append(items, gin.H{
			"id":          token.Id,
			"name":        token.Name,
			"masked_key":  maskedKey,
			"display":     "#" + strconv.Itoa(token.Id) + " / " + token.Name + " / " + maskedKey,
			"user_id":     token.UserId,
			"group":       token.Group,
			"accessed_at": token.AccessedTime,
		})
	}
	common.ApiSuccess(c, items)
}

func TriggerConversationExportCleanup(c *gin.Context) {
	service.CleanupExpiredConversationExports()
	deleted, err := service.CleanupOldConversationSnapshots(context.Background())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"deleted_snapshots": deleted,
		},
	})
}
