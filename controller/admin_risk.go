package controller

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type riskSignal struct {
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	EventKey    string  `json:"event_key,omitempty"`
	TargetType  string  `json:"target_type,omitempty"`
	TargetId    string  `json:"target_id,omitempty"`
	UserID      int     `json:"user_id,omitempty"`
	Username    string  `json:"username,omitempty"`
	IP          string  `json:"ip,omitempty"`
	TokenID     int     `json:"token_id,omitempty"`
	TradeNo     string  `json:"trade_no,omitempty"`
	Count       int64   `json:"count"`
	Amount      float64 `json:"amount,omitempty"`
	Message     string  `json:"message"`
	FirstSeenAt int64   `json:"first_seen_at,omitempty"`
	LastSeenAt  int64   `json:"last_seen_at,omitempty"`
}

type riskUserRow struct {
	UserID          int     `json:"user_id"`
	Username        string  `json:"username"`
	Email           string  `json:"email,omitempty"`
	Status          int     `json:"status"`
	Role            int     `json:"role"`
	Group           string  `json:"group,omitempty"`
	CreatedAt       int64   `json:"created_at"`
	LastLoginAt     int64   `json:"last_login_at"`
	Quota           int     `json:"quota"`
	UsedQuota       int     `json:"used_quota"`
	RequestCount    int     `json:"request_count"`
	TopupCount      int64   `json:"topup_count"`
	TopupPaidAmount float64 `json:"topup_paid_amount"`
	ErrorCount      int64   `json:"error_count"`
	ConsumeCount    int64   `json:"consume_count"`
	ConsumeQuota    int64   `json:"consume_quota"`
	UniqueIPCount   int64   `json:"unique_ip_count"`
	TokenCount      int64   `json:"token_count"`
	SignalCount     int     `json:"signal_count"`
	Severity        string  `json:"severity"`
}

type riskLogMetric struct {
	UserID        int   `json:"user_id"`
	ErrorCount    int64 `json:"error_count"`
	ConsumeCount  int64 `json:"consume_count"`
	ConsumeQuota  int64 `json:"consume_quota"`
	UniqueIPCount int64 `json:"unique_ip_count"`
}

type riskLogRow struct {
	ID        int    `json:"id"`
	UserID    int    `json:"user_id"`
	Username  string `json:"username"`
	Type      int    `json:"type"`
	Content   string `json:"content"`
	Quota     int    `json:"quota"`
	IP        string `json:"ip"`
	TokenID   int    `json:"token_id"`
	TokenName string `json:"token_name"`
	ModelName string `json:"model_name"`
	CreatedAt int64  `json:"created_at"`
}

type riskOrderRow struct {
	OrderType                string  `json:"order_type"`
	TradeNo                  string  `json:"trade_no"`
	UserID                   int     `json:"user_id"`
	Username                 string  `json:"username"`
	Status                   string  `json:"status"`
	PaidAmount               float64 `json:"paid_amount"`
	PaidCurrency             string  `json:"paid_currency"`
	PaymentProvider          string  `json:"payment_provider"`
	PaymentMethod            string  `json:"payment_method"`
	ReferralCommissionStatus string  `json:"referral_commission_status"`
	ReferralCommissionError  string  `json:"referral_commission_error"`
	CreatedAt                int64   `json:"created_at"`
}

type riskTokenRow struct {
	TokenID       int    `json:"token_id"`
	TokenName     string `json:"token_name"`
	UserID        int    `json:"user_id"`
	Username      string `json:"username"`
	Status        int    `json:"status"`
	RequestCount  int64  `json:"request_count"`
	ErrorCount    int64  `json:"error_count"`
	ConsumeQuota  int64  `json:"consume_quota"`
	UniqueIPCount int64  `json:"unique_ip_count"`
	LastSeenAt    int64  `json:"last_seen_at"`
}

type riskIPRow struct {
	IP            string  `json:"ip"`
	UserCount     int64   `json:"user_count"`
	TokenCount    int64   `json:"token_count"`
	RequestCount  int64   `json:"request_count"`
	ErrorCount    int64   `json:"error_count"`
	ConsumeQuota  int64   `json:"consume_quota"`
	FailureRate   float64 `json:"failure_rate"`
	FirstSeenAt   int64   `json:"first_seen_at"`
	LastSeenAt    int64   `json:"last_seen_at"`
	Whitelisted   bool    `json:"whitelisted"`
	WhitelistNote string  `json:"whitelist_note,omitempty"`
}

type riskReferralRow struct {
	AffiliateID      int     `json:"affiliate_id"`
	InviterUserID    int     `json:"inviter_user_id"`
	InviterUsername  string  `json:"inviter_username"`
	InviteeCount     int64   `json:"invitee_count"`
	CommissionCount  int64   `json:"commission_count"`
	CommissionAmount float64 `json:"commission_amount"`
	WithdrawalCount  int64   `json:"withdrawal_count"`
	WithdrawalAmount float64 `json:"withdrawal_amount"`
	Severity         string  `json:"severity"`
	Reason           string  `json:"reason"`
}

type riskDetailResponse struct {
	Type        string                `json:"type"`
	WindowHours int                   `json:"window_hours"`
	IP          string                `json:"ip,omitempty"`
	UserID      int                   `json:"user_id,omitempty"`
	TokenID     int                   `json:"token_id,omitempty"`
	TradeNo     string                `json:"trade_no,omitempty"`
	Event       *model.RiskEvent      `json:"event,omitempty"`
	Users       []riskUserRow         `json:"users"`
	Logs        []riskLogRow          `json:"logs"`
	Orders      []riskOrderRow        `json:"orders"`
	Tokens      []riskTokenRow        `json:"tokens"`
	IPs         []riskIPRow           `json:"ips"`
	Referrals   []riskReferralRow     `json:"referrals"`
	Actions     []model.RiskAction    `json:"actions"`
	Whitelists  []model.RiskWhitelist `json:"whitelists"`
}

type riskActionRequest struct {
	EventID    int    `json:"event_id"`
	Reason     string `json:"reason"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	UserID     int    `json:"user_id"`
	TokenID    int    `json:"token_id"`
	IP         string `json:"ip"`
	ExpiresAt  int64  `json:"expires_at"`
}

func GetRiskOverview(c *gin.Context) {
	windowHours := parseRiskWindowHours(c.Query("window_hours"))
	since := common.GetTimestamp() - int64(windowHours*3600)
	signals, err := collectRiskSignals(since, windowHours)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var disabledUsers int64
	_ = model.DB.Model(&model.User{}).Where("status = ?", common.UserStatusDisabled).Count(&disabledUsers).Error
	var newUsers int64
	_ = model.DB.Model(&model.User{}).Where("created_at >= ?", since).Count(&newUsers).Error
	var openEvents int64
	_ = model.DB.Model(&model.RiskEvent{}).Where("status IN ?", []string{model.RiskEventStatusOpen, model.RiskEventStatusViewed}).Count(&openEvents).Error
	var highEvents int64
	_ = model.DB.Model(&model.RiskEvent{}).Where("status IN ? AND severity = ?", []string{model.RiskEventStatusOpen, model.RiskEventStatusViewed}, model.RiskSeverityHigh).Count(&highEvents).Error

	common.ApiSuccess(c, gin.H{
		"window_hours":     windowHours,
		"signal_count":     len(signals),
		"open_event_count": openEvents,
		"high_event_count": highEvents,
		"disabled_users":   disabledUsers,
		"new_user_count":   newUsers,
		"signals":          signals,
	})
}

func ScanRiskEvents(c *gin.Context) {
	windowHours := parseRiskWindowHours(c.Query("window_hours"))
	events, err := scanAndPersistRiskEvents(windowHours)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"window_hours": windowHours,
		"count":        len(events),
		"events":       events,
	})
}

func GetRiskEvents(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	status := strings.TrimSpace(c.DefaultQuery("status", model.RiskEventStatusOpen))
	severity := strings.TrimSpace(c.Query("severity"))
	riskType := strings.TrimSpace(c.Query("type"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	query := model.DB.Model(&model.RiskEvent{})
	if status != "" && status != "all" {
		if status == model.RiskEventStatusOpen {
			query = query.Where("status IN ?", []string{model.RiskEventStatusOpen, model.RiskEventStatusViewed})
		} else {
			query = query.Where("status = ?", status)
		}
	}
	if severity != "" && severity != "all" {
		query = query.Where("severity = ?", severity)
	}
	if riskType != "" && riskType != "all" {
		query = query.Where("type = ?", riskType)
	}
	if keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("event_key LIKE ? OR username LIKE ? OR ip LIKE ? OR trade_no LIKE ? OR token_name LIKE ? OR summary LIKE ?", like, like, like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var events []model.RiskEvent
	if err := query.Order("case severity when 'high' then 0 when 'warning' then 1 else 2 end, last_seen_at desc, id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&events).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(events)
	common.ApiSuccess(c, pageInfo)
}

func GetRiskDetail(c *gin.Context) {
	windowHours := parseRiskWindowHours(c.Query("window_hours"))
	since := common.GetTimestamp() - int64(windowHours*3600)
	riskType := strings.TrimSpace(c.Query("type"))
	ip := strings.TrimSpace(c.Query("ip"))
	userID, _ := strconv.Atoi(strings.TrimSpace(c.Query("user_id")))
	tokenID, _ := strconv.Atoi(strings.TrimSpace(c.Query("token_id")))
	eventID, _ := strconv.Atoi(strings.TrimSpace(c.Query("event_id")))
	tradeNo := strings.TrimSpace(c.Query("trade_no"))

	var event *model.RiskEvent
	if eventID > 0 {
		var item model.RiskEvent
		if err := model.DB.Where("id = ?", eventID).First(&item).Error; err == nil {
			event = &item
			if riskType == "" {
				riskType = item.Type
			}
			if ip == "" {
				ip = item.Ip
			}
			if userID == 0 {
				userID = item.UserId
			}
			if tokenID == 0 {
				tokenID = item.TokenId
			}
			if tradeNo == "" {
				tradeNo = item.TradeNo
			}
		}
	}

	userIDs := make([]int, 0, 16)
	if userID > 0 {
		userIDs = append(userIDs, userID)
	}
	switch riskType {
	case "shared_ip", "ip_detail":
		if ip != "" {
			ids, err := riskUserIDsByIP(ip, since)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			userIDs = append(userIDs, ids...)
		}
	case "token_rotation", "token_detail":
		if tokenID > 0 {
			ids, err := riskUserIDsByTokenID(tokenID, since)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			userIDs = append(userIDs, ids...)
		}
	case "order_detail":
		ids, err := riskUserIDsByTradeNo(tradeNo)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userIDs = append(userIDs, ids...)
	case "new_users":
		ids, err := riskNewUserIDs(since)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userIDs = append(userIDs, ids...)
	case "disabled_users":
		ids, err := riskUserIDsByStatus(common.UserStatusDisabled)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userIDs = append(userIDs, ids...)
	}
	userIDs = uniquePositiveInts(userIDs)

	users, err := riskUsersByIDs(userIDs, since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	logs, err := riskLogsForDetail(riskType, ip, tokenID, userIDs, since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	orders, err := riskOrdersByUserIDs(userIDs, since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if tradeNo != "" {
		orderRows, err := riskOrdersByTradeNo(tradeNo)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		orders = append(orderRows, orders...)
	}
	tokens, err := riskTokensForDetail(userIDs, tokenID, since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ips, err := riskIPsForDetail(ip, userIDs, since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	referrals, err := riskReferralsForDetail(userIDs, since)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	actions, err := riskActionsForDetail(eventID, userID, tokenID, ip)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	eventTargetType := ""
	eventTargetID := ""
	if event != nil {
		eventTargetType = event.TargetType
		eventTargetID = event.TargetId
	}
	whitelists, err := riskWhitelistsForDetail(userID, tokenID, ip, eventTargetType, eventTargetID)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, riskDetailResponse{
		Type:        riskType,
		WindowHours: windowHours,
		IP:          ip,
		UserID:      userID,
		TokenID:     tokenID,
		TradeNo:     tradeNo,
		Event:       event,
		Users:       users,
		Logs:        logs,
		Orders:      uniqueRiskOrders(orders),
		Tokens:      tokens,
		IPs:         ips,
		Referrals:   referrals,
		Actions:     actions,
		Whitelists:  whitelists,
	})
}

func GetRiskUsers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	windowHours := parseRiskWindowHours(c.Query("window_hours"))
	keyword := strings.TrimSpace(c.Query("keyword"))
	since := common.GetTimestamp() - int64(windowHours*3600)

	query := model.DB.Table("users AS u").
		Select(`
			u.id AS user_id,
			u.username,
			u.email,
			u.status,
			u.role,
			`+commonGroupColSelect("u")+` AS `+commonGroupAliasSelect()+`,
			u.created_at,
			u.last_login_at,
			u.quota,
			u.used_quota,
			u.request_count,
			coalesce(topup.topup_count, 0) AS topup_count,
			coalesce(topup.topup_paid_amount, 0) AS topup_paid_amount,
			coalesce(token_stats.token_count, 0) AS token_count
		`).
		Joins(`LEFT JOIN (
			SELECT user_id, count(*) AS topup_count, coalesce(sum(paid_amount), 0) AS topup_paid_amount
			FROM top_ups
			WHERE create_time >= ? AND status = ?
			GROUP BY user_id
		) topup ON topup.user_id = u.id`, since, common.TopUpStatusSuccess).
		Joins(`LEFT JOIN (
			SELECT user_id, count(*) AS token_count
			FROM tokens
			WHERE deleted_at IS NULL
			GROUP BY user_id
		) token_stats ON token_stats.user_id = u.id`)
	if keyword != "" {
		like := "%" + keyword + "%"
		if _, err := strconv.Atoi(keyword); err == nil {
			query = query.Where("u.id = ? OR u.username LIKE ? OR u.email LIKE ?", keyword, like, like)
		} else {
			query = query.Where("u.username LIKE ? OR u.email LIKE ?", like, like)
		}
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	var rows []riskUserRow
	if err := query.Order("coalesce(topup.topup_paid_amount,0) desc, coalesce(topup.topup_count,0) desc, u.id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Scan(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := fillRiskUserLogMetrics(rows, since); err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range rows {
		rows[i].SignalCount, rows[i].Severity = scoreRiskUser(rows[i])
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

func MarkRiskEventViewed(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("event_id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid risk event id"))
		return
	}
	adminId := c.GetInt("id")
	adminName := c.GetString("username")
	var event model.RiskEvent
	if err := model.DB.Where("id = ?", id).First(&event).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if event.Status == model.RiskEventStatusOpen {
		if err := model.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&model.RiskEvent{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status":      model.RiskEventStatusViewed,
				"reviewed_at": common.GetTimestamp(),
				"reviewed_by": adminId,
			}).Error; err != nil {
				return err
			}
			return createRiskActionWithDB(tx, event, model.RiskActionViewed, adminId, adminName, "查看风险事件", "", "", c)
		}); err != nil {
			common.ApiError(c, err)
			return
		}
	} else if err := createRiskAction(event, model.RiskActionViewed, adminId, adminName, "查看风险事件", "", "", c); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": id})
}

func ResolveRiskEvent(c *gin.Context) {
	updateRiskEventStatus(c, model.RiskEventStatusResolved, model.RiskActionResolved)
}

func IgnoreRiskEvent(c *gin.Context) {
	updateRiskEventStatus(c, model.RiskEventStatusIgnored, model.RiskActionIgnored)
}

func BanRiskUser(c *gin.Context) {
	setRiskUserStatus(c, common.UserStatusDisabled, model.RiskActionBanUser)
}

func UnbanRiskUser(c *gin.Context) {
	setRiskUserStatus(c, common.UserStatusEnabled, model.RiskActionUnbanUser)
}

func DisableRiskToken(c *gin.Context) {
	var req riskActionRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	tokenID, err := strconv.Atoi(c.Param("token_id"))
	if err != nil || tokenID <= 0 {
		tokenID = req.TokenID
	}
	if tokenID <= 0 {
		common.ApiError(c, errors.New("invalid token id"))
		return
	}
	token, err := model.GetTokenById(tokenID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var owner model.User
	if err := model.DB.Where("id = ?", token.UserId).First(&owner).Error; err == nil {
		if !canManageTargetRole(c.GetInt("role"), owner.Role) {
			common.ApiError(c, errors.New("no permission to manage this user's token"))
			return
		}
	}
	event := eventForAction(req.EventID)
	if event.Id == 0 {
		event = model.RiskEvent{
			TargetType: model.RiskTargetToken,
			TargetId:   strconv.Itoa(tokenID),
			TokenId:    tokenID,
			UserId:     token.UserId,
		}
	}
	actionEvent := event
	actionEvent.TargetType = model.RiskTargetToken
	actionEvent.TargetId = strconv.Itoa(tokenID)
	actionEvent.TokenId = tokenID
	actionEvent.UserId = token.UserId
	reason := normalizeRiskReason(req.Reason, "风控中心禁用 Token")
	oldStatus := token.Status
	newStatus := common.TokenStatusDisabled
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Token{}).Where("id = ?", tokenID).Update("status", newStatus).Error; err != nil {
			return err
		}
		return createRiskActionWithDB(tx, actionEvent, model.RiskActionDisableToken, c.GetInt("id"), c.GetString("username"), reason, strconv.Itoa(oldStatus), strconv.Itoa(newStatus), c)
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	token.Status = newStatus
	if err := model.InvalidateUserTokensCache(token.UserId); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", token.UserId, err.Error()))
	}
	common.ApiSuccess(c, gin.H{"token_id": tokenID, "status": newStatus})
}

func CreateRiskWhitelist(c *gin.Context) {
	var req riskActionRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	targetType := strings.TrimSpace(req.TargetType)
	targetID := strings.TrimSpace(req.TargetID)
	if targetType == "" || targetID == "" {
		common.ApiError(c, errors.New("target_type and target_id are required"))
		return
	}
	adminId := c.GetInt("id")
	adminName := c.GetString("username")
	whitelist := model.RiskWhitelist{
		TargetType:     targetType,
		TargetId:       targetID,
		Reason:         strings.TrimSpace(req.Reason),
		OperatorUserId: adminId,
		OperatorName:   adminName,
		ExpiresAt:      req.ExpiresAt,
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		var existing model.RiskWhitelist
		err := tx.Where("target_type = ? AND target_id = ?", targetType, targetID).First(&existing).Error
		if err == nil {
			if err := tx.Model(&model.RiskWhitelist{}).Where("id = ?", existing.Id).Updates(map[string]interface{}{
				"reason":           whitelist.Reason,
				"operator_user_id": adminId,
				"operator_name":    adminName,
				"expires_at":       whitelist.ExpiresAt,
			}).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", existing.Id).First(&whitelist).Error; err != nil {
				return err
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Create(&whitelist).Error; err != nil {
				return err
			}
		} else {
			return err
		}
		event := eventForActionWithDB(tx, req.EventID)
		if event.Id == 0 {
			event = model.RiskEvent{TargetType: targetType, TargetId: targetID}
		}
		return createRiskActionWithDB(tx, event, model.RiskActionWhitelist, adminId, adminName, normalizeRiskReason(req.Reason, "加入风控白名单"), "", targetType+":"+targetID, c)
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, whitelist)
}

func DeleteRiskWhitelist(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid whitelist id"))
		return
	}
	var whitelist model.RiskWhitelist
	if err := model.DB.Where("id = ?", id).First(&whitelist).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	event := model.RiskEvent{TargetType: whitelist.TargetType, TargetId: whitelist.TargetId}
	switch whitelist.TargetType {
	case model.RiskTargetIP:
		event.Ip = whitelist.TargetId
	case model.RiskTargetUser, model.RiskTargetReferral:
		if targetID, err := strconv.Atoi(whitelist.TargetId); err == nil {
			event.UserId = targetID
		}
	case model.RiskTargetToken:
		if targetID, err := strconv.Atoi(whitelist.TargetId); err == nil {
			event.TokenId = targetID
		}
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&model.RiskWhitelist{}, id).Error; err != nil {
			return err
		}
		return createRiskActionWithDB(tx, event, model.RiskActionRemoveWhitelist, c.GetInt("id"), c.GetString("username"), "移除风控白名单", whitelist.TargetType+":"+whitelist.TargetId, "", c)
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": id})
}

func GetRiskActions(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	query := model.DB.Model(&model.RiskAction{})
	if action := strings.TrimSpace(c.Query("action")); action != "" && action != "all" {
		query = query.Where("action = ?", action)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var rows []model.RiskAction
	if err := query.Order("id desc").
		Limit(pageInfo.GetPageSize()).
		Offset(pageInfo.GetStartIdx()).
		Find(&rows).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(rows)
	common.ApiSuccess(c, pageInfo)
}

func updateRiskEventStatus(c *gin.Context, status string, action string) {
	id, err := strconv.Atoi(c.Param("event_id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid risk event id"))
		return
	}
	var req riskActionRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	var event model.RiskEvent
	if err := model.DB.Where("id = ?", id).First(&event).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	adminId := c.GetInt("id")
	adminName := c.GetString("username")
	now := common.GetTimestamp()
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.RiskEvent{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status":       status,
			"resolved_at":  now,
			"resolved_by":  adminId,
			"resolve_note": strings.TrimSpace(req.Reason),
		}).Error; err != nil {
			return err
		}
		return createRiskActionWithDB(tx, event, action, adminId, adminName, normalizeRiskReason(req.Reason, "更新风险事件状态"), event.Status, status, c)
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"id": id, "status": status})
}

func setRiskUserStatus(c *gin.Context, status int, action string) {
	var req riskActionRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	userID, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userID <= 0 {
		userID = req.UserID
	}
	if userID <= 0 {
		common.ApiError(c, errors.New("invalid user id"))
		return
	}
	var user model.User
	if err := model.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiError(c, errors.New("no permission to manage this user"))
		return
	}
	if user.Role == common.RoleRootUser && status == common.UserStatusDisabled {
		common.ApiError(c, errors.New("root user cannot be disabled"))
		return
	}
	oldStatus := user.Status
	event := eventForAction(req.EventID)
	if event.Id == 0 {
		event = model.RiskEvent{
			TargetType: model.RiskTargetUser,
			TargetId:   strconv.Itoa(userID),
			UserId:     userID,
			Username:   user.Username,
		}
	}
	actionEvent := event
	actionEvent.TargetType = model.RiskTargetUser
	actionEvent.TargetId = strconv.Itoa(userID)
	actionEvent.UserId = userID
	actionEvent.Username = user.Username
	reason := normalizeRiskReason(req.Reason, "风控中心更新用户状态")
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.User{}).Where("id = ?", userID).Update("status", status).Error; err != nil {
			return err
		}
		return createRiskActionWithDB(tx, actionEvent, action, c.GetInt("id"), c.GetString("username"), reason, strconv.Itoa(oldStatus), strconv.Itoa(status), c)
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.InvalidateUserCache(userID); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate user cache for user %d: %s", userID, err.Error()))
	}
	if err := model.InvalidateUserTokensCache(userID); err != nil {
		common.SysLog(fmt.Sprintf("failed to invalidate tokens cache for user %d: %s", userID, err.Error()))
	}
	common.ApiSuccess(c, gin.H{"user_id": userID, "status": status})
}

func createRiskAction(event model.RiskEvent, action string, adminId int, adminName string, reason string, oldValue string, newValue string, c *gin.Context) error {
	return createRiskActionWithDB(model.DB, event, action, adminId, adminName, reason, oldValue, newValue, c)
}

func createRiskActionWithDB(db *gorm.DB, event model.RiskEvent, action string, adminId int, adminName string, reason string, oldValue string, newValue string, c *gin.Context) error {
	targetType := event.TargetType
	targetID := event.TargetId
	if targetType == "" {
		targetType = model.RiskTargetRule
	}
	if targetID == "" {
		targetID = event.EventKey
	}
	record := model.RiskAction{
		EventId:        event.Id,
		Action:         action,
		TargetType:     targetType,
		TargetId:       targetID,
		UserId:         event.UserId,
		TokenId:        event.TokenId,
		Ip:             event.Ip,
		OperatorUserId: adminId,
		OperatorName:   adminName,
		Reason:         reason,
		OldValue:       oldValue,
		NewValue:       newValue,
		Evidence:       event.Evidence,
		ClientIP:       c.ClientIP(),
		UserAgent:      c.GetHeader("User-Agent"),
	}
	return db.Create(&record).Error
}

func eventForAction(eventID int) model.RiskEvent {
	return eventForActionWithDB(model.DB, eventID)
}

func eventForActionWithDB(db *gorm.DB, eventID int) model.RiskEvent {
	if eventID <= 0 {
		return model.RiskEvent{}
	}
	var event model.RiskEvent
	if err := db.Where("id = ?", eventID).First(&event).Error; err != nil {
		return model.RiskEvent{}
	}
	return event
}

func normalizeRiskReason(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func scanAndPersistRiskEvents(windowHours int) ([]model.RiskEvent, error) {
	since := common.GetTimestamp() - int64(windowHours*3600)
	inputs, err := buildRiskEventInputs(since, windowHours)
	if err != nil {
		return nil, err
	}
	events := make([]model.RiskEvent, 0, len(inputs))
	for _, input := range inputs {
		whitelisted, err := isRiskWhitelisted(input.TargetType, input.TargetId)
		if err != nil {
			return nil, err
		}
		if whitelisted {
			continue
		}
		event, err := model.UpsertRiskEvent(input)
		if err != nil {
			return nil, err
		}
		events = append(events, *event)
	}
	return events, nil
}

func buildRiskEventInputs(since int64, windowHours int) ([]model.RiskEventUpsert, error) {
	inputs := make([]model.RiskEventUpsert, 0)

	var sharedIPs []riskSignal
	if err := model.LOG_DB.Table("logs").
		Select("ip, count(distinct user_id) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND ip <> '' AND user_id > 0", since).
		Group("ip").
		Having("count(distinct user_id) >= ?", 5).
		Order("count desc").
		Limit(40).
		Scan(&sharedIPs).Error; err != nil {
		return nil, err
	}
	for _, item := range sharedIPs {
		inputs = append(inputs, model.RiskEventUpsert{
			EventKey:    "shared_ip:" + item.IP,
			Type:        "shared_ip",
			TargetType:  model.RiskTargetIP,
			TargetId:    item.IP,
			Ip:          item.IP,
			Severity:    model.RiskSeverityWarning,
			Title:       "同 IP 多账号",
			Summary:     fmt.Sprintf("同一 IP 在 %d 小时内关联 %d 个账号", windowHours, item.Count),
			HitCount:    item.Count,
			WindowHours: windowHours,
			FirstSeenAt: item.FirstSeenAt,
			LastSeenAt:  item.LastSeenAt,
			Evidence: map[string]interface{}{
				"ip":            item.IP,
				"user_count":    item.Count,
				"window_hours":  windowHours,
				"first_seen_at": item.FirstSeenAt,
				"last_seen_at":  item.LastSeenAt,
			},
		})
	}

	var highErrors []riskSignal
	if err := model.LOG_DB.Table("logs").
		Select("user_id, username, count(*) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND type = ? AND user_id > 0", since, model.LogTypeError).
		Group("user_id, username").
		Having("count(*) >= ?", 20).
		Order("count desc").
		Limit(40).
		Scan(&highErrors).Error; err != nil {
		return nil, err
	}
	for _, item := range highErrors {
		inputs = append(inputs, model.RiskEventUpsert{
			EventKey:    fmt.Sprintf("high_error_count:%d", item.UserID),
			Type:        "high_error_count",
			TargetType:  model.RiskTargetUser,
			TargetId:    strconv.Itoa(item.UserID),
			UserId:      item.UserID,
			Username:    item.Username,
			Severity:    model.RiskSeverityWarning,
			Title:       "错误日志过多",
			Summary:     fmt.Sprintf("用户在 %d 小时内产生 %d 条错误日志", windowHours, item.Count),
			HitCount:    item.Count,
			WindowHours: windowHours,
			FirstSeenAt: item.FirstSeenAt,
			LastSeenAt:  item.LastSeenAt,
			Evidence: map[string]interface{}{
				"user_id":      item.UserID,
				"username":     item.Username,
				"error_count":  item.Count,
				"window_hours": windowHours,
			},
		})
	}

	var highTopups []riskSignal
	if err := model.DB.Table("top_ups AS t").
		Select("t.user_id, u.username, count(*) AS count, coalesce(sum(t.paid_amount), 0) AS amount, min(t.create_time) AS first_seen_at, max(t.create_time) AS last_seen_at").
		Joins("LEFT JOIN users u ON u.id = t.user_id").
		Where("t.create_time >= ? AND t.status = ?", since, common.TopUpStatusSuccess).
		Group("t.user_id, u.username").
		Having("count(*) >= ? OR coalesce(sum(t.paid_amount), 0) >= ?", 5, 1000).
		Order("amount desc").
		Limit(40).
		Scan(&highTopups).Error; err != nil {
		return nil, err
	}
	for _, item := range highTopups {
		inputs = append(inputs, model.RiskEventUpsert{
			EventKey:    fmt.Sprintf("high_topup_activity:%d", item.UserID),
			Type:        "high_topup_activity",
			TargetType:  model.RiskTargetUser,
			TargetId:    strconv.Itoa(item.UserID),
			UserId:      item.UserID,
			Username:    item.Username,
			Severity:    model.RiskSeverityInfo,
			Title:       "充值异常",
			Summary:     fmt.Sprintf("用户在 %d 小时内成功充值 %d 笔，实付合计 %.2f", windowHours, item.Count, item.Amount),
			HitCount:    item.Count,
			Amount:      item.Amount,
			WindowHours: windowHours,
			FirstSeenAt: item.FirstSeenAt,
			LastSeenAt:  item.LastSeenAt,
			Evidence: map[string]interface{}{
				"user_id":      item.UserID,
				"username":     item.Username,
				"topup_count":  item.Count,
				"paid_amount":  item.Amount,
				"window_hours": windowHours,
			},
		})
	}

	var consumeCandidates []riskSignal
	if err := model.LOG_DB.Table("logs").
		Select("user_id, coalesce(sum(quota), 0) AS amount, count(*) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND type = ? AND user_id > 0", since, model.LogTypeConsume).
		Group("user_id").
		Having("coalesce(sum(quota), 0) >= ?", 1000000).
		Order("amount desc").
		Limit(80).
		Scan(&consumeCandidates).Error; err != nil {
		return nil, err
	}
	if len(consumeCandidates) > 0 {
		ids := make([]int, 0, len(consumeCandidates))
		for _, item := range consumeCandidates {
			ids = append(ids, item.UserID)
		}
		var newUsers []struct {
			ID        int
			Username  string
			CreatedAt int64
		}
		if err := model.DB.Model(&model.User{}).
			Select("id, username, created_at").
			Where("id IN ? AND created_at >= ?", ids, since).
			Scan(&newUsers).Error; err != nil {
			return nil, err
		}
		usernames := make(map[int]string, len(newUsers))
		for _, user := range newUsers {
			usernames[user.ID] = user.Username
		}
		for _, item := range consumeCandidates {
			username, ok := usernames[item.UserID]
			if !ok {
				continue
			}
			inputs = append(inputs, model.RiskEventUpsert{
				EventKey:    fmt.Sprintf("new_user_high_consume:%d", item.UserID),
				Type:        "new_user_high_consume",
				TargetType:  model.RiskTargetUser,
				TargetId:    strconv.Itoa(item.UserID),
				UserId:      item.UserID,
				Username:    username,
				Severity:    model.RiskSeverityWarning,
				Title:       "新号高消耗",
				Summary:     fmt.Sprintf("新注册账号在 %d 小时内消耗 %.0f 额度", windowHours, item.Amount),
				HitCount:    item.Count,
				Amount:      item.Amount,
				WindowHours: windowHours,
				FirstSeenAt: item.FirstSeenAt,
				LastSeenAt:  item.LastSeenAt,
				Evidence: map[string]interface{}{
					"user_id":       item.UserID,
					"username":      username,
					"consume_quota": item.Amount,
					"request_count": item.Count,
					"window_hours":  windowHours,
				},
			})
		}
	}

	tokenEvents, err := buildTokenRotationEvents(since, windowHours)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, tokenEvents...)

	referralEvents, err := buildReferralRiskEvents(since, windowHours)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, referralEvents...)

	paymentEvents, err := buildPaymentRiskEvents(since, windowHours)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, paymentEvents...)

	return inputs, nil
}

func collectRiskSignals(since int64, windowHours int) ([]riskSignal, error) {
	inputs, err := buildRiskEventInputs(since, windowHours)
	if err != nil {
		return nil, err
	}
	signals := make([]riskSignal, 0, len(inputs))
	for _, item := range inputs {
		whitelisted, err := isRiskWhitelisted(item.TargetType, item.TargetId)
		if err != nil {
			return nil, err
		}
		if whitelisted {
			continue
		}
		signals = append(signals, riskSignal{
			Type:        item.Type,
			Severity:    item.Severity,
			EventKey:    item.EventKey,
			TargetType:  item.TargetType,
			TargetId:    item.TargetId,
			UserID:      item.UserId,
			Username:    item.Username,
			IP:          item.Ip,
			TokenID:     item.TokenId,
			TradeNo:     item.TradeNo,
			Count:       item.HitCount,
			Amount:      item.Amount,
			Message:     item.Summary,
			FirstSeenAt: item.FirstSeenAt,
			LastSeenAt:  item.LastSeenAt,
		})
	}
	return signals, nil
}

func buildTokenRotationEvents(since int64, windowHours int) ([]model.RiskEventUpsert, error) {
	var rows []riskTokenRow
	if err := model.LOG_DB.Table("logs AS l").
		Select(`
			l.token_id,
			coalesce(max(l.token_name), '') AS token_name,
			coalesce(max(l.user_id), 0) AS user_id,
			coalesce(max(l.username), '') AS username,
			count(*) AS request_count,
			coalesce(sum(case when l.type = ? then 1 else 0 end), 0) AS error_count,
			coalesce(sum(case when l.type = ? then l.quota else 0 end), 0) AS consume_quota,
			count(distinct nullif(l.ip, '')) AS unique_ip_count,
			max(l.created_at) AS last_seen_at
		`, model.LogTypeError, model.LogTypeConsume).
		Where("l.created_at >= ? AND l.token_id > 0", since).
		Group("l.token_id").
		Having("count(distinct nullif(l.ip, '')) >= ?", 5).
		Order("unique_ip_count desc, request_count desc").
		Limit(40).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]model.RiskEventUpsert, 0, len(rows))
	for _, item := range rows {
		events = append(events, model.RiskEventUpsert{
			EventKey:    fmt.Sprintf("token_rotation:%d", item.TokenID),
			Type:        "token_rotation",
			TargetType:  model.RiskTargetToken,
			TargetId:    strconv.Itoa(item.TokenID),
			UserId:      item.UserID,
			Username:    item.Username,
			TokenId:     item.TokenID,
			TokenName:   item.TokenName,
			Severity:    model.RiskSeverityWarning,
			Title:       "Token 多 IP 使用",
			Summary:     fmt.Sprintf("Token 在 %d 小时内使用 %d 个 IP", windowHours, item.UniqueIPCount),
			HitCount:    item.UniqueIPCount,
			Amount:      float64(item.ConsumeQuota),
			WindowHours: windowHours,
			FirstSeenAt: since,
			LastSeenAt:  item.LastSeenAt,
			Evidence: map[string]interface{}{
				"token_id":        item.TokenID,
				"token_name":      item.TokenName,
				"user_id":         item.UserID,
				"username":        item.Username,
				"unique_ip_count": item.UniqueIPCount,
				"request_count":   item.RequestCount,
				"consume_quota":   item.ConsumeQuota,
				"window_hours":    windowHours,
			},
		})
	}
	return events, nil
}

func buildReferralRiskEvents(since int64, windowHours int) ([]model.RiskEventUpsert, error) {
	rows, err := queryReferralRiskRows(since, 20)
	if err != nil {
		return nil, err
	}
	events := make([]model.RiskEventUpsert, 0, len(rows))
	for _, item := range rows {
		if item.Severity == "" || item.Severity == "normal" {
			continue
		}
		events = append(events, model.RiskEventUpsert{
			EventKey:       fmt.Sprintf("referral_anomaly:%d", item.InviterUserID),
			Type:           "referral_anomaly",
			TargetType:     model.RiskTargetReferral,
			TargetId:       strconv.Itoa(item.InviterUserID),
			UserId:         item.InviterUserID,
			Username:       item.InviterUsername,
			ReferralUserId: item.InviterUserID,
			Severity:       item.Severity,
			Title:          "推广异常",
			Summary:        item.Reason,
			HitCount:       item.InviteeCount,
			Amount:         item.CommissionAmount,
			WindowHours:    windowHours,
			FirstSeenAt:    since,
			LastSeenAt:     common.GetTimestamp(),
			Evidence: map[string]interface{}{
				"affiliate_id":      item.AffiliateID,
				"inviter_user_id":   item.InviterUserID,
				"invitee_count":     item.InviteeCount,
				"commission_count":  item.CommissionCount,
				"commission_amount": item.CommissionAmount,
				"withdrawal_count":  item.WithdrawalCount,
				"withdrawal_amount": item.WithdrawalAmount,
			},
		})
	}
	return events, nil
}

func buildPaymentRiskEvents(since int64, windowHours int) ([]model.RiskEventUpsert, error) {
	events := make([]model.RiskEventUpsert, 0)
	var topups []riskOrderRow
	if err := model.DB.Table("top_ups AS t").
		Select(`
			'topup' AS order_type,
			t.trade_no,
			t.user_id,
			u.username,
			t.status,
			t.paid_amount,
			t.paid_currency,
			t.payment_provider,
			t.payment_method,
			t.referral_commission_status,
			t.referral_commission_error,
			t.create_time AS created_at
		`).
		Joins("LEFT JOIN users u ON u.id = t.user_id").
		Joins("LEFT JOIN subscription_orders so ON so.trade_no = t.trade_no AND t.trade_no <> ''").
		Where("t.create_time >= ? AND t.status = ? AND so.id IS NULL AND (t.referral_commission_status = ? OR t.paid_amount <= 0)", since, common.TopUpStatusSuccess, model.ReferralCommissionJobStatusFailed).
		Order("t.create_time desc").
		Limit(40).
		Scan(&topups).Error; err != nil {
		return nil, err
	}
	for _, order := range topups {
		summary := "充值订单存在返佣或支付审计异常"
		if order.ReferralCommissionStatus == model.ReferralCommissionJobStatusFailed {
			summary = "充值订单返佣异常：" + order.ReferralCommissionError
		}
		events = append(events, orderRiskEvent(order, summary, windowHours))
	}

	var subs []riskOrderRow
	if err := model.DB.Table("subscription_orders AS s").
		Select(`
			'subscription' AS order_type,
			s.trade_no,
			s.user_id,
			u.username,
			s.status,
			s.paid_amount,
			s.paid_currency,
			s.payment_provider,
			s.payment_method,
			s.referral_commission_status,
			s.referral_commission_error,
			s.create_time AS created_at
		`).
		Joins("LEFT JOIN users u ON u.id = s.user_id").
		Where("s.create_time >= ? AND s.status = ? AND (s.referral_commission_status = ? OR s.paid_amount <= 0)", since, common.TopUpStatusSuccess, model.ReferralCommissionJobStatusFailed).
		Order("s.create_time desc").
		Limit(40).
		Scan(&subs).Error; err != nil {
		return nil, err
	}
	for _, order := range subs {
		summary := "订阅订单存在返佣或支付审计异常"
		if order.ReferralCommissionStatus == model.ReferralCommissionJobStatusFailed {
			summary = "订阅订单返佣异常：" + order.ReferralCommissionError
		}
		events = append(events, orderRiskEvent(order, summary, windowHours))
	}
	return events, nil
}

func orderRiskEvent(order riskOrderRow, summary string, windowHours int) model.RiskEventUpsert {
	return model.RiskEventUpsert{
		EventKey:    "payment_anomaly:" + order.OrderType + ":" + order.TradeNo,
		Type:        "payment_anomaly",
		TargetType:  model.RiskTargetOrder,
		TargetId:    order.TradeNo,
		UserId:      order.UserID,
		Username:    order.Username,
		OrderType:   order.OrderType,
		TradeNo:     order.TradeNo,
		Severity:    model.RiskSeverityWarning,
		Title:       "支付/返佣异常",
		Summary:     summary,
		HitCount:    1,
		Amount:      order.PaidAmount,
		WindowHours: windowHours,
		FirstSeenAt: order.CreatedAt,
		LastSeenAt:  order.CreatedAt,
		Evidence: map[string]interface{}{
			"order_type":                 order.OrderType,
			"trade_no":                   order.TradeNo,
			"user_id":                    order.UserID,
			"username":                   order.Username,
			"status":                     order.Status,
			"paid_amount":                order.PaidAmount,
			"paid_currency":              order.PaidCurrency,
			"payment_provider":           order.PaymentProvider,
			"payment_method":             order.PaymentMethod,
			"referral_commission_status": order.ReferralCommissionStatus,
			"referral_commission_error":  order.ReferralCommissionError,
		},
	}
}

func isRiskWhitelisted(targetType string, targetID string) (bool, error) {
	if targetType == "" || targetID == "" {
		return false, nil
	}
	now := common.GetTimestamp()
	var count int64
	if err := model.DB.Model(&model.RiskWhitelist{}).
		Where("target_type = ? AND target_id = ? AND (expires_at = 0 OR expires_at > ?)", targetType, targetID, now).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func riskUserIDsByIP(ip string, since int64) ([]int, error) {
	var rows []struct {
		UserID int
	}
	if err := model.LOG_DB.Table("logs").
		Select("distinct user_id").
		Where("created_at >= ? AND ip = ? AND user_id > 0", since, ip).
		Limit(200).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func riskUserIDsByTokenID(tokenID int, since int64) ([]int, error) {
	if tokenID <= 0 {
		return []int{}, nil
	}
	var rows []struct {
		UserID int
	}
	if err := model.LOG_DB.Table("logs").
		Select("distinct user_id").
		Where("created_at >= ? AND token_id = ? AND user_id > 0", since, tokenID).
		Limit(200).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func riskUserIDsByTradeNo(tradeNo string) ([]int, error) {
	if tradeNo == "" {
		return []int{}, nil
	}
	ids := make([]int, 0, 2)
	var topup model.TopUp
	if err := model.DB.Select("user_id").Where("trade_no = ?", tradeNo).First(&topup).Error; err == nil && topup.UserId > 0 {
		ids = append(ids, topup.UserId)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var sub model.SubscriptionOrder
	if err := model.DB.Select("user_id").Where("trade_no = ?", tradeNo).First(&sub).Error; err == nil && sub.UserId > 0 {
		ids = append(ids, sub.UserId)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return uniquePositiveInts(ids), nil
}

func riskUserIDsByStatus(status int) ([]int, error) {
	var rows []struct {
		UserID int
	}
	if err := model.DB.Table("users").
		Select("id AS user_id").
		Where("status = ?", status).
		Order("id desc").
		Limit(200).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func riskNewUserIDs(since int64) ([]int, error) {
	var rows []struct {
		UserID int
	}
	if err := model.DB.Table("users").
		Select("id AS user_id").
		Where("created_at >= ?", since).
		Order("id desc").
		Limit(200).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func riskUsersByIDs(userIDs []int, since int64) ([]riskUserRow, error) {
	userIDs = uniquePositiveInts(userIDs)
	if len(userIDs) == 0 {
		return []riskUserRow{}, nil
	}
	var rows []riskUserRow
	if err := model.DB.Table("users AS u").
		Select(`
			u.id AS user_id,
			u.username,
			u.email,
			u.status,
			u.role,
			`+commonGroupColSelect("u")+` AS `+commonGroupAliasSelect()+`,
			u.created_at,
			u.last_login_at,
			u.quota,
			u.used_quota,
			u.request_count,
			coalesce(topup.topup_count, 0) AS topup_count,
			coalesce(topup.topup_paid_amount, 0) AS topup_paid_amount,
			coalesce(token_stats.token_count, 0) AS token_count
		`).
		Joins(`LEFT JOIN (
			SELECT user_id, count(*) AS topup_count, coalesce(sum(paid_amount), 0) AS topup_paid_amount
			FROM top_ups
			WHERE create_time >= ? AND status = ?
			GROUP BY user_id
		) topup ON topup.user_id = u.id`, since, common.TopUpStatusSuccess).
		Joins(`LEFT JOIN (
			SELECT user_id, count(*) AS token_count
			FROM tokens
			WHERE deleted_at IS NULL
			GROUP BY user_id
		) token_stats ON token_stats.user_id = u.id`).
		Where("u.id IN ?", userIDs).
		Order("u.id desc").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	if err := fillRiskUserLogMetrics(rows, since); err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].SignalCount, rows[i].Severity = scoreRiskUser(rows[i])
	}
	return rows, nil
}

func fillRiskUserLogMetrics(rows []riskUserRow, since int64) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	var metrics []riskLogMetric
	if err := model.LOG_DB.Table("logs").
		Select(`
			user_id,
			coalesce(sum(case when type = ? then 1 else 0 end), 0) AS error_count,
			coalesce(sum(case when type = ? then 1 else 0 end), 0) AS consume_count,
			coalesce(sum(case when type = ? then quota else 0 end), 0) AS consume_quota,
			count(distinct nullif(ip, '')) AS unique_ip_count
		`, model.LogTypeError, model.LogTypeConsume, model.LogTypeConsume).
		Where("created_at >= ? AND user_id IN ?", since, ids).
		Group("user_id").
		Scan(&metrics).Error; err != nil {
		return err
	}
	byUser := make(map[int]riskLogMetric, len(metrics))
	for _, metric := range metrics {
		byUser[metric.UserID] = metric
	}
	for i := range rows {
		metric := byUser[rows[i].UserID]
		rows[i].ErrorCount = metric.ErrorCount
		rows[i].ConsumeCount = metric.ConsumeCount
		rows[i].ConsumeQuota = metric.ConsumeQuota
		rows[i].UniqueIPCount = metric.UniqueIPCount
	}
	return nil
}

func riskLogsForDetail(riskType string, ip string, tokenID int, userIDs []int, since int64) ([]riskLogRow, error) {
	query := model.LOG_DB.Table("logs").
		Select("id, user_id, username, type, content, quota, ip, token_id, token_name, model_name, created_at").
		Where("created_at >= ?", since)
	switch riskType {
	case "shared_ip", "ip_detail":
		if ip == "" {
			return []riskLogRow{}, nil
		}
		query = query.Where("ip = ?", ip)
	case "token_rotation", "token_detail":
		if tokenID <= 0 {
			return []riskLogRow{}, nil
		}
		query = query.Where("token_id = ?", tokenID)
	case "high_error_count", "errors":
		query = query.Where("type = ?", model.LogTypeError)
		if len(userIDs) > 0 {
			query = query.Where("user_id IN ?", userIDs)
		}
	case "new_user_high_consume", "consume":
		query = query.Where("type = ?", model.LogTypeConsume)
		if len(userIDs) > 0 {
			query = query.Where("user_id IN ?", userIDs)
		}
	default:
		if len(userIDs) > 0 {
			query = query.Where("user_id IN ?", userIDs)
		} else if ip != "" {
			query = query.Where("ip = ?", ip)
		} else {
			return []riskLogRow{}, nil
		}
	}

	var rows []riskLogRow
	if err := query.Order("id desc").Limit(120).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func riskOrdersByUserIDs(userIDs []int, since int64) ([]riskOrderRow, error) {
	userIDs = uniquePositiveInts(userIDs)
	if len(userIDs) == 0 {
		return []riskOrderRow{}, nil
	}
	rows := make([]riskOrderRow, 0, 80)
	var topups []riskOrderRow
	if err := model.DB.Table("top_ups AS t").
		Select(`
			'topup' AS order_type,
			t.trade_no,
			t.user_id,
			u.username,
			t.status,
			t.paid_amount,
			t.paid_currency,
			t.payment_provider,
			t.payment_method,
			t.referral_commission_status,
			t.referral_commission_error,
			t.create_time AS created_at
		`).
		Joins("LEFT JOIN users u ON u.id = t.user_id").
		Where("t.create_time >= ? AND t.user_id IN ?", since, userIDs).
		Order("t.create_time desc").
		Limit(80).
		Scan(&topups).Error; err != nil {
		return nil, err
	}
	rows = append(rows, topups...)

	var subscriptions []riskOrderRow
	if err := model.DB.Table("subscription_orders AS s").
		Select(`
			'subscription' AS order_type,
			s.trade_no,
			s.user_id,
			u.username,
			s.status,
			s.paid_amount,
			s.paid_currency,
			s.payment_provider,
			s.payment_method,
			s.referral_commission_status,
			s.referral_commission_error,
			s.create_time AS created_at
		`).
		Joins("LEFT JOIN users u ON u.id = s.user_id").
		Where("s.create_time >= ? AND s.user_id IN ?", since, userIDs).
		Order("s.create_time desc").
		Limit(80).
		Scan(&subscriptions).Error; err != nil {
		return nil, err
	}
	rows = append(rows, subscriptions...)
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].CreatedAt > rows[j].CreatedAt
	})
	if len(rows) > 80 {
		rows = rows[:80]
	}
	return rows, nil
}

func riskOrdersByTradeNo(tradeNo string) ([]riskOrderRow, error) {
	if tradeNo == "" {
		return []riskOrderRow{}, nil
	}
	rows := make([]riskOrderRow, 0, 2)
	var topups []riskOrderRow
	if err := model.DB.Table("top_ups AS t").
		Select(`
			'topup' AS order_type,
			t.trade_no,
			t.user_id,
			u.username,
			t.status,
			t.paid_amount,
			t.paid_currency,
			t.payment_provider,
			t.payment_method,
			t.referral_commission_status,
			t.referral_commission_error,
			t.create_time AS created_at
		`).
		Joins("LEFT JOIN users u ON u.id = t.user_id").
		Where("t.trade_no = ?", tradeNo).
		Scan(&topups).Error; err != nil {
		return nil, err
	}
	rows = append(rows, topups...)
	var subs []riskOrderRow
	if err := model.DB.Table("subscription_orders AS s").
		Select(`
			'subscription' AS order_type,
			s.trade_no,
			s.user_id,
			u.username,
			s.status,
			s.paid_amount,
			s.paid_currency,
			s.payment_provider,
			s.payment_method,
			s.referral_commission_status,
			s.referral_commission_error,
			s.create_time AS created_at
		`).
		Joins("LEFT JOIN users u ON u.id = s.user_id").
		Where("s.trade_no = ?", tradeNo).
		Scan(&subs).Error; err != nil {
		return nil, err
	}
	rows = append(rows, subs...)
	return rows, nil
}

func uniqueRiskOrders(rows []riskOrderRow) []riskOrderRow {
	seen := make(map[string]struct{}, len(rows))
	result := make([]riskOrderRow, 0, len(rows))
	for _, row := range rows {
		key := row.OrderType + ":" + row.TradeNo
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, row)
	}
	return result
}

func riskTokensForDetail(userIDs []int, tokenID int, since int64) ([]riskTokenRow, error) {
	query := model.LOG_DB.Table("logs AS l").
		Select(`
			l.token_id,
			coalesce(max(l.token_name), '') AS token_name,
			coalesce(max(l.user_id), 0) AS user_id,
			coalesce(max(l.username), '') AS username,
			count(*) AS request_count,
			coalesce(sum(case when l.type = ? then 1 else 0 end), 0) AS error_count,
			coalesce(sum(case when l.type = ? then l.quota else 0 end), 0) AS consume_quota,
			count(distinct nullif(l.ip, '')) AS unique_ip_count,
			max(l.created_at) AS last_seen_at
		`, model.LogTypeError, model.LogTypeConsume).
		Where("l.created_at >= ? AND l.token_id > 0", since).
		Group("l.token_id")
	if tokenID > 0 {
		query = query.Where("l.token_id = ?", tokenID)
	} else if len(userIDs) > 0 {
		query = query.Where("l.user_id IN ?", userIDs)
	} else {
		return []riskTokenRow{}, nil
	}
	var rows []riskTokenRow
	if err := query.Order("unique_ip_count desc, request_count desc").Limit(80).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 0 {
		tokenIDs := make([]int, 0, len(rows))
		for _, row := range rows {
			tokenIDs = append(tokenIDs, row.TokenID)
		}
		statusByID, err := riskTokenStatusesByID(tokenIDs)
		if err != nil {
			return nil, err
		}
		for i := range rows {
			if status, ok := statusByID[rows[i].TokenID]; ok {
				rows[i].Status = status
			}
		}
	} else if tokenID > 0 {
		token, err := model.GetTokenById(tokenID)
		if err == nil {
			username, _ := model.GetUsernameById(token.UserId, false)
			rows = append(rows, riskTokenRow{
				TokenID:   token.Id,
				TokenName: token.Name,
				UserID:    token.UserId,
				Username:  username,
				Status:    token.Status,
			})
		}
	}
	return rows, nil
}

func riskTokenStatusesByID(tokenIDs []int) (map[int]int, error) {
	tokenIDs = uniquePositiveInts(tokenIDs)
	if len(tokenIDs) == 0 {
		return map[int]int{}, nil
	}
	var rows []struct {
		ID     int
		Status int
	}
	if err := model.DB.Model(&model.Token{}).
		Unscoped().
		Select("id, status").
		Where("id IN ?", tokenIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	statusByID := make(map[int]int, len(rows))
	for _, row := range rows {
		statusByID[row.ID] = row.Status
	}
	return statusByID, nil
}

func riskIPsForDetail(ip string, userIDs []int, since int64) ([]riskIPRow, error) {
	query := model.LOG_DB.Table("logs").
		Select(`
			ip,
			count(distinct user_id) AS user_count,
			count(distinct token_id) AS token_count,
			count(*) AS request_count,
			coalesce(sum(case when type = ? then 1 else 0 end), 0) AS error_count,
			coalesce(sum(case when type = ? then quota else 0 end), 0) AS consume_quota,
			min(created_at) AS first_seen_at,
			max(created_at) AS last_seen_at
		`, model.LogTypeError, model.LogTypeConsume).
		Where("created_at >= ? AND ip <> ''", since).
		Group("ip")
	if ip != "" {
		query = query.Where("ip = ?", ip)
	} else if len(userIDs) > 0 {
		query = query.Where("user_id IN ?", userIDs)
	} else {
		return []riskIPRow{}, nil
	}
	var rows []riskIPRow
	if err := query.Order("user_count desc, request_count desc").Limit(80).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].RequestCount > 0 {
			rows[i].FailureRate = float64(rows[i].ErrorCount) / float64(rows[i].RequestCount)
		}
		var whitelist model.RiskWhitelist
		if err := model.DB.Where("target_type = ? AND target_id = ? AND (expires_at = 0 OR expires_at > ?)", model.RiskWhitelistIP, rows[i].IP, common.GetTimestamp()).First(&whitelist).Error; err == nil {
			rows[i].Whitelisted = true
			rows[i].WhitelistNote = whitelist.Reason
		}
	}
	return rows, nil
}

func riskReferralsForDetail(userIDs []int, since int64) ([]riskReferralRow, error) {
	if len(userIDs) == 0 {
		return queryReferralRiskRows(since, 20)
	}
	rows, err := queryReferralRiskRows(since, 80)
	if err != nil {
		return nil, err
	}
	allowed := make(map[int]struct{}, len(userIDs))
	for _, id := range userIDs {
		allowed[id] = struct{}{}
	}
	result := make([]riskReferralRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := allowed[row.InviterUserID]; ok {
			result = append(result, row)
		}
	}
	return result, nil
}

func queryReferralRiskRows(since int64, limit int) ([]riskReferralRow, error) {
	var rows []riskReferralRow
	if err := model.DB.Table("referral_affiliates AS a").
		Select(`
			a.id AS affiliate_id,
			a.user_id AS inviter_user_id,
			u.username AS inviter_username,
			coalesce(binding.invitee_count, 0) AS invitee_count,
			coalesce(comm.commission_count, 0) AS commission_count,
			coalesce(comm.commission_amount, 0) AS commission_amount,
			coalesce(withdrawal.withdrawal_count, 0) AS withdrawal_count,
			coalesce(withdrawal.withdrawal_amount, 0) AS withdrawal_amount
		`).
		Joins("LEFT JOIN users u ON u.id = a.user_id").
		Joins(`LEFT JOIN (
			SELECT inviter_user_id, count(*) AS invitee_count
			FROM referral_bindings
			WHERE created_at >= ?
			GROUP BY inviter_user_id
		) binding ON binding.inviter_user_id = a.user_id`, since).
		Joins(`LEFT JOIN (
			SELECT affiliate_user_id, count(*) AS commission_count, coalesce(sum(commission_amount), 0) AS commission_amount
			FROM referral_commissions
			WHERE created_at >= ?
			GROUP BY affiliate_user_id
		) comm ON comm.affiliate_user_id = a.user_id`, since).
		Joins(`LEFT JOIN (
			SELECT user_id, count(*) AS withdrawal_count, coalesce(sum(amount), 0) AS withdrawal_amount
			FROM referral_withdrawals
			WHERE created_at >= ?
			GROUP BY user_id
		) withdrawal ON withdrawal.user_id = a.user_id`, since).
		Order("invitee_count desc, commission_amount desc").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		switch {
		case rows[i].InviteeCount >= 20 && rows[i].CommissionAmount > 0:
			rows[i].Severity = model.RiskSeverityWarning
			rows[i].Reason = "短时间拉新和佣金增长较高"
		case rows[i].InviteeCount >= 10:
			rows[i].Severity = model.RiskSeverityInfo
			rows[i].Reason = "短时间拉新数量较高"
		case rows[i].WithdrawalCount >= 3:
			rows[i].Severity = model.RiskSeverityInfo
			rows[i].Reason = "短时间提现申请较多"
		default:
			rows[i].Severity = "normal"
			rows[i].Reason = "未命中异常阈值"
		}
	}
	return rows, nil
}

func riskActionsForDetail(eventID int, userID int, tokenID int, ip string) ([]model.RiskAction, error) {
	query := model.DB.Model(&model.RiskAction{})
	if eventID > 0 {
		query = query.Where("event_id = ?", eventID)
	} else if userID > 0 {
		query = query.Where("user_id = ?", userID)
	} else if tokenID > 0 {
		query = query.Where("token_id = ?", tokenID)
	} else if ip != "" {
		query = query.Where("ip = ? OR (target_type = ? AND target_id = ?)", ip, model.RiskTargetIP, ip)
	} else {
		return []model.RiskAction{}, nil
	}
	var rows []model.RiskAction
	if err := query.Order("id desc").Limit(50).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func riskWhitelistsForDetail(userID int, tokenID int, ip string, eventTargetType string, eventTargetID string) ([]model.RiskWhitelist, error) {
	conditions := make([]string, 0, 4)
	args := make([]interface{}, 0, 8)
	if userID > 0 {
		conditions = append(conditions, "(target_type = ? AND target_id = ?)")
		args = append(args, model.RiskWhitelistUser, strconv.Itoa(userID))
	}
	if tokenID > 0 {
		conditions = append(conditions, "(target_type = ? AND target_id = ?)")
		args = append(args, model.RiskWhitelistToken, strconv.Itoa(tokenID))
	}
	if ip != "" {
		conditions = append(conditions, "(target_type = ? AND target_id = ?)")
		args = append(args, model.RiskWhitelistIP, ip)
	}
	if eventTargetType != "" && eventTargetID != "" {
		conditions = append(conditions, "(target_type = ? AND target_id = ?)")
		args = append(args, eventTargetType, eventTargetID)
	}
	if len(conditions) == 0 {
		return []model.RiskWhitelist{}, nil
	}
	var rows []model.RiskWhitelist
	if err := model.DB.Where(strings.Join(conditions, " OR "), args...).Order("id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func parseRiskWindowHours(value string) int {
	window, _ := strconv.Atoi(value)
	switch {
	case window <= 0:
		return 24
	case window > 24*30:
		return 24 * 30
	default:
		return window
	}
}

func scoreRiskUser(row riskUserRow) (int, string) {
	score := 0
	if row.Status == common.UserStatusDisabled {
		score++
	}
	if row.ErrorCount >= 20 {
		score += 2
	}
	if row.UniqueIPCount >= 5 {
		score += 2
	}
	if row.TopupCount >= 5 || row.TopupPaidAmount >= 1000 {
		score++
	}
	if row.ConsumeQuota >= 1000000 {
		score++
	}
	if score >= 4 {
		return score, model.RiskSeverityHigh
	}
	if score > 0 {
		return score, model.RiskSeverityWarning
	}
	return 0, "normal"
}

func commonGroupColSelect(alias string) string {
	if common.UsingPostgreSQL {
		return alias + `."group"`
	}
	return alias + ".`group`"
}

func commonGroupAliasSelect() string {
	if common.UsingPostgreSQL {
		return `"group"`
	}
	return "`group`"
}
