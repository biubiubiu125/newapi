package model

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const UserNameMaxLength = 20
const RegisterUserNameMaxLength = 12

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
	AccessToken             *string        `json:"access_token" gorm:"type:char(32);column:access_token;uniqueIndex"`
	Quota                   int            `json:"quota" gorm:"type:int;default:0"`
	UsedQuota               int            `json:"used_quota" gorm:"type:int;default:0;column:used_quota"`
	RequestCount            int            `json:"request_count" gorm:"type:int;default:0;"`
	Group                   string         `json:"group" gorm:"type:varchar(64);default:'default'"`
	ReferralInviterId       int            `json:"referral_inviter_id,omitempty" gorm:"-"`
	ReferralInviterUsername string         `json:"referral_inviter_username,omitempty" gorm:"-"`
	DeletedAt               gorm.DeletedAt `gorm:"index"`
	LinuxDOId               string         `json:"linux_do_id" gorm:"column:linux_do_id;index"`
	Setting                 string         `json:"setting" gorm:"type:text;column:setting"`
	Remark                  string         `json:"remark,omitempty" gorm:"type:varchar(255)" validate:"max=255"`
	StripeCustomer          string         `json:"stripe_customer" gorm:"type:varchar(64);column:stripe_customer;index"`
	CreatedAt               int64          `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	LastLoginAt             int64          `json:"last_login_at" gorm:"default:0;column:last_login_at"`
}

func NormalizeUserEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
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
		Group:    user.Group,
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
		setting.SidebarModules = generateDefaultSidebarConfigForRole(role)
	}
	user.SetSetting(setting)
}

func generateDefaultSidebarConfigForRole(userRole int) string {
	defaultConfig := map[string]interface{}{}
	defaultConfig["chat"] = map[string]interface{}{
		"enabled":    true,
		"playground": true,
		"chat":       true,
	}
	defaultConfig["console"] = map[string]interface{}{
		"enabled":    true,
		"detail":     true,
		"token":      true,
		"log":        true,
		"midjourney": true,
		"task":       true,
	}
	defaultConfig["personal"] = map[string]interface{}{
		"enabled":  true,
		"topup":    true,
		"personal": true,
	}

	if userRole == common.RoleAdminUser {
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    false,
		}
	} else if userRole == common.RoleRootUser {
		defaultConfig["admin"] = map[string]interface{}{
			"enabled":    true,
			"channel":    true,
			"models":     true,
			"redemption": true,
			"user":       true,
			"setting":    true,
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
	var user User
	var result *gorm.DB
	email = NormalizeUserEmail(email)
	if email == "" {
		result = DB.Unscoped().Where("username = ?", username).Find(&user)
	} else {
		result = DB.Unscoped().Where("username = ? or email_canonical = ?", username, email).Find(&user)
	}
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
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

	return users, total, nil
}

func SearchUsers(keyword string, group string, startIdx int, num int) ([]*User, int64, error) {
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

	keywordInt, err := strconv.Atoi(keyword)
	if err == nil {
		likeCondition = "users.id = ? OR " + likeCondition
		if group != "" {
			query = query.Where("("+likeCondition+") AND "+groupColumn+" = ?",
				keywordInt, "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%", group)
		} else {
			query = query.Where(likeCondition,
				keywordInt, "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
		}
	} else {
		if group != "" {
			query = query.Where("("+likeCondition+") AND "+groupColumn+" = ?",
				"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%", group)
		} else {
			query = query.Where(likeCondition,
				"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
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

	return users, total, nil
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
	err := DB.Unscoped().Delete(&User{}, "id = ?", id).Error
	return err
}

func (user *User) Insert(_ int) error {
	var err error
	user.normalizeEmailForPersistence()
	if user.Password != "" {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	user.Quota = common.QuotaForNewUser

	user.initializeDefaultSettingForRole()

	result := DB.Create(user)
	if result.Error != nil {
		return result.Error
	}

	if common.QuotaForNewUser > 0 {
		RecordLog(user.Id, LogTypeSystem, fmt.Sprintf("新用户注册赠送 %s", logger.LogQuota(common.QuotaForNewUser)))
	}
	return nil
}

func (user *User) InsertWithTx(tx *gorm.DB, _ int) error {
	var err error
	user.normalizeEmailForPersistence()
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
	if updatePassword {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}
	newUser := *user
	DB.First(&user, user.Id)
	if err = DB.Model(user).Updates(newUser).Error; err != nil {
		return err
	}
	return updateUserCache(*user)
}

func (user *User) Edit(updatePassword bool) error {
	var err error
	if updatePassword {
		user.Password, err = common.Password2Hash(user.Password)
		if err != nil {
			return err
		}
	}

	newUser := *user
	updates := map[string]interface{}{
		"username":     newUser.Username,
		"display_name": newUser.DisplayName,
		"group":        newUser.Group,
		"remark":       newUser.Remark,
	}
	if updatePassword {
		updates["password"] = newUser.Password
	}

	DB.First(&user, user.Id)
	if err = DB.Model(user).Updates(updates).Error; err != nil {
		return err
	}
	return updateUserCache(*user)
}

func (user *User) ClearBinding(bindingType string) error {
	if user.Id == 0 {
		return errors.New("user id is empty")
	}

	if bindingType == "email" {
		if err := DB.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]any{
			"email":           "",
			"email_canonical": nil,
		}).Error; err != nil {
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
	return DB.Unscoped().Delete(user).Error
}

func (user *User) ValidateAndFill() (err error) {
	if user.Username == "" || user.Password == "" {
		return ErrUserEmptyCredentials
	}

	originalPassword := user.Password
	if err := user.FillUserByUsername(); err != nil {
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
	return DB.Model(&User{}).Where("email_canonical = ?", email).Update("password", hashedPassword).Error
}

var (
	ErrUserDisabled          = errors.New("user disabled")
	ErrUserDeleted           = errors.New("user deleted")
	ErrUserPasswordIncorrect = errors.New("password incorrect")
)

func IsUserEmailUniqueError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "idx_users_email_canonical_unique") ||
		(strings.Contains(message, "email_canonical") &&
			(strings.Contains(message, "duplicate") ||
				strings.Contains(message, "unique") ||
				strings.Contains(message, "constraint")))
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
			return userCache.Group, nil
		}
	}
	err = DB.Select("group").First(&user, id).Error
	return user.Group, err
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
