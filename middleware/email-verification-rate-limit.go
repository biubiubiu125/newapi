package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const (
	EmailVerificationRateLimitMark = "EV"
	EmailVerificationMaxRequests   = 2  // 30秒内最多2次
	EmailVerificationDuration      = 30 // 30秒时间窗口
)

func redisEmailVerificationRateLimiter(c *gin.Context) {
	key := emailQueryRateLimitKey(c, EmailVerificationRateLimitMark)
	if key == "" {
		c.Next()
		return
	}
	ctx := context.Background()
	rdb := common.RDB

	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		// fallback
		memoryEmailVerificationRateLimiter(c)
		return
	}

	// 第一次设置键时设置过期时间
	if count == 1 {
		_ = rdb.Expire(ctx, key, time.Duration(EmailVerificationDuration)*time.Second).Err()
	}

	// 检查是否超出限制
	if count <= int64(EmailVerificationMaxRequests) {
		c.Next()
		return
	}

	// 获取剩余等待时间
	ttl, err := rdb.TTL(ctx, key).Result()
	waitSeconds := int64(EmailVerificationDuration)
	if err == nil && ttl > 0 {
		waitSeconds = int64(ttl.Seconds())
	}

	c.JSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"message": fmt.Sprintf("发送过于频繁，请等待 %d 秒后再试", waitSeconds),
	})
	c.Abort()
}

func memoryEmailVerificationRateLimiter(c *gin.Context) {
	key := emailQueryRateLimitKey(c, EmailVerificationRateLimitMark)
	if key == "" {
		c.Next()
		return
	}

	if !inMemoryRateLimiter.Request(key, EmailVerificationMaxRequests, EmailVerificationDuration) {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"message": "发送过于频繁，请稍后再试",
		})
		c.Abort()
		return
	}

	c.Next()
}

func emailQueryRateLimitKey(c *gin.Context, mark string) string {
	email := model.NormalizeUserEmail(c.Query("email"))
	if strings.TrimSpace(email) == "" {
		if key := fallbackRateLimitKey(c, mark); key != "" {
			return "emailRateLimit:" + key
		}
		return ""
	}
	return "emailRateLimit:" + mark + ":email:" + common.Sha1([]byte(email))
}

func EmailVerificationRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if common.RedisEnabled {
			redisEmailVerificationRateLimiter(c)
		} else {
			inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
			memoryEmailVerificationRateLimiter(c)
		}
	}
}

func EmailQueryRateLimit(mark string) gin.HandlerFunc {
	if !common.CriticalRateLimitEnable {
		return defNext
	}
	return func(c *gin.Context) {
		key := emailQueryRateLimitKey(c, mark)
		if key == "" {
			c.Next()
			return
		}
		if common.RedisEnabled {
			ctx := context.Background()
			allowed, err := redisSlidingWindowAllowed(ctx, key, common.CriticalRateLimitNum, common.CriticalRateLimitDuration)
			abortOnRateLimitResult(c, allowed, err)
			return
		}
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		if !inMemoryRateLimiter.Request(key, common.CriticalRateLimitNum, common.CriticalRateLimitDuration) {
			abortTooManyRequests(c)
			return
		}
		c.Next()
	}
}
