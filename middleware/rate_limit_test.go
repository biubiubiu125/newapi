package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestMemoryRateLimiterConcurrentRequestsDoNotExceedLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	inMemoryRateLimiter = common.InMemoryRateLimiter{}
	t.Cleanup(func() {
		common.RedisEnabled = redisEnabled
		inMemoryRateLimiter = common.InMemoryRateLimiter{}
	})

	const limit = 5
	router := gin.New()
	router.Use(rateLimitFactory(limit, int64(time.Minute/time.Second), "T"))
	router.GET("/limited", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	var wg sync.WaitGroup
	statuses := make(chan int, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/limited", nil)
			req.RemoteAddr = "203.0.113.10:12345"
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			statuses <- recorder.Code
		}()
	}
	wg.Wait()
	close(statuses)

	okCount := 0
	tooManyCount := 0
	for code := range statuses {
		switch code {
		case http.StatusOK:
			okCount++
		case http.StatusTooManyRequests:
			tooManyCount++
		default:
			t.Fatalf("unexpected status code: %d", code)
		}
	}

	require.Equal(t, limit, okCount)
	require.Equal(t, 100-limit, tooManyCount)
}
