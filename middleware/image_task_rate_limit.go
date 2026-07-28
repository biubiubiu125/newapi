package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

var imageTaskCreateAdmissionTTL = 10 * time.Minute
var imageTaskCreateAdmissionRenewInterval = 2 * time.Minute

// ImageTaskCreateAdmission applies a database-backed rate and capacity guard
// to new public image task creation after idempotent replays had a chance to return.
func ImageTaskCreateAdmission() func(c *gin.Context) {
	return func(c *gin.Context) {
		userID := c.GetInt("id")
		tokenID := c.GetInt("token_id")
		if userID <= 0 || tokenID <= 0 {
			abortImageTaskRateLimitUnauthorized(c)
			return
		}

		maxBodyMB := constant.MaxRequestBodyMB
		if maxBodyMB <= 0 {
			maxBodyMB = 128
		}
		maxBodyBytes := int64(maxBodyMB) << 20
		reservedBytes := c.Request.ContentLength
		if reservedBytes <= 0 || reservedBytes > maxBodyBytes {
			reservedBytes = maxBodyBytes
		}
		limits := model.ImageTaskCreateAdmissionLimits{
			RequestLimit:          constant.ImageTaskCreateRateLimitCount,
			WindowSeconds:         int64(constant.ImageTaskCreateRateLimitDurationSeconds),
			MaxInFlight:           constant.ImageTaskCreateMaxInFlight,
			MaxReservedBytes:      int64(constant.ImageTaskCreateMaxReservedMB) << 20,
			ReservationTTLSeconds: imageTaskCreateAdmissionTTLSeconds(),
		}
		reservationID, err := model.AcquireImageTaskCreateAdmission(userID, tokenID, reservedBytes, 0, limits)
		if err != nil {
			switch {
			case errors.Is(err, model.ErrImageTaskCreateRateLimitExceeded):
				abortImageTaskCreateAdmissionExceeded(c, int64(constant.ImageTaskCreateRateLimitDurationSeconds), "image task creation rate limit exceeded")
			case errors.Is(err, model.ErrImageTaskCreateCapacityExceeded):
				abortImageTaskCreateAdmissionExceeded(c, 1, "image task creation capacity is temporarily full")
			default:
				common.SysLog("image task create admission failed: " + err.Error())
				abortWithImageTaskMessage(c, http.StatusInternalServerError, "internal_error", "image task admission failed")
			}
			return
		}
		stopRenewal := startImageTaskCreateAdmissionRenewal(
			c.Request.Context(),
			reservationID,
			limits.ReservationTTLSeconds,
			imageTaskCreateAdmissionRenewInterval,
		)
		defer func() {
			stopRenewal()
			if err := model.ReleaseImageTaskCreateAdmission(reservationID); err != nil {
				common.SysLog("release image task create admission failed: " + err.Error())
			}
		}()
		c.Next()
	}
}

func imageTaskCreateAdmissionTTLSeconds() int64 {
	seconds := int64(imageTaskCreateAdmissionTTL / time.Second)
	if seconds <= 0 {
		return 1
	}
	return seconds
}

func startImageTaskCreateAdmissionRenewal(parent context.Context, reservationID string, ttlSeconds int64, interval time.Duration) func() {
	if reservationID == "" {
		return func() {}
	}
	if interval <= 0 {
		interval = time.Duration(ttlSeconds) * time.Second / 3
		if interval <= 0 {
			interval = time.Second
		}
	}
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				renewed, err := model.RenewImageTaskCreateAdmission(reservationID, 0, ttlSeconds)
				if err != nil {
					common.SysLog("renew image task create admission failed: " + err.Error())
					continue
				}
				if !renewed {
					common.SysLog("image task create admission expired while request is still active")
					return
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func abortImageTaskCreateAdmissionExceeded(c *gin.Context, retryAfter int64, message string) {
	if retryAfter <= 0 {
		retryAfter = 1
	}
	c.Header("Retry-After", strconv.FormatInt(retryAfter, 10))
	abortWithImageTaskMessage(c, http.StatusTooManyRequests, "rate_limit_exceeded", message)
}

// ImageTaskAccessRateLimit 为 /v1/image-tasks 的状态、结果、ACK 和取消接口提供限流。
//
// 这些接口不经过 ModelRequestRateLimit（没有模型维度），也不在 /api 的
// GlobalAPIRateLimit 覆盖范围内，如果不单独限流就是完全裸奔的。异步 API 的正常用法
// 就是循环轮询任务状态，因此额度必须按"轮询"而不是按"敏感写操作"来给：不能复用
// CriticalRateLimit 那套默认 20 分钟 50 次的配置。
//
// 限流键取「用户 + 令牌」：任务归属本身就是按令牌校验的，按令牌隔离可以避免同一用户
// 的某个令牌把其他令牌一起打满。必须挂在 TokenAuth 之后才能取到这两个值。
func ImageTaskAccessRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		maxRequestNum := constant.ImageTaskAccessRateLimitCount
		duration := int64(constant.ImageTaskAccessRateLimitDurationSeconds)
		if maxRequestNum <= 0 || duration <= 0 {
			c.Next()
			return
		}

		userID := c.GetInt("id")
		if userID == 0 {
			abortImageTaskRateLimitUnauthorized(c)
			return
		}
		key := fmt.Sprintf("IMGT:user:%d:token:%d", userID, c.GetInt("token_id"))

		allowed := true
		if common.RedisEnabled {
			var err error
			allowed, err = redisSlidingWindowAllowed(context.Background(), "rateLimit:"+key, maxRequestNum, duration)
			if err != nil {
				// 限流器故障不应该阻断已受理任务的状态查询和取消，放行并留痕。
				common.SysLog("image task access rate limiter error: " + err.Error())
				c.Next()
				return
			}
		} else {
			inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
			allowed = inMemoryRateLimiter.Request(key, maxRequestNum, duration)
		}
		if !allowed {
			abortImageTaskRateLimitExceeded(c, duration)
			return
		}
		c.Next()
	}
}

// abortImageTaskRateLimitExceeded 使用 /v1/image-tasks/* 的统一错误信封，
// 而不是 abortTooManyRequests 的 {"success":false,"message":...} 形态。
func abortImageTaskRateLimitExceeded(c *gin.Context, duration int64) {
	c.Header("Retry-After", strconv.FormatInt(duration, 10))
	c.JSON(http.StatusTooManyRequests, gin.H{"error": gin.H{
		"code":    "rate_limit_exceeded",
		"message": fmt.Sprintf("too many image task requests, at most %d requests per %d seconds", constant.ImageTaskAccessRateLimitCount, duration),
		"type":    "image_task_error",
	}})
	c.Abort()
}

func abortImageTaskRateLimitUnauthorized(c *gin.Context) {
	c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{
		"code":    "unauthorized",
		"message": "image task access requires a valid token",
		"type":    "image_task_error",
	}})
	c.Abort()
}
