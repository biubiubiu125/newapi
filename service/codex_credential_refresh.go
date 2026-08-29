package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

type CodexCredentialRefreshOptions struct {
	ResetCaches bool
}

var ErrCodexCredentialCacheRefresh = errors.New("codex credential cache refresh failed")
var refreshCodexOAuthTokenWithProxyAndSettings = RefreshCodexOAuthTokenWithProxyAndSettings
var codexCredentialRefreshGroup singleflight.Group

const (
	codexCredentialRefreshLeaseSeconds = 45
	codexCredentialRefreshPollInterval = 100 * time.Millisecond
)

type codexCredentialRefreshResult struct {
	oauthKey *CodexOAuthKey
	channel  *model.Channel
	err      error
}

type codexCredentialRefreshLease struct {
	lockType string
	taskID   string
	lockedBy string
}

func codexCredentialRefreshLockType(channelID int) string {
	return fmt.Sprintf("codex-credential-refresh:%d", channelID)
}

func codexCredentialRefreshLockTaskID(channelID int) string {
	return fmt.Sprintf("channel:%d", channelID)
}

func acquireCodexCredentialRefreshLease(ctx context.Context, channelID int) (*codexCredentialRefreshLease, bool, error) {
	if channelID <= 0 {
		return nil, false, errors.New("invalid channel id")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	now := common.GetTimestamp()
	lockType := codexCredentialRefreshLockType(channelID)
	lockOwner, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return nil, false, err
	}
	lock := &model.SystemTaskLock{
		Type:        lockType,
		TaskID:      codexCredentialRefreshLockTaskID(channelID),
		LockedBy:    lockOwner,
		LockedUntil: now + codexCredentialRefreshLeaseSeconds,
		UpdatedAt:   now,
	}
	createErr := model.DB.WithContext(ctx).Create(lock).Error
	if createErr == nil {
		return &codexCredentialRefreshLease{
			lockType: lockType,
			taskID:   lock.TaskID,
			lockedBy: lockOwner,
		}, true, nil
	}

	var existing model.SystemTaskLock
	existingErr := model.DB.WithContext(ctx).Where("type = ?", lockType).First(&existing).Error
	if existingErr != nil {
		if errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return nil, false, createErr
		}
		return nil, false, existingErr
	}
	if existing.LockedUntil >= now {
		return nil, false, nil
	}

	result := model.DB.WithContext(ctx).
		Model(&model.SystemTaskLock{}).
		Where("type = ? AND locked_until < ?", lockType, now).
		Updates(map[string]any{
			"task_id":      lock.TaskID,
			"locked_by":    lockOwner,
			"locked_until": now + codexCredentialRefreshLeaseSeconds,
			"updated_at":   now,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, false, nil
	}
	return &codexCredentialRefreshLease{
		lockType: lockType,
		taskID:   lock.TaskID,
		lockedBy: lockOwner,
	}, true, nil
}

func releaseCodexCredentialRefreshLease(lease *codexCredentialRefreshLease) error {
	if lease == nil {
		return nil
	}
	return model.DB.Where("type = ? AND task_id = ? AND locked_by = ?", lease.lockType, lease.taskID, lease.lockedBy).
		Delete(&model.SystemTaskLock{}).Error
}

func waitForCodexCredentialRefreshResolution(ctx context.Context, channelID int, originalKey string) (bool, *model.Channel, *CodexOAuthKey, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalizedOriginalKey := strings.TrimSpace(originalKey)
	lockType := codexCredentialRefreshLockType(channelID)

	for {
		if err := ctx.Err(); err != nil {
			return false, nil, nil, err
		}

		currentChannel := &model.Channel{Id: channelID}
		if err := model.DB.WithContext(ctx).First(currentChannel, "id = ?", channelID).Error; err != nil {
			return false, nil, nil, err
		}
		if currentChannel.Type != constant.ChannelTypeCodex {
			return false, nil, nil, fmt.Errorf("channel type is not Codex")
		}

		currentKey := strings.TrimSpace(currentChannel.Key)
		if currentKey != normalizedOriginalKey {
			currentOAuthKey, err := parseCodexOAuthKey(currentKey)
			if err != nil {
				return false, nil, nil, fmt.Errorf("codex channel credential changed during refresh and current credential is invalid: %w", err)
			}
			return true, currentChannel, currentOAuthKey, nil
		}

		var existing model.SystemTaskLock
		err := model.DB.WithContext(ctx).Where("type = ?", lockType).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && existing.LockedUntil < common.GetTimestamp()) {
			return false, nil, nil, nil
		}
		if err != nil {
			return false, nil, nil, err
		}

		select {
		case <-ctx.Done():
			return false, nil, nil, ctx.Err()
		case <-time.After(codexCredentialRefreshPollInterval):
		}
	}
}

func refreshCodexRuntimeChannelCache() error {
	var cacheErr error
	for attempt := 0; attempt < 3; attempt++ {
		cacheErr = model.InitChannelCacheWithError()
		if cacheErr == nil {
			return nil
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}
	return cacheErr
}

type CodexOAuthKey struct {
	IDToken      string `json:"id_token,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`

	AccountID   string `json:"account_id,omitempty"`
	LastRefresh string `json:"last_refresh,omitempty"`
	Email       string `json:"email,omitempty"`
	Type        string `json:"type,omitempty"`
	Expired     string `json:"expired,omitempty"`
}

func UpdateCodexChannelCredentialIfUnchanged(channelID int, originalKey string, refreshedKey string) (bool, error) {
	return UpdateCodexChannelCredentialIfUnchangedWithContext(
		context.Background(),
		channelID,
		originalKey,
		refreshedKey,
	)
}

func UpdateCodexChannelCredentialIfUnchangedWithContext(
	ctx context.Context,
	channelID int,
	originalKey string,
	refreshedKey string,
) (bool, error) {
	if channelID <= 0 {
		return false, errors.New("invalid channel id")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result := model.DB.WithContext(ctx).
		Model(&model.Channel{}).
		Where(map[string]interface{}{"id": channelID, "key": originalKey}).
		Update("key", refreshedKey)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}

func parseCodexOAuthKey(raw string) (*CodexOAuthKey, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("codex channel: empty oauth key")
	}
	var key CodexOAuthKey
	if err := common.Unmarshal([]byte(raw), &key); err != nil {
		return nil, errors.New("codex channel: invalid oauth key json")
	}
	return &key, nil
}

func RefreshCodexChannelCredential(ctx context.Context, channelID int, opts CodexCredentialRefreshOptions) (*CodexOAuthKey, *model.Channel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if channelID <= 0 {
		return refreshCodexChannelCredential(ctx, channelID, opts)
	}

	resultChannel := codexCredentialRefreshGroup.DoChan(
		fmt.Sprintf("codex-channel-%d", channelID),
		func() (any, error) {
			oauthKey, channel, err := refreshCodexChannelCredential(ctx, channelID, opts)
			return codexCredentialRefreshResult{
				oauthKey: oauthKey,
				channel:  channel,
				err:      err,
			}, nil
		},
	)

	var result singleflight.Result
	select {
	case result = <-resultChannel:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	refreshResult, ok := result.Val.(codexCredentialRefreshResult)
	if !ok {
		return nil, nil, errors.New("codex credential refresh returned an invalid result")
	}

	// A caller requesting cache reset may have shared a refresh started by the
	// background task, which intentionally skips cache work. Compensate for
	// that here, and retry typed cache failures from the shared refresh.
	if opts.ResetCaches &&
		(result.Shared || errors.Is(refreshResult.err, ErrCodexCredentialCacheRefresh)) &&
		(refreshResult.err == nil || errors.Is(refreshResult.err, ErrCodexCredentialCacheRefresh)) {
		if err := refreshCodexRuntimeChannelCache(); err != nil {
			return refreshResult.oauthKey, refreshResult.channel,
				fmt.Errorf("%w: %v", ErrCodexCredentialCacheRefresh, err)
		}
		return refreshResult.oauthKey, refreshResult.channel, nil
	}
	return refreshResult.oauthKey, refreshResult.channel, refreshResult.err
}

func refreshCodexChannelCredential(ctx context.Context, channelID int, opts CodexCredentialRefreshOptions) (*CodexOAuthKey, *model.Channel, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ch := &model.Channel{Id: channelID}
	err := model.DB.WithContext(ctx).First(ch, "id = ?", channelID).Error
	if err != nil {
		return nil, nil, err
	}
	if ch == nil {
		return nil, nil, fmt.Errorf("channel not found")
	}
	if ch.Type != constant.ChannelTypeCodex {
		return nil, nil, fmt.Errorf("channel type is not Codex")
	}

	oauthKey, err := parseCodexOAuthKey(strings.TrimSpace(ch.Key))
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(oauthKey.RefreshToken) == "" {
		return nil, nil, fmt.Errorf("codex channel: refresh_token is required to refresh credential")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	for {
		lease, acquired, err := acquireCodexCredentialRefreshLease(ctx, ch.Id)
		if err != nil {
			return nil, nil, err
		}
		if acquired {
			defer func() {
				_ = releaseCodexCredentialRefreshLease(lease)
			}()
			break
		}

		changed, currentChannel, currentOAuthKey, waitErr := waitForCodexCredentialRefreshResolution(ctx, ch.Id, ch.Key)
		if waitErr != nil {
			return nil, nil, waitErr
		}
		if changed {
			if opts.ResetCaches {
				if err := refreshCodexRuntimeChannelCache(); err != nil {
					return currentOAuthKey, currentChannel, fmt.Errorf("%w: %v", ErrCodexCredentialCacheRefresh, err)
				}
			}
			return currentOAuthKey, currentChannel, nil
		}

		ch = &model.Channel{Id: channelID}
		err = model.DB.WithContext(ctx).First(ch, "id = ?", channelID).Error
		if err != nil {
			return nil, nil, err
		}
		if ch.Type != constant.ChannelTypeCodex {
			return nil, nil, fmt.Errorf("channel type is not Codex")
		}
		oauthKey, err = parseCodexOAuthKey(strings.TrimSpace(ch.Key))
		if err != nil {
			return nil, nil, err
		}
		if strings.TrimSpace(oauthKey.RefreshToken) == "" {
			return nil, nil, fmt.Errorf("codex channel: refresh_token is required to refresh credential")
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
	}

	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res, err := refreshCodexOAuthTokenWithProxyAndSettings(refreshCtx, oauthKey.RefreshToken, ch.GetSetting().Proxy, ch.GetSetting())
	if err != nil {
		return nil, nil, err
	}

	oauthKey.AccessToken = res.AccessToken
	oauthKey.RefreshToken = res.RefreshToken
	oauthKey.LastRefresh = time.Now().Format(time.RFC3339)
	oauthKey.Expired = res.ExpiresAt.Format(time.RFC3339)
	if strings.TrimSpace(oauthKey.Type) == "" {
		oauthKey.Type = "codex"
	}

	if strings.TrimSpace(oauthKey.AccountID) == "" {
		if accountID, ok := ExtractCodexAccountIDFromJWT(oauthKey.AccessToken); ok {
			oauthKey.AccountID = accountID
		}
	}
	if strings.TrimSpace(oauthKey.Email) == "" {
		if email, ok := ExtractEmailFromJWT(oauthKey.AccessToken); ok {
			oauthKey.Email = email
		}
	}

	encoded, err := common.Marshal(oauthKey)
	if err != nil {
		return nil, nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	updated, err := UpdateCodexChannelCredentialIfUnchangedWithContext(ctx, ch.Id, ch.Key, string(encoded))
	if err != nil {
		return nil, nil, err
	}
	cacheReset := false
	if !updated {
		currentChannel := &model.Channel{Id: ch.Id}
		if err := model.DB.WithContext(ctx).First(currentChannel, "id = ?", ch.Id).Error; err != nil {
			return nil, nil, fmt.Errorf("codex channel credential changed during refresh and current credential could not be loaded: %w", err)
		}
		if currentChannel.Type != constant.ChannelTypeCodex {
			return nil, nil, errors.New("codex channel credential changed during refresh")
		}
		currentOAuthKey, err := parseCodexOAuthKey(strings.TrimSpace(currentChannel.Key))
		if err != nil {
			return nil, nil, fmt.Errorf("codex channel credential changed during refresh and current credential is invalid: %w", err)
		}
		ch = currentChannel
		oauthKey = currentOAuthKey
		if opts.ResetCaches {
			if err := refreshCodexRuntimeChannelCache(); err != nil {
				return oauthKey, ch, fmt.Errorf("%w: %v", ErrCodexCredentialCacheRefresh, err)
			}
			cacheReset = true
		}
	} else {
		ch.Key = string(encoded)
	}

	if opts.ResetCaches && !cacheReset {
		if err := refreshCodexRuntimeChannelCache(); err != nil {
			// The credential is already durable. Return it to callers so a
			// provider retry can use the fresh token, while preserving a typed
			// error that lets the caller perform a compensating cache refresh.
			return oauthKey, ch, fmt.Errorf("%w: %v", ErrCodexCredentialCacheRefresh, err)
		}
	}

	return oauthKey, ch, nil
}
