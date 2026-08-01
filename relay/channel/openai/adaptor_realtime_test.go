package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupRequestHeaderUsesRealtimeBetaOnlyForPreviewModels(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	tests := []struct {
		name             string
		model            string
		webSocket        bool
		wantBetaHeader   string
		wantWebSocketHdr string
	}{
		{
			name:           "ga http",
			model:          "gpt-realtime-2",
			wantBetaHeader: "",
		},
		{
			name:           "preview http",
			model:          "gpt-4o-realtime-preview",
			wantBetaHeader: "realtime=v1",
		},
		{
			name:             "ga websocket",
			model:            "gpt-realtime-2",
			webSocket:        true,
			wantWebSocketHdr: "realtime,openai-insecure-api-key.test-key",
		},
		{
			name:             "preview websocket",
			model:            "gpt-4o-realtime-preview",
			webSocket:        true,
			wantWebSocketHdr: "realtime,openai-insecure-api-key.test-key,openai-beta.realtime-v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
			if tt.webSocket {
				c.Request.Header.Set("Sec-WebSocket-Protocol", "realtime")
			}

			header := http.Header{}
			info := &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeRealtime,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType:       constant.ChannelTypeOpenAI,
					ApiKey:            "test-key",
					UpstreamModelName: tt.model,
				},
			}

			err := (&Adaptor{}).SetupRequestHeader(c, &header, info)

			require.NoError(t, err)
			assert.Equal(t, tt.wantBetaHeader, header.Get("openai-beta"))
			assert.Equal(t, tt.wantWebSocketHdr, header.Get("Sec-WebSocket-Protocol"))
			if !tt.webSocket {
				assert.Equal(t, "Bearer test-key", header.Get("Authorization"))
			}
		})
	}
}
