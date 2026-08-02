package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
)

func useRateLimitMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()

	previousRedisEnabled := common.RedisEnabled
	previousRedisClient := common.RDB
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	require.NoError(t, redisClient.Ping(context.Background()).Err())

	common.RedisEnabled = true
	common.RDB = redisClient
	t.Cleanup(func() {
		_ = redisClient.Close()
		common.RedisEnabled = previousRedisEnabled
		common.RDB = previousRedisClient
	})

	return redisServer, redisClient
}

func TestMemoryRateLimiterConcurrentRequestsDoNotExceedLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = redisEnabled
	})

	const limit = 5
	router := gin.New()
	mark := fmt.Sprintf("%s:%d", t.Name(), time.Now().UnixNano())
	router.Use(rateLimitFactory(limit, int64(time.Minute/time.Second), mark))
	router.GET("/limited", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	var wg sync.WaitGroup
	results := make(chan struct {
		status     int
		retryAfter string
	}, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/limited", nil)
			req.RemoteAddr = "203.0.113.10:12345"
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			results <- struct {
				status     int
				retryAfter string
			}{
				status:     recorder.Code,
				retryAfter: recorder.Header().Get("Retry-After"),
			}
		}()
	}
	wg.Wait()
	close(results)

	okCount := 0
	tooManyCount := 0
	for result := range results {
		switch result.status {
		case http.StatusOK:
			okCount++
		case http.StatusTooManyRequests:
			tooManyCount++
			require.Equal(t, "60", result.retryAfter)
		default:
			t.Fatalf("unexpected status code: %d", result.status)
		}
	}

	require.Equal(t, limit, okCount)
	require.Equal(t, 100-limit, tooManyCount)
}

func TestEmailQueryRateLimitNormalizesEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousEnabled := common.CriticalRateLimitEnable
	previousLimit := common.CriticalRateLimitNum
	previousDuration := common.CriticalRateLimitDuration
	previousRedisEnabled := common.RedisEnabled
	common.CriticalRateLimitEnable = true
	common.CriticalRateLimitNum = 1
	common.CriticalRateLimitDuration = 60
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.CriticalRateLimitEnable = previousEnabled
		common.CriticalRateLimitNum = previousLimit
		common.CriticalRateLimitDuration = previousDuration
		common.RedisEnabled = previousRedisEnabled
	})

	router := gin.New()
	router.Use(EmailQueryRateLimit(fmt.Sprintf("%s:%d", t.Name(), time.Now().UnixNano())))
	router.GET("/reset", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/reset?email=User%40Example.com", nil))
	require.Equal(t, http.StatusOK, first.Code)

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/reset?email=%20user%40example.COM%20", nil))
	require.Equal(t, http.StatusTooManyRequests, second.Code)
}
