package model

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const UserNameMaxLength = 20
const RegisterUserNameMaxLength = UserNameMaxLength

var userSortColumns = map[string]string{
	"id":            "id",
	"username":      "username",
	"quota":         "quota",
	"group":         "group",
	"created_at":    "created_at",
	"last_login_at": "last_login_at",
}

var usernameInvalidCharacterRegex = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

type UserSortOptions struct {
	SortBy    string
	SortOrder string
}

func NewUserSortOptions(sortBy string, sortOrder string) UserSortOptions {
	normalizedSortBy := strings.ToLower(strings.TrimSpace(sortBy))
	normalizedSortOrder := strings.ToLower(strings.TrimSpace(sortOrder))
	if _, ok := userSortColumns[normalizedSortBy]; !ok {
		normalizedSortBy = "id"
		normalizedSortOrder = "desc"
	} else if normalizedSortOrder != "asc" {
		normalizedSortOrder = "desc"
	}

	return UserSortOptions{
		SortBy:    normalizedSortBy,
		SortOrder: normalizedSortOrder,
	}
}

func (options UserSortOptions) Apply(query *gorm.DB) *gorm.DB {
	columnName, ok := userSortColumns[options.SortBy]
	if !ok {
		columnName = "id"
	}
	q := query.Order(clause.OrderByColumn{
		Column: clause.Column{Name: columnName},
		Desc:   options.SortOrder != "asc",
	})
	if columnName != "id" {
		q = q.Order(clause.OrderByColumn{
			Column: clause.Column{Name: "id"},
			Desc:   true,
		})
	}
	return q
}

func resolveUserSortOptions(sortOptions []UserSortOptions) UserSortOptions {
	if len(sortOptions) == 0 {
		return NewUserSortOptions("", "")
	}
	return sortOptions[0]
}

// User if you add sensitive fields, don't forget to clean them in setupLogin function.
// Otherwise, the sensitive information will be saved on local storage in plain text!
type User struct {
	Id                      int                        `json:"id"`
	Username                string                     `json:"username" gorm:"unique;index" validate:"max=20"`
	Password                string                     `json:"password" gorm:"not null;" validate:"min=8,max=20"`
	OriginalPassword        string                     `json:"original_password" gorm:"-:all"` // this field is only for Password change verification, don't save it to database!
	DisplayName             string                     `json:"display_name" gorm:"index" validate:"max=20"`
	Role                    int                        `json:"role" gorm:"type:int;default:1"`   // admin, common
	Status                  int                        `json:"status" gorm:"type:int;default:1"` // enabled, disabled
	Email                   string                     `json:"email" gorm:"index" validate:"max=50"`
	EmailCanonical          *string                    `json:"-" gorm:"column:email_canonical;type:varchar(191)"`
	GitHubId                string                     `json:"github_id" gorm:"column:github_id;index"`
	DiscordId               string                     `json:"discord_id" gorm:"column:discord_id;index"`
	OidcId                  string                     `json:"oidc_id" gorm:"column:oidc_id;index"`
	WeChatId                string                     `json:"wechat_id" gorm:"column:wechat_id;index"`
	TelegramId              string                     `json:"telegram_id" gorm:"column:telegram_id;index"`
	VerificationCode        string                     `json:"verification_code" gorm:"-:all"`                         // this field is only for Email verification, don't save it to database!
	AccessToken             *string                    `json:"-" gorm:"type:char(32);column:access_token;uniqueIndex"` // this token is for system management
	Quota                   int                        `json:"quota" gorm:"type:int;default:0"`
	UsedQuota               int                        `json:"used_quota" gorm:"type:int;default:0;column:used_quota"` // used quota
	RequestCount            int                        `json:"request_count" gorm:"type:int;default:0;"`               // request number
	Group                   string                     `json:"group" gorm:"type:varchar(64);default:'default'"`
	ReferralInviterId       int                        `json:"referral_inviter_id,omitempty" gorm:"-"`
	ReferralInviterUsername string                     `json:"referral_inviter_username,omitempty" gorm:"-"`
	ActiveSubscriptionName  string                     `json:"active_subscription_name,omitempty" gorm:"-"`
	LastActiveAt            int64                      `json:"last_active_at" gorm:"-"`
	AffCode                 string                     `json:"aff_code" gorm:"type:varchar(32);column:aff_code;uniqueIndex"`
	AffCount                int                        `json:"aff_count" gorm:"type:int;default:0;column:aff_count"`
	AffQuota                int                        `json:"aff_quota" gorm:"type:int;default:0;column:aff_quota"`           // 邀请剩余额度
	AffHistoryQuota         int                        `json:"aff_history_quota" gorm:"type:int;default:0;column:aff_history"` // 邀请历史额度
	InviterId               int                        `json:"inviter_id" gorm:"type:int;column:inviter_id;index"`
	DeletedAt               gorm.DeletedAt             `gorm:"index"`
	LinuxDOId               string                     `json:"linux_do_id" gorm:"column:linux_do_id;index"`
	Setting                 string                     `json:"setting" gorm:"type:text;column:setting"`
	Remark                  string                     `json:"remark,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	StripeCustomer          string                     `json:"stripe_customer" gorm:"type:varchar(64);column:stripe_customer;index"`
	CreatedAt               int64                      `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	LastLoginAt             int64                      `json:"last_login_at" gorm:"default:0;column:last_login_at"`
	AuthVersion             int64                      `json:"-" gorm:"type:bigint;not null;default:1;column:auth_version"`
	AdminPermissions        map[string]map[string]bool `json:"admin_permissions,omitempty" gorm:"-:all"`
}

type UserLoginIdentifier struct {
	Id         int            `json:"id"`
	UserId     int            `json:"user_id" gorm:"index;not null"`
	Identifier string         `json:"identifier" gorm:"type:varchar(191);uniqueIndex:idx_user_login_identifiers_identifier;not null"`
	Kind       string         `json:"kind" gorm:"type:varchar(16);not null"`
	CreatedAt  int64          `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

func NormalizeUserEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func isValidUsernameChars(username string) bool {
	if username == "" {
		return false
	}
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func IsValidNewUserUsername(username string) bool {
	return isValidUsernameChars(username) && len([]rune(username)) <= RegisterUserNameMaxLength
}

func ValidateNewUserUsername(username string) error {
	if !isValidUsernameChars(username) {
		return ErrUserUsernameInvalid
	}
	if len([]rune(username)) > RegisterUserNameMaxLength {
		return ErrUserUsernameTooLong
	}
	return nil
}

func NormalizeNewUserUsernameCandidate(username string) string {
	username = usernameInvalidCharacterRegex.ReplaceAllString(strings.TrimSpace(username), "")
	runes := []rune(username)
	if len(runes) > RegisterUserNameMaxLength {
		return string(runes[:RegisterUserNameMaxLength])
	}
	return username
}

func GenerateNewUserUsername(prefix string) string {
	prefix = NormalizeNewUserUsernameCandidate(prefix)
	if prefix == "" {
		prefix = "user"
	}
	idPart := strconv.Itoa(GetMaxUserId()+1) + common.GetRandomString(6)
	maxPrefixLength := RegisterUserNameMaxLength - len([]rune(idPart))
	if maxPrefixLength <= 0 {
		runes := []rune(idPart)
		return string(runes[len(runes)-RegisterUserNameMaxLength:])
	}
	runes := []rune(prefix)
	if len(runes) > maxPrefixLength {
		prefix = string(runes[:maxPrefixLength])
	}
	return prefix + idPart
}

func SelectNewUserUsername(preferredUsername, fallbackPrefix string) string {
	preferredUsername = NormalizeNewUserUsernameCandidate(preferredUsername)
	if IsValidNewUserUsername(preferredUsername) {
		if exists, err := CheckUserExistOrDeleted(preferredUsername, ""); err == nil && !exists {
			return preferredUsername
		}
	}
	return GenerateNewUserUsername(fallbackPrefix)
}

func (user *User) normalizeEmailForPersistence() {
	email := NormalizeUserEmail(user.Email)
	user.Email = email
	if email == "" {
		user.EmailCanonical = nil
		return
	}
	user.EmailCanonical = &email
}

func (user *User) BeforeSave(_ *gorm.DB) error {
	user.normalizeEmailForPersistence()
	return nil
}

func (user *User) BeforeCreate(_ *gorm.DB) error {
	if user.AffCode == "" {
		user.AffCode = common.GetRandomString(4)
	}
	return nil
}

func (user *User) ToBaseUser() *UserBase {
	cache := &UserBase{
		Id:          user.Id,
		Group:       user.Group,
		Quota:       user.Quota,
		Status:      user.Status,
		Role:        user.Role,
		Username:    user.Username,
		Setting:     user.Setting,
		Email:       user.Email,
		AuthVersion: user.AuthVersion,
		CacheSchema: userCacheSchemaVersion,
	}
	return cache
}

func (user *User) GetAccessToken() string {
	if user.AccessToken == nil {
		return ""
	}
	return *user.AccessToken
}

func (user *User) SetAccessToken(token string) {
	user.AccessToken = &token
}

// UpdateUserAccessToken rotates a dashboard personal access token without
// writing a stale user snapshot back over concurrently updated fields.
func UpdateUserAccessToken(id int, token string) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	result := DB.Model(&User{}).Where("id = ?", id).Update("access_token", token)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (user *User) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := common.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

func (user *User) SetSetting(setting dto.UserSetting) {
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		common.SysLog("failed to marshal setting: " + err.Error())
		return
	}
	user.Setting = string(settingBytes)
}

func UpdateUserSetting(userId int, setting dto.UserSetting) error {
	if userId == 0 {
		return errors.New("id 为空！")
	}
	settingBytes, err := common.Marshal(setting)
	if err != nil {
		return err
	}
	settingValue := string(settingBytes)
	if err = DB.Model(&User{}).Where("id = ?", userId).Update("setting", settingValue).Error; err != nil {
		return err
	}
	return updateUserSettingCache(userId, settingValue)
}

// userBindColumns 允许通过 UpdateUserBindColumn 更新的第三方账号绑定列白名单。
// 列名只可能来自代码内部的 provider 实现，白名单是防御纵深，不依赖调用方自律。
var userBindColumns = map[string]bool{
	"github_id":   true,
	"discord_id":  true,
	"oidc_id":     true,
	"linux_do_id": true,
	"wechat_id":   true,
}

// UpdateUserBindColumn 第三方账号绑定字段的专用更新。
// 绑定操作必须只写绑定列：若改为“读取完整用户 → 改一个字段 → 整体更新”，
// 读快照期间并发发生的封禁、降权或分组变更会被旧快照覆盖恢复。
// 角色、状态、分组只允许通过各自带锁/CAS 的专用方法修改。
func UpdateUserBindColumn(userId int, column string, value string) error {
	if userId <= 0 {
		return errors.New("id 为空！")
	}
	if !userBindColumns[column] {
		return fmt.Errorf("invalid user bind column: %s", column)
	}
	return DB.Model(&User{}).Where("id = ?", userId).Update(column, value).Error
}

// 根据用户角色生成默认的边栏配置
func generateDefaultSidebarConfigForRole(userRole int) string {
	defaultConfig := map[string]interface{}{}

	// 聊天区域 - 所有用户都可以访问
	defaultConfig["chat"] = map[string]interface{}{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}

	// 控制台区域 - 所有用户都可以访问
	defaultConfig["console"] = map[string]interface{}{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
	}

	// 个人中心区域 - 所有用户都可以访问
	defaultConfig["personal"] = map[string]interface{}{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	// 管理员区域 - 根据角色决定
	if userRole == common.RoleAdminUser {
		// 管理员可以访问管理员区域，但不能访问系统设置
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    false, // 管理员不能访问系统设置
		}
	} else if userRole == common.RoleRootUser {
		// 超级管理员可以访问所有功能
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    true,
		}
	}
	// 普通用户不包含admin区域

	// 转换为JSON字符串
	configBytes, err := common.Marshal(defaultConfig)
	if err != nil {
		common.SysLog("生成默认边栏配置失败: " + err.Error())
		return ""
	}

	return string(configBytes)
}

func GenerateDefaultSidebarConfigForRole(userRole int) string {
	return generateDefaultSidebarConfigForRole(userRole)
}

// CheckUserExistOrDeleted check if user exist or deleted, if not exist, return false, nil, if deleted or exist, return true, nil
func CheckUserExistOrDeleted(username string, email string) (bool, error) {
	return IsLoginIdentifierTakenByOther(username, email, 0)
}

func NormalizeEmail(email string) string {
	return NormalizeUserEmail(email)
}

func emailQuery(tx *gorm.DB, email string) *gorm.DB {
	if tx == nil {
		tx = DB
	}
	email = NormalizeUserEmail(email)
	return tx.Unscoped().Model(&User{}).Where("email_canonical = ? OR LOWER(email) = ?", email, email)
}

func CountUsersByEmail(email string) (int64, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return 0, nil
	}
	var count int64
	err := emailQuery(DB, email).Count(&count).Error
	return count, err
}

func IsEmailAvailable(email string, excludeUserID int) (bool, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return true, nil
	}
	query := emailQuery(DB, email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count == 0, nil
}

func EnsureEmailAvailable(email string, excludeUserID int) error {
	available, err := IsEmailAvailable(email, excludeUserID)
	if err != nil {
		return err
	}
	if !available {
		return ErrEmailAlreadyTaken
	}
	return nil
}

// withNormalizedEmailLock serializes concurrent writers that target the same
// normalized email inside tx, so a "check then write" sequence cannot be raced
// by two transactions. It must be called inside an active transaction; the lock
// is scoped to that transaction and released on commit/rollback.
//
//   - PostgreSQL: transaction-level advisory lock keyed by the normalized email.
//   - MySQL (default REPEATABLE READ): a locking read that takes a next-key/gap
//     lock on the email index, blocking concurrent inserts of the same value.
//   - SQLite: no explicit lock; the single-writer model already serializes the
//     write, so a racing second write fails instead of duplicating.
//
// An empty email is allowed to repeat and needs no serialization.
func withNormalizedEmailLock(tx *gorm.DB, email string, fn func(tx *gorm.DB) error) error {
	email = NormalizeEmail(email)
	if email == "" {
		return fn(tx)
	}
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", email).Error; err != nil {
			return err
		}
	case common.UsingMainDatabase(common.DatabaseTypeMySQL):
		var ids []int
		if err := tx.Raw("SELECT id FROM users WHERE email_canonical = ? OR LOWER(email) = ? FOR UPDATE", email, email).Scan(&ids).Error; err != nil {
			return err
		}
	}
	return fn(tx)
}

func GetMaxUserId() int {
	var user User
	DB.Unscoped().Last(&user)
	return user.Id
}

func CountUsersAfterID(afterID int) (int64, int, error) {
	var summary struct {
		Count    int64 `gorm:"column:count"`
		LatestID int   `gorm:"column:latest_id"`
	}
	if err := DB.Unscoped().Model(&User{}).
		Select("count(*) AS count, coalesce(max(id), 0) AS latest_id").
		Where("id > ?", afterID).
		Scan(&summary).Error; err != nil {
		return 0, 0, err
	}
	if summary.Count == 0 {
		return 0, afterID, nil
	}
	return summary.Count, summary.LatestID, nil
}

func GetLatestUserID() (int, error) {
	var latestID int
	if err := DB.Unscoped().Model(&User{}).Select("coalesce(max(id), 0)").Scan(&latestID).Error; err != nil {
		return 0, err
	}
	return latestID, nil
}

func GetAllUsers(pageInfo *common.PageInfo, sortOptions ...UserSortOptions) (users []*User, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Get total count within transaction
	err = tx.Unscoped().Model(&User{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated users within same transaction
	order := resolveUserSortOptions(sortOptions)
	err = order.Apply(tx.Unscoped()).Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Omit("password", "access_token").Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	populateReferralInviters(users)
	populateActiveSubscriptionNames(users)
	populateUserActivity(users)
	return users, total, nil
}

func SearchUsers(keyword string, group string, role *int, status *int, startIdx int, num int, sortOptions ...UserSortOptions) ([]*User, int64, error) {
	var users []*User
	var total int64
	var err error

	// 开始事务
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// 构建基础查询
	query := tx.Unscoped().Model(&User{})

	// 构建搜索条件
	likeCondition := "username LIKE ? OR email LIKE ? OR display_name LIKE ?"
	likeArgs := []interface{}{"%" + keyword + "%", "%" + keyword + "%", "%" + keyword + "%"}

	// 尝试将关键字转换为整数ID
	keywordInt, err := strconv.Atoi(keyword)
	if err == nil {
		// 如果是数字，同时搜索ID和其他字段
		likeCondition = "id = ? OR " + likeCondition
		likeArgs = append([]interface{}{keywordInt}, likeArgs...)
	}

	if strings.TrimSpace(keyword) != "" {
		inviterUserIDs := tx.Table("referral_bindings AS rb").
			Select("rb.invitee_user_id").
			Joins("LEFT JOIN users inviter_users ON inviter_users.id = rb.inviter_user_id").
			Where("inviter_users.username LIKE ?", "%"+keyword+"%")
		likeCondition += " OR id IN (?)"
		likeArgs = append(likeArgs, inviterUserIDs)
	}
	query = query.Where("("+likeCondition+")", likeArgs...)
	if group != "" {
		query = query.Where(commonGroupCol+" = ?", group)
	}
	if role != nil {
		query = query.Where("role = ?", *role)
	}
	if status != nil {
		if *status == -1 {
			query = query.Where("deleted_at IS NOT NULL")
		} else {
			query = query.Where("deleted_at IS NULL").Where("status = ?", *status)
		}
	}

	// 获取总数
	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 获取分页数据
	order := resolveUserSortOptions(sortOptions)
	err = order.Apply(query.Omit("password", "access_token")).Limit(num).Offset(startIdx).Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// 提交事务
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	populateReferralInviters(users)
	populateActiveSubscriptionNames(users)
	populateUserActivity(users)
	return users, total, nil
}

func userEnrichmentIndex(users []*User) ([]int, map[int]*User) {
	userIDs := make([]int, 0, len(users))
	userByID := make(map[int]*User, len(users))
	for _, user := range users {
		if user == nil || user.Id == 0 {
			continue
		}
		userIDs = append(userIDs, user.Id)
		userByID[user.Id] = user
	}
	return userIDs, userByID
}

func populateUserActivity(users []*User) {
	userIDs, userByID := userEnrichmentIndex(users)
	if len(userIDs) == 0 || LOG_DB == nil {
		return
	}

	type activeRow struct {
		UserID       int
		LastActiveAt int64
	}
	var activeRows []activeRow
	if err := LOG_DB.Table("logs").
		Select("user_id, max(created_at) AS last_active_at").
		Where("user_id IN ? AND type = ?", userIDs, LogTypeConsume).
		Group("user_id").
		Scan(&activeRows).Error; err != nil {
		common.SysLog("failed to populate user last active time: " + err.Error())
		return
	}
	for _, row := range activeRows {
		if user := userByID[row.UserID]; user != nil {
			user.LastActiveAt = row.LastActiveAt
		}
	}
}

func populateReferralInviters(users []*User) {
	userIDs, userByID := userEnrichmentIndex(users)
	if len(userIDs) == 0 || DB == nil {
		return
	}

	type inviterRow struct {
		InviteeUserID   int
		InviterUserID   int
		InviterUsername string
	}
	var rows []inviterRow
	err := DB.Table("referral_bindings AS rb").
		Select("rb.invitee_user_id, rb.inviter_user_id, inviter_users.username AS inviter_username").
		Joins("LEFT JOIN users inviter_users ON inviter_users.id = rb.inviter_user_id").
		Where("rb.invitee_user_id IN ?", userIDs).
		Scan(&rows).Error
	if err != nil {
		common.SysLog("failed to populate referral inviters: " + err.Error())
		return
	}
	for _, row := range rows {
		user := userByID[row.InviteeUserID]
		if user == nil {
			continue
		}
		user.ReferralInviterId = row.InviterUserID
		user.ReferralInviterUsername = row.InviterUsername
	}
}

func populateActiveSubscriptionNames(users []*User) {
	userIDs, userByID := userEnrichmentIndex(users)
	if len(userIDs) == 0 || DB == nil {
		return
	}

	type subscriptionRow struct {
		UserID int
		Title  string
	}
	var rows []subscriptionRow
	now := common.GetTimestamp()
	err := DB.Table("user_subscriptions AS us").
		Select("us.user_id, sp.title").
		Joins("LEFT JOIN subscription_plans sp ON sp.id = us.plan_id").
		Where("us.user_id IN ? AND us.status = ? AND us.end_time > ?", userIDs, "active", now).
		Order("us.end_time desc, us.id desc").
		Scan(&rows).Error
	if err != nil {
		common.SysLog("failed to populate active subscriptions: " + err.Error())
		return
	}
	for _, row := range rows {
		user := userByID[row.UserID]
		if user == nil || user.ActiveSubscriptionName != "" {
			continue
		}
		user.ActiveSubscriptionName = strings.TrimSpace(row.Title)
	}
}

func GetUserById(id int, selectAll bool) (*User, error) {
	if id == 0 {
		return nil, errors.New("id is empty")
	}
	user := User{Id: id}
	var err error = nil
	if selectAll {
		err = DB.First(&user, "id = ?", id).Error
	} else {
		err = DB.Omit("password", "access_token").First(&user, "id = ?", id).Error
	}
	return &user, err
}

func GetUserByIdUnscoped(id int, selectAll bool) (*User, error) {
	if id == 0 {
		return nil, errors.New("id 为空！")
	}
	user := User{Id: id}
	query := DB.Unscoped()
	if !selectAll {
		query = query.Omit("password", "access_token")
	}
	err := query.First(&user, "id = ?", id).Error
	return &user, err
}

func GetUserIdByAffCode(affCode string) (int, error) {
	if affCode == "" {
		return 0, errors.New("affCode 为空！")
	}
	var user User
	err := DB.Select("id").First(&user, "aff_code = ?", affCode).Error
	return user.Id, err
}

func DeleteUserById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := User{Id: id}
	return user.Delete()
}

func HardDeleteUserById(id int) error {
	if id == 0 {
		return errors.New("id 为空！")
	}
	user := User{Id: id}
	return user.HardDelete()
}

func inviteUser(inviterId int) error {
	result := DB.Model(&User{}).Where("id = ?", inviterId).Updates(map[string]interface{}{
		"aff_count":   gorm.Expr("aff_count + ?", 1),
		"aff_quota":   gorm.Expr("aff_quota + ?", common.QuotaForInviter),
		"aff_history": gorm.Expr("aff_history + ?", common.QuotaForInviter),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (user *User) TransferAffQuotaToQuota(quota int) error {
	// 检查quota是否小于最小额度
	if float64(quota) < common.QuotaPerUnit {
		return fmt.Errorf("转移额度最小为%s！", logger.LogQuota(common.QuotaFromFloat(common.QuotaPerUnit)))
	}

	// 开始数据库事务
	tx := DB.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer tx.Rollback() // 确保在函数退出时事务能回滚

	// 加锁查询用户以确保数据一致性
	err := lockForUpdate(tx).First(user, user.Id).Error
	if err != nil {
		return err
	}

	// 再次检查用户的AffQuota是否足够
	if user.AffQuota < quota {
		return errors.New("邀请额度不足！")
	}

	// 更新用户额度
	user.AffQuota -= quota
	user.Quota += quota

	// 保存用户状态
	if err := tx.Save(user).Error; err != nil {
		return err
	}

	// 提交事务
	return tx.Commit().Error
}

func (user *User) prepareForInsert(tx *gorm.DB) error {
	user.normalizeEmailForPersistence()
	exists, err := isLoginIdentifierTakenByOtherWithTx(tx, user.Username, user.Email, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrUserLoginIdentifierTaken
	}
	if err := ensureEmailAvailableWithTx(tx, user.Email, 0); err != nil {
		return err
	}
	if user.Password == "" {
		return nil
	}
	user.Password, err = common.Password2Hash(user.Password)
	return err
}

// BindEmailToUser atomically checks email availability and assigns it to the
// user, serializing concurrent binds of the same email so two accounts cannot
// end up sharing one address. The email is normalized before check and store.
func BindEmailToUser(user *User, email string) error {
	email = NormalizeEmail(email)
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return withNormalizedEmailLock(tx, email, func(tx *gorm.DB) error {
			if err := ensureEmailAvailableWithTx(tx, email, user.Id); err != nil {
				return err
			}
			user.Email = email
			return user.UpdateWithTx(tx, false)
		})
	}); err != nil {
		return err
	}
	return updateUserCache(*user)
}

func ensureEmailAvailableWithTx(tx *gorm.DB, email string, excludeUserID int) error {
	email = NormalizeEmail(email)
	if email == "" {
		return nil
	}
	query := emailQuery(tx, email)
	if excludeUserID > 0 {
		query = query.Where("id <> ?", excludeUserID)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrEmailAlreadyTaken
	}
	return nil
}

func (user *User) Insert(inviterId int) error {
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
			if err := user.prepareForInsert(tx); err != nil {
				return err
			}
			user.Quota = common.QuotaForNewUser
			user.AffCode = common.GetRandomString(4)

			// 初始化用户设置，包括默认的边栏配置
			if user.Setting == "" {
				defaultSetting := dto.UserSetting{}
				// 这里暂时不设置SidebarModules，因为需要在用户创建后根据角色设置
				user.SetSetting(defaultSetting)
			}

			if err := tx.Create(user).Error; err != nil {
				return err
			}
			return syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email)
		})
	}); err != nil {
		return err
	}

	user.finishInsert(inviterId)
	return nil
}

func (user *User) finishInsert(inviterId int) {
	// 用户创建成功后，根据角色初始化边栏配置
	// 需要重新获取用户以确保有正确的ID和Role
	var createdUser User
	if err := DB.Where("username = ?", user.Username).First(&createdUser).Error; err == nil {
		// 生成基于角色的默认边栏配置
		defaultSidebarConfig := generateDefaultSidebarConfigForRole(createdUser.Role)
		if defaultSidebarConfig != "" {
			currentSetting := createdUser.GetSetting()
			currentSetting.SidebarModules = defaultSidebarConfig
			createdUser.SetSetting(currentSetting)
			createdUser.Update(false)
			common.SysLog(fmt.Sprintf("为新用户 %s (角色: %d) 初始化边栏配置", createdUser.Username, createdUser.Role))
		}
	}

	if common.QuotaForNewUser > 0 {
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(common.QuotaForNewUser)))
	}
	if inviterId != 0 && operation_setting.IsPaymentComplianceConfirmed() {
		if common.QuotaForInvitee > 0 {
			_ = IncreaseUserQuota(user.Id, common.QuotaForInvitee, true)
			RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("使用邀请码赠送 %s", logger.LogQuota(common.QuotaForInvitee)))
		}
		if common.QuotaForInviter > 0 {
			//_ = IncreaseUserQuota(inviterId, common.QuotaForInviter)
			RecordLog(inviterId, LogTypeSystem, fmt.Sprintf("邀请用户赠送 %s", logger.LogQuota(common.QuotaForInviter)))
			_ = inviteUser(inviterId)
		}
	}
}

func (user *User) FinishInsert(inviterId int) {
	user.finishInsert(inviterId)
}

// InsertWithTx inserts a new user within an existing transaction.
// This is used for OAuth registration where user creation and binding need to be atomic.
// Post-creation tasks (sidebar config, logs, inviter rewards) are handled after the transaction commits.
func (user *User) InsertWithTx(tx *gorm.DB, inviterId int) error {
	return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
		if err := user.prepareForInsert(tx); err != nil {
			return err
		}
		user.Quota = common.QuotaForNewUser
		user.AffCode = common.GetRandomString(4)

		// 初始化用户设置
		if user.Setting == "" {
			defaultSetting := dto.UserSetting{}
			user.SetSetting(defaultSetting)
		}

		if err := tx.Create(user).Error; err != nil {
			return err
		}
		return syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email)
	})
}

// FinalizeOAuthUserCreation performs post-transaction tasks for OAuth user creation.
// This should be called after the transaction commits successfully.
func (user *User) FinalizeOAuthUserCreation(inviterId int) {
	// 用户创建成功后，根据角色初始化边栏配置
	var createdUser User
	if err := DB.Where("id = ?", user.Id).First(&createdUser).Error; err == nil {
		defaultSidebarConfig := generateDefaultSidebarConfigForRole(createdUser.Role)
		if defaultSidebarConfig != "" {
			currentSetting := createdUser.GetSetting()
			currentSetting.SidebarModules = defaultSidebarConfig
			createdUser.SetSetting(currentSetting)
			createdUser.Update(false)
			common.SysLog(fmt.Sprintf("为新用户 %s (角色: %d) 初始化边栏配置", createdUser.Username, createdUser.Role))
		}
	}

	if common.QuotaForNewUser > 0 {
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(common.QuotaForNewUser)))
	}
	if inviterId != 0 && operation_setting.IsPaymentComplianceConfirmed() {
		if common.QuotaForInvitee > 0 {
			_ = IncreaseUserQuota(user.Id, common.QuotaForInvitee, true)
			RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("使用邀请码赠送 %s", logger.LogQuota(common.QuotaForInvitee)))
		}
		if common.QuotaForInviter > 0 {
			RecordLog(inviterId, LogTypeSystem, fmt.Sprintf("邀请用户赠送 %s", logger.LogQuota(common.QuotaForInviter)))
			_ = inviteUser(inviterId)
		}
	}
}

func (user *User) Update(updatePassword bool) error {
	return user.UpdateWithSessionRevocationReason(updatePassword, "user_security_changed")
}

func (user *User) UpdateWithSessionRevocationReason(updatePassword bool, revocationReason string) error {
	var previousAuthVersion int64
	if err := DB.Model(&User{}).Where("id = ?", user.Id).Select("auth_version").Find(&previousAuthVersion).Error; err != nil {
		return err
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return user.UpdateWithTx(tx, updatePassword)
	}); err != nil {
		return err
	}
	return FinalizeUserAuthChange(*user, previousAuthVersion, revocationReason)
}

func externalLoginBindingChanged(current User, newUser User) bool {
	return nonEmptyStringChanged(current.GitHubId, newUser.GitHubId) ||
		nonEmptyStringChanged(current.DiscordId, newUser.DiscordId) ||
		nonEmptyStringChanged(current.OidcId, newUser.OidcId) ||
		nonEmptyStringChanged(current.WeChatId, newUser.WeChatId) ||
		nonEmptyStringChanged(current.TelegramId, newUser.TelegramId) ||
		nonEmptyStringChanged(current.LinuxDOId, newUser.LinuxDOId)
}

func nonEmptyStringChanged(current string, next string) bool {
	return next != "" && current != next
}

func authSensitiveBindingType(bindingType string) bool {
	switch strings.TrimSpace(strings.ToLower(bindingType)) {
	case ExternalIdentityProviderGitHub,
		ExternalIdentityProviderDiscord,
		ExternalIdentityProviderOIDC,
		ExternalIdentityProviderWeChat,
		ExternalIdentityProviderTelegram,
		ExternalIdentityProviderLinuxDO:
		return true
	default:
		return false
	}
}

func getUserLoginBindingField(user *User, bindingType string) string {
	if user == nil {
		return ""
	}
	switch strings.TrimSpace(strings.ToLower(bindingType)) {
	case ExternalIdentityProviderGitHub:
		return user.GitHubId
	case ExternalIdentityProviderDiscord:
		return user.DiscordId
	case ExternalIdentityProviderOIDC:
		return user.OidcId
	case ExternalIdentityProviderWeChat:
		return user.WeChatId
	case ExternalIdentityProviderTelegram:
		return user.TelegramId
	case ExternalIdentityProviderLinuxDO:
		return user.LinuxDOId
	default:
		return ""
	}
}

func (user *User) UpdateWithTx(tx *gorm.DB, updatePassword bool) error {
	var err error
	current := User{}
	if err = tx.First(&current, user.Id).Error; err != nil {
		return err
	}
	newUser := *user
	if newUser.Username == "" {
		newUser.Username = current.Username
	}
	if newUser.Email == "" {
		newUser.Email = current.Email
	}
	newUser.normalizeEmailForPersistence()
	exists, err := isLoginIdentifierTakenByOtherWithTx(tx, newUser.Username, newUser.Email, user.Id)
	if err != nil {
		return err
	}
	if exists {
		return ErrUserLoginIdentifierTaken
	}
	if updatePassword {
		newUser.Password, err = common.Password2Hash(newUser.Password)
		if err != nil {
			return err
		}
	}
	// Updates(struct) ignores zero values. Match that behavior when deciding
	// whether this request actually changes authentication-sensitive state;
	// partial self-profile updates intentionally leave role/status/group empty.
	authChanged := (updatePassword && current.Password != newUser.Password) ||
		(newUser.Role != 0 && current.Role != newUser.Role) ||
		(newUser.Status != 0 && current.Status != newUser.Status) ||
		(newUser.Group != "" && current.Group != newUser.Group) ||
		current.Email != newUser.Email ||
		externalLoginBindingChanged(current, newUser)
	if authChanged {
		newUser.AuthVersion, err = IncrementUserAuthVersionWithTx(tx, user.Id)
		if err != nil {
			return err
		}
	}
	if err = tx.Model(&current).Omit(
		"access_token",
		"quota",
		"used_quota",
		"request_count",
		"aff_count",
		"aff_quota",
		"aff_history",
		"auth_version",
	).Updates(newUser).Error; err != nil {
		return err
	}
	if err = syncUserLoginIdentifiersWithTx(tx, user.Id, newUser.Username, newUser.Email); err != nil {
		return err
	}
	return tx.First(user, user.Id).Error
}

func (user *User) Edit(updatePassword bool) error {
	var previousAuthVersion int64
	if err := DB.Model(&User{}).Where("id = ?", user.Id).Select("auth_version").Find(&previousAuthVersion).Error; err != nil {
		return err
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return user.EditWithTx(tx, updatePassword)
	}); err != nil {
		return err
	}
	return FinalizeUserAuthChange(*user, previousAuthVersion, "user_security_changed")
}

func (user *User) EditWithTx(tx *gorm.DB, updatePassword bool) error {
	var err error
	current := User{}
	if err = tx.First(&current, user.Id).Error; err != nil {
		return err
	}
	newUser := *user
	if newUser.Username == "" {
		newUser.Username = current.Username
	}
	newUser.Email = current.Email
	newUser.normalizeEmailForPersistence()
	exists, err := isLoginIdentifierTakenByOtherWithTx(tx, newUser.Username, newUser.Email, user.Id)
	if err != nil {
		return err
	}
	if exists {
		return ErrUserLoginIdentifierTaken
	}
	if updatePassword {
		newUser.Password, err = common.Password2Hash(newUser.Password)
		if err != nil {
			return err
		}
	}
	updates := map[string]interface{}{
		"username":        newUser.Username,
		"display_name":    newUser.DisplayName,
		"email":           newUser.Email,
		"email_canonical": newUser.EmailCanonical,
		"group":           newUser.Group,
		"remark":          newUser.Remark,
	}
	if updatePassword {
		updates["password"] = newUser.Password
	}
	authChanged := (updatePassword && current.Password != newUser.Password) ||
		current.Group != newUser.Group
	if authChanged {
		newUser.AuthVersion, err = IncrementUserAuthVersionWithTx(tx, user.Id)
		if err != nil {
			return err
		}
	}
	if err = tx.Model(&current).Updates(updates).Error; err != nil {
		return err
	}
	if err = syncUserLoginIdentifiersWithTx(tx, user.Id, newUser.Username, newUser.Email); err != nil {
		return err
	}
	return tx.First(user, user.Id).Error
}

func (user *User) ClearBinding(bindingType string) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}

	if bindingType == "email" {
		if err := DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
				"email":           "",
				"email_canonical": nil,
			}).Error; err != nil {
				return err
			}
			return syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, "")
		}); err != nil {
			return err
		}
		if err := DB.Where("id = ?", user.Id).First(user).Error; err != nil {
			return err
		}
		return updateUserCache(*user)
	}

	bindingColumnMap := map[string]string{
		ExternalIdentityProviderGitHub:   "github_id",
		ExternalIdentityProviderDiscord:  "discord_id",
		ExternalIdentityProviderOIDC:     "oidc_id",
		ExternalIdentityProviderWeChat:   "wechat_id",
		ExternalIdentityProviderTelegram: "telegram_id",
		ExternalIdentityProviderLinuxDO:  "linux_do_id",
	}

	column, ok := bindingColumnMap[bindingType]
	if !ok {
		return errors.New("invalid binding type")
	}

	var previousAuthVersion int64
	if err := DB.Model(&User{}).Where("id = ?", user.Id).Select("auth_version").Find(&previousAuthVersion).Error; err != nil {
		return err
	}

	if err := DB.Transaction(func(tx *gorm.DB) error {
		if authSensitiveBindingType(bindingType) {
			var current User
			if err := tx.Select([]string{"id", "auth_version", column}).Where("id = ?", user.Id).First(&current).Error; err != nil {
				return err
			}
			if getUserLoginBindingField(&current, bindingType) != "" {
				if _, err := IncrementUserAuthVersionWithTx(tx, user.Id); err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Update(column, "").Error; err != nil {
			return err
		}
		if _, ok := externalIdentityUserColumn(bindingType); ok {
			return ReleaseExternalIdentityWithTx(tx, bindingType, user.Id)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := DB.Where("id = ?", user.Id).First(user).Error; err != nil {
		return err
	}

	return FinalizeUserAuthChange(*user, previousAuthVersion, "user_security_changed")
}

func (user *User) Delete() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	var nextAuthVersion int64
	if err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		nextAuthVersion, err = IncrementUserAuthVersionWithTx(tx, user.Id)
		if err != nil {
			return err
		}
		return tx.Delete(user).Error
	}); err != nil {
		return err
	}
	publishErr := publishCommittedUserAuthVersion(user.Id, nextAuthVersion)
	_, revokeErr := RevokeAllUserSessions(user.Id, "user_deleted")
	cacheErr := invalidateUserCache(user.Id)
	return errors.Join(publishErr, revokeErr, cacheErr)
}

func (user *User) HardDelete() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	var tokens []Token
	var deletedAuthVersion int64
	err := DB.Transaction(func(tx *gorm.DB) error {
		var err error
		deletedAuthVersion, err = IncrementUserAuthVersionWithTx(tx, user.Id)
		if err != nil {
			return err
		}
		if common.RedisEnabled {
			if err := tx.Unscoped().Select("id", commonKeyCol).Where("user_id = ?", user.Id).Find(&tokens).Error; err != nil {
				return err
			}
		}
		if err := deleteUserAuthenticationData(tx, user.Id); err != nil {
			return err
		}
		return tx.Unscoped().Delete(user).Error
	})
	if err != nil {
		return err
	}
	if err := publishCommittedUserAuthVersion(user.Id, deletedAuthVersion); err != nil {
		common.SysError(fmt.Sprintf("failed to publish auth tombstone after hard deleting user %d: %v", user.Id, err))
	}
	if err := invalidateTokensCache(tokens); err != nil {
		common.SysError(fmt.Sprintf("failed to invalidate token cache after hard deleting user %d: %v", user.Id, err))
	}
	if err := invalidateUserCache(user.Id); err != nil {
		common.SysError(fmt.Sprintf("failed to invalidate user cache after hard deleting user %d: %v", user.Id, err))
	}
	return nil
}

func deleteUserAuthenticationData(tx *gorm.DB, userId int) error {
	if err := releaseAllExternalIdentitiesWithTx(tx, userId); err != nil {
		return err
	}
	for _, authenticationData := range []any{
		&UserLoginIdentifier{},
		&TwoFABackupCode{},
		&TwoFA{},
		&UserSession{},
		&AuthFlow{},
		&PasskeyCredential{},
		&Token{},
	} {
		if err := tx.Unscoped().Where("user_id = ?", userId).Delete(authenticationData).Error; err != nil {
			return err
		}
	}
	return deleteUserOAuthBindingsByUserId(tx, userId)
}

// ValidateAndFill check password & user status
func (user *User) ValidateAndFill() (err error) {
	password := user.Password
	user.Username = strings.TrimSpace(user.Username)
	if user.Username == "" || password == "" {
		return ErrUserEmptyCredentials
	}
	if err = user.FillUserByUsernameOrEmail(); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrUserPasswordIncorrect
		}
		return fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	if user.Id == 0 {
		return ErrUserDeleted
	}
	if user.Status != common.UserStatusEnabled {
		return ErrUserDisabled
	}
	if !common.ValidatePasswordAndHash(password, user.Password) {
		return ErrUserPasswordIncorrect
	}
	return nil
}

func (user *User) FillUserById() error {
	if user.Id == 0 {
		return errors.New("id 为空！")
	}
	DB.Where(User{Id: user.Id}).First(user)
	return nil
}

func (user *User) FillUserByEmail() error {
	user.Email = NormalizeUserEmail(user.Email)
	if user.Email == "" {
		return errors.New("email 为空！")
	}
	return DB.First(user, "email_canonical = ? OR LOWER(email) = ?", user.Email, user.Email).Error
}

func (user *User) FillUserByGitHubId() error {
	if user.GitHubId == "" {
		return errors.New("GitHub id 为空！")
	}
	return user.fillByExternalIdentity(ExternalIdentityProviderGitHub, user.GitHubId)
}

// UpdateGitHubId updates the user's GitHub ID (used for migration from login to numeric ID)
func (user *User) UpdateGitHubId(newGitHubId string) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}
	return user.ClaimExternalIdentity(ExternalIdentityProviderGitHub, newGitHubId)
}

func (user *User) FillUserByDiscordId() error {
	if user.DiscordId == "" {
		return errors.New("discord id 为空！")
	}
	return user.fillByExternalIdentity(ExternalIdentityProviderDiscord, user.DiscordId)
}

func (user *User) FillUserByOidcId() error {
	if user.OidcId == "" {
		return errors.New("oidc id 为空！")
	}
	return user.fillByExternalIdentity(ExternalIdentityProviderOIDC, user.OidcId)
}

func (user *User) FillUserByWeChatId() error {
	if user.WeChatId == "" {
		return errors.New("WeChat id 为空！")
	}
	return user.fillByExternalIdentity(ExternalIdentityProviderWeChat, user.WeChatId)
}

func (user *User) FillUserByTelegramId() error {
	if user.TelegramId == "" {
		return errors.New("Telegram id 为空！")
	}
	err := user.fillByExternalIdentity(ExternalIdentityProviderTelegram, user.TelegramId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("该 Telegram 账户未绑定")
	}
	return err
}

func IsEmailAlreadyTaken(email string) bool {
	count, err := CountUsersByEmail(email)
	return err == nil && count > 0
}

func IsActiveEmailAlreadyTaken(email string) bool {
	email = NormalizeUserEmail(email)
	if email == "" {
		return false
	}
	var count int64
	err := emailQuery(DB, email).
		Where("status = ? AND deleted_at IS NULL", common.UserStatusEnabled).
		Count(&count).Error
	return err == nil && count > 0
}

func GetUniqueUserByEmail(email string) (*User, error) {
	email = NormalizeUserEmail(email)
	if email == "" {
		return nil, ErrEmailNotFound
	}
	var users []User
	if err := DB.Unscoped().Where("email_canonical = ? OR LOWER(email) = ?", email, email).Limit(2).Find(&users).Error; err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, ErrEmailNotFound
	case 1:
		return &users[0], nil
	default:
		return nil, ErrEmailAmbiguous
	}
}

func IsWeChatIdAlreadyTaken(wechatId string) bool {
	return DB.Unscoped().Where("wechat_id = ?", wechatId).Find(&User{}).RowsAffected > 0
}

func IsGitHubIdAlreadyTaken(githubId string) bool {
	return DB.Unscoped().Where("github_id = ?", githubId).Find(&User{}).RowsAffected > 0
}

func IsDiscordIdAlreadyTaken(discordId string) bool {
	return DB.Unscoped().Where("discord_id = ?", discordId).Find(&User{}).RowsAffected > 0
}

func IsOidcIdAlreadyTaken(oidcId string) bool {
	return DB.Unscoped().Where("oidc_id = ?", oidcId).Find(&User{}).RowsAffected > 0
}

func IsTelegramIdAlreadyTaken(telegramId string) bool {
	return DB.Unscoped().Where("telegram_id = ?", telegramId).Find(&User{}).RowsAffected > 0
}

func ResetUserPasswordByEmail(email string, password string) error {
	email = NormalizeUserEmail(email)
	if email == "" || password == "" {
		return errors.New("邮箱地址或密码为空！")
	}
	user, err := GetUniqueUserByEmail(email)
	if err != nil {
		return err
	}
	previousAuthVersion := user.AuthVersion
	hashedPassword, err := common.Password2Hash(password)
	if err != nil {
		return err
	}
	if err = DB.Transaction(func(tx *gorm.DB) error {
		if _, err := IncrementUserAuthVersionWithTx(tx, user.Id); err != nil {
			return err
		}
		return tx.Model(&User{}).Where("id = ?", user.Id).Update("password", hashedPassword).Error
	}); err != nil {
		return err
	}
	return FinalizeUserAuthChangeByID(user.Id, previousAuthVersion, "password_reset")
}

func IsAdmin(userId int) bool {
	if userId == 0 {
		return false
	}
	var user User
	err := DB.Where("id = ?", userId).Select("role").Find(&user).Error
	if err != nil {
		common.SysLog("no such user " + err.Error())
		return false
	}
	return user.Role >= common.RoleAdminUser
}

func ValidateAccessToken(token string) (*User, error) {
	if token == "" {
		return nil, nil
	}
	token = strings.Replace(token, "Bearer ", "", 1)
	user := &User{}
	err := DB.Where("access_token = ?", token).First(user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	return user, nil
}

// GetUserQuota gets quota from Redis first, falls back to DB if needed
func GetUserQuota(id int, fromDB bool) (quota int, err error) {
	if !fromDB && common.RedisEnabled {
		return getUserQuotaCache(id)
	}
	err = DB.Model(&User{}).Where("id = ?", id).Select("quota").Find(&quota).Error
	if err != nil {
		return 0, err
	}

	return quota, nil
}

func GetUserUsedQuota(id int) (quota int, err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Select("used_quota").Find(&quota).Error
	return quota, err
}

func GetUserEmail(id int) (email string, err error) {
	err = DB.Model(&User{}).Where("id = ?", id).Select("email").Find(&email).Error
	return email, err
}

// GetUserGroup gets group from Redis first, falls back to DB if needed
func GetUserGroup(id int, fromDB bool) (group string, err error) {
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) {
			gopool.Go(func() {
				if err := RefreshUserGroupCache(id); err != nil {
					common.SysLog("failed to update user group cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		group, err := getUserGroupCache(id)
		if err == nil {
			return group, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	err = DB.Model(&User{}).Where("id = ?", id).Select(commonGroupCol).Find(&group).Error
	if err != nil {
		return "", err
	}

	return group, nil
}

// GetUserSetting gets setting from Redis first, falls back to DB if needed
func GetUserSetting(id int, fromDB bool) (settingMap dto.UserSetting, err error) {
	var setting string
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) {
			gopool.Go(func() {
				if err := updateUserSettingCache(id, setting); err != nil {
					common.SysLog("failed to update user setting cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		setting, err := getUserSettingCache(id)
		if err == nil {
			return setting, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	// can be nil setting
	var safeSetting sql.NullString
	err = DB.Model(&User{}).Where("id = ?", id).Select("setting").Find(&safeSetting).Error
	if err != nil {
		return settingMap, err
	}
	if safeSetting.Valid {
		setting = safeSetting.String
	} else {
		setting = ""
	}
	userBase := &UserBase{
		Setting: setting,
	}
	return userBase.GetSetting(), nil
}

func IncreaseUserQuota(id int, quota int, db bool) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return nil
	}
	if err := increaseUserQuota(id, quota); err != nil {
		return err
	}
	refreshUserQuotaCacheBestEffort(id)
	return nil
}

func increaseUserQuota(id int, quota int) (err error) {
	result := DB.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user quota update failed, user_id=%d, delta_quota=%d", id, quota)
	}
	return nil
}

func IncreaseUserQuotaTx(tx *gorm.DB, id int, quota int) error {
	if tx == nil {
		return errors.New("database transaction is required")
	}
	if quota < 0 {
		return errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return nil
	}
	result := tx.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user quota update failed, user_id=%d, delta_quota=%d", id, quota)
	}
	return nil
}

func DecreaseUserQuota(id int, quota int, db bool) (err error) {
	if quota < 0 {
		return errors.New("quota 不能为负数！")
	}
	if quota == 0 {
		return nil
	}
	if err := decreaseUserQuota(id, quota); err != nil {
		return err
	}
	refreshUserQuotaCacheBestEffort(id)
	return nil
}

func decreaseUserQuota(id int, quota int) (err error) {
	result := DB.Model(&User{}).
		Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user quota update failed, user_id=%d, delta_quota=%d", id, -quota)
	}
	return nil
}

func DecreaseUserQuotaAllowNegative(id int, quota int, db bool) (err error) {
	if quota < 0 {
		return errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return nil
	}
	if err := decreaseUserQuotaAllowNegativeWithDB(DB, id, quota); err != nil {
		return err
	}
	refreshUserQuotaCacheBestEffort(id)
	return nil
}

func decreaseUserQuotaAllowNegativeWithDB(tx *gorm.DB, id int, quota int) error {
	if tx == nil {
		return errors.New("database transaction is required")
	}
	result := tx.Model(&User{}).
		Where("id = ?", id).
		Update("quota", gorm.Expr("quota - ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user quota update failed, user_id=%d, delta_quota=%d", id, -quota)
	}
	return nil
}

func DecreaseUserQuotaTx(tx *gorm.DB, id int, quota int) error {
	if tx == nil {
		return errors.New("database transaction is required")
	}
	if quota < 0 {
		return errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return nil
	}
	result := tx.Model(&User{}).
		Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user quota is not enough or user not found, user_id=%d, need_quota=%d", id, quota)
	}
	return nil
}

func DecreaseUserQuotaAllowNegativeTx(tx *gorm.DB, id int, quota int) error {
	if quota < 0 {
		return errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return nil
	}
	return decreaseUserQuotaAllowNegativeWithDB(tx, id, quota)
}

func DeltaUpdateUserQuota(id int, delta int) (err error) {
	if delta == 0 {
		return nil
	}
	if delta > 0 {
		return IncreaseUserQuota(id, delta, false)
	} else {
		return DecreaseUserQuota(id, -delta, false)
	}
}

//func GetRootUserEmail() (email string) {
//	DB.Model(&User{}).Where("role = ?", common.RoleRootUser).Select("email").Find(&email)
//	return email
//}

func GetRootUser() (user *User) {
	DB.Where("role = ?", common.RoleRootUser).First(&user)
	return user
}

func UpdateUserLastLoginAt(id int) {
	if err := DB.Model(&User{}).Where("id = ?", id).Update("last_login_at", common.GetTimestamp()).Error; err != nil {
		common.SysLog("failed to update user last_login_at: " + err.Error())
	}
}

func UpdateUserUsedQuotaAndRequestCount(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUsedQuota, id, quota)
		addNewRecord(BatchUpdateTypeRequestCount, id, 1)
		return
	}
	updateUserUsedQuotaAndRequestCount(id, quota, 1)
}

// UpdateUserUsedQuota adjusts accumulated usage without changing request count.
func UpdateUserUsedQuota(id int, quota int) {
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUsedQuota, id, quota)
		return
	}
	if err := DB.Model(&User{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error; err != nil {
		common.SysLog("failed to update user used quota: " + err.Error())
	}
}

func updateUserUsedQuotaAndRequestCount(id int, quota int, count int) {
	err := DB.Model(&User{}).Where("id = ?", id).Updates(
		map[string]interface{}{
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"request_count": gorm.Expr("request_count + ?", count),
		},
	).Error
	if err != nil {
		common.SysLog("failed to update user used quota and request count: " + err.Error())
		return
	}

	//// 更新缓存
	//if err := invalidateUserCache(id); err != nil {
	//	common.SysError("failed to invalidate user cache: " + err.Error())
	//}
}

func updateUserUsedQuota(id int, quota int) error {
	if quota == 0 {
		return nil
	}
	if err := updateUserUsedQuotaWithDB(DB, id, quota); err != nil {
		common.SysLog("failed to update user used quota: " + err.Error())
		return err
	}
	refreshUserQuotaCacheBestEffort(id)
	return nil
}

func updateUserRequestCount(id int, count int) {
	err := DB.Model(&User{}).Where("id = ?", id).Update("request_count", gorm.Expr("request_count + ?", count)).Error
	if err != nil {
		common.SysLog("failed to update user request count: " + err.Error())
	}
}

// GetUsernameById gets username from Redis first, falls back to DB if needed
func GetUsernameById(id int, fromDB bool) (username string, err error) {
	defer func() {
		// Update Redis cache asynchronously on successful DB read
		if shouldUpdateRedis(fromDB, err) {
			gopool.Go(func() {
				if err := updateUserNameCache(id, username); err != nil {
					common.SysLog("failed to update user name cache: " + err.Error())
				}
			})
		}
	}()
	if !fromDB && common.RedisEnabled {
		username, err := getUserNameCache(id)
		if err == nil {
			return username, nil
		}
		// Don't return error - fall through to DB
	}
	fromDB = true
	err = DB.Model(&User{}).Where("id = ?", id).Select("username").Find(&username).Error
	if err != nil {
		return "", err
	}

	return username, nil
}

func IsLinuxDOIdAlreadyTaken(linuxDOId string) bool {
	var user User
	err := DB.Unscoped().Where("linux_do_id = ?", linuxDOId).First(&user).Error
	return !errors.Is(err, gorm.ErrRecordNotFound)
}

func (user *User) FillUserByLinuxDOId() error {
	if user.LinuxDOId == "" {
		return errors.New("linux do id is empty")
	}
	return user.fillByExternalIdentity(ExternalIdentityProviderLinuxDO, user.LinuxDOId)
}

func (user *User) fillByExternalIdentity(provider string, subject string) error {
	found, err := GetUniqueUserByExternalIdentity(provider, subject)
	if err != nil {
		return err
	}
	*user = *found
	return nil
}

func RootUserExists() bool {
	var user User
	err := DB.Where("role = ?", common.RoleRootUser).First(&user).Error
	if err != nil {
		return false
	}
	return true
}
