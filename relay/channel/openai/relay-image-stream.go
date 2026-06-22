package openai

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const contextKeyImageStreamAllowed = "image_stream_allowed"

func OpenaiImageResponseHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if !isImageStreamAllowed(c, info) {
		return OpenaiImageHandler(c, info, resp)
	}
	if isEventStreamContentType(resp.Header.Get("Content-Type")) {
		return OpenaiImageStreamHandler(c, info, resp)
	}
	return OpenaiImageJSONAsStreamHandler(c, info, resp)
}

func isEventStreamContentType(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

func isImageStreamAllowed(c *gin.Context, info *relaycommon.RelayInfo) bool {
	if c != nil {
		if allowed, exists := c.Get(contextKeyImageStreamAllowed); exists {
			return allowed == true
		}
	}
	return info != nil && info.IsStream
}
