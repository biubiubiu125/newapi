package model

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		condition, pattern, err := buildLogLikeCondition(column, value)
		if err != nil {
			return nil, err
		}
		return tx.Where(condition, pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

func userIDColumnForLogUsername(column string) string {
	if strings.HasPrefix(column, "logs.") {
		return "logs.user_id"
	}
	return "user_id"
}

func logUserIDsByUsername(username string) ([]int, error) {
	username = strings.TrimSpace(username)
	if username == "" || DB == nil {
		return nil, nil
	}
	query := DB.Model(&User{}).Select("id")
	if strings.Contains(username, "%") {
		pattern, err := sanitizeLikePattern(username)
		if err != nil {
			return nil, err
		}
		query = query.Where("username LIKE ? ESCAPE '!'", pattern)
	} else {
		query = query.Where("username = ?", username)
	}
	userIDs := make([]int, 0)
	if err := query.Pluck("id", &userIDs).Error; err != nil {
		return nil, err
	}
	return userIDs, nil
}

func applyLogUsernameFilter(tx *gorm.DB, column string, username string) (*gorm.DB, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return tx, nil
	}
	userIDs, err := logUserIDsByUsername(username)
	if err != nil {
		return nil, err
	}
	if len(userIDs) == 0 {
		return applyExplicitLogTextFilter(tx, column, username)
	}
	userIDColumn := userIDColumnForLogUsername(column)
	if !strings.Contains(username, "%") {
		return tx.Where(userIDColumn+" IN ?", userIDs), nil
	}
	condition, pattern, err := buildLogLikeCondition(column, username)
	if err != nil {
		return nil, err
	}
	return tx.Where("("+userIDColumn+" IN ? OR "+condition+")", userIDs, pattern), nil
}

func buildLogLikeCondition(column string, value string) (string, string, error) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		pattern, err := sanitizeClickHouseLikePattern(value)
		if err != nil {
			return "", "", err
		}
		return column + " LIKE ?", pattern, nil
	}

	pattern, err := sanitizeLikePattern(value)
	if err != nil {
		return "", "", err
	}
	return column + " LIKE ? ESCAPE '!'", pattern, nil
}

func sanitizeClickHouseLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, `\`, `\\`)
	input = strings.ReplaceAll(input, `_`, `\_`)

	if err := validateLikePattern(input); err != nil {
		return "", err
	}
	return input, nil
}

type Log struct {
	Id                int    `json:"id" gorm:"index:idx_created_at_id,priority:2;index:idx_user_id_id,priority:2"`
	UserId            int    `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:1;index:idx_created_at_type"`
	Type              int    `json:"type" gorm:"index:idx_created_at_type"`
	Content           string `json:"content"`
	Username          string `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName         string `json:"token_name" gorm:"index;default:''"`
	ModelName         string `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota             int    `json:"quota" gorm:"default:0"`
	PromptTokens      int    `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens  int    `json:"completion_tokens" gorm:"default:0"`
	UseTime           int    `json:"use_time" gorm:"default:0"`
	IsStream          bool   `json:"is_stream"`
	ChannelId         int    `json:"channel" gorm:"index"`
	ChannelName       string `json:"channel_name" gorm:"->"`
	TokenId           int    `json:"token_id" gorm:"default:0;index"`
	Group             string `json:"group" gorm:"index"`
	Ip                string `json:"ip" gorm:"index;default:''"`
	RequestId         string `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId string `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	Other             string `json:"other"`
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
)

func ensureLogRequestId(log *Log) {
	if log != nil && log.RequestId == "" {
		log.RequestId = common.NewRequestId()
	}
}

func resolveLogUsername(c *gin.Context, userId int) string {
	username := ""
	if c != nil {
		username = strings.TrimSpace(c.GetString("username"))
	}
	if username != "" || userId <= 0 {
		return username
	}
	username, _ = GetUsernameById(userId, false)
	return strings.TrimSpace(username)
}

func createLog(log *Log) error {
	ensureLogRequestId(log)
	return LOG_DB.Create(log).Error
}

const logUsernameBackfillBatchSize = 500

type logUsernameBackfillRow struct {
	ID     int `gorm:"column:id"`
	UserID int `gorm:"column:user_id"`
}

func migrateLogUsernames() error {
	if DB == nil {
		return nil
	}
	if err := migrateQuotaDataUsernames(); err != nil {
		return err
	}
	if LOG_DB == nil || !LOG_DB.Migrator().HasTable(&Log{}) {
		return nil
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return migrateClickHouseLogUsernames()
	}
	return migrateRelationalLogUsernames()
}

func migrateQuotaDataUsernames() error {
	if DB == nil || !DB.Migrator().HasTable(&QuotaData{}) || !DB.Migrator().HasTable(&User{}) {
		return nil
	}
	total, err := backfillUsernameRows(DB, "quota_data")
	if err != nil {
		return fmt.Errorf("migrate quota_data usernames: %w", err)
	}
	if total > 0 {
		common.SysLog(fmt.Sprintf("backfilled %d quota_data usernames", total))
	}
	return nil
}

func migrateRelationalLogUsernames() error {
	if !DB.Migrator().HasTable(&User{}) {
		return nil
	}
	total, err := backfillUsernameRows(LOG_DB, "logs")
	if err != nil {
		return fmt.Errorf("migrate log usernames: %w", err)
	}
	if total > 0 {
		common.SysLog(fmt.Sprintf("backfilled %d log usernames", total))
	}
	return nil
}

func backfillUsernameRows(targetDB *gorm.DB, tableName string) (int64, error) {
	var total int64
	lastID := 0
	for {
		var rows []logUsernameBackfillRow
		err := targetDB.Table(tableName).
			Select("id", "user_id").
			Where("id > ? AND user_id > 0 AND (username = '' OR username IS NULL)", lastID).
			Order("id ASC").
			Limit(logUsernameBackfillBatchSize).
			Find(&rows).Error
		if err != nil {
			return total, err
		}
		if len(rows) == 0 {
			return total, nil
		}
		lastID = rows[len(rows)-1].ID
		usernames, err := usernamesByIDs(userIDsFromBackfillRows(rows))
		if err != nil {
			return total, err
		}
		for _, row := range rows {
			username := strings.TrimSpace(usernames[row.UserID])
			if username == "" {
				continue
			}
			result := targetDB.Table(tableName).
				Where("id = ? AND (username = '' OR username IS NULL)", row.ID).
				Update("username", username)
			if result.Error != nil {
				return total, result.Error
			}
			total += result.RowsAffected
		}
	}
}

func migrateClickHouseLogUsernames() error {
	if LOG_DB == nil || !DB.Migrator().HasTable(&User{}) {
		return nil
	}
	total, err := migrateClickHouseLogUsernamesInBatches(
		func(lastUserID int, limit int) ([]int, error) {
			var rows []struct {
				UserID int `gorm:"column:user_id"`
			}
			if err := LOG_DB.Raw(clickHouseLogUsernameUserIDsSQL(limit), lastUserID).Scan(&rows).Error; err != nil {
				return nil, fmt.Errorf("query clickhouse log usernames: %w", err)
			}
			userIDs := make([]int, 0, len(rows))
			for _, row := range rows {
				userIDs = append(userIDs, row.UserID)
			}
			return userIDs, nil
		},
		usernamesByIDs,
		func(userID int, username string) (int64, error) {
			result := LOG_DB.Exec(clickHouseLogUsernameUpdateSQL(), username, userID)
			if result.Error != nil {
				return 0, fmt.Errorf("update clickhouse log username for user %d: %w", userID, result.Error)
			}
			return result.RowsAffected, nil
		},
	)
	if err != nil {
		return err
	}
	if total > 0 {
		common.SysLog(fmt.Sprintf("backfilled %d clickhouse log username groups", total))
	}
	return nil
}

func clickHouseLogUsernameUserIDsSQL(limit int) string {
	return fmt.Sprintf("SELECT DISTINCT user_id FROM logs WHERE user_id > ? AND username = '' ORDER BY user_id ASC LIMIT %d", limit)
}

func clickHouseLogUsernameUpdateSQL() string {
	return "ALTER TABLE logs UPDATE username = ? WHERE user_id = ? AND username = '' SETTINGS mutations_sync = 1"
}

func migrateClickHouseLogUsernamesInBatches(
	fetchUserIDs func(lastUserID int, limit int) ([]int, error),
	resolveUsernames func(userIDs []int) (map[int]string, error),
	updateUsername func(userID int, username string) (int64, error),
) (int64, error) {
	var total int64
	lastUserID := 0
	for {
		userIDs, err := fetchUserIDs(lastUserID, logUsernameBackfillBatchSize)
		if err != nil {
			return total, err
		}
		if len(userIDs) == 0 {
			return total, nil
		}

		maxUserID := lastUserID
		for _, userID := range userIDs {
			if userID > maxUserID {
				maxUserID = userID
			}
		}
		if maxUserID <= lastUserID {
			return total, fmt.Errorf("clickhouse log username backfill did not advance past user_id %d", lastUserID)
		}

		usernames, err := resolveUsernames(userIDs)
		if err != nil {
			return total, err
		}
		for _, userID := range userIDs {
			username := strings.TrimSpace(usernames[userID])
			if username == "" {
				continue
			}
			rowsAffected, err := updateUsername(userID, username)
			if err != nil {
				return total, err
			}
			total += rowsAffected
		}
		lastUserID = maxUserID
	}
}

func userIDsFromBackfillRows(rows []logUsernameBackfillRow) []int {
	seen := make(map[int]struct{}, len(rows))
	userIDs := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.UserID <= 0 {
			continue
		}
		if _, ok := seen[row.UserID]; ok {
			continue
		}
		seen[row.UserID] = struct{}{}
		userIDs = append(userIDs, row.UserID)
	}
	return userIDs
}

func usernamesByIDs(userIDs []int) (map[int]string, error) {
	usernames := make(map[int]string, len(userIDs))
	if len(userIDs) == 0 || DB == nil {
		return usernames, nil
	}
	var users []User
	if err := DB.Model(&User{}).Select("id", "username").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, user := range users {
		usernames[user.Id] = user.Username
	}
	return usernames, nil
}

func logUserIDs(logs []*Log) []int {
	seen := make(map[int]struct{}, len(logs))
	userIDs := make([]int, 0, len(logs))
	for _, log := range logs {
		if log == nil || log.UserId <= 0 {
			continue
		}
		if _, ok := seen[log.UserId]; ok {
			continue
		}
		seen[log.UserId] = struct{}{}
		userIDs = append(userIDs, log.UserId)
	}
	return userIDs
}

func fillLogCurrentUsernames(logs []*Log) error {
	usernames, err := usernamesByIDs(logUserIDs(logs))
	if err != nil {
		return err
	}
	for _, log := range logs {
		if log == nil {
			continue
		}
		if username := usernames[log.UserId]; username != "" {
			log.Username = username
		}
	}
	return nil
}

func clickHouseLogOrder(prefix string) string {
	return prefix + "created_at desc, " + prefix + "request_id desc"
}

func assignDisplayLogIds(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].Id = startIdx + i + 1
	}
}

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			delete(otherMap, "audit_info")
			// delete(otherMap, "reject_reason")
			delete(otherMap, "stream_status")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
	}
	assignDisplayLogIds(logs, startIdx)
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	order := "id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("")
	}
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order(order).Limit(common.MaxRecentItems).Find(&logs).Error
	if err != nil {
		return logs, err
	}
	if err = fillLogCurrentUsernames(logs); err != nil {
		return logs, err
	}
	formatUserLogs(logs, 0)
	return logs, nil
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo 记录操作日志，并将管理员相关信息存入 Other.admin_info，
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

func RecordOperationAuditLog(userId int, content string, ip string, action string, params map[string]interface{}, adminInfo map[string]interface{}, auditInfo map[string]interface{}) {
	username, _ := GetUsernameById(userId, false)
	other := map[string]interface{}{}
	if len(adminInfo) > 0 {
		other["admin_info"] = adminInfo
	}
	if action = strings.TrimSpace(action); action != "" {
		other["action"] = action
	}
	if len(params) > 0 {
		other["params"] = params
	}
	if len(auditInfo) > 0 {
		other["audit_info"] = auditInfo
	}

	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeManage,
		Content:   content,
		Ip:        strings.TrimSpace(ip),
	}
	if len(other) > 0 {
		log.Other = common.MapToJsonStr(other)
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record operation audit log: " + err.Error())
	}
}

func RecordLoginLog(userId int, username string, content string, ip string, action string, params map[string]interface{}, extra map[string]interface{}) {
	other := map[string]interface{}{}
	if action = strings.TrimSpace(action); action != "" {
		other["action"] = action
	}
	if len(params) > 0 {
		other["params"] = params
	}
	if len(extra) > 0 {
		other["login_info"] = extra
	}
	log := &Log{
		UserId:    userId,
		Username:  strings.TrimSpace(username),
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeSystem,
		Content:   content,
		Ip:        strings.TrimSpace(ip),
	}
	if log.Username == "" {
		log.Username, _ = GetUsernameById(userId, false)
	}
	if len(other) > 0 {
		log.Other = common.MapToJsonStr(other)
	}
	if err := createLog(log); err != nil {
		common.SysLog("failed to record login log: " + err.Error())
	}
}

type PaymentAuditLogInfo struct {
	CallerIP              string
	PaymentMethod         string
	CallbackPaymentMethod string
	PaymentProvider       string
	OrderType             string
	ProductName           string
	PaidAmount            float64
	PaidCurrency          string
}

func paymentAuditServerAddress() (string, string) {
	raw := strings.TrimSpace(operation_setting.CustomCallbackAddress)
	if !isPublicPaymentAuditAddress(raw) {
		raw = strings.TrimSpace(system_setting.ServerAddress)
	}
	if !isPublicPaymentAuditAddress(raw) {
		return "", ""
	}
	raw = strings.TrimRight(raw, "/")
	parsed, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(parsed.Hostname()) == "" {
		return raw, raw
	}
	return raw, parsed.Hostname()
}

func isPublicPaymentAuditAddress(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host != "" && host != "localhost" && host != "127.0.0.1" && host != "::1"
}

func RecordPaymentAuditLog(userId int, content string, info PaymentAuditLogInfo) {
	username, _ := GetUsernameById(userId, false)
	serverAddress, serverHost := paymentAuditServerAddress()
	adminInfo := map[string]interface{}{
		"node_name":               common.NodeName,
		"caller_ip":               strings.TrimSpace(info.CallerIP),
		"payment_method":          strings.TrimSpace(info.PaymentMethod),
		"callback_payment_method": strings.TrimSpace(info.CallbackPaymentMethod),
		"payment_provider":        strings.TrimSpace(info.PaymentProvider),
		"order_type":              strings.TrimSpace(info.OrderType),
		"product_name":            strings.TrimSpace(info.ProductName),
		"version":                 common.Version,
	}
	if serverAddress != "" {
		adminInfo["server_address"] = serverAddress
	}
	if serverHost != "" {
		adminInfo["server_host"] = serverHost
		adminInfo["server_ip"] = serverHost
	}
	if info.PaidAmount > 0 {
		adminInfo["paid_amount"] = info.PaidAmount
	}
	if strings.TrimSpace(info.PaidCurrency) != "" {
		adminInfo["paid_currency"] = strings.TrimSpace(info.PaidCurrency)
	}
	other := map[string]interface{}{
		"admin_info": adminInfo,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        strings.TrimSpace(info.CallerIP),
		Other:     common.MapToJsonStr(other),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	RecordPaymentAuditLog(userId, content, PaymentAuditLogInfo{
		CallerIP:              callerIp,
		PaymentMethod:         paymentMethod,
		CallbackPaymentMethod: callbackPaymentMethod,
		OrderType:             "topup",
	})
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, content))
	username := resolveLogUsername(c, userId)
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	otherStr := common.MapToJsonStr(other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return common.GetClientIP(c)
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	}
}

type RecordConsumeLogParams struct {
	ChannelId        int                    `json:"channel_id"`
	PromptTokens     int                    `json:"prompt_tokens"`
	CompletionTokens int                    `json:"completion_tokens"`
	ModelName        string                 `json:"model_name"`
	TokenName        string                 `json:"token_name"`
	Quota            int                    `json:"quota"`
	Content          string                 `json:"content"`
	TokenId          int                    `json:"token_id"`
	UseTimeSeconds   int                    `json:"use_time_seconds"`
	IsStream         bool                   `json:"is_stream"`
	Group            string                 `json:"group"`
	Other            map[string]interface{} `json:"other"`
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) error {
	if !common.LogConsumeEnabled {
		return nil
	}
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := resolveLogUsername(c, userId)
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	otherStr := common.MapToJsonStr(params.Other)
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return common.GetClientIP(c)
			}
			return ""
		}(),
		RequestId:         requestId,
		UpstreamRequestId: upstreamRequestId,
		Other:             otherStr,
	}
	err := createLog(log)
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
		return err
	}
	if common.DataExportEnabled {
		gopool.Go(func() {
			LogQuotaData(QuotaDataLogParams{
				UserID:    userId,
				Username:  username,
				ModelName: params.ModelName,
				Quota:     params.Quota,
				CreatedAt: common.GetTimestamp(),
				TokenUsed: params.PromptTokens + params.CompletionTokens,
				UseGroup:  params.Group,
				TokenID:   params.TokenId,
				ChannelID: params.ChannelId,
				NodeName:  common.NodeName,
			})
		})
	}
	return nil
}

type RecordTaskBillingLogParams struct {
	UserId        int
	LogType       int
	Content       string
	ChannelId     int
	ModelName     string
	Quota         int
	TokenId       int
	Group         string
	Other         map[string]interface{}
	NodeName      string // task creation node; empty falls back to current node
	SkipQuotaData bool
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) error {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return nil
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	createdAt := common.GetTimestamp()
	log := &Log{
		UserId:    params.UserId,
		Username:  username,
		CreatedAt: createdAt,
		Type:      params.LogType,
		Content:   params.Content,
		TokenName: tokenName,
		ModelName: params.ModelName,
		Quota:     params.Quota,
		ChannelId: params.ChannelId,
		TokenId:   params.TokenId,
		Group:     params.Group,
		Other:     common.MapToJsonStr(params.Other),
	}
	err := createLog(log)
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
		return err
	}
	if (params.LogType == LogTypeConsume || params.LogType == LogTypeRefund) && common.DataExportEnabled && !params.SkipQuotaData {
		nodeName := params.NodeName
		if nodeName == "" {
			nodeName = common.NodeName
		}
		quota := params.Quota
		if params.LogType == LogTypeRefund {
			quota = -quota
		}
		LogQuotaData(QuotaDataLogParams{
			UserID:        params.UserId,
			Username:      username,
			ModelName:     params.ModelName,
			Quota:         quota,
			CreatedAt:     createdAt,
			UseGroup:      params.Group,
			TokenID:       params.TokenId,
			ChannelID:     params.ChannelId,
			NodeName:      nodeName,
			Count:         0,
			ExplicitCount: true,
		})
	}
	return nil
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyLogUsernameFilter(tx, "logs.username", username); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	order := "logs.created_at desc, logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, startIdx)
	}
	if err = fillLogCurrentUsernames(logs); err != nil {
		return nil, 0, err
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

const logSearchCountLimit = 10000

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	order := "logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	if err = fillLogCurrentUsernames(logs); err != nil {
		return nil, 0, err
	}
	formatUserLogs(logs, startIdx)
	return logs, total, err
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

func applyRealRequestLogFilter(tx *gorm.DB) *gorm.DB {
	return tx.Where("(other IS NULL OR other = '' OR other NOT LIKE ?)", `%"pre_consumed_quota"%`)
}

func logStatQuotaSelect(logType int) (string, []interface{}) {
	if logType == LogTypeUnknown {
		return "COALESCE(sum(CASE WHEN type = ? THEN -quota ELSE quota END), 0) quota", []interface{}{LogTypeRefund}
	}
	return "COALESCE(sum(quota), 0) quota", nil
}

func applyLogStatQuotaTypeFilter(tx *gorm.DB, logType int) *gorm.DB {
	if logType == LogTypeUnknown {
		return tx.Where("type IN ?", []int{LogTypeConsume, LogTypeRefund})
	}
	return tx.Where("type = ?", logType)
}

func applyLogStatRpmTpmTypeFilter(tx *gorm.DB, logType int) *gorm.DB {
	if logType != LogTypeUnknown && logType != LogTypeConsume {
		return tx.Where("1 = 0")
	}
	return applyRealRequestLogFilter(tx.Where("type = ?", LogTypeConsume))
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string) (stat Stat, err error) {
	quotaSelect, quotaSelectArgs := logStatQuotaSelect(logType)
	tx := LOG_DB.Table("logs").Select(quotaSelect, quotaSelectArgs...)

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) tpm")

	if tx, err = applyLogUsernameFilter(tx, "username", username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyLogUsernameFilter(rpmTpmQuery, "username", username); err != nil {
		return stat, err
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = applyLogStatQuotaTypeFilter(tx, logType)
	rpmTpmQuery = applyLogStatRpmTpmTypeFilter(rpmTpmQuery, logType)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

func SumUsedQuotaByUserId(logType int, startTimestamp int64, endTimestamp int64, modelName string, userId int, tokenName string, channel int, group string) (stat Stat, err error) {
	quotaSelect, quotaSelectArgs := logStatQuotaSelect(logType)
	tx := LOG_DB.Table("logs").Select(quotaSelect, quotaSelectArgs...).Where("user_id = ?", userId)

	rpmTpmQuery := LOG_DB.Table("logs").
		Select("count(*) rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) tpm").
		Where("user_id = ?", userId)

	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx = applyLogStatQuotaTypeFilter(tx, logType)
	rpmTpmQuery = applyLogStatRpmTpmTypeFilter(rpmTpmQuery, logType)
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat by user id: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat by user id: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0)")
	var err error
	if tx, err = applyLogUsernameFilter(tx, "username", username); err != nil {
		common.SysError("failed to apply username filter for used token stat: " + err.Error())
		return 0
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if limit <= 0 {
		limit = 100
	}

	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		rowsAffected, err := DeleteOldLogBatch(ctx, targetTimestamp, limit)
		if nil != err {
			return total, err
		}

		total += rowsAffected

		if rowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}

func CountOldLog(ctx context.Context, targetTimestamp int64) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	var total int64
	if err := LOG_DB.WithContext(ctx).Model(&Log{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

func DeleteOldLogBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = 100
	}

	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		total, err := CountOldLog(ctx, targetTimestamp)
		if err != nil {
			return 0, err
		}
		if total == 0 {
			return 0, nil
		}
		if err := LOG_DB.WithContext(ctx).Exec(
			"ALTER TABLE logs DELETE WHERE created_at < ? SETTINGS mutations_sync = 1",
			targetTimestamp,
		).Error; err != nil {
			return 0, err
		}
		return total, nil
	}

	result := LOG_DB.WithContext(ctx).Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
