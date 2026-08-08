package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"gorm.io/gorm"
)

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

func IsLoginIdentifierTakenByOther(username string, email string, userID int) (bool, error) {
	return isLoginIdentifierTakenByOtherWithTx(DB, username, email, userID)
}

func isLoginIdentifierTakenByOtherWithTx(tx *gorm.DB, username string, email string, userID int) (bool, error) {
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
		Where("user_id <> ? AND identifier IN ?", userID, identifiers).
		First(&loginIdentifier)
	if result.Error == nil {
		return true, nil
	}
	if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return false, result.Error
	}

	var user User
	result = tx.Unscoped().
		Where("id <> ? AND (username IN ? OR email_canonical IN ?)", userID, identifiers, identifiers).
		First(&user)
	if result.Error == nil {
		return true, nil
	}
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, result.Error
}

func syncUserLoginIdentifiersWithTx(tx *gorm.DB, userID int, username string, email string) error {
	if userID == 0 {
		return errors.New("user id is empty")
	}
	if hasDuplicateUserLoginIdentifiers(username, email) {
		return ErrUserLoginIdentifierTaken
	}
	identifiers := getUserLoginIdentifiers(username, email)
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).
		Unscoped().
		Where("user_id = ?", userID).
		Delete(&UserLoginIdentifier{}).Error; err != nil {
		return err
	}
	for identifier, kind := range identifiers {
		if err := tx.Create(&UserLoginIdentifier{
			UserId:     userID,
			Identifier: identifier,
			Kind:       kind,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func SyncUserLoginIdentifiers(userID int) error {
	var user User
	if err := DB.Unscoped().First(&user, userID).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		return syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email)
	})
}

func setUserEmailIfEmptyWithTx(tx *gorm.DB, userID int, email string) (bool, error) {
	email = NormalizeUserEmail(email)
	if email == "" {
		return false, nil
	}
	if err := common.Validate.Var(email, "email"); err != nil {
		return false, err
	}

	updated := false
	err := withNormalizedEmailLock(tx, email, func(tx *gorm.DB) error {
		var user User
		if err := tx.First(&user, userID).Error; err != nil {
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

func (user *User) FillUserByUsername() error {
	if strings.TrimSpace(user.Username) == "" {
		return errors.New("username is empty")
	}
	return DB.First(user, "username = ?", strings.TrimSpace(user.Username)).Error
}

func (user *User) FillUserByUsernameOrEmail() error {
	identifier := strings.TrimSpace(user.Username)
	if err := user.FillUserByUsername(); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	user.Email = NormalizeUserEmail(identifier)
	return user.FillUserByEmail()
}

func (user *User) InsertPreserveQuota(inviterID int) error {
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return withNormalizedEmailLock(tx, user.Email, func(tx *gorm.DB) error {
			if err := user.prepareForInsert(tx); err != nil {
				return err
			}
			user.AffCode = common.GetRandomString(4)
			if user.Setting == "" {
				user.SetSetting(dto.UserSetting{})
			}
			if err := tx.Create(user).Error; err != nil {
				return err
			}
			return syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email)
		})
	}); err != nil {
		return err
	}
	user.finishInsert(inviterID)
	return nil
}

func (user *User) EditWithTransactionHook(updatePassword bool, hook func(tx *gorm.DB) error, updateEmail ...bool) error {
	updateEmailRequested := len(updateEmail) > 0 && updateEmail[0]
	var previousAuthVersion int64
	if err := DB.Model(&User{}).Where("id = ?", user.Id).Select("auth_version").Find(&previousAuthVersion).Error; err != nil {
		return err
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		var current User
		if err := tx.First(&current, user.Id).Error; err != nil {
			return err
		}

		newUser := *user
		if newUser.Username == "" {
			newUser.Username = current.Username
		}
		if !updateEmailRequested {
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

		authChanged := (updatePassword && current.Password != newUser.Password) ||
			current.Group != newUser.Group ||
			current.Email != newUser.Email
		if authChanged {
			newUser.AuthVersion, err = IncrementUserAuthVersionWithTx(tx, user.Id)
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
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return err
		}
		if err := syncUserLoginIdentifiersWithTx(tx, user.Id, newUser.Username, newUser.Email); err != nil {
			return err
		}
		if hook != nil {
			if err := hook(tx); err != nil {
				return err
			}
		}
		return tx.First(user, user.Id).Error
	}); err != nil {
		return err
	}
	return FinalizeUserAuthChange(*user, previousAuthVersion, "user_security_changed")
}

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

func ensureUserEmailCanonicalUniqueIndex() error {
	if !DB.Migrator().HasColumn(&User{}, "email_canonical") {
		if err := DB.Migrator().AddColumn(&User{}, "EmailCanonical"); err != nil {
			return err
		}
	}
	if err := DB.Model(&User{}).
		Where("email IS NULL OR TRIM(email) = ''").
		Update("email_canonical", nil).Error; err != nil {
		return err
	}
	if err := DB.Model(&User{}).
		Where("email IS NOT NULL AND TRIM(email) <> ''").
		Update("email_canonical", gorm.Expr("LOWER(TRIM(email))")).Error; err != nil {
		return err
	}

	type duplicateEmail struct {
		EmailCanonical string
		Count          int64
	}
	var duplicate duplicateEmail
	if err := DB.Model(&User{}).
		Select("email_canonical, COUNT(*) AS count").
		Where("email_canonical IS NOT NULL AND email_canonical <> ''").
		Group("email_canonical").
		Having("COUNT(*) > 1").
		Limit(1).
		Scan(&duplicate).Error; err != nil {
		return err
	}
	if duplicate.Count > 1 {
		return fmt.Errorf("duplicate user email exists before creating unique index: %s", duplicate.EmailCanonical)
	}

	const indexName = "idx_users_email_canonical_unique"
	if DB.Migrator().HasIndex(&User{}, indexName) {
		return nil
	}
	return DB.Exec("CREATE UNIQUE INDEX " + indexName + " ON users (email_canonical)").Error
}

func ensureUserLoginIdentifiers() error {
	if err := DB.AutoMigrate(&UserLoginIdentifier{}); err != nil {
		return err
	}
	if err := rejectExistingUserLoginIdentifierConflicts(); err != nil {
		return err
	}
	var users []User
	if err := DB.Unscoped().Find(&users).Error; err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).
			Unscoped().
			Delete(&UserLoginIdentifier{}).Error; err != nil {
			return err
		}
		for _, user := range users {
			if err := syncUserLoginIdentifiersWithTx(tx, user.Id, user.Username, user.Email); err != nil {
				return err
			}
		}
		return nil
	})
}

func rejectExistingUserLoginIdentifierConflicts() error {
	var users []User
	if err := DB.Unscoped().Select("id", "username", "email_canonical").Find(&users).Error; err != nil {
		return err
	}
	ownerByIdentifier := map[string]int{}
	for _, user := range users {
		email := ""
		if user.EmailCanonical != nil {
			email = *user.EmailCanonical
		}
		if hasDuplicateUserLoginIdentifiers(user.Username, email) {
			return fmt.Errorf("duplicate user login identifier exists on user %d: %s", user.Id, strings.TrimSpace(user.Username))
		}
		for identifier := range getUserLoginIdentifiers(user.Username, email) {
			if ownerID, ok := ownerByIdentifier[identifier]; ok && ownerID != user.Id {
				return fmt.Errorf("duplicate user login identifier exists before syncing login identifiers: %s", identifier)
			}
			ownerByIdentifier[identifier] = user.Id
		}
	}
	return nil
}
