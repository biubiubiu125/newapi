package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionRejectsLongWalletNotice(t *testing.T) {
	body := []byte(fmt.Sprintf(
		`{"key":"payment_setting.wallet_notice","value":%q}`,
		strings.Repeat("a", walletNoticeMaxLength+1),
	))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/option/",
		bytes.NewReader(body),
	)

	UpdateOption(ctx)

	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.Equal(t, false, payload["success"])
	require.Contains(t, payload["message"], "1000")
}
