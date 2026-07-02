package model

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const UserNameMaxLength = 20
const RegisterUserNameMaxLength = UserNameMaxLength
const NewUserUsernameFormatError = "username can only contain letters, numbers, underscores, and hyphens"

var usernameInvalidCharacterRegex = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// User if you add sensitive fields, don't forget to clean them in setupLogin function.
// Otherwise, the sensitive information will be saved on local storage in plain text!
type User struct {
	Id                      int            `json:"id"`
	Username                string         `json:"username" gorm:"unique;index" validate:"max=20"`
	Password                string         `json:"password" gorm:"not null;" validate:"min=8,max=20"`
	OriginalPassword        string         `json:"original_password" gorm:"-:all"`
	DisplayName             string         `json:"display_name" gorm:"index" validate:"max=20"`
	Role                    int            `json:"role" gorm:"type:int;default:1"`
	Status                  int            `json:"status" gorm:"type:int;default:1"`
	Email                   string         `json:"email" gorm:"index" validate:"max=50"`
	EmailCanonical          *string        `json:"-" gorm:"column:email_canonical;type:varchar(191)"`
	GitHubId                string         `json:"github_id" gorm:"column:github_id;index"`
	DiscordId               string         `json:"discord_id" gorm:"column:discord_id;index"`
	OidcId                  string         `json:"oidc_id" gorm:"column:oidc_id;index"`
	WeChatId                string         `json:"wechat_id" gorm:"column:wechat_id;index"`
	TelegramId              string         `json:"telegram_id" gorm:"column:telegram_id;index"`
	VerificationCode        string         `json:"verification_code" gorm:"-:all"`
	AccessToken             *string        `json:"-" gorm:"type:char(32);column:access_token;uniqueIndex"`
	Quota                   int            `json:"quota" gorm:"type:int;default:0"`
	UsedQuota               int            `json:"used_quota" gorm:"type:int;default:0;column:used_quota"`
	RequestCount            int            `json:"request_count" gorm:"type:int;default:0;"`
	Group                   string         `json:"group" gorm:"type:varchar(64);default:'default'"`
	ReferralInviterId       int            `json:"referral_inviter_id,omitempty" gorm:"-"`
	ReferralInviterUsername string         `json:"referral_inviter_username,omitempty" gorm:"-"`
	ActiveSubscriptionName  string         `json:"active_subscription_name,omitempty" gorm:"-"`
	LastActiveAt            int64          `json:"last_active_at" gorm:"-"`
	DeletedAt               gorm.DeletedAt `gorm:"index"`
	LinuxDOId               string         `json:"linux_do_id" gorm:"column:linux_do_id;index"`
	Setting                 string         `json:"setting" gorm:"type:text;column:setting"`
	Remark                  string         `json:"remark,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	StripeCustomer          string         `json:"stripe_customer" gorm:"type:varchar(64);column:stripe_customer;index"`
	CreatedAt               int64          `json:"created_at" gorm:"autoCreateTime;column:created_at;index"`
	LastLoginAt             int64          `json:"last_login_at" gorm:"default:0;column:last_login_at"`
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
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '_' || r == '-' {
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
	usernameRunes := []rune(username)
	if len(usernameRunes) > RegisterUserNameMaxLength {
		return string(usernameRunes[:RegisterUserNameMaxLength])
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
		idRunes := []rune(idPart)
		return string(idRunes[len(idRunes)-RegisterUserNameMaxLength:])
	}
	prefixRunes := []rune(prefix)
	if len(prefixRunes) > maxPrefixLength {
		prefix = string(prefixRunes[:maxPrefixLength])
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

func (user *User) BeforeSave(tx *gorm.DB) error {
	user.normalizeEmailForPersistence()
	return nil
}

func (user *User) ToBaseUser() *UserBase {
	cache := &UserBase{
		Id:       user.Id,
		Group:    strings.TrimSpace(user.Group),
		Quota:    user.Quota,
		Status:   user.Status,
		Username: user.Username,
		Setting:  user.Setting,
		Email:    user.Email,
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

func (user *User) GetSetting() dto.UserSetting {
	setting := dto.UserSetting{}
	if user.Setting != "" {
		err := json.Unmarshal([]byte(user.Setting), &setting)
		if err != nil {
			common.SysLog("failed to unmarshal setting: " + err.Error())
		}
	}
	return setting
}

func (user *User) SetSetting(setting dto.UserSetting) {
	settingBytes, err := json.Marshal(setting)
	if err != nil {
		common.SysLog("failed to marshal setting: " + err.Error())
		return
	}
	user.Setting = string(settingBytes)
}

func (user *User) initializeDefaultSettingForRole() {
	setting := user.GetSetting()
	role := user.Role
	if role == 0 {
		role = common.RoleCommonUser
	}
	if setting.SidebarModules == "" {
		setting.SidebarModules = GenerateDefaultSidebarConfigForRole(role)
	}
	user.SetSetting(setting)
}

func GenerateDefaultSidebarConfigForRole(userRole int) string {
	defaultConfig := map[string]interface{}{}
	defaultConfig["chat"] = map[string]interface{}{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}
	defaultConfig["console"] = map[string]interface{}{
		"enabled":     true,
		"detail":      true,
		"token":       true,
		"image2":      true,
		"model_check": true,
		"log":         true,
		"midjourney":  true,
		"task":        true,
	}
	defaultConfig["personal"] = map[string]interface{}{
		"enabled":  true,
		"topup":    true,
		"referral": true,
		"tickets":  true,
		"personal": true,
	}

	if userRole == common.RoleAdminUser {
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":               true,
			"channel":               true,
			"models":                true,
			"redemption":            true,
			"user":                  true,
			"subscription":          true,
			"referral":              true,
			"ticket_management":     true,
			"recharge_audit":        true,
			"setting":               false,
		}
	} else if userRole == common.RoleRootUser {
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":               true,
			"channel":               true,
			"models":                true,
			"redemption":            true,
			"user":                  true,
			"subscription":          true,
			"referral":              true,
			"ticket_management":     true,
			"recharge_audit":        true,
			"setting":               true,
		}
	}

	configBytes, err := json.Marshal(defaultConfig)
	if err != nil {
		common.SysLog("生成默认边栏配置失败: " + err.Error())
		return ""
	}
	return string(configBytes)
}

func CheckUserExistOrDeleted(username string, email string) (bool, error) {
	return IsLoginIdentifierTakenByOther(username, email, 0)
}

func IsLoginIdentifierTakenByOther(username string, email string, userId int) (bool, error) {
	return isLoginIdentifierTakenByOtherWithTx(DB, username, email, userId)
}

func getUserLoginIdentifiers(username string, email string) map[string]string {
	username = strings.TrimSpace(username)
	email = NormalizeUserEmail(email)
	identifiers := map[string]string{}
	if username != "" {
		if strings.Contains(username, "@") {
			username = NormalizeUserEmail(username)
		}
		identifiers[username] = "username"
	}
	if email != "" {
		identifiers[email] = "email"
	}
	return identifiers
}

func hasDuplicateUserLoginIdentifiers(username string, email string) bool {
	username = strings.TrimSpace(username)
	if strings.Contains(username, "@") {
		username = NormalizeUserEmail(username)
	}
	email = NormalizeUserEmail(email)
	return username != "" && email != "" && username == email
}

func isLoginIdentifierTakenByOtherWithTx(tx *gorm.DB, username string, email string, userId int) (bool, error) {
	if hasDuplicateUserLoginIdentifiers(username, email) {
		return true, nil
	}
	identifierMap := getUserLoginIdentifiers(username, email)
	identifiers := make([]string, 0, len(identifierMap))
	for identifier := range identifierMap {
		identifiers = append(identifiers, identifier)
	}
	if len(identifiers) == 0 {
		return false, nil
	}

	var loginIdentifier UserLoginIdentifier
	result := tx.Unscoped().
		Where("user_id <> ? AND identifier IN ?", userId, identifiers).
		First(&loginIdentifier)
	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return false, result.Error
		}
	} else {
		return true, nil
	}

	var user User
	result = tx.Unscoped().
		Where("id <> ? AND (username IN ? OR email_canonical IN ?)", userId, identifiers, identifiers).
		First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, result.Error
	}
	return true, nil
}

func syncUserLoginIdentifiersWithTx(tx *gorm.DB, userId int, username string, email string) error {
	if userId == 0 {
		return errors.New("user id is empty")
	}
	if hasDuplicateUserLoginIdentifiers(username, email) {
		return ErrUserLoginIdentifierTaken
	}
	identifiers := getUserLoginIdentifiers(username, email)
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Where("user_id = ?", userId).Delete(&UserLoginIdentifier{}).Error; err != nil {
		return err
	}
	for identifier, kind := range identifiers {
		loginIdentifier := UserLoginIdentifier{
			UserId:     userId,
			Identifier: identifier,
			Kind:       kind,
		}
		if err := tx.Create(&loginIdentifier).Error; err != nil {
			return err
		}
	}
	return nil
}

func SyncUserLoginIdentifiers(userId int) error {
	var user User
	if err := DB.Unscoped().First(&user, userId).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email)
	})
}

func setUserEmailIfEmptyWithTx(tx *gorm.DB, userId int, email string) (bool, error) {
	email = NormalizeUserEmail(email)
	if email == "" {
		return false, nil
	}
	if err := common.Validate.Var(email, "email"); err != nil {
		return false, err
	}

	updated := false
	err := tx.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := tx.First(&user, userId).Error; err != nil {
			return err
		}
		if user.Email != "" {
			return nil
		}
		exists, err := isLoginIdentifierTakenByOtherWithTx(tx, user.Username, email, user.Id)
		if err != nil {
			return err
		}
		if exists {
			return ErrUserLoginIdentifierTaken
		}

		user.Email = email
		user.normalizeEmailForPersistence()
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]interface{}{
			"email":           user.Email,
			"email_canonical": user.EmailCanonical,
		}).Error; err != nil {
			return err
		}
		if err := syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email); err != nil {
			return err
		}
		updated = true
		return nil
	})
	return updated, err
}

func GetMaxUserId() int {
	var user User
	DB.Unscoped().Last(&user)
	return user.Id
}

func GetAllUsers(pageInfo *common.PageInfo) (users []*User, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	err = tx.Unscoped().Model(&User{}).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = tx.Unscoped().Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Omit("password").Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	populateReferralInviters(users)
	populateActiveSubscriptionNames(users)
	populateUserActivity(users)

	return users, total, nil
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

func SearchUsers(keyword string, group string, role *int, status *int, startIdx int, num int) ([]*User, int64, error) {
	var users []*User
	var total int64
	var err error

	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Unscoped().Model(&User{}).
		Joins("LEFT JOIN referral_bindings rb ON rb.invitee_user_id = users.id").
		Joins("LEFT JOIN users inviter_users ON inviter_users.id = rb.inviter_user_id")
	groupColumn := "users." + commonGroupCol
	likeCondition := "users.username LIKE ? OR users.email LIKE ? OR users.display_name LIKE ? OR inviter_users.username LIKE ?"
	likeArgs := []interface{}{"%" + keyword + "%", "%" + keyword + "%", "%" + keyword + "%", "%" + keyword + "%"}

	keywordInt, err := strconv.Atoi(keyword)
	if err == nil {
		likeCondition = "users.id = ? OR " + likeCondition
		likeArgs = append([]interface{}{keywordInt}, likeArgs...)
	}

	query = query.Where("("+likeCondition+")", likeArgs...)
	if group != "" {
		query = query.Where(groupColumn+" = ?", group)
	}
	if role != nil {
		query = query.Where("users.role = ?", *role)
	}
	if status != nil {
		if *status == -1 {
			query = query.Where("users.deleted_at IS NOT NULL")
		} else {
			query = query.Where("users.deleted_at IS NULL").Where("users.status = ?", *status)
		}
	}

	err = query.Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	err = query.Omit("password").Order("users.id desc").Limit(num).Offset(startIdx).Find(&users).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	populateReferralInviters(users)
	populateActiveSubscriptionNames(users)
	populateUserActivity(users)

	return users, total, nil
}

func populateUserActivity(users []*User) {
	if len(users) == 0 {
		return
	}
	userIds := make([]int, 0, len(users))
	userById := make(map[int]*User, len(users))
	for _, user := range users {
		if user == nil || user.Id == 0 {
			continue
		}
		userIds = append(userIds, user.Id)
		userById[user.Id] = user
	}
	if len(userIds) == 0 {
		return
	}

	type activeRow struct {
		UserID       int
		LastActiveAt int64
	}
	var activeRows []activeRow
	if err := LOG_DB.Table("logs").
		Select("user_id, max(created_at) AS last_active_at").
		Where("user_id IN ? AND type = ?", userIds, LogTypeConsume).
		Group("user_id").
		Scan(&activeRows).Error; err != nil {
		common.SysLog("failed to populate user last active time: " + err.Error())
	} else {
		for _, row := range activeRows {
			if user := userById[row.UserID]; user != nil {
				user.LastActiveAt = row.LastActiveAt
			}
		}
	}
}

func populateReferralInviters(users []*User) {
	if len(users) == 0 {
		return
	}
	userIds := make([]int, 0, len(users))
	userById := make(map[int]*User, len(users))
	for _, user := range users {
		if user == nil || user.Id == 0 {
			continue
		}
		userIds = append(userIds, user.Id)
		userById[user.Id] = user
	}
	if len(userIds) == 0 {
		return
	}

	type inviterRow struct {
		InviteeUserId   int
		InviterUserId   int
		InviterUsername string
	}
	var rows []inviterRow
	err := DB.Table("referral_bindings AS rb").
		Select("rb.invitee_user_id, rb.inviter_user_id, inviter_users.username AS inviter_username").
		Joins("LEFT JOIN users inviter_users ON inviter_users.id = rb.inviter_user_id").
		Where("rb.invitee_user_id IN ?", userIds).
		Scan(&rows).Error
	if err != nil {
		common.SysLog("failed to populate referral inviters: " + err.Error())
		return
	}
	for _, row := range rows {
		user := userById[row.InviteeUserId]
		if user == nil {
			continue
		}
		user.ReferralInviterId = row.InviterUserId
		user.ReferralInviterUsername = row.InviterUsername
	}
}

func populateActiveSubscriptionNames(users []*User) {
	if len(users) == 0 {
		return
	}
	userIds := make([]int, 0, len(users))
	userById := make(map[int]*User, len(users))
	for _, user := range users {
		if user == nil || user.Id == 0 {
			continue
		}
		userIds = append(userIds, user.Id)
		userById[user.Id] = user
	}
	if len(userIds) == 0 {
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
		Where("us.user_id IN ? AND us.status = ? AND us.end_time > ?", userIds, "active", now).
		Order("us.end_time desc, us.id desc").
		Scan(&rows).Error
	if err != nil {
		common.SysLog("failed to populate active subscriptions: " + err.Error())
		return
	}
	for _, row := range rows {
		user := userById[row.UserID]
		if user == nil || user.ActiveSubscriptionName != "" {
			continue
		}
		user.ActiveSubscriptionName = strings.TrimSpace(row.Title)
	}
}

func GetUserById(id int, selectAll bool) (*User, error) {
	if id == 0 {
		return nil, errors.New("id 为空")
	}
	user := User{Id: id}
	var err error
	if selectAll {
		err = DB.First(&user, "id = ?", id).Error
	} else {
		err = DB.Omit("password").First(&user, "id = ?", id).Error
	}
	return &user, err
}

func GetUserByIdUnscoped(id int, selectAll bool) (*User, error) {
	if id == 0 {
		return nil, errors.New("id 为空")
	}
	user := User{Id: id}
	query := DB.Unscoped()
	var err error
	if selectAll {
		err = query.First(&user, "id = ?", id).Error
	} else {
		err = query.Omit("password").First(&user, "id = ?", id).Error
	}
	return &user, err
}

func DeleteUserById(id int) (err error) {
	if id == 0 {
		return errors.New("id 为空")
	}
	user := User{Id: id}
	return user.Delete()
}

func HardDeleteUserById(id int) error {
	if id == 0 {
		return errors.New("id 为空")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("user_id = ?", id).Delete(&UserLoginIdentifier{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(&User{}, "id = ?", id).Error
	})
}

func (user *User) Insert(_ int) error {
	return user.insert(false)
}

func (user *User) InsertPreserveQuota(_ int) error {
	return user.insert(true)
}

func (user *User) insert(preserveQuota bool) error {
	var err error
	user.normalizeEmailForPersistence()
	exists, err := IsLoginIdentifierTakenByOther(user.Username, user.Email, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrUserLoginIdentifierTaken
	}
	if user.Password != "" {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	if !preserveQuota {
		user.Quota = common.QuotaForNewUser
	}

	user.initializeDefaultSettingForRole()

	err = DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Create(user)
		if result.Error != nil {
			return result.Error
		}
		return syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email)
	})
	if err != nil {
		return err
	}

	if common.QuotaForNewUser > 0 {
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(common.QuotaForNewUser)))
	}
	return nil
}

func (user *User) InsertWithTx(tx *gorm.DB, _ int) error {
	var err error
	user.normalizeEmailForPersistence()
	exists, err := isLoginIdentifierTakenByOtherWithTx(tx, user.Username, user.Email, 0)
	if err != nil {
		return err
	}
	if exists {
		return ErrUserLoginIdentifierTaken
	}
	if user.Password != "" {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	user.Quota = common.QuotaForNewUser

	user.initializeDefaultSettingForRole()

	result := tx.Create(user)
	if result.Error != nil {
		return result.Error
	}
	if err = syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email); err != nil {
		return err
	}
	return nil
}

func (user *User) FinalizeOAuthUserCreation(_ int) {
	if common.QuotaForNewUser > 0 {
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(common.QuotaForNewUser)))
	}
}

func (user *User) Update(updatePassword bool) error {
	var err error
	user.normalizeEmailForPersistence()
	currentUser := User{}
	if err = DB.First(&currentUser, user.Id).Error; err != nil {
		return err
	}
	if user.Username == "" {
		user.Username = currentUser.Username
	}
	if user.Email == "" {
		user.Email = currentUser.Email
		user.normalizeEmailForPersistence()
	}
	exists, err := IsLoginIdentifierTakenByOther(user.Username, user.Email, user.Id)
	if err != nil {
		return err
	}
	if exists {
		return ErrUserLoginIdentifierTaken
	}
	if updatePassword {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	newUser := *user
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err = tx.Model(user).Updates(newUser).Error; err != nil {
			return err
		}
		return syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email)
	})
	if err != nil {
		return err
	}
	if err = DB.First(user, user.Id).Error; err != nil {
		return err
	}
	return updateUserCache(*user)
}

func (user *User) Edit(updatePassword bool, updateEmail ...bool) error {
	var err error
	shouldUpdateEmail := len(updateEmail) > 0 && updateEmail[0]
	user.normalizeEmailForPersistence()
	if updatePassword {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}

	newUser := *user
	currentUser := User{}
	if err = DB.First(&currentUser, user.Id).Error; err != nil {
		return err
	}
	if !shouldUpdateEmail {
		newUser.Email = currentUser.Email
		newUser.normalizeEmailForPersistence()
	}
	exists, err := IsLoginIdentifierTakenByOther(newUser.Username, newUser.Email, user.Id)
	if err != nil {
		return err
	}
	if exists {
		return ErrUserLoginIdentifierTaken
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

	err = DB.Transaction(func(tx *gorm.DB) error {
		if err = tx.Model(user).Updates(updates).Error; err != nil {
			return err
		}
		return syncUserLoginIdentifiersWithTx(tx, user.Id, newUser.Username, newUser.Email)
	})
	if err != nil {
		return err
	}
	if err = DB.First(user, user.Id).Error; err != nil {
		return err
	}
	return updateUserCache(*user)
}

func (user *User) ClearBinding(bindingType string) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}

	if bindingType == "email" {
		err := DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]any{
				"email":           "",
				"email_canonical": nil,
			}).Error; err != nil {
				return err
			}
			return syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, "")
		})
		if err != nil {
			return err
		}
		if err := DB.Where("id = ?", user.Id).First(user).Error; err != nil {
			return err
		}
		return updateUserCache(*user)
	}

	bindingColumnMap := map[string]string{
		"github":   "github_id",
		"discord":  "discord_id",
		"oidc":     "oidc_id",
		"wechat":   "wechat_id",
		"telegram": "telegram_id",
		"linuxdo":  "linux_do_id",
	}

	column, ok := bindingColumnMap[bindingType]
	if !ok {
		return errors.New("invalid binding type")
	}

	if err := DB.Model(&User{}).Where("id = ?", user.Id).Update(column, "").Error; err != nil {
		return err
	}
	if err := DB.Where("id = ?", user.Id).First(user).Error; err != nil {
		return err
	}
	return updateUserCache(*user)
}

func (user *User) Delete() error {
	return DB.Delete(user).Error
}

func (user *User) HardDelete() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("user_id = ?", user.Id).Delete(&UserLoginIdentifier{}).Error; err != nil {
			return err
		}
		return tx.Unscoped().Delete(user).Error
	})
}

func (user *User) ValidateAndFill() (err error) {
	if user.Username == "" || user.Password == "" {
		return ErrUserEmptyCredentials
	}

	originalPassword := user.Password
	if err := user.FillUserByUsernameOrEmail(); err != nil {
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
	if !common.ValidatePasswordAndHash(originalPassword, user.Password) {
		return ErrUserPasswordIncorrect
	}
	return nil
}

func (user *User) FillUserById() error {
	return DB.First(user, "id = ?", user.Id).Error
}

func (user *User) FillUserByEmail() error {
	user.Email = NormalizeUserEmail(user.Email)
	return DB.First(user, "email_canonical = ?", user.Email).Error
}

func (user *User) FillUserByUsernameOrEmail() error {
	loginIdentifier := user.Username
	if err := user.FillUserByUsername(); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	user.Username = loginIdentifier
	user.Email = loginIdentifier
	return user.FillUserByEmail()
}

func (user *User) FillUserByGitHubId() error {
	return DB.First(user, "github_id = ?", user.GitHubId).Error
}

func (user *User) UpdateGitHubId(newGitHubId string) error {
	return DB.Model(user).Update("github_id", newGitHubId).Error
}

func (user *User) FillUserByDiscordId() error {
	return DB.First(user, "discord_id = ?", user.DiscordId).Error
}

func (user *User) FillUserByOidcId() error {
	return DB.First(user, "oidc_id = ?", user.OidcId).Error
}

func (user *User) FillUserByWeChatId() error {
	return DB.First(user, "wechat_id = ?", user.WeChatId).Error
}

func (user *User) FillUserByTelegramId() error {
	return DB.First(user, "telegram_id = ?", user.TelegramId).Error
}

func IsEmailAlreadyTaken(email string) bool {
	email = NormalizeUserEmail(email)
	if email == "" {
		return false
	}
	return DB.Unscoped().Where("email_canonical = ?", email).First(&User{}).RowsAffected > 0
}

func IsActiveEmailAlreadyTaken(email string) bool {
	email = NormalizeUserEmail(email)
	if email == "" {
		return false
	}
	return DB.Where("email_canonical = ?", email).First(&User{}).RowsAffected > 0
}

func IsEmailAlreadyTakenByOther(email string, userId int) bool {
	email = NormalizeUserEmail(email)
	if email == "" {
		return false
	}
	return DB.Unscoped().Where("email_canonical = ? AND id <> ?", email, userId).First(&User{}).RowsAffected > 0
}

func IsWeChatIdAlreadyTaken(wechatId string) bool {
	return DB.Unscoped().Where("wechat_id = ?", wechatId).First(&User{}).RowsAffected > 0
}

func IsGitHubIdAlreadyTaken(githubId string) bool {
	return DB.Unscoped().Where("github_id = ?", githubId).First(&User{}).RowsAffected > 0
}

func IsDiscordIdAlreadyTaken(discordId string) bool {
	return DB.Unscoped().Where("discord_id = ?", discordId).First(&User{}).RowsAffected > 0
}

func IsOidcIdAlreadyTaken(oidcId string) bool {
	return DB.Unscoped().Where("oidc_id = ?", oidcId).First(&User{}).RowsAffected > 0
}

func IsTelegramIdAlreadyTaken(telegramId string) bool {
	return DB.Unscoped().Where("telegram_id = ?", telegramId).First(&User{}).RowsAffected > 0
}

func ResetUserPasswordByEmail(email string, password string) error {
	email = NormalizeUserEmail(email)
	if email == "" || password == "" {
		return errors.New("email or password is empty")
	}
	hashedPassword, err := common.Password2Hash(password)
	if err != nil {
		return err
	}
	result := DB.Model(&User{}).Where("email_canonical = ?", email).Update("password", hashedPassword)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

var (
	ErrUserDisabled             = errors.New("user disabled")
	ErrUserDeleted              = errors.New("user deleted")
	ErrUserPasswordIncorrect    = errors.New("password incorrect")
	ErrUserLoginIdentifierTaken = errors.New("user login identifier taken")
	ErrUserUsernameInvalid      = errors.New(NewUserUsernameFormatError)
	ErrUserUsernameTooLong      = fmt.Errorf("username must be at most %d characters long", RegisterUserNameMaxLength)
)

func IsUserEmailUniqueError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrUserLoginIdentifierTaken) {
		return true
	}
	message := strings.ToLower(err.Error())
	hasUniqueSignal := strings.Contains(message, "duplicate") ||
		strings.Contains(message, "unique") ||
		strings.Contains(message, "constraint")
	return (strings.Contains(message, "idx_users_email_canonical_unique") && hasUniqueSignal) ||
		(strings.Contains(message, "idx_user_login_identifiers_identifier") && hasUniqueSignal) ||
		(strings.Contains(message, "user_login_identifiers") && hasUniqueSignal) ||
		(strings.Contains(message, "email_canonical") && hasUniqueSignal) ||
		(strings.Contains(message, "users.username") && hasUniqueSignal) ||
		(strings.Contains(message, "idx_users_username") && hasUniqueSignal)
}

func IsAdmin(userId int) bool {
	user := User{Id: userId}
	if err := DB.Select("role").First(&user).Error; err != nil {
		return false
	}
	return user.Role >= common.RoleAdminUser
}

func ValidateAccessToken(token string) (*User, error) {
	user := &User{}
	if err := DB.Where("access_token = ?", token).First(user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("access token is invalid")
		}
		return nil, fmt.Errorf("%w: %v", ErrDatabase, err)
	}
	return user, nil
}

func GetUserQuota(id int, fromDB bool) (quota int, err error) {
	var user User
	if !fromDB {
		userCache, cacheErr := CacheGetUserById(id)
		if cacheErr == nil && userCache != nil {
			return userCache.Quota, nil
		}
	}
	err = DB.Select("quota").First(&user, id).Error
	return user.Quota, err
}

func GetUserUsedQuota(id int) (quota int, err error) {
	var user User
	err = DB.Select("used_quota").First(&user, id).Error
	return user.UsedQuota, err
}

func GetUserEmail(id int) (email string, err error) {
	var user User
	err = DB.Select("email").First(&user, id).Error
	return user.Email, err
}

func GetUserGroup(id int, fromDB bool) (group string, err error) {
	var user User
	if !fromDB {
		userCache, cacheErr := CacheGetUserById(id)
		if cacheErr == nil && userCache != nil {
			return strings.TrimSpace(userCache.Group), nil
		}
	}
	err = DB.Select("group").First(&user, id).Error
	return strings.TrimSpace(user.Group), err
}

func GetUserSetting(id int, fromDB bool) (settingMap dto.UserSetting, err error) {
	var user User
	if !fromDB {
		userCache, cacheErr := CacheGetUserById(id)
		if cacheErr == nil && userCache != nil {
			if userCache.Setting != "" {
				_ = json.Unmarshal([]byte(userCache.Setting), &settingMap)
			}
			return settingMap, nil
		}
	}
	err = DB.Select("setting").First(&user, id).Error
	if err != nil {
		return settingMap, err
	}
	if user.Setting != "" {
		err = json.Unmarshal([]byte(user.Setting), &settingMap)
	}
	return settingMap, err
}

func IncreaseUserQuota(id int, quota int, db bool) (err error) {
	if quota == 0 {
		return nil
	}
	if db {
		err = increaseUserQuota(id, quota)
		if err != nil {
			return err
		}
		return CacheUpdateUserQuota(id)
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUserQuota, id, quota)
		if common.RedisEnabled {
			if err := cacheIncrUserQuota(id, int64(quota)); err != nil {
				common.SysLog("failed to increase user quota cache: " + err.Error())
			}
		}
		return nil
	}
	if err = increaseUserQuota(id, quota); err != nil {
		return err
	}
	return CacheUpdateUserQuota(id)
}

func increaseUserQuota(id int, quota int) (err error) {
	return DB.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", quota)).Error
}

func DecreaseUserQuota(id int, quota int, db bool) (err error) {
	if quota == 0 {
		return nil
	}
	if db {
		err = decreaseUserQuota(id, quota)
		if err != nil {
			return err
		}
		return CacheUpdateUserQuota(id)
	}
	if common.BatchUpdateEnabled {
		addNewRecord(BatchUpdateTypeUserQuota, id, -quota)
		if common.RedisEnabled {
			if err := cacheDecrUserQuota(id, int64(quota)); err != nil {
				common.SysLog("failed to decrease user quota cache: " + err.Error())
			}
		}
		return nil
	}
	if err = decreaseUserQuota(id, quota); err != nil {
		return err
	}
	return CacheUpdateUserQuota(id)
}

func decreaseUserQuota(id int, quota int) (err error) {
	return DB.Model(&User{}).Where("id = ? AND quota >= ?", id, quota).Update("quota", gorm.Expr("quota - ?", quota)).Error
}

func DeltaUpdateUserQuota(id int, delta int) (err error) {
	if delta > 0 {
		return IncreaseUserQuota(id, delta, true)
	}
	if delta < 0 {
		return DecreaseUserQuota(id, -delta, true)
	}
	return nil
}

func GetRootUser() (user *User) {
	user = &User{}
	_ = DB.Where("role = ?", common.RoleRootUser).First(user).Error
	return user
}

func UpdateUserLastLoginAt(id int) {
	_ = DB.Model(&User{}).Where("id = ?", id).Update("last_login_at", common.GetTimestamp()).Error
}

func UpdateUserUsedQuotaAndRequestCount(id int, quota int) {
	gopool.Go(func() {
		updateUserUsedQuotaAndRequestCount(id, quota, 1)
	})
}

func updateUserUsedQuotaAndRequestCount(id int, quota int, count int) {
	_ = DB.Model(&User{}).Where("id = ?", id).Updates(map[string]interface{}{
		"used_quota":    gorm.Expr("used_quota + ?", quota),
		"request_count": gorm.Expr("request_count + ?", count),
	}).Error
	_ = CacheUpdateUserQuota(id)
}

func updateUserUsedQuota(id int, quota int) {
	_ = DB.Model(&User{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", quota)).Error
}

func updateUserRequestCount(id int, count int) {
	_ = DB.Model(&User{}).Where("id = ?", id).Update("request_count", gorm.Expr("request_count + ?", count)).Error
}

func GetUsernameById(id int, fromDB bool) (username string, err error) {
	if id <= 0 {
		return "", nil
	}
	var user User
	if !fromDB {
		userCache, cacheErr := CacheGetUserById(id)
		if cacheErr == nil && userCache != nil {
			return userCache.Username, nil
		}
	}
	err = DB.Select("username").First(&user, id).Error
	return user.Username, err
}

func IsLinuxDOIdAlreadyTaken(linuxDOId string) bool {
	return DB.Unscoped().Where("linux_do_id = ?", linuxDOId).First(&User{}).RowsAffected > 0
}

func (user *User) FillUserByLinuxDOId() error {
	return DB.First(user, "linux_do_id = ?", user.LinuxDOId).Error
}

func (user *User) FillUserByUsername() error {
	return DB.First(user, "username = ?", user.Username).Error
}

func RootUserExists() bool {
	var count int64
	_ = DB.Model(&User{}).Where("role = ?", common.RoleRootUser).Count(&count).Error
	return count > 0
}

type NullString = sql.NullString
