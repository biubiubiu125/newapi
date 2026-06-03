package controller

import (
	"errors"
	"fmt"
	"io"
	"net"
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
	RegisterIP      string  `json:"register_ip,omitempty"`
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

type riskDetailTimeRange struct {
	Since int64
	Until int64
}

func applyRiskDetailUntil(column string, until int64) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if until <= 0 {
			return db
		}
		return db.Where(column+" <= ?", until)
	}
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

const (
	// 风控总览和手动扫描在管理页同步触发，候选结果需要有边界，避免一次请求写入或返回过多事件。
	riskScanCandidateLimit        = 40
	riskScanConsumeCandidateLimit = 80
	// Internal batch size for role filtering; this does not cap the final result set.
	riskFilterBatchSize = 100
)

var errRiskPermissionDenied = errors.New("risk permission denied")

type riskPermissionDeniedError struct {
	message string
}

func (err riskPermissionDeniedError) Error() string {
	if err.message != "" {
		return err.message
	}
	return errRiskPermissionDenied.Error()
}

func (err riskPermissionDeniedError) Is(target error) bool {
	return target == errRiskPermissionDenied
}

func newRiskPermissionDeniedError(message string) error {
	return riskPermissionDeniedError{message: message}
}

func GetRiskOverview(c *gin.Context) {
	windowHours := parseRiskWindowHours(c.Query("window_hours"))
	since := common.GetTimestamp() - int64(windowHours*3600)
	signals, err := collectRiskSignalsForContext(c, since, windowHours)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	userCountQuery := func() *gorm.DB {
		query := model.DB.Model(&model.User{})
		if role := c.GetInt("role"); role != common.RoleRootUser {
			query = query.Where("role < ?", role)
		}
		return query
	}

	var disabledUsers int64
	if err := userCountQuery().Where("status = ?", common.UserStatusDisabled).Count(&disabledUsers).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var newUsers int64
	if err := userCountQuery().Where("created_at >= ?", since).Count(&newUsers).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	openEvents, err := countManageableRiskEvents(c, model.DB.Model(&model.RiskEvent{}).Where("status IN ?", []string{model.RiskEventStatusOpen, model.RiskEventStatusViewed}))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	highEvents, err := countManageableRiskEvents(c, model.DB.Model(&model.RiskEvent{}).Where("status IN ? AND severity = ?", []string{model.RiskEventStatusOpen, model.RiskEventStatusViewed}, model.RiskSeverityHigh))
	if err != nil {
		common.ApiError(c, err)
		return
	}

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
	events, err := scanAndPersistRiskEvents(c, windowHours)
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
	var events []model.RiskEvent
	orderExpr := "case severity when 'high' then 0 when 'warning' then 1 else 2 end, last_seen_at desc, id desc"
	if c.GetInt("role") == common.RoleRootUser {
		var total int64
		if err := query.Count(&total).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		if err := query.Order(orderExpr).
			Limit(pageInfo.GetPageSize()).
			Offset(pageInfo.GetStartIdx()).
			Find(&events).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		pageInfo.SetTotal(int(total))
		pageInfo.SetItems(events)
		common.ApiSuccess(c, pageInfo)
		return
	}
	events, total, err := paginateManageableRiskEvents(c, query, orderExpr, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(total)
	pageInfo.SetItems(events)
	common.ApiSuccess(c, pageInfo)
}

func GetRiskDetail(c *gin.Context) {
	windowHours := parseRiskWindowHours(c.Query("window_hours"))
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
			if err := validateRiskEventManageable(c, item, "no permission to view this risk event"); err != nil {
				common.ApiError(c, err)
				return
			}
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
		} else {
			common.ApiError(c, err)
			return
		}
	}
	windowHours, timeRange := riskDetailQueryWindow(windowHours, event)

	userIDs := make([]int, 0, 16)
	if userID > 0 {
		userIDs = append(userIDs, userID)
	}
	switch riskType {
	case "shared_ip", "shared_log_ip", "shared_register_ip", "ip_detail":
		if ip != "" {
			var ids []int
			var err error
			if riskType == "shared_register_ip" {
				ids, err = riskUserIDsByRegisterIPRange(ip, timeRange)
			} else {
				ids, err = riskUserIDsByIPRange(ip, timeRange)
			}
			if err != nil {
				common.ApiError(c, err)
				return
			}
			userIDs = append(userIDs, ids...)
		}
	case "token_rotation", "token_detail":
		if tokenID > 0 {
			ids, err := riskUserIDsByTokenIDRange(tokenID, timeRange)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			userIDs = append(userIDs, ids...)
			ownerIDs, err := riskTokenOwnerUserIDs([]int{tokenID})
			if err != nil {
				common.ApiError(c, err)
				return
			}
			userIDs = append(userIDs, ownerIDs...)
		}
	case "order_detail":
		ids, err := riskUserIDsByTradeNo(tradeNo)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userIDs = append(userIDs, ids...)
	case "new_users":
		ids, err := riskNewUserIDsRange(c, timeRange)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userIDs = append(userIDs, ids...)
	case "disabled_users":
		ids, err := riskUserIDsByStatus(c, common.UserStatusDisabled)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		userIDs = append(userIDs, ids...)
	}
	userIDs = uniquePositiveInts(userIDs)
	if err := validateRiskUserIDsManageable(c, userIDs, "no permission to view this risk detail"); err != nil {
		common.ApiError(c, err)
		return
	}

	users, err := riskUsersByIDs(userIDs, timeRange)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	logs, err := riskLogsForDetail(riskType, ip, tokenID, userIDs, timeRange)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	orders, err := riskOrdersByUserIDs(userIDs, timeRange)
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
	tokens, err := riskTokensForDetail(userIDs, tokenID, timeRange)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateRiskTokensManageable(c, tokens, "no permission to view this token risk detail"); err != nil {
		common.ApiError(c, err)
		return
	}
	ips, err := riskIPsForDetail(c, riskType, ip, userIDs, timeRange)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	referrals, err := riskReferralsForDetail(userIDs, timeRange)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	actions, err := riskActionsForDetail(c, riskType, eventID, userID, tokenID, ip, tradeNo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	eventTargetType := ""
	eventTargetID := ""
	if event != nil {
		eventTargetType = event.TargetType
		eventTargetID = event.TargetId
	} else if riskType == "order_detail" && tradeNo != "" {
		eventTargetType = model.RiskTargetOrder
		eventTargetID = tradeNo
	}
	whitelists, err := riskWhitelistsForDetail(c, userID, tokenID, ip, eventTargetType, eventTargetID)
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
	myRole := c.GetInt("role")

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
			u.register_ip,
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
	if myRole != common.RoleRootUser {
		query = query.Where("u.role < ?", myRole)
	}
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
	if err := fillRiskUserLogMetrics(rows, riskDetailTimeRange{Since: since}); err != nil {
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
	if err := validateRiskEventManageable(c, event, "no permission to view this risk event"); err != nil {
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
	reason, err := requireRiskReason(req.Reason)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenById(tokenID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var owner model.User
	if err := model.DB.Where("id = ?", token.UserId).First(&owner).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if !canManageTargetRole(c.GetInt("role"), owner.Role) {
		common.ApiError(c, errors.New("no permission to manage this user's token"))
		return
	}
	event, err := eventForRiskActionRequest(c, req.EventID, "no permission to use this risk event")
	if err != nil {
		common.ApiError(c, err)
		return
	}
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
	reason, err := requireRiskReason(req.Reason)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateRiskWhitelistTargetForCreate(c, targetType, targetID); err != nil {
		common.ApiError(c, err)
		return
	}
	adminId := c.GetInt("id")
	adminName := c.GetString("username")
	whitelist := model.RiskWhitelist{
		TargetType:     targetType,
		TargetId:       targetID,
		Reason:         reason,
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
		event, err := eventForRiskActionRequestWithDB(c, tx, req.EventID, "no permission to use this risk event")
		if err != nil {
			return err
		}
		if event.Id == 0 {
			event = model.RiskEvent{TargetType: targetType, TargetId: targetID}
		}
		return createRiskActionWithDB(tx, event, model.RiskActionWhitelist, adminId, adminName, reason, "", targetType+":"+targetID, c)
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
	var req riskActionRequest
	if err := decodeOptionalRiskActionRequest(c, &req); err != nil {
		common.ApiError(c, err)
		return
	}
	reason, err := requireRiskReason(req.Reason)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var whitelist model.RiskWhitelist
	if err := model.DB.Where("id = ?", id).First(&whitelist).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := validateRiskWhitelistTargetForDelete(c, whitelist.TargetType, whitelist.TargetId); err != nil {
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
		return createRiskActionWithDB(tx, event, model.RiskActionRemoveWhitelist, c.GetInt("id"), c.GetString("username"), reason, whitelist.TargetType+":"+whitelist.TargetId, "", c)
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
	var rows []model.RiskAction
	if c.GetInt("role") == common.RoleRootUser {
		var total int64
		if err := query.Count(&total).Error; err != nil {
			common.ApiError(c, err)
			return
		}
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
		return
	}
	rows, total, err := paginateManageableRiskActions(c, query, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(total)
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
	reason, err := requireRiskReason(req.Reason)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if event.Status == model.RiskEventStatusResolved || event.Status == model.RiskEventStatusIgnored {
		common.ApiError(c, errors.New("risk event is already closed"))
		return
	}
	if err := validateRiskEventManageable(c, event, "no permission to update this risk event"); err != nil {
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
			"resolve_note": reason,
		}).Error; err != nil {
			return err
		}
		return createRiskActionWithDB(tx, event, action, adminId, adminName, reason, event.Status, status, c)
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
	reason, err := requireRiskReason(req.Reason)
	if err != nil {
		common.ApiError(c, err)
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
	event, err := eventForRiskActionRequest(c, req.EventID, "no permission to use this risk event")
	if err != nil {
		common.ApiError(c, err)
		return
	}
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
		ClientIP:       common.GetClientIP(c),
		UserAgent:      c.GetHeader("User-Agent"),
	}
	return db.Create(&record).Error
}

func validateRiskWhitelistTargetForCreate(c *gin.Context, targetType string, targetID string) error {
	switch targetType {
	case model.RiskWhitelistUser, model.RiskWhitelistReferral:
		userID, err := strconv.Atoi(targetID)
		if err != nil || userID <= 0 {
			return errors.New("invalid whitelist user target")
		}
		var user model.User
		if err := model.DB.Select("id", "role").Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}
		if !canManageTargetRole(c.GetInt("role"), user.Role) {
			return errors.New("no permission to whitelist this user")
		}
	case model.RiskWhitelistToken:
		tokenID, err := strconv.Atoi(targetID)
		if err != nil || tokenID <= 0 {
			return errors.New("invalid whitelist token target")
		}
		token, err := model.GetTokenById(tokenID)
		if err != nil {
			return err
		}
		var owner model.User
		if err := model.DB.Select("id", "role").Where("id = ?", token.UserId).First(&owner).Error; err != nil {
			return err
		}
		if !canManageTargetRole(c.GetInt("role"), owner.Role) {
			return errors.New("no permission to whitelist this user's token")
		}
	case model.RiskWhitelistIP:
		if c.GetInt("role") != common.RoleRootUser {
			return errors.New("only root user can manage ip whitelist")
		}
		if net.ParseIP(targetID) == nil {
			return errors.New("invalid whitelist ip target")
		}
	case model.RiskTargetOrder:
		userIDs, err := riskOrderTargetUserIDs(targetID)
		if err != nil {
			return err
		}
		if len(userIDs) == 0 {
			return errors.New("risk whitelist order target does not exist")
		}
		if err := validateRiskUserIDsManageable(c, userIDs, "no permission to whitelist this user's order"); err != nil {
			return err
		}
	default:
		return errors.New("invalid whitelist target type")
	}
	return nil
}

func validateRiskWhitelistTargetForDelete(c *gin.Context, targetType string, targetID string) error {
	switch targetType {
	case model.RiskWhitelistUser, model.RiskWhitelistReferral:
		userID, err := strconv.Atoi(targetID)
		if err != nil || userID <= 0 {
			return nil
		}
		var user model.User
		if err := model.DB.Select("id", "role").Where("id = ?", userID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if !canManageTargetRole(c.GetInt("role"), user.Role) {
			return errors.New("no permission to remove this user's whitelist")
		}
	case model.RiskWhitelistToken:
		tokenID, err := strconv.Atoi(targetID)
		if err != nil || tokenID <= 0 {
			return nil
		}
		token, err := model.GetTokenById(tokenID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		var owner model.User
		if err := model.DB.Select("id", "role").Where("id = ?", token.UserId).First(&owner).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if !canManageTargetRole(c.GetInt("role"), owner.Role) {
			return errors.New("no permission to remove this token's whitelist")
		}
	case model.RiskWhitelistIP:
		if c.GetInt("role") != common.RoleRootUser {
			return errors.New("only root user can manage ip whitelist")
		}
		if net.ParseIP(targetID) == nil {
			return nil
		}
	case model.RiskTargetOrder:
		userIDs, err := riskOrderTargetUserIDs(targetID)
		if err != nil {
			return err
		}
		if len(userIDs) == 0 {
			return nil
		}
		if err := validateRiskUserIDsManageable(c, userIDs, "no permission to remove this order's whitelist"); err != nil {
			return err
		}
	}
	return nil
}

func riskOrderTargetUserIDs(tradeNo string) ([]int, error) {
	tradeNo = strings.TrimSpace(tradeNo)
	if tradeNo == "" {
		return []int{}, nil
	}
	ids := make([]int, 0, 2)
	var topups []struct {
		UserID int
	}
	if err := model.DB.Model(&model.TopUp{}).Select("user_id").Where("trade_no = ?", tradeNo).Scan(&topups).Error; err != nil {
		return nil, err
	}
	for _, row := range topups {
		ids = append(ids, row.UserID)
	}
	var subs []struct {
		UserID int
	}
	if err := model.DB.Model(&model.SubscriptionOrder{}).Select("user_id").Where("trade_no = ?", tradeNo).Scan(&subs).Error; err != nil {
		return nil, err
	}
	for _, row := range subs {
		ids = append(ids, row.UserID)
	}
	return uniquePositiveInts(ids), nil
}

func validateRiskUserIDsManageable(c *gin.Context, userIDs []int, message string) error {
	userIDs = uniquePositiveInts(userIDs)
	if len(userIDs) == 0 {
		if c.GetInt("role") == common.RoleRootUser {
			return nil
		}
		return newRiskPermissionDeniedError(message)
	}
	var users []model.User
	if err := model.DB.Unscoped().Select("id", "role").Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return err
	}
	if len(users) != len(userIDs) {
		return errors.New("risk target user does not exist")
	}
	myRole := c.GetInt("role")
	for _, user := range users {
		if !canManageTargetRole(myRole, user.Role) {
			return newRiskPermissionDeniedError(message)
		}
	}
	return nil
}

func riskEventInputManageable(c *gin.Context, input model.RiskEventUpsert) (bool, error) {
	if c.GetInt("role") == common.RoleRootUser {
		return true, nil
	}
	event := model.RiskEvent{
		Type:           input.Type,
		TargetType:     input.TargetType,
		TargetId:       input.TargetId,
		UserId:         input.UserId,
		Ip:             input.Ip,
		TokenId:        input.TokenId,
		TradeNo:        input.TradeNo,
		ReferralUserId: input.ReferralUserId,
		WindowHours:    input.WindowHours,
		FirstSeenAt:    input.FirstSeenAt,
		LastSeenAt:     input.LastSeenAt,
	}
	err := validateRiskEventManageable(c, event, "")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, errRiskPermissionDenied) {
		return false, nil
	}
	return false, err
}

func countManageableRiskEvents(c *gin.Context, query *gorm.DB) (int64, error) {
	if c.GetInt("role") == common.RoleRootUser {
		var total int64
		if err := query.Count(&total).Error; err != nil {
			return 0, err
		}
		return total, nil
	}
	var events []model.RiskEvent
	if err := query.Find(&events).Error; err != nil {
		return 0, err
	}
	filtered, err := filterRiskEventsManageable(c, events)
	if err != nil {
		return 0, err
	}
	return int64(len(filtered)), nil
}

func filterRiskEventsManageable(c *gin.Context, events []model.RiskEvent) ([]model.RiskEvent, error) {
	if c.GetInt("role") == common.RoleRootUser {
		return events, nil
	}
	result := make([]model.RiskEvent, 0, len(events))
	for _, event := range events {
		if err := validateRiskEventManageable(c, event, ""); err != nil {
			if errors.Is(err, errRiskPermissionDenied) {
				continue
			}
			return nil, err
		}
		result = append(result, event)
	}
	return result, nil
}

func filterRiskActionsManageable(c *gin.Context, actions []model.RiskAction) ([]model.RiskAction, error) {
	if c.GetInt("role") == common.RoleRootUser {
		return actions, nil
	}
	result := make([]model.RiskAction, 0, len(actions))
	for _, action := range actions {
		userIDs, err := riskActionTargetUserIDs(c, action)
		if err != nil {
			return nil, err
		}
		if err := validateRiskUserIDsManageable(c, userIDs, ""); err != nil {
			if errors.Is(err, errRiskPermissionDenied) {
				continue
			}
			return nil, err
		}
		result = append(result, action)
	}
	return result, nil
}

func paginateManageableRiskEvents(c *gin.Context, query *gorm.DB, orderExpr string, pageInfo *common.PageInfo) ([]model.RiskEvent, int, error) {
	start := pageInfo.GetStartIdx()
	pageSize := pageInfo.GetPageSize()
	offset := 0
	total := 0
	items := make([]model.RiskEvent, 0, pageSize)
	for {
		var batch []model.RiskEvent
		if err := query.Session(&gorm.Session{}).Order(orderExpr).Limit(riskFilterBatchSize).Offset(offset).Find(&batch).Error; err != nil {
			return nil, 0, err
		}
		if len(batch) == 0 {
			break
		}
		filtered, err := filterRiskEventsManageable(c, batch)
		if err != nil {
			return nil, 0, err
		}
		for _, item := range filtered {
			if total >= start && len(items) < pageSize {
				items = append(items, item)
			}
			total++
		}
		if len(batch) < riskFilterBatchSize {
			break
		}
		offset += len(batch)
	}
	return items, total, nil
}

func paginateManageableRiskActions(c *gin.Context, query *gorm.DB, pageInfo *common.PageInfo) ([]model.RiskAction, int, error) {
	start := pageInfo.GetStartIdx()
	pageSize := pageInfo.GetPageSize()
	offset := 0
	total := 0
	items := make([]model.RiskAction, 0, pageSize)
	for {
		var batch []model.RiskAction
		if err := query.Session(&gorm.Session{}).Order("id desc").Limit(riskFilterBatchSize).Offset(offset).Find(&batch).Error; err != nil {
			return nil, 0, err
		}
		if len(batch) == 0 {
			break
		}
		filtered, err := filterRiskActionsManageable(c, batch)
		if err != nil {
			return nil, 0, err
		}
		for _, item := range filtered {
			if total >= start && len(items) < pageSize {
				items = append(items, item)
			}
			total++
		}
		if len(batch) < riskFilterBatchSize {
			break
		}
		offset += len(batch)
	}
	return items, total, nil
}

func riskActionTargetUserIDs(c *gin.Context, action model.RiskAction) ([]int, error) {
	ids := make([]int, 0, 4)
	if action.EventId > 0 {
		var event model.RiskEvent
		err := model.DB.Where("id = ?", action.EventId).First(&event).Error
		if err == nil {
			eventIDs, err := riskEventTargetUserIDs(c, event)
			if err != nil {
				return nil, err
			}
			ids = append(ids, eventIDs...)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if action.UserId > 0 {
		ids = append(ids, action.UserId)
	}
	switch action.TargetType {
	case model.RiskTargetUser, model.RiskTargetReferral:
		if targetID, err := strconv.Atoi(action.TargetId); err == nil && targetID > 0 {
			ids = append(ids, targetID)
		}
	case model.RiskTargetToken:
		if targetID, err := strconv.Atoi(action.TargetId); err == nil && targetID > 0 {
			ownerIDs, err := riskTokenOwnerUserIDs([]int{targetID})
			if err != nil {
				return nil, err
			}
			ids = append(ids, ownerIDs...)
		}
	case model.RiskTargetOrder:
		orderIDs, err := riskOrderTargetUserIDs(action.TargetId)
		if err != nil {
			return nil, err
		}
		ids = append(ids, orderIDs...)
	case model.RiskTargetIP:
		ipIDs, err := riskUserIDsByIP(action.TargetId, 0)
		if err != nil {
			return nil, err
		}
		ids = append(ids, ipIDs...)
	}
	if action.TokenId > 0 {
		ownerIDs, err := riskTokenOwnerUserIDs([]int{action.TokenId})
		if err != nil {
			return nil, err
		}
		ids = append(ids, ownerIDs...)
	}
	if action.Ip != "" {
		ipIDs, err := riskUserIDsByIP(action.Ip, 0)
		if err != nil {
			return nil, err
		}
		ids = append(ids, ipIDs...)
	}
	return uniquePositiveInts(ids), nil
}

func paginateRiskEvents(events []model.RiskEvent, pageInfo *common.PageInfo) []model.RiskEvent {
	start := pageInfo.GetStartIdx()
	if start >= len(events) {
		return []model.RiskEvent{}
	}
	end := pageInfo.GetEndIdx()
	if end > len(events) {
		end = len(events)
	}
	return events[start:end]
}

func paginateRiskActions(actions []model.RiskAction, pageInfo *common.PageInfo) []model.RiskAction {
	start := pageInfo.GetStartIdx()
	if start >= len(actions) {
		return []model.RiskAction{}
	}
	end := pageInfo.GetEndIdx()
	if end > len(actions) {
		end = len(actions)
	}
	return actions[start:end]
}

func validateRiskTokensManageable(c *gin.Context, tokens []riskTokenRow, message string) error {
	if len(tokens) == 0 {
		return nil
	}
	userIDs := make([]int, 0, len(tokens))
	tokenIDs := make([]int, 0, len(tokens))
	for _, token := range tokens {
		if token.UserID > 0 {
			userIDs = append(userIDs, token.UserID)
		}
		if token.TokenID > 0 {
			tokenIDs = append(tokenIDs, token.TokenID)
		}
	}
	ownerIDs, err := riskTokenOwnerUserIDs(tokenIDs)
	if err != nil {
		return err
	}
	userIDs = append(userIDs, ownerIDs...)
	return validateRiskUserIDsManageable(c, userIDs, message)
}

func validateRiskEventManageable(c *gin.Context, event model.RiskEvent, message string) error {
	userIDs, err := riskEventTargetUserIDs(c, event)
	if err != nil {
		return err
	}
	return validateRiskUserIDsManageable(c, userIDs, message)
}

func riskEventTargetUserIDs(c *gin.Context, event model.RiskEvent) ([]int, error) {
	ids := make([]int, 0, 4)
	if event.UserId > 0 {
		ids = append(ids, event.UserId)
	}
	if event.ReferralUserId > 0 {
		ids = append(ids, event.ReferralUserId)
	}
	switch event.TargetType {
	case model.RiskTargetUser, model.RiskTargetReferral:
		if targetID, err := strconv.Atoi(event.TargetId); err == nil && targetID > 0 {
			ids = append(ids, targetID)
		}
	case model.RiskTargetToken:
		if tokenID, err := strconv.Atoi(event.TargetId); err == nil && tokenID > 0 {
			ownerIDs, err := riskTokenOwnerUserIDs([]int{tokenID})
			if err != nil {
				return nil, err
			}
			ids = append(ids, ownerIDs...)
		}
	case model.RiskTargetOrder:
		orderIDs, err := riskOrderTargetUserIDs(event.TargetId)
		if err != nil {
			return nil, err
		}
		ids = append(ids, orderIDs...)
	case model.RiskTargetIP:
		ip := event.TargetId
		if ip == "" {
			ip = event.Ip
		}
		ipIDs, err := riskUserIDsByIPTargetRange(event.Type, ip, riskEventTimeRange(event))
		if err != nil {
			return nil, err
		}
		ids = append(ids, ipIDs...)
	}
	if event.TokenId > 0 {
		ownerIDs, err := riskTokenOwnerUserIDs([]int{event.TokenId})
		if err != nil {
			return nil, err
		}
		ids = append(ids, ownerIDs...)
	}
	if event.TradeNo != "" {
		orderIDs, err := riskOrderTargetUserIDs(event.TradeNo)
		if err != nil {
			return nil, err
		}
		ids = append(ids, orderIDs...)
	}
	return uniquePositiveInts(ids), nil
}

func riskTokenOwnerUserIDs(tokenIDs []int) ([]int, error) {
	tokenIDs = uniquePositiveInts(tokenIDs)
	if len(tokenIDs) == 0 {
		return []int{}, nil
	}
	var rows []struct {
		UserID int
	}
	if err := model.DB.Unscoped().Model(&model.Token{}).Select("user_id").Where("id IN ?", tokenIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return uniquePositiveInts(ids), nil
}

func eventForAction(eventID int) model.RiskEvent {
	return eventForActionWithDB(model.DB, eventID)
}

func eventForRiskActionRequest(c *gin.Context, eventID int, message string) (model.RiskEvent, error) {
	return eventForRiskActionRequestWithDB(c, model.DB, eventID, message)
}

func eventForRiskActionRequestWithDB(c *gin.Context, db *gorm.DB, eventID int, message string) (model.RiskEvent, error) {
	if eventID <= 0 {
		return model.RiskEvent{}, nil
	}
	var event model.RiskEvent
	if err := db.Where("id = ?", eventID).First(&event).Error; err != nil {
		return model.RiskEvent{}, err
	}
	if err := validateRiskEventManageable(c, event, message); err != nil {
		return model.RiskEvent{}, err
	}
	return event, nil
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

func requireRiskReason(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("reason is required")
	}
	return value, nil
}

func decodeOptionalRiskActionRequest(c *gin.Context, req *riskActionRequest) error {
	if c.Request.Body == nil {
		return nil
	}
	err := common.DecodeJson(c.Request.Body, req)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func scanAndPersistRiskEvents(c *gin.Context, windowHours int) ([]model.RiskEvent, error) {
	since := common.GetTimestamp() - int64(windowHours*3600)
	inputs, err := buildRiskEventInputs(since, windowHours)
	if err != nil {
		return nil, err
	}
	inputs, err = filterRiskEventInputsForContext(c, inputs)
	if err != nil {
		return nil, err
	}
	events := make([]model.RiskEvent, 0, len(inputs))
	for _, input := range inputs {
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

	registerIPEvents, err := buildSharedRegisterIPEvents(since, windowHours)
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, registerIPEvents...)

	var sharedIPs []riskSignal
	if err := model.LOG_DB.Table("logs").
		Select("ip, count(distinct user_id) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND ip <> '' AND user_id > 0", since).
		Group("ip").
		Having("count(distinct user_id) >= ?", 5).
		Order("count desc").
		Scan(&sharedIPs).Error; err != nil {
		return nil, err
	}
	for _, item := range sharedIPs {
		if !isRiskIPCandidate(item.IP) {
			continue
		}
		inputs = append(inputs, model.RiskEventUpsert{
			EventKey:    "shared_ip:" + item.IP,
			Type:        "shared_log_ip",
			TargetType:  model.RiskTargetIP,
			TargetId:    item.IP,
			Ip:          item.IP,
			Severity:    model.RiskSeverityWarning,
			Title:       "同访问 IP 多账号",
			Summary:     fmt.Sprintf("同一访问/API IP 在 %d 小时内关联 %d 个账号", windowHours, item.Count),
			HitCount:    item.Count,
			WindowHours: windowHours,
			FirstSeenAt: item.FirstSeenAt,
			LastSeenAt:  item.LastSeenAt,
			Evidence: map[string]interface{}{
				"ip":            item.IP,
				"ip_source":     "log",
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

func buildSharedRegisterIPEvents(since int64, windowHours int) ([]model.RiskEventUpsert, error) {
	var rows []riskSignal
	if err := model.DB.Table("users").
		Select("register_ip AS ip, count(*) AS count, min(created_at) AS first_seen_at, max(created_at) AS last_seen_at").
		Where("created_at >= ? AND register_ip <> '' AND deleted_at IS NULL", since).
		Group("register_ip").
		Having("count(*) >= ?", 5).
		Order("count desc").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]model.RiskEventUpsert, 0, len(rows))
	for _, item := range rows {
		if !isRiskIPCandidate(item.IP) {
			continue
		}
		events = append(events, model.RiskEventUpsert{
			EventKey:    "shared_register_ip:" + item.IP,
			Type:        "shared_register_ip",
			TargetType:  model.RiskTargetIP,
			TargetId:    item.IP,
			Ip:          item.IP,
			Severity:    model.RiskSeverityWarning,
			Title:       "同注册 IP 多账号",
			Summary:     fmt.Sprintf("同一注册 IP 在 %d 小时内关联 %d 个账号", windowHours, item.Count),
			HitCount:    item.Count,
			WindowHours: windowHours,
			FirstSeenAt: item.FirstSeenAt,
			LastSeenAt:  item.LastSeenAt,
			Evidence: map[string]interface{}{
				"ip":            item.IP,
				"ip_source":     "register",
				"user_count":    item.Count,
				"window_hours":  windowHours,
				"first_seen_at": item.FirstSeenAt,
				"last_seen_at":  item.LastSeenAt,
			},
		})
	}
	return events, nil
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

func collectRiskSignalsForContext(c *gin.Context, since int64, windowHours int) ([]riskSignal, error) {
	inputs, err := buildRiskEventInputs(since, windowHours)
	if err != nil {
		return nil, err
	}
	inputs, err = filterRiskEventInputsForContext(c, inputs)
	if err != nil {
		return nil, err
	}
	signals := make([]riskSignal, 0, len(inputs))
	for _, item := range inputs {
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

func filterRiskEventInputsForContext(c *gin.Context, inputs []model.RiskEventUpsert) ([]model.RiskEventUpsert, error) {
	filtered := make([]model.RiskEventUpsert, 0, len(inputs))
	counts := make(map[string]int)
	for _, input := range inputs {
		whitelisted, err := isRiskWhitelisted(input.TargetType, input.TargetId)
		if err != nil {
			return nil, err
		}
		if whitelisted {
			continue
		}
		manageable, err := riskEventInputManageable(c, input)
		if err != nil {
			return nil, err
		}
		if !manageable {
			continue
		}
		limit := riskInputLimitForType(input.Type)
		if limit > 0 && counts[input.Type] >= limit {
			continue
		}
		filtered = append(filtered, input)
		counts[input.Type]++
	}
	return filtered, nil
}

func riskInputLimitForType(riskType string) int {
	switch riskType {
	case "new_user_high_consume":
		return riskScanConsumeCandidateLimit
	case "shared_ip", "shared_log_ip", "shared_register_ip", "high_error_count", "high_topup_activity", "token_rotation", "payment_anomaly":
		return riskScanCandidateLimit
	default:
		return 0
	}
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
	rows, err := queryReferralRiskRows(since, 0)
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
	return riskUserIDsByIPRange(ip, riskDetailTimeRange{Since: since})
}

func riskUserIDsByIPRange(ip string, timeRange riskDetailTimeRange) ([]int, error) {
	var rows []struct {
		UserID int
	}
	query := model.LOG_DB.Table("logs").
		Select("distinct user_id").
		Where("ip = ? AND user_id > 0", ip)
	if timeRange.Since > 0 {
		query = query.Where("created_at >= ?", timeRange.Since)
	}
	if timeRange.Until > 0 {
		query = query.Where("created_at <= ?", timeRange.Until)
	}
	if err := query.
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func riskUserIDsByIPTarget(riskType string, ip string, since int64) ([]int, error) {
	return riskUserIDsByIPTargetRange(riskType, ip, riskDetailTimeRange{Since: since})
}

func riskUserIDsByIPTargetRange(riskType string, ip string, timeRange riskDetailTimeRange) ([]int, error) {
	if riskType == "shared_register_ip" {
		return riskUserIDsByRegisterIPRange(ip, timeRange)
	}
	return riskUserIDsByIPRange(ip, timeRange)
}

func riskUserIDsByRegisterIP(ip string, since int64) ([]int, error) {
	return riskUserIDsByRegisterIPRange(ip, riskDetailTimeRange{Since: since})
}

func riskUserIDsByRegisterIPRange(ip string, timeRange riskDetailTimeRange) ([]int, error) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return []int{}, nil
	}
	var rows []struct {
		UserID int
	}
	query := model.DB.Table("users").
		Select("id AS user_id").
		Where("register_ip = ? AND deleted_at IS NULL", ip)
	if timeRange.Since > 0 {
		query = query.Where("created_at >= ?", timeRange.Since)
	}
	if timeRange.Until > 0 {
		query = query.Where("created_at <= ?", timeRange.Until)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	return ids, nil
}

func isRiskIPCandidate(value string) bool {
	normalized := common.NormalizeIP(value)
	if normalized == "" {
		return false
	}
	ip := net.ParseIP(normalized)
	if ip == nil {
		return false
	}
	if common.IsPrivateIP(ip) || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	return !common.IsTrustedProxyIP(normalized)
}

func riskUserIDsByTokenID(tokenID int, since int64) ([]int, error) {
	return riskUserIDsByTokenIDRange(tokenID, riskDetailTimeRange{Since: since})
}

func riskUserIDsByTokenIDRange(tokenID int, timeRange riskDetailTimeRange) ([]int, error) {
	if tokenID <= 0 {
		return []int{}, nil
	}
	var rows []struct {
		UserID int
	}
	query := model.LOG_DB.Table("logs").
		Select("distinct user_id").
		Where("created_at >= ? AND token_id = ? AND user_id > 0", timeRange.Since, tokenID)
	if timeRange.Until > 0 {
		query = query.Where("created_at <= ?", timeRange.Until)
	}
	if err := query.Limit(200).Scan(&rows).Error; err != nil {
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

func riskUserIDsByStatus(c *gin.Context, status int) ([]int, error) {
	var rows []struct {
		UserID int
	}
	query := model.DB.Table("users").
		Select("id AS user_id").
		Where("status = ?", status)
	if c.GetInt("role") != common.RoleRootUser {
		query = query.Where("role < ?", c.GetInt("role"))
	}
	if err := query.
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

func riskNewUserIDs(c *gin.Context, since int64) ([]int, error) {
	return riskNewUserIDsRange(c, riskDetailTimeRange{Since: since})
}

func riskNewUserIDsRange(c *gin.Context, timeRange riskDetailTimeRange) ([]int, error) {
	var rows []struct {
		UserID int
	}
	query := model.DB.Table("users").
		Select("id AS user_id").
		Where("created_at >= ?", timeRange.Since)
	if timeRange.Until > 0 {
		query = query.Where("created_at <= ?", timeRange.Until)
	}
	if c.GetInt("role") != common.RoleRootUser {
		query = query.Where("role < ?", c.GetInt("role"))
	}
	if err := query.
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

func riskUsersByIDs(userIDs []int, timeRange riskDetailTimeRange) ([]riskUserRow, error) {
	userIDs = uniquePositiveInts(userIDs)
	if len(userIDs) == 0 {
		return []riskUserRow{}, nil
	}
	var rows []riskUserRow
	topupQuery := "create_time >= ? AND status = ?"
	topupArgs := []interface{}{timeRange.Since, common.TopUpStatusSuccess}
	if timeRange.Until > 0 {
		topupQuery += " AND create_time <= ?"
		topupArgs = append(topupArgs, timeRange.Until)
	}
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
			u.register_ip,
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
			WHERE `+topupQuery+`
			GROUP BY user_id
		) topup ON topup.user_id = u.id`, topupArgs...).
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
	if err := fillRiskUserLogMetrics(rows, timeRange); err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].SignalCount, rows[i].Severity = scoreRiskUser(rows[i])
	}
	return rows, nil
}

func fillRiskUserLogMetrics(rows []riskUserRow, timeRange riskDetailTimeRange) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	var metrics []riskLogMetric
	query := model.LOG_DB.Table("logs").
		Select(`
			user_id,
			coalesce(sum(case when type = ? then 1 else 0 end), 0) AS error_count,
			coalesce(sum(case when type = ? then 1 else 0 end), 0) AS consume_count,
			coalesce(sum(case when type = ? then quota else 0 end), 0) AS consume_quota,
			count(distinct nullif(ip, '')) AS unique_ip_count
		`, model.LogTypeError, model.LogTypeConsume, model.LogTypeConsume).
		Where("created_at >= ? AND user_id IN ?", timeRange.Since, ids)
	if timeRange.Until > 0 {
		query = query.Where("created_at <= ?", timeRange.Until)
	}
	if err := query.Group("user_id").Scan(&metrics).Error; err != nil {
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

func riskLogsForDetail(riskType string, ip string, tokenID int, userIDs []int, timeRange riskDetailTimeRange) ([]riskLogRow, error) {
	query := model.LOG_DB.Table("logs").
		Select("id, user_id, username, type, content, quota, ip, token_id, token_name, model_name, created_at").
		Where("created_at >= ?", timeRange.Since).
		Scopes(applyRiskDetailUntil("created_at", timeRange.Until))
	switch riskType {
	case "shared_ip", "shared_log_ip", "ip_detail":
		if ip == "" {
			return []riskLogRow{}, nil
		}
		query = query.Where("ip = ?", ip)
	case "shared_register_ip":
		if len(userIDs) == 0 {
			return []riskLogRow{}, nil
		}
		query = query.Where("user_id IN ?", userIDs)
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

func riskOrdersByUserIDs(userIDs []int, timeRange riskDetailTimeRange) ([]riskOrderRow, error) {
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
		Where("t.create_time >= ? AND t.user_id IN ?", timeRange.Since, userIDs).
		Scopes(applyRiskDetailUntil("t.create_time", timeRange.Until)).
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
		Where("s.create_time >= ? AND s.user_id IN ?", timeRange.Since, userIDs).
		Scopes(applyRiskDetailUntil("s.create_time", timeRange.Until)).
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

func riskTokensForDetail(userIDs []int, tokenID int, timeRange riskDetailTimeRange) ([]riskTokenRow, error) {
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
		Where("l.created_at >= ? AND l.token_id > 0", timeRange.Since).
		Scopes(applyRiskDetailUntil("l.created_at", timeRange.Until)).
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

func riskIPsForDetail(c *gin.Context, riskType string, ip string, userIDs []int, timeRange riskDetailTimeRange) ([]riskIPRow, error) {
	if riskType == "shared_register_ip" {
		return riskRegisterIPsForDetail(c, ip, userIDs, timeRange)
	}
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
		Where("created_at >= ? AND ip <> ''", timeRange.Since).
		Scopes(applyRiskDetailUntil("created_at", timeRange.Until)).
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
		if c.GetInt("role") == common.RoleRootUser {
			var whitelist model.RiskWhitelist
			if err := model.DB.Where("target_type = ? AND target_id = ? AND (expires_at = 0 OR expires_at > ?)", model.RiskWhitelistIP, rows[i].IP, common.GetTimestamp()).First(&whitelist).Error; err == nil {
				rows[i].Whitelisted = true
				rows[i].WhitelistNote = whitelist.Reason
			}
		}
	}
	return rows, nil
}

func riskRegisterIPsForDetail(c *gin.Context, ip string, userIDs []int, timeRange riskDetailTimeRange) ([]riskIPRow, error) {
	query := model.DB.Table("users").
		Select(`
			register_ip AS ip,
			count(*) AS user_count,
			0 AS token_count,
			0 AS request_count,
			0 AS error_count,
			0 AS consume_quota,
			min(created_at) AS first_seen_at,
			max(created_at) AS last_seen_at
		`).
		Where("created_at >= ? AND register_ip <> '' AND deleted_at IS NULL", timeRange.Since).
		Scopes(applyRiskDetailUntil("created_at", timeRange.Until)).
		Group("register_ip")
	if ip != "" {
		query = query.Where("register_ip = ?", ip)
	} else if len(userIDs) > 0 {
		query = query.Where("id IN ?", userIDs)
	} else {
		return []riskIPRow{}, nil
	}
	var rows []riskIPRow
	if err := query.Order("user_count desc").Limit(80).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		if c.GetInt("role") == common.RoleRootUser {
			var whitelist model.RiskWhitelist
			if err := model.DB.Where("target_type = ? AND target_id = ? AND (expires_at = 0 OR expires_at > ?)", model.RiskWhitelistIP, rows[i].IP, common.GetTimestamp()).First(&whitelist).Error; err == nil {
				rows[i].Whitelisted = true
				rows[i].WhitelistNote = whitelist.Reason
			}
		}
	}
	return rows, nil
}

func riskReferralsForDetail(userIDs []int, timeRange riskDetailTimeRange) ([]riskReferralRow, error) {
	userIDs = uniquePositiveInts(userIDs)
	if len(userIDs) == 0 {
		return []riskReferralRow{}, nil
	}
	rows, err := queryReferralRiskRowsForUsers(timeRange, userIDs)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func queryReferralRiskRows(since int64, limit int) ([]riskReferralRow, error) {
	return queryReferralRiskRowsWithScope(riskDetailTimeRange{Since: since}, limit, nil)
}

func queryReferralRiskRowsForUsers(timeRange riskDetailTimeRange, userIDs []int) ([]riskReferralRow, error) {
	return queryReferralRiskRowsWithScope(timeRange, 0, uniquePositiveInts(userIDs))
}

func queryReferralRiskRowsWithScope(timeRange riskDetailTimeRange, limit int, userIDs []int) ([]riskReferralRow, error) {
	var rows []riskReferralRow
	bindingWhere := "created_at >= ?"
	bindingArgs := []interface{}{timeRange.Since}
	commissionWhere := "created_at >= ?"
	commissionArgs := []interface{}{timeRange.Since}
	withdrawalWhere := "created_at >= ?"
	withdrawalArgs := []interface{}{timeRange.Since}
	if timeRange.Until > 0 {
		bindingWhere += " AND created_at <= ?"
		bindingArgs = append(bindingArgs, timeRange.Until)
		commissionWhere += " AND created_at <= ?"
		commissionArgs = append(commissionArgs, timeRange.Until)
		withdrawalWhere += " AND created_at <= ?"
		withdrawalArgs = append(withdrawalArgs, timeRange.Until)
	}
	query := model.DB.Table("referral_affiliates AS a").
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
			WHERE `+bindingWhere+`
			GROUP BY inviter_user_id
		) binding ON binding.inviter_user_id = a.user_id`, bindingArgs...).
		Joins(`LEFT JOIN (
			SELECT affiliate_user_id, count(*) AS commission_count, coalesce(sum(commission_amount), 0) AS commission_amount
			FROM referral_commissions
			WHERE `+commissionWhere+`
			GROUP BY affiliate_user_id
		) comm ON comm.affiliate_user_id = a.user_id`, commissionArgs...).
		Joins(`LEFT JOIN (
			SELECT user_id, count(*) AS withdrawal_count, coalesce(sum(amount), 0) AS withdrawal_amount
			FROM referral_withdrawals
			WHERE `+withdrawalWhere+`
			GROUP BY user_id
		) withdrawal ON withdrawal.user_id = a.user_id`, withdrawalArgs...).
		Order("invitee_count desc, commission_amount desc")
	if len(userIDs) > 0 {
		query = query.Where("a.user_id IN ?", userIDs)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Scan(&rows).Error; err != nil {
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

func riskActionsForDetail(c *gin.Context, riskType string, eventID int, userID int, tokenID int, ip string, tradeNo string) ([]model.RiskAction, error) {
	query := model.DB.Model(&model.RiskAction{})
	if eventID > 0 {
		query = query.Where("event_id = ?", eventID)
	} else if riskType == "order_detail" && tradeNo != "" {
		query = query.Where("target_type = ? AND target_id = ?", model.RiskTargetOrder, tradeNo)
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
	return filterRiskActionsManageable(c, rows)
}

func riskWhitelistsForDetail(c *gin.Context, userID int, tokenID int, ip string, eventTargetType string, eventTargetID string) ([]model.RiskWhitelist, error) {
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
	if ip != "" && c.GetInt("role") == common.RoleRootUser {
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
	now := common.GetTimestamp()
	if err := model.DB.Where("("+strings.Join(conditions, " OR ")+") AND (expires_at = 0 OR expires_at > ?)", append(args, now)...).Order("id desc").Find(&rows).Error; err != nil {
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

func riskEventTimeRange(event model.RiskEvent) riskDetailTimeRange {
	if event.FirstSeenAt <= 0 && event.LastSeenAt <= 0 {
		return riskDetailTimeRange{}
	}
	_, timeRange := riskDetailQueryWindow(parseRiskWindowHours(strconv.Itoa(event.WindowHours)), &event)
	return timeRange
}

func riskDetailQueryWindow(windowHours int, event *model.RiskEvent) (int, riskDetailTimeRange) {
	if event != nil {
		if event.WindowHours > 0 {
			windowHours = event.WindowHours
		}
		if event.FirstSeenAt > 0 {
			return windowHours, riskDetailTimeRange{Since: event.FirstSeenAt, Until: event.LastSeenAt}
		}
		if event.LastSeenAt > 0 {
			return windowHours, riskDetailTimeRange{
				Since: event.LastSeenAt - int64(windowHours*3600),
				Until: event.LastSeenAt,
			}
		}
	}
	return windowHours, riskDetailTimeRange{Since: common.GetTimestamp() - int64(windowHours*3600)}
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
