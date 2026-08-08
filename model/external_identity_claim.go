package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ExternalIdentityProviderGitHub   = "github"
	ExternalIdentityProviderDiscord  = "discord"
	ExternalIdentityProviderOIDC     = "oidc"
	ExternalIdentityProviderWeChat   = "wechat"
	ExternalIdentityProviderTelegram = "telegram"
	ExternalIdentityProviderLinuxDO  = "linuxdo"
)

var ErrExternalIdentityAlreadyClaimed = errors.New("external identity is already claimed")
var ErrExternalIdentityAmbiguous = errors.New("external identity is ambiguous")

// ExternalIdentityClaim is the durable ownership record for an identity issued
// by an external provider. The two unique indexes make both the provider
// subject and the user's provider slot single-owner without relying on a
// check-then-update sequence.
type ExternalIdentityClaim struct {
	Id        int64     `json:"id" gorm:"primaryKey"`
	Provider  string    `json:"provider" gorm:"type:varchar(32);not null;uniqueIndex:idx_external_identity_subject,priority:1;uniqueIndex:idx_external_identity_user,priority:1"`
	Subject   string    `json:"subject" gorm:"type:varchar(256);not null;uniqueIndex:idx_external_identity_subject,priority:2"`
	UserId    int       `json:"user_id" gorm:"not null;index;uniqueIndex:idx_external_identity_user,priority:2"`
	CreatedAt time.Time `json:"created_at"`
}

func (ExternalIdentityClaim) TableName() string {
	return "external_identity_claims"
}

// ClaimExternalIdentityWithTx atomically claims a provider subject for one
// user. Repeating the exact mapping is idempotent; every competing subject or
// user is rejected. Ownership is read back instead of trusting RowsAffected,
// whose duplicate-key semantics differ between supported databases.
func ClaimExternalIdentityWithTx(tx *gorm.DB, provider, subject string, userId int) error {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	if tx == nil || provider == "" || subject == "" || userId == 0 {
		return errors.New("external identity claim is invalid")
	}
	if err := ensureExternalIdentityNotOwnedByAnotherActiveUserWithTx(tx, provider, subject, userId); err != nil {
		return err
	}

	claim := ExternalIdentityClaim{Provider: provider, Subject: subject, UserId: userId}
	result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&claim)
	if result.Error != nil {
		return result.Error
	}
	var subjectOwner ExternalIdentityClaim
	if err := tx.Where("provider = ? AND subject = ?", provider, subject).First(&subjectOwner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrExternalIdentityAlreadyClaimed
		}
		return err
	}
	if subjectOwner.UserId != userId {
		return ErrExternalIdentityAlreadyClaimed
	}

	var userClaim ExternalIdentityClaim
	if err := tx.Where("provider = ? AND user_id = ?", provider, userId).First(&userClaim).Error; err != nil {
		return err
	}
	if userClaim.Subject != subject {
		return ErrExternalIdentityAlreadyClaimed
	}
	return nil
}

func ReleaseExternalIdentityWithTx(tx *gorm.DB, provider string, userId int) error {
	provider = strings.TrimSpace(provider)
	if tx == nil || provider == "" || userId == 0 {
		return errors.New("external identity release is invalid")
	}
	return tx.Where("provider = ? AND user_id = ?", provider, userId).
		Delete(&ExternalIdentityClaim{}).Error
}

func releaseAllExternalIdentitiesWithTx(tx *gorm.DB, userId int) error {
	if tx == nil || userId == 0 {
		return errors.New("external identity release is invalid")
	}
	return tx.Where("user_id = ?", userId).Delete(&ExternalIdentityClaim{}).Error
}

func externalIdentityUserColumn(provider string) (string, bool) {
	switch strings.TrimSpace(provider) {
	case ExternalIdentityProviderGitHub:
		return "github_id", true
	case ExternalIdentityProviderDiscord:
		return "discord_id", true
	case ExternalIdentityProviderOIDC:
		return "oidc_id", true
	case ExternalIdentityProviderWeChat:
		return "wechat_id", true
	case ExternalIdentityProviderTelegram:
		return "telegram_id", true
	case ExternalIdentityProviderLinuxDO:
		return "linux_do_id", true
	default:
		return "", false
	}
}

func setUserExternalIdentityField(user *User, provider string, subject string) {
	switch strings.TrimSpace(provider) {
	case ExternalIdentityProviderGitHub:
		user.GitHubId = subject
	case ExternalIdentityProviderDiscord:
		user.DiscordId = subject
	case ExternalIdentityProviderOIDC:
		user.OidcId = subject
	case ExternalIdentityProviderWeChat:
		user.WeChatId = subject
	case ExternalIdentityProviderTelegram:
		user.TelegramId = subject
	case ExternalIdentityProviderLinuxDO:
		user.LinuxDOId = subject
	}
}

func getUserExternalIdentityField(user *User, provider string) string {
	if user == nil {
		return ""
	}
	switch strings.TrimSpace(provider) {
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

func (user *User) ClaimExternalIdentity(provider string, subject string) error {
	if user == nil || user.Id == 0 {
		return errors.New("external identity binding is invalid")
	}
	var previousAuthVersion int64
	if err := DB.Model(&User{}).Where("id = ?", user.Id).Select("auth_version").Find(&previousAuthVersion).Error; err != nil {
		return err
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return user.ClaimExternalIdentityWithTx(tx, provider, subject)
	}); err != nil {
		return err
	}
	return FinalizeUserAuthChange(*user, previousAuthVersion, "user_security_changed")
}

func (user *User) ClaimExternalIdentityWithTx(tx *gorm.DB, provider string, subject string) error {
	provider = strings.TrimSpace(provider)
	subject = strings.TrimSpace(subject)
	column, ok := externalIdentityUserColumn(provider)
	if tx == nil || user == nil || user.Id == 0 || !ok || subject == "" {
		return errors.New("external identity binding is invalid")
	}
	var current User
	if err := tx.Select([]string{"id", "auth_version", column}).Where("id = ?", user.Id).First(&current).Error; err != nil {
		return err
	}
	if getUserExternalIdentityField(&current, provider) != subject {
		if _, err := IncrementUserAuthVersionWithTx(tx, user.Id); err != nil {
			return err
		}
	}
	if err := ReleaseExternalIdentityWithTx(tx, provider, user.Id); err != nil {
		return err
	}
	if err := ClaimExternalIdentityWithTx(tx, provider, subject, user.Id); err != nil {
		return err
	}
	if err := tx.Model(&User{}).Where("id = ?", user.Id).Update(column, subject).Error; err != nil {
		return err
	}
	setUserExternalIdentityField(user, provider, subject)
	return tx.First(user, user.Id).Error
}

func ensureExternalIdentityNotOwnedByAnotherActiveUserWithTx(tx *gorm.DB, provider string, subject string, userID int) error {
	column, ok := externalIdentityUserColumn(provider)
	if tx == nil || !ok || strings.TrimSpace(subject) == "" || userID <= 0 {
		return errors.New("external identity binding is invalid")
	}
	var ownerIDs []int
	if err := tx.Model(&User{}).Where(column+" = ?", subject).Pluck("id", &ownerIDs).Error; err != nil {
		return err
	}
	for _, ownerID := range ownerIDs {
		if ownerID != userID {
			return ErrExternalIdentityAlreadyClaimed
		}
	}
	return nil
}

func GetUniqueUserByExternalIdentity(provider string, subject string) (*User, error) {
	column, ok := externalIdentityUserColumn(provider)
	if !ok || strings.TrimSpace(subject) == "" {
		return nil, errors.New("external identity binding is invalid")
	}
	var users []User
	if err := DB.Where(column+" = ?", subject).Limit(2).Find(&users).Error; err != nil {
		return nil, err
	}
	switch len(users) {
	case 0:
		return nil, gorm.ErrRecordNotFound
	case 1:
		return &users[0], nil
	default:
		return nil, ErrExternalIdentityAmbiguous
	}
}

// InitializeExternalIdentityClaims imports legacy external identity bindings
// after the claim table is migrated. Ambiguous legacy records are kept for
// manual resolution and skipped so an upgrade cannot fail to start.
func InitializeExternalIdentityClaims() error {
	var users []User
	if err := DB.Select("id", "github_id", "discord_id", "oidc_id", "wechat_id", "telegram_id", "linux_do_id").
		Where("github_id <> ? OR discord_id <> ? OR oidc_id <> ? OR wechat_id <> ? OR telegram_id <> ? OR linux_do_id <> ?", "", "", "", "", "", "").
		Find(&users).Error; err != nil {
		return err
	}
	type legacyBinding struct {
		provider string
		subject  string
		userID   int
	}
	bindings := make([]legacyBinding, 0, len(users)*6)
	bindingOwners := make(map[string]map[int]struct{})
	for _, user := range users {
		for _, binding := range []legacyBinding{
			{provider: ExternalIdentityProviderGitHub, subject: user.GitHubId, userID: user.Id},
			{provider: ExternalIdentityProviderDiscord, subject: user.DiscordId, userID: user.Id},
			{provider: ExternalIdentityProviderOIDC, subject: user.OidcId, userID: user.Id},
			{provider: ExternalIdentityProviderWeChat, subject: user.WeChatId, userID: user.Id},
			{provider: ExternalIdentityProviderTelegram, subject: user.TelegramId, userID: user.Id},
			{provider: ExternalIdentityProviderLinuxDO, subject: user.LinuxDOId, userID: user.Id},
		} {
			binding.subject = strings.TrimSpace(binding.subject)
			if binding.subject == "" {
				continue
			}
			bindings = append(bindings, binding)
			key := binding.provider + "\x00" + binding.subject
			if bindingOwners[key] == nil {
				bindingOwners[key] = make(map[int]struct{})
			}
			bindingOwners[key][binding.userID] = struct{}{}
		}
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		for _, binding := range bindings {
			key := binding.provider + "\x00" + binding.subject
			if len(bindingOwners[key]) > 1 {
				common.SysError(fmt.Sprintf("skipping ambiguous legacy external identity provider=%s user_count=%d", binding.provider, len(bindingOwners[key])))
				continue
			}
			if err := ClaimExternalIdentityWithTx(tx, binding.provider, binding.subject, binding.userID); err != nil {
				return fmt.Errorf("backfill %s identity for user %d: %w", binding.provider, binding.userID, err)
			}
		}
		return nil
	})
}
