package common

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetPageQueryClampsNonPositivePageSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/items", func(c *gin.Context) {
		pageInfo := GetPageQuery(c)
		require.Equal(t, ItemsPerPage, pageInfo.GetPageSize())
		require.Equal(t, 1, pageInfo.GetPage())
	})

	req := httptest.NewRequest("GET", "/items?page_size=-1", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)
}

func TestGetPageQueryClampsCompatiblePageSizeAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/items", func(c *gin.Context) {
		pageInfo := GetPageQuery(c)
		require.Equal(t, ItemsPerPage, pageInfo.GetPageSize())
	})

	req := httptest.NewRequest("GET", "/items?ps=-20", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)
}

func TestGetPageQuerySupportsPageAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/items", func(c *gin.Context) {
		pageInfo := GetPageQuery(c)
		require.Equal(t, 3, pageInfo.GetPage())
		require.Equal(t, 40, pageInfo.GetStartIdx())
	})

	req := httptest.NewRequest("GET", "/items?page=3&page_size=20", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)
}
