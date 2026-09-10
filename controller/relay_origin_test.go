package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
)

func TestCheckRelayWebSocketOrigin(t *testing.T) {
	previousTrusted := common.SessionCookieTrustedURLs
	t.Cleanup(func() { common.SessionCookieTrustedURLs = previousTrusted })
	common.SessionCookieTrustedURLs = []string{"https://trusted.example"}
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://configured.example")

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{name: "missing origin", want: true},
		{name: "same origin", origin: "https://panel.example", want: true},
		{name: "trusted origin", origin: "https://trusted.example", want: true},
		{name: "configured origin", origin: "https://configured.example", want: true},
		{name: "arbitrary origin", origin: "https://evil.example", want: false},
		{name: "null origin", origin: "null", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "https://panel.example/realtime", nil)
			request.Host = "panel.example"
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			assert.Equal(t, test.want, checkRelayWebSocketOrigin(request))
		})
	}
}
