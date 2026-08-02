package middleware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

var timeFormat = "2006-01-02T15:04:05.000Z"

var inMemoryRateLimiter common.InMemoryRateLimiter

var defNext = func(c *gin.Context) {
	c.Next()
}

const redisRateLimitNamespace = "rateLimit:v2"

// Redis fixed-window limiting is kept for compatibility with endpoint-level
// limiters that need the current window count and remaining TTL.
const redisFixedWindowScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
end
local ttl = redis.call('TTL', KEYS[1])
if ttl < 0 then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
  ttl = redis.call('TTL', KEYS[1])
end
if count > tonumber(ARGV[1]) then
  return {0, count, ttl}
end
return {1, count, ttl}
`

const redisSlidingWindowRateLimitScript = `
local key = KEYS[1]
local max = tonumber(ARGV[1])
local duration = tonumber(ARGV[2])
local now = ARGV[3]
local expiration = tonumber(ARGV[4])

local length = redis.call("LLEN", key)
if length < max then
  redis.call("LPUSH", key, now)
  redis.call("EXPIRE", key, expiration)
  return 1
end

local oldest = redis.call("LINDEX", key, -1)
if oldest == false then
  redis.call("LPUSH", key, now)
  redis.call("EXPIRE", key, expiration)
  return 1
end

local oldestNum = tonumber(oldest)
if oldestNum == nil then
  redis.call("DEL", key)
  redis.call("LPUSH", key, now)
  redis.call("EXPIRE", key, expiration)
  return 1
end

if tonumber(now) - oldestNum < duration then
  redis.call("EXPIRE", key, expiration)
  return 0
end

redis.call("LPUSH", key, now)
redis.call("LTRIM", key, 0, max - 1)
redis.call("EXPIRE", key, expiration)
return 1
`

func redisIPRateLimitKey(mark string, clientIP string) string {
	return fmt.Sprintf("%s:ip:%s:%s", redisRateLimitNamespace, mark, clientIP)
}

func redisReplyInteger(value interface{}) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis integer reply type %T", value)
	}
}

func redisFixedWindowTake(ctx context.Context, key string, maxRequestNum int, duration int64) (bool, int64, int64, error) {
	if common.RDB == nil {
		return false, 0, 0, errors.New("Redis client is not initialized")
	}
	if key == "" {
		return false, 0, 0, errors.New("rate limit key is empty")
	}
	if maxRequestNum <= 0 {
		return false, 0, 0, errors.New("rate limit maximum must be positive")
	}
	if duration <= 0 {
		return false, 0, 0, errors.New("rate limit duration must be positive")
	}

	values, err := common.RDB.Eval(
		ctx,
		redisFixedWindowScript,
		[]string{key},
		maxRequestNum,
		duration,
	).Slice()
	if err != nil {
		return false, 0, 0, err
	}
	if len(values) != 3 {
		return false, 0, 0, fmt.Errorf("unexpected Redis rate limit reply length %d", len(values))
	}

	allowedValue, err := redisReplyInteger(values[0])
	if err != nil {
		return false, 0, 0, err
	}
	count, err := redisReplyInteger(values[1])
	if err != nil {
		return false, 0, 0, err
	}
	ttlSeconds, err := redisReplyInteger(values[2])
	if err != nil {
		return false, 0, 0, err
	}

	return allowedValue == 1, count, ttlSeconds, nil
}

func redisSlidingWindowAllowed(ctx context.Context, key string, maxRequestNum int, duration int64) (bool, error) {
	now := time.Now().UnixMilli()
	result, err := common.RDB.Eval(
		ctx,
		redisSlidingWindowRateLimitScript,
		[]string{key},
		maxRequestNum,
		duration*1000,
		now,
		int(common.RateLimitKeyExpirationDuration/time.Second),
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func abortOnRateLimitResult(c *gin.Context, allowed bool, err error, retryAfterSeconds int64) {
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("rate limit check failed: %v", err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "rate limiter error",
		})
		c.Abort()
		return
	}
	if !allowed {
		abortTooManyRequests(c, retryAfterSeconds)
	}
}

func abortTooManyRequests(c *gin.Context, retryAfterSeconds int64) {
	if retryAfterSeconds > 0 {
		c.Header("Retry-After", strconv.FormatInt(retryAfterSeconds, 10))
	}
	c.JSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"message": "Too many requests",
	})
	c.Abort()
}

func abortUnauthorized(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{
		"success": false,
		"message": "Unauthorized",
	})
	c.Abort()
}

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	ctx := context.Background()
	allowed, err := redisSlidingWindowAllowed(ctx, "rateLimit:"+key, maxRequestNum, duration)
	abortOnRateLimitResult(c, allowed, err, duration)
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		abortTooManyRequests(c, duration)
		return
	}
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	return rateLimitFactoryWithKeyFunc(maxRequestNum, duration, mark, fallbackRateLimitKey)
}

func rateLimitFactoryWithKeyFunc(maxRequestNum int, duration int64, mark string, keyFunc func(*gin.Context, string) string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			key := keyFunc(c, mark)
			if key == "" {
				if !c.IsAborted() {
					c.Next()
				}
				return
			}
			redisRateLimiter(c, maxRequestNum, duration, key)
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		return func(c *gin.Context) {
			key := keyFunc(c, mark)
			if key == "" {
				if !c.IsAborted() {
					c.Next()
				}
				return
			}
			memoryRateLimiter(c, maxRequestNum, duration, key)
		}
	}
}

func identityAwareRateLimitKey(c *gin.Context, mark string) string {
	if identity := requestRateLimitIdentity(c); identity != "" {
		return fmt.Sprintf("%s:%s", mark, identity)
	}
	return ""
}

func requestRateLimitIdentity(c *gin.Context) string {
	if identity := authenticatedRateLimitIdentity(c); identity != "" {
		return identity
	}
	if identity := authorizationRateLimitIdentity(c); identity != "" {
		return identity
	}
	return sessionRateLimitIdentity(c)
}

func authorizationRateLimitIdentity(c *gin.Context) (identity string) {
	defer func() {
		if recover() != nil {
			identity = ""
		}
	}()
	key := normalizedAuthorizationKey(c)
	if key == "" {
		return ""
	}
	token, err := model.GetTokenByKey(key, false)
	if err == nil && token != nil && token.Id > 0 {
		return fmt.Sprintf("token:%d", token.Id)
	}
	user, err := model.ValidateAccessToken(c.Request.Header.Get("Authorization"))
	if err == nil && user != nil && user.Id > 0 {
		return fmt.Sprintf("user:%d", user.Id)
	}
	return ""
}

func submittedAuthorizationValue(c *gin.Context) string {
	key := c.Request.Header.Get("Authorization")
	if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
		key = strings.TrimSpace(key[7:])
	}
	key = strings.TrimSpace(key)
	if key == "" || key == "midjourney-proxy" {
		key = c.Request.Header.Get("mj-api-secret")
		if strings.HasPrefix(key, "Bearer ") || strings.HasPrefix(key, "bearer ") {
			key = strings.TrimSpace(key[7:])
		}
		key = strings.TrimSpace(key)
	}
	if key == "" || key == "midjourney-proxy" {
		return ""
	}
	return key
}

func normalizedAuthorizationKey(c *gin.Context) string {
	key := submittedAuthorizationValue(c)
	if key == "" {
		return ""
	}
	key = strings.TrimPrefix(key, "sk-")
	parts := strings.Split(key, "-")
	return strings.TrimSpace(parts[0])
}

func sessionRateLimitIdentity(c *gin.Context) (identity string) {
	defer func() {
		if recover() != nil {
			identity = ""
		}
	}()
	session := sessions.Default(c)
	if userId := rateLimitInt(session.Get("id")); userId > 0 {
		return fmt.Sprintf("user:%d", userId)
	}
	return ""
}

func authenticatedRateLimitIdentity(c *gin.Context) string {
	if tokenId := c.GetInt("token_id"); tokenId > 0 {
		return fmt.Sprintf("token:%d", tokenId)
	}
	if userId := c.GetInt("id"); userId > 0 {
		return fmt.Sprintf("user:%d", userId)
	}
	return ""
}

func rateLimitInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case int32:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			return parsed
		}
	}
	return 0
}

func hashedRateLimitValue(prefix string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return prefix + ":" + common.Sha1([]byte(value))
}

func submittedCredentialRateLimitKey(c *gin.Context, mark string) string {
	if value := submittedAuthorizationValue(c); value != "" {
		return mark + ":credential:" + common.Sha1([]byte(value))
	}
	return ""
}

func preAuthRateLimitKey(c *gin.Context, mark string) string {
	if key := submittedCredentialRateLimitKey(c, mark); key != "" {
		return key
	}
	if key := identityAwareRateLimitKey(c, mark); key != "" {
		return key
	}
	return mark + ":anonymous"
}

func fallbackRateLimitKey(c *gin.Context, mark string) string {
	if key := identityAwareRateLimitKey(c, mark); key != "" {
		return key
	}
	if key := submittedCredentialRateLimitKey(c, mark); key != "" {
		return key
	}
	if key := sessionNonceRateLimitKey("critical_rate_limit_nonce")(c, mark); key != "" {
		return key
	}
	if c.IsAborted() {
		return ""
	}
	return clientIPRateLimitKey(c, mark)
}

func clientIPRateLimitKey(c *gin.Context, mark string) string {
	ip := common.GetClientIP(c)
	if ip == "" {
		return ""
	}
	return mark + ":ip:" + common.Sha1([]byte(ip))
}

func queryRateLimitKey(param string, normalizer func(string) string) func(*gin.Context, string) string {
	return func(c *gin.Context, mark string) string {
		value := c.Query(param)
		if normalizer != nil {
			value = normalizer(value)
		}
		if key := hashedRateLimitValue(param, value); key != "" {
			return mark + ":" + key
		}
		return fallbackRateLimitKey(c, mark)
	}
}

func sessionFieldRateLimitKey(field string) func(*gin.Context, string) string {
	return func(c *gin.Context, mark string) string {
		if value := sessionRateLimitString(c, field); value != "" {
			return mark + ":session:" + field + ":" + common.Sha1([]byte(value))
		}
		return fallbackRateLimitKey(c, mark)
	}
}

func sessionNonceRateLimitKey(field string) func(*gin.Context, string) string {
	return func(c *gin.Context, mark string) (key string) {
		defer func() {
			if recover() != nil {
				key = identityAwareRateLimitKey(c, mark)
			}
		}()
		session := sessions.Default(c)
		nonce, _ := session.Get(field).(string)
		nonce = strings.TrimSpace(nonce)
		if nonce == "" {
			nonce = common.GetRandomString(32)
			session.Set(field, nonce)
			if err := session.Save(); err != nil {
				abortOnRateLimitResult(c, false, err, 0)
				return ""
			}
		}
		return mark + ":session_nonce:" + field + ":" + common.Sha1([]byte(nonce))
	}
}

func sessionRateLimitString(c *gin.Context, field string) (value string) {
	defer func() {
		if recover() != nil {
			value = ""
		}
	}()
	raw := sessions.Default(c).Get(field)
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	case int:
		if typed > 0 {
			return fmt.Sprintf("%d", typed)
		}
	case int64:
		if typed > 0 {
			return fmt.Sprintf("%d", typed)
		}
	case int32:
		if typed > 0 {
			return fmt.Sprintf("%d", typed)
		}
	case float64:
		if typed > 0 {
			return fmt.Sprintf("%.0f", typed)
		}
	}
	return ""
}

func jsonFieldRateLimitKey(field string, normalizer func(string) string) func(*gin.Context, string) string {
	return func(c *gin.Context, mark string) string {
		value := requestJSONField(c, field)
		if normalizer != nil {
			value = normalizer(value)
		}
		if key := hashedRateLimitValue(field, value); key != "" {
			return mark + ":" + key
		}
		return fallbackRateLimitKey(c, mark)
	}
}

func registerRateLimitKey(c *gin.Context, mark string) string {
	email := model.NormalizeUserEmail(requestJSONField(c, "email"))
	if key := hashedRateLimitValue("email", email); key != "" {
		return mark + ":" + key
	}
	username := strings.ToLower(requestJSONField(c, "username"))
	if key := hashedRateLimitValue("username", username); key != "" {
		return mark + ":" + key
	}
	return fallbackRateLimitKey(c, mark)
}

func requestJSONField(c *gin.Context, field string) string {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return ""
	}
	defer func() {
		_, _ = storage.Seek(0, io.SeekStart)
		c.Request.Body = io.NopCloser(storage)
	}()
	var payload map[string]any
	if err := common.DecodeJson(storage, &payload); err != nil {
		return ""
	}
	value, ok := payload[field]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%.0f", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func telegramRateLimitKey(c *gin.Context, mark string) string {
	if key := hashedRateLimitValue("telegram", c.Query("id")); key != "" {
		return mark + ":" + key
	}
	return fallbackRateLimitKey(c, mark)
}

func GlobalWebRateLimit() func(c *gin.Context) {
	if common.GlobalWebRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW", fallbackRateLimitKey)
	}
	return defNext
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if common.GlobalApiRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA", fallbackRateLimitKey)
	}
	return defNext
}

func GlobalAPIPreAuthRateLimit() func(c *gin.Context) {
	if common.GlobalApiRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA:pre", preAuthRateLimitKey)
	}
	return defNext
}

func CriticalRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	}
	return defNext
}

func UsernameCriticalRateLimit(mark string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark, jsonFieldRateLimitKey("username", strings.ToLower))
	}
	return defNext
}

func EmailBodyCriticalRateLimit(mark string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark, jsonFieldRateLimitKey("email", model.NormalizeUserEmail))
	}
	return defNext
}

func EmailQueryRateLimit(mark string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark, queryRateLimitKey("email", model.NormalizeUserEmail))
	}
	return defNext
}

func RegisterCriticalRateLimit(mark string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark, registerRateLimitKey)
	}
	return defNext
}

func QueryCriticalRateLimit(mark string, param string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark, queryRateLimitKey(param, nil))
	}
	return defNext
}

func SessionNonceCriticalRateLimit(mark string, field string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark, sessionNonceRateLimitKey(field))
	}
	return defNext
}

func SessionFieldCriticalRateLimit(mark string, field string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark, sessionFieldRateLimitKey(field))
	}
	return defNext
}

func TelegramCriticalRateLimit(mark string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactoryWithKeyFunc(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark, telegramRateLimitKey)
	}
	return defNext
}

func UserCriticalRateLimit(mark string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return userRateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark)
	}
	return defNext
}

func SessionUserCriticalRateLimit(mark string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return sessionUserRateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark)
	}
	return defNext
}

func AuthenticatedCriticalRateLimit(mark string) func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return authenticatedRateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, mark)
	}
	return defNext
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			userId := c.GetInt("id")
			if userId == 0 {
				abortUnauthorized(c)
				return
			}
			key := fmt.Sprintf("rateLimit:%s:user:%d", mark, userId)
			userRedisRateLimiter(c, maxRequestNum, duration, key)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			abortUnauthorized(c)
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userId)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			abortTooManyRequests(c, duration)
			return
		}
	}
}

func sessionUserRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			identity := sessionRateLimitIdentity(c)
			if identity == "" {
				abortUnauthorized(c)
				return
			}
			key := fmt.Sprintf("rateLimit:%s:%s", mark, identity)
			userRedisRateLimiter(c, maxRequestNum, duration, key)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		identity := sessionRateLimitIdentity(c)
		if identity == "" {
			abortUnauthorized(c)
			return
		}
		key := fmt.Sprintf("%s:%s", mark, identity)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			abortTooManyRequests(c, duration)
			return
		}
	}
}

func authenticatedRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			identity := authenticatedRateLimitIdentity(c)
			if identity == "" {
				abortUnauthorized(c)
				return
			}
			key := fmt.Sprintf("rateLimit:%s:%s", mark, identity)
			userRedisRateLimiter(c, maxRequestNum, duration, key)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		identity := authenticatedRateLimitIdentity(c)
		if identity == "" {
			abortUnauthorized(c)
			return
		}
		key := fmt.Sprintf("%s:%s", mark, identity)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			abortTooManyRequests(c, duration)
			return
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	ctx := context.Background()
	allowed, err := redisSlidingWindowAllowed(ctx, key, maxRequestNum, duration)
	abortOnRateLimitResult(c, allowed, err, duration)
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, "SR")
}
