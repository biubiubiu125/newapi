package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// serveRevalidatedJSON writes a public JSON payload with a weak semantic ETag
// and answers conditional requests with 304 Not Modified. The namespace keeps
// different endpoints from sharing validators when their content is equal.
func serveRevalidatedJSON(c *gin.Context, namespace, content string, payload any) {
	body, err := common.Marshal(payload)
	if err != nil {
		common.ApiErrorWithStatus(c, http.StatusInternalServerError, err)
		return
	}

	etag := common.ETagFor(namespace, content)
	c.Header("ETag", etag)
	c.Header("Cache-Control", "no-cache")
	c.Header("Vary", "Accept-Encoding")
	if common.ETagMatches(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		c.Writer.WriteHeaderNow()
		return
	}

	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}
