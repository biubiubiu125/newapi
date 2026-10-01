package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type cacheQuotaResult int

const (
	cacheQuotaInsufficient cacheQuotaResult = iota
	cacheQuotaOK
	cacheQuotaMiss
)

const quotaIntegerCompareScript = `
local function cmp_int(a, b)
  if type(a) ~= 'string' or type(b) ~= 'string' then return nil end
  if string.match(a, '^-?%d+$') == nil or string.match(b, '^-?%d+$') == nil then return nil end
  local function norm(s)
    local neg = string.sub(s, 1, 1) == '-'
    if neg then s = string.sub(s, 2) end
    s = string.gsub(s, '^0+', '')
    if s == '' then return false, '0' end
    return neg, s
  end
  local an, av = norm(a)
  local bn, bv = norm(b)
  if an ~= bn then
    if an then return -1 else return 1 end
  end
  local c = 0
  if #av ~= #bv then
    if #av > #bv then c = 1 else c = -1 end
  elseif av ~= bv then
    if av > bv then c = 1 else c = -1 end
  end
  if an then return -c end
  return c
end
local function intarg(v)
  local s = tostring(v)
  if string.match(s, '^-?%d+$') == nil then return nil end
  local neg = string.sub(s, 1, 1) == '-'
  if neg then s = string.sub(s, 2) end
  s = string.gsub(s, '^0+', '')
  if s == '' then return '0' end
  if neg then return '-' .. s end
  return s
end
`

const userQuotaReserveScript = quotaIntegerCompareScript + `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local amount = intarg(ARGV[1])
local quota = redis.call('HGET', KEYS[1], 'Quota')
local order = nil
if amount ~= nil and string.sub(amount, 1, 1) ~= '-' then
  order = cmp_int(tostring(quota), amount)
end
if order == nil or order < 0 then
  return 0
end
local delta = '0'
if amount ~= '0' then delta = '-' .. amount end
redis.call('HINCRBY', KEYS[1], 'Quota', delta)
return 1`

const userQuotaDeltaScript = quotaIntegerCompareScript + `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local delta = intarg(ARGV[1])
if delta == nil then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'Quota', delta)
return 1`

// userQuotaCreditScript applies a positive quota credit without raising the
// cache to a stale ceiling. A missing hash stays missing. The credit is
// applied when current + delta still fits under the ceiling, including a
// live reserve that already sits below the pre-credit balance. If the cache
// is already above that pre-credit balance, adding the full delta would pass
// the ceiling, so the hash is left unchanged instead of being clamped upward.
const userQuotaCreditScript = quotaIntegerCompareScript + `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local delta = intarg(ARGV[1])
local ceiling = intarg(ARGV[4])
local expected = intarg(ARGV[5])
if delta == nil or ceiling == nil or expected == nil or string.sub(delta, 1, 1) == '-' or string.sub(expected, 1, 1) == '-' then
  return -1
end
local current = redis.call('HGET', KEYS[1], 'Quota')
if cmp_int(tostring(current), ceiling) == nil or cmp_int(tostring(current), expected) == nil then
  return -1
end
if cmp_int(tostring(current), ceiling) >= 0 or cmp_int(tostring(current), expected) > 0 then
  return 1
end
redis.call('HINCRBY', KEYS[1], 'Quota', delta)
return 1`

const tokenQuotaReserveScript = quotaIntegerCompareScript + `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
local amount = intarg(ARGV[1])
local remain = redis.call('HGET', KEYS[1], 'RemainQuota')
local order = nil
if amount ~= nil and string.sub(amount, 1, 1) ~= '-' then
  order = cmp_int(tostring(remain), amount)
end
if order == nil or order < 0 then
  return 0
end
local delta = '0'
if amount ~= '0' then delta = '-' .. amount end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', delta)
redis.call('HINCRBY', KEYS[1], 'UsedQuota', amount)
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[3])
return 1`

func quotaResultFromLua(result int, err error) (cacheQuotaResult, error) {
	if err != nil {
		return cacheQuotaMiss, err
	}
	switch result {
	case 1:
		return cacheQuotaOK, nil
	case 0:
		return cacheQuotaInsufficient, nil
	default:
		return cacheQuotaMiss, nil
	}
}

func cacheTryReserveUserQuota(userID int, amount int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaReserveScript,
		[]string{getUserCacheKey(userID)}, amount, userID, userCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyUserQuotaDelta(userID int, delta int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), userQuotaDeltaScript,
		[]string{getUserCacheKey(userID)}, delta, userID, userCacheSchemaVersion).Int()
	return quotaResultFromLua(result, err)
}

func cacheCreditUserQuota(userID int, delta int64, ceiling int64) error {
	if delta <= 0 || ceiling < delta {
		return nil
	}
	_, err := common.RDB.Eval(context.Background(), userQuotaCreditScript,
		[]string{getUserCacheKey(userID)}, delta, userID, userCacheSchemaVersion, ceiling, ceiling-delta).Int()
	return err
}

func cacheTryReserveTokenQuota(id int, key string, amount int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), tokenQuotaReserveScript,
		[]string{getTokenCacheKey(key)}, amount, id, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

func cacheApplyTokenQuotaDelta(id int, key string, delta int64) (cacheQuotaResult, error) {
	return cacheApplyTokenQuotaAccounting(id, key, delta, -delta)
}

const tokenQuotaAccountingScript = quotaIntegerCompareScript + `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return -1
end
local remainDelta = intarg(ARGV[1])
local usedDelta = intarg(ARGV[3])
if remainDelta == nil or usedDelta == nil then
  return -1
end
redis.call('HINCRBY', KEYS[1], 'RemainQuota', remainDelta)
redis.call('HINCRBY', KEYS[1], 'UsedQuota', usedDelta)
redis.call('HSET', KEYS[1], 'AccessedTime', ARGV[4])
return 1`

func cacheApplyTokenQuotaAccounting(id int, key string, remainDelta, usedDelta int64) (cacheQuotaResult, error) {
	if remainDelta == 0 && usedDelta == 0 {
		return cacheQuotaOK, nil
	}
	result, err := common.RDB.Eval(context.Background(), tokenQuotaAccountingScript,
		[]string{getTokenCacheKey(key)}, remainDelta, id, usedDelta, common.GetTimestamp()).Int()
	return quotaResultFromLua(result, err)
}

// persistUserQuotaDelta 把已在缓存侧预扣成功的增量立即落库。
// 批量入队会在 remain/quota 约束落地前返回成功，预扣窗口内可能超扣。
func persistUserQuotaDelta(id int, delta int64) error {
	query := DB.Model(&User{}).Where("id = ?", id)
	if delta < 0 {
		query = query.Where("quota >= ?", -delta)
	}
	result := query.Update("quota", gorm.Expr("quota + ?", delta))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func persistTokenQuotaDelta(id int, delta int64) error {
	query := DB.Model(&Token{}).Where("id = ?", id)
	if delta < 0 {
		query = query.Where("remain_quota >= ?", -delta)
	}
	result := query.Updates(
		map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota + ?", delta),
			"used_quota":    gorm.Expr("used_quota - ?", delta),
			"accessed_time": common.GetTimestamp(),
		},
	)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func reserveUserQuotaDB(id int, quota int64) (bool, error) {
	result := DB.Model(&User{}).
		Where("id = ? AND quota >= ?", id, quota).
		Update("quota", gorm.Expr("quota - ?", quota))
	return result.RowsAffected == 1, result.Error
}

func reserveTokenQuotaDB(id int, quota int64) (bool, error) {
	result := DB.Model(&Token{}).
		Where("id = ? AND remain_quota >= ?", id, quota).
		Updates(map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota - ?", quota),
			"used_quota":    gorm.Expr("used_quota + ?", quota),
			"accessed_time": common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// TryReserveUserQuota atomically checks and deducts a user's wallet quota.
// 缓存命中时以缓存余额为准，落库始终同步；Redis 异常或水合失败时降级为数据库条件更新。
func TryReserveUserQuota(id int, quota int) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return true, nil
	}
	if !common.RedisEnabled {
		return reserveUserQuotaDB(id, int64(quota))
	}

	result, err := cacheTryReserveUserQuota(id, int64(quota))
	if err == nil && result == cacheQuotaMiss {
		if _, hydrateErr := GetUserCache(id); hydrateErr == nil {
			result, err = cacheTryReserveUserQuota(id, int64(quota))
		}
	}
	if err != nil || result == cacheQuotaMiss {
		if err != nil {
			common.SysLog("user quota cache reserve unavailable, falling back to database: " + err.Error())
		}
		return reserveUserQuotaDB(id, int64(quota))
	}
	if result == cacheQuotaInsufficient {
		// 缓存偏低不能直接判余额不足。缓存可能还停在失败补偿之前，
		// 数据库够付时只走条件更新，不再对这块缓存做一次 HINCRBY。
		return reserveUserQuotaWhenCacheIsShort(id, int64(quota))
	}
	if err = persistUserQuotaDelta(id, -int64(quota)); err != nil {
		compensateReservedUserQuotaCache(id, int64(quota))
		if errors.Is(err, gorm.ErrRecordNotFound) {
			rejected, missing, checkErr := reserveRejectedByPersistedBalance(id, int64(quota))
			if checkErr != nil {
				return false, checkErr
			}
			if missing {
				return false, err
			}
			// 缓存高于数据库且数据库余额不够时，这是余额不足，不是数据库故障。
			// wallet_first 只在这种结果上回退到订阅。用户不存在或余额其实够的竞态失败仍返回原错误。
			if rejected {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

func reserveRejectedByPersistedBalance(id int, quota int64) (rejected bool, missing bool, err error) {
	var user User
	err = DB.Select("id", "quota").Where("id = ?", id).Take(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	return user.Quota < quota, false, nil
}

// TryReserveTokenQuota atomically checks and deducts a token quota. Unlimited
// tokens skip the balance check but still update remain/used accounting.
func TryReserveTokenQuota(id int, key string, quota int, unlimited bool) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota cannot be negative")
	}
	if quota == 0 {
		return true, nil
	}
	if unlimited {
		return true, DecreaseTokenQuota(id, key, int64(quota))
	}
	if !common.RedisEnabled {
		return reserveTokenQuotaDB(id, int64(quota))
	}

	result, err := cacheTryReserveTokenQuota(id, key, int64(quota))
	if err == nil && result == cacheQuotaMiss {
		if _, hydrateErr := GetTokenByKey(key, true); hydrateErr == nil {
			result, err = cacheTryReserveTokenQuota(id, key, int64(quota))
		}
	}
	if err != nil || result == cacheQuotaMiss {
		if err != nil {
			common.SysLog("token quota cache reserve unavailable, falling back to database: " + err.Error())
		}
		return reserveTokenQuotaDB(id, int64(quota))
	}
	if result == cacheQuotaInsufficient {
		return false, nil
	}
	if err = persistTokenQuotaDelta(id, -int64(quota)); err != nil {
		compensateReservedTokenQuotaCache(id, key, int64(quota))
		return false, err
	}
	return true, nil
}

const cacheQuotaCompensateAttempts = 3

func compensateReservedUserQuotaCache(id int, amount int64) {
	var lastResult cacheQuotaResult
	var lastErr error
	for attempt := 1; attempt <= cacheQuotaCompensateAttempts; attempt++ {
		result, err := cacheApplyUserQuotaDelta(id, amount)
		if err == nil && result == cacheQuotaOK {
			return
		}
		lastResult, lastErr = result, err
	}
	if invErr := InvalidateUserCache(id); invErr != nil {
		common.SysError(fmt.Sprintf("failed to invalidate user quota cache after compensate failure: userId=%d error=%v", id, invErr))
	}
	common.SysError(fmt.Sprintf("failed to compensate reserved user quota after %d attempts: userId=%d result=%d error=%v", cacheQuotaCompensateAttempts, id, lastResult, lastErr))
}

func compensateReservedTokenQuotaCache(id int, key string, amount int64) {
	var lastResult cacheQuotaResult
	var lastErr error
	for attempt := 1; attempt <= cacheQuotaCompensateAttempts; attempt++ {
		result, err := cacheApplyTokenQuotaDelta(id, key, amount)
		if err == nil && result == cacheQuotaOK {
			return
		}
		lastResult, lastErr = result, err
	}
	if dropErr := dropTokenQuotaCache(key); dropErr != nil {
		common.SysError(fmt.Sprintf("failed to drop token quota cache after compensate failure: tokenId=%d error=%v", id, dropErr))
	}
	common.SysError(fmt.Sprintf("failed to compensate reserved token quota after %d attempts: tokenId=%d result=%d error=%v", cacheQuotaCompensateAttempts, id, lastResult, lastErr))
}

func dropTokenQuotaCache(key string) error {
	if !common.RedisEnabled || key == "" {
		return nil
	}
	return common.RDB.Del(context.Background(), getTokenCacheKey(key)).Err()
}

// WalletQuotaForPreConsume is the wallet_first gate. A cache hit that already
// covers the estimate is used as-is. A missing, empty, or low cache is checked
// against the database so a stale-low hash cannot send a payable wallet onto
// the subscription.
func WalletQuotaForPreConsume(id int, required int64) (int64, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return GetUserQuota(id, true)
	}
	cached, cacheErr := getUserQuotaCache(id)
	if cacheErr == nil && cached > 0 && cached >= required {
		return cached, nil
	}
	dbQuota, dbErr := GetUserQuota(id, true)
	if dbErr != nil {
		if cacheErr == nil && cached > 0 {
			return cached, nil
		}
		if cacheErr != nil {
			return 0, cacheErr
		}
		return 0, dbErr
	}
	if cacheErr == nil && cached > dbQuota {
		return cached, nil
	}
	return dbQuota, nil
}

func reserveUserQuotaWhenCacheIsShort(id int, amount int64) (bool, error) {
	if amount <= 0 {
		return true, nil
	}
	quota, err := GetUserQuota(id, true)
	if err != nil {
		return false, err
	}
	if quota < amount {
		return false, nil
	}
	if err := InvalidateUserCache(id); err != nil {
		return false, err
	}
	if err := DecreaseUserQuota(id, amount, false); err != nil {
		if walletQuotaUpdateRejected(err) {
			return false, nil
		}
		return false, err
	}
	if err := InvalidateUserCache(id); err != nil {
		common.SysLog(fmt.Sprintf("failed to drop user quota cache after database reserve, userId=%d amount=%d: %s", id, amount, err.Error()))
	}
	return true, nil
}

func walletQuotaUpdateRejected(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "not enough") || strings.Contains(message, "update failed")
}
