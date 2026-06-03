package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

var timeFormat = "2006-01-02T15:04:05.000Z"

var inMemoryRateLimiter common.InMemoryRateLimiter

var defNext = func(c *gin.Context) {
	c.Next()
}

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

func abortOnRateLimitResult(c *gin.Context, allowed bool, err error) {
	if err != nil {
		fmt.Println(err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "rate limiter error",
		})
		c.Abort()
		return
	}
	if !allowed {
		abortTooManyRequests(c)
	}
}

func abortTooManyRequests(c *gin.Context) {
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

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	ctx := context.Background()
	key := "rateLimit:" + mark + common.GetClientIP(c)
	allowed, err := redisSlidingWindowAllowed(ctx, key, maxRequestNum, duration)
	abortOnRateLimitResult(c, allowed, err)
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + common.GetClientIP(c)
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		abortTooManyRequests(c)
		return
	}
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		return func(c *gin.Context) {
			memoryRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
}

func GlobalWebRateLimit() func(c *gin.Context) {
	if common.GlobalWebRateLimitEnable {
		return rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
	}
	return defNext
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if common.GlobalApiRateLimitEnable {
		return rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
	}
	return defNext
}

func CriticalRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
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
			abortTooManyRequests(c)
			return
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	ctx := context.Background()
	allowed, err := redisSlidingWindowAllowed(ctx, key, maxRequestNum, duration)
	abortOnRateLimitResult(c, allowed, err)
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, "SR")
}
