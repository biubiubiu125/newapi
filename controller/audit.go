package controller

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// auditContentTemplates 将稳定的操作标识 action 映射为英文兜底模板，渲染后写入
// Log.Content（供导出等非本地化消费者使用）。占位符为 ${name}，由该
// action 的 params 填充。本地化展示文案在前端 i18n 模板中维护，本表是语言中立的
// 英文基线——调用方因此无需在每个埋点处手写句子（避免与 params 重复书写同一份值）。
var auditContentTemplates = map[string]string{
	"user.create":               "Created user ${username} (role ${role})",
	"user.update":               "Updated user ${username} (ID: ${id})",
	"user.delete":               "Deleted user ${username} (ID: ${id})",
	"user.account_delete":       "Account deletion",
	"user.manage":               "Performed ${action} on user ${username} (ID: ${id})",
	"user.quota_add":            "Increased user quota by ${quota}",
	"user.quota_subtract":       "Decreased user quota by ${quota}",
	"user.quota_override":       "Overrode user quota from ${from} to ${to}",
	"user.binding_clear":        "Cleared ${bindingType} binding for user ${username}",
	"user.2fa_disable":          "Force-disabled two-factor authentication for the user",
	"user.passkey_register":     "Registered a passkey",
	"user.passkey_delete":       "Deleted a passkey",
	"user.reset_passkey":        "Reset the user passkey",
	"access_token.generate":     "Generated an access token",
	"access_token.revoke":       "Revoked an access token",
	"access_token.rename":       "Renamed an access token",
	"access_token.update":       "Changed access token permissions",
	"token.create":              "Created API token ${name}",
	"token.update":              "Updated API token ${name} (ID: ${id})",
	"token.status_update":       "Updated API token status ${name} (ID: ${id})",
	"token.delete":              "Deleted API token ${name} (ID: ${id})",
	"token.delete_batch":        "Batch deleted ${count} API tokens",
	"token.key_view":            "Viewed API token key ${name} (ID: ${id})",
	"token.key_view_batch":      "Viewed ${count} API token keys",
	"user.2fa_setup":            "Started two-factor authentication setup",
	"user.2fa_enable":           "Enabled two-factor authentication",
	"user.2fa_disable_self":     "Disabled two-factor authentication",
	"user.2fa_backup_codes":     "Regenerated two-factor backup codes",
	"user.security_verify":      "Completed security verification",
	"user.password_change":      "Account password change",
	"user.binding_start":        "Account binding request",
	"user.binding_bind":         "Account binding",
	"user.binding_unbind":       "Account unlinking",
	"user.email_binding_resend": "Email confirmation code resend",
	"option.update":             "Updated system setting ${key}",
	"option.passkey_domains":           "Updated Passkey domains: removed ${domains}; affected ${known}; unknown ${unknown}",
	"option.passkey_domains_confirmed": "Confirmed removal of Passkey domains: ${domains}; affected ${known}; unknown ${unknown}",
	"option.passkey_domains_blocked":   "Passkey domain change blocked: ${domains}; affected ${known}; unknown ${unknown}",
	"option.passkey_domains_failed":    "Passkey domain update failed",

	"channel.create":              "Created channel ${name} (type ${type}, count ${count})",
	"channel.update":              "Updated channel ${name} (ID: ${id})",
	"channel.delete":              "Deleted channel ${name} (ID: ${id})",
	"channel.delete_batch":        "Batch deleted ${count} channels",
	"channel.delete_disabled":     "Deleted all disabled channels (${count})",
	"channel.key_view":            "Viewed channel key ${name} (ID: ${id})",
	"channel.tag_disable":         "Disabled channels with tag ${tag}",
	"channel.tag_enable":          "Enabled channels with tag ${tag}",
	"channel.tag_edit":            "Edited channels with tag ${tag}",
	"channel.tag_batch_set":       "Batch set tag for ${count} channels",
	"channel.copy":                "Copied channel (source ID: ${sourceId}) to ${name} (new ID: ${id})",
	"channel.multi_key_manage":    "Multi-key management ${action} on channel (ID: ${id})",
	"channel.upstream_detect":     "Detected upstream model changes for channel (ID: ${id}, add ${add_count}, remove ${remove_count}, auto-added ${auto_added_count})",
	"channel.upstream_detect_all": "Started upstream model change detection for all channels (task ${task_id})",
	"channel.upstream_apply":      "Applied upstream model changes to channel (ID: ${id})",
	"channel.upstream_apply_all":  "Started upstream model changes apply task (task ${task_id})",

	"redemption.create":       "Created ${count} redemption codes named ${name} (${quota} each)",
	"redemption.delete_batch": "Batch deleted ${count} redemption codes",

	"model.delete":           "Deleted model metadata",
	"model.delete_batch":     "Batch deleted model metadata",
	"model.pricing.update":   "Updated model pricing",
	"vendor.metadata.save":   "Saved vendor metadata",
	"vendor.metadata.delete": "Deleted vendor metadata",
	"vendor.assign":          "Assigned models to a vendor",
	"vendor.merge":           "Merged vendors",
	"vendor.delete":          "Deleted vendors",

	"subscription.plan_reset":      "Reset active subscriptions for plan ${plan_id}",
	"subscription.user_plan_reset": "Reset active plan ${plan_id} subscriptions for user ${target_user_id}",
}

func recordPasskeyDomainAudit(c *gin.Context, change *model.PasskeyDomainChange, confirmed bool, err error) {
	confirmed = confirmed && err == nil && change != nil && len(change.RemovedRPIDs) > 0
	params := map[string]any{"success": err == nil, "confirmed": confirmed}
	if change != nil {
		params["domains"] = strings.Join(change.RemovedRPIDs, ", ")
		params["removed_rp_ids"] = change.RemovedRPIDs
		params["known"] = change.AffectedCredentials
		params["unknown"] = change.UnknownCredentials
		params["previous_rp_id"] = change.PreviousRPID
		params["effective_rp_id"] = change.EffectiveRPID
	}
	action := "option.passkey_domains"
	if errors.Is(err, model.ErrPasskeyDomainRemovalConfirmation) {
		action = "option.passkey_domains_blocked"
	} else if err != nil {
		action = "option.passkey_domains_failed"
	} else if confirmed && change != nil && len(change.RemovedRPIDs) > 0 {
		action = "option.passkey_domains_confirmed"
	}
	auditInfo := &model.AuditRequestInfo{
		Method: c.Request.Method, Route: c.FullPath(), Path: c.FullPath(),
		Status: c.Writer.Status(), Success: err == nil,
	}
	writeOperationAuditLog(c, c.GetInt("id"), auditContentEN(action, params), c.ClientIP(), action, params, auditOperatorInfo(c), auditInfo)
	markAuditLogged(c)
}

// auditContentEN 按 action 模板渲染英文兜底文本；未登记的 action 退回 action 本身。
func auditContentEN(action string, params map[string]any) string {
	tmpl, ok := auditContentTemplates[action]
	if !ok {
		return action
	}
	return os.Expand(tmpl, func(key string) string {
		if v, ok := params[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	})
}

// auditOperatorInfo 从上下文构建操作者身份信息（管理员 id/用户名/角色）。
func auditOperatorInfo(c *gin.Context) *model.AuditAdminInfo {
	return &model.AuditAdminInfo{
		AdminID:       c.GetInt("id"),
		AdminUsername: c.GetString("username"),
		AdminRole:     c.GetInt("role"),
		AuthMethod:    auditAuthMethod(c),
	}
}

func auditAuthMethod(c *gin.Context) string {
	if c.GetBool("use_access_token") {
		return "access_token"
	}
	return "session"
}

// markAuditLogged 标记当前请求已在 handler 内手动记录审计日志，
// 使鉴权链路中的审计兜底（finishAdminAudit）跳过兜底记录，避免重复。
func markAuditLogged(c *gin.Context) {
	common.SetContextKey(c, constant.ContextKeyAuditLogged, true)
}

// recordManageAudit 记录一条由操作者本人归属的管理/高危审计日志（资源类操作：
// 渠道 / 系统设置 / 兑换码等）。content 由 action+params 自动渲染。
func recordManageAudit(c *gin.Context, action string, params map[string]any) {
	recordManageAuditFor(c, c.GetInt("id"), action, params)
}

// recordManageAuditFor 记录一条管理审计日志，日志归属于操作者；targetUserId
// 只表示被操作用户，用于在结构化参数中保留目标上下文。
func recordManageAuditFor(c *gin.Context, targetUserId int, action string, params map[string]any) {
	if params == nil {
		params = map[string]any{}
	}
	operatorUserId := c.GetInt("id")
	if _, ok := params["target_user_id"]; !ok && targetUserId > 0 && targetUserId != operatorUserId {
		params["target_user_id"] = targetUserId
	}
	writeOperationAuditLog(c, operatorUserId, auditContentEN(action, params), c.ClientIP(), action, params, auditOperatorInfo(c), nil)
	markAuditLogged(c)
}

// recordUserSecurityAudit 记录普通用户自己的安全敏感操作（如 passkey 绑定/解绑）。
// 这类日志没有管理员操作者，不写 admin_info；同时不依赖 AdminAuth/RootAuth 的兜底。
func recordUserSecurityAudit(c *gin.Context, userId int, action string, params map[string]any) {
	if code := c.GetString("security_error_code"); code != "" {
		if params == nil {
			params = map[string]any{}
		}
		params["code"] = code
	}
	if c.GetBool("use_access_token") {
		if params == nil {
			params = map[string]any{}
		}
		params["token_ref"] = c.GetString("access_token_ref")
	}
	var auditInfo *model.AuditRequestInfo
	if success, ok := params["success"].(bool); ok {
		auditInfo = &model.AuditRequestInfo{
			Method: c.Request.Method, Route: c.FullPath(), Path: c.FullPath(),
			Status: c.Writer.Status(), Success: success,
		}
	}
	writeOperationAuditLog(c, userId, auditContentEN(action, params), c.ClientIP(), action, params, nil, auditInfo)
}

func writeOperationAuditLog(c *gin.Context, userId int, content, ip, action string, params map[string]any, admin *model.AuditAdminInfo, request *model.AuditRequestInfo) {
	fields := model.AuditFields{}
	for key, value := range params {
		fields[key] = value
	}
	entry := model.AuditLog{
		UserId: userId, Category: model.AuditCategoryOperation, Action: action, Content: content, Ip: ip,
		Other: model.AuditOther{Op: &model.AuditOperation{Action: action, Params: fields}},
	}
	if admin != nil {
		entry.ActorRole = admin.AdminRole
		entry.Other.AdminInfo = admin
	}
	if request != nil {
		entry.Status = request.Status
		entry.Success = request.Success
		entry.Other.AuditInfo = request
	} else {
		entry.Success = true
	}
	if entry.ActorRole == 0 && c != nil {
		entry.ActorRole = c.GetInt("role")
	}
	model.RecordAuditLog(c, entry)
}

func recordLegacyGitHubBindingAudit(c *gin.Context, user *model.User, success bool, params map[string]any) {
	if user == nil || c == nil {
		return
	}
	if params == nil {
		params = map[string]any{}
	}
	params["provider"] = "github"
	params["legacy_migration"] = true
	params["success"] = success
	fields := model.AuditFields{}
	for key, value := range params {
		fields[key] = value
	}
	model.RecordAuditLog(c, model.AuditLog{
		UserId: user.Id, Username: user.Username, ActorRole: user.Role,
		Category: model.AuditCategoryOperation, Action: "user.binding_bind",
		Content: auditContentEN("user.binding_bind", params), Ip: c.ClientIP(), Success: success,
		Other: model.AuditOther{Op: &model.AuditOperation{Action: "user.binding_bind", Params: fields}},
	})
}

func tokenAuditParams(c *gin.Context) model.AuditFields {
	params, ok := common.GetContextKeyType[model.AuditFields](c, constant.ContextKeyTokenAuditParams)
	if !ok {
		params = model.AuditFields{}
		common.SetContextKey(c, constant.ContextKeyTokenAuditParams, params)
	}
	return params
}

func tokenBatchAuditParams(c *gin.Context, ids []int) model.AuditFields {
	params := tokenAuditParams(c)
	params["total"] = len(ids)
	// Bound audit payloads without changing the batch operation's limits.
	params["requested_ids"] = append([]int{}, ids[:min(len(ids), 100)]...)
	if len(ids) > 100 {
		params["requested_ids_truncated"] = true
	}
	return params
}
