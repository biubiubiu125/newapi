package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedCode     int
	}{
		{
			name:             "map string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedCode:     503,
		},
		{
			name:             "map int value",
			statusCode:       429,
			statusCodeConfig: `{"429":503}`,
			expectedCode:     503,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedCode:     429,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":503}`,
			expectedCode:     200,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.expectedCode, newAPIError.StatusCode)
		})
	}
}

func TestTaskErrorWrapperHidesInternalDatabaseError(t *testing.T) {
	wrapped := TaskErrorWrapper(errors.New("ERROR: password authentication failed (SQLSTATE 28P01)"), "get_task_failed", http.StatusInternalServerError)

	require.NotNil(t, wrapped)
	require.Equal(t, model.TaskPublicInternalFailReason, wrapped.Message)
	require.NotContains(t, wrapped.Message, "password")
	require.NotContains(t, wrapped.Message, "SQLSTATE")
}

func TestTaskErrorWrapperKeepsPublicTaskMessage(t *testing.T) {
	wrapped := TaskErrorWrapper(errors.New("task_not_exist"), "get_task_failed", http.StatusOK)

	require.NotNil(t, wrapped)
	require.Equal(t, "task_not_exist", wrapped.Message)
}

func TestTaskErrorWrapperRewritesLocatorOnlyError(t *testing.T) {
	wrapped := TaskErrorWrapper(errors.New("https://cdn.example/video.mp4"), "get_task_failed", http.StatusInternalServerError)

	require.NotNil(t, wrapped)
	require.Equal(t, "request failed", wrapped.Message)
	require.NotContains(t, wrapped.Message, "cdn.example")
}

func TestTaskErrorFromAPIErrorHidesInternalDatabaseError(t *testing.T) {
	apiErr := types.NewError(errors.New("sql: database is closed"), types.ErrorCodeQueryDataError)
	wrapped := TaskErrorFromAPIError(apiErr)

	require.NotNil(t, wrapped)
	require.Equal(t, model.TaskPublicInternalFailReason, wrapped.Message)
	require.NotContains(t, wrapped.Message, "sql")
	require.NotContains(t, wrapped.Message, "database")
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)
	logBuffer, restoreWriter := captureErrorWriter(t)
	defer restoreWriter()

	body := strings.Repeat("b", common.LocalLogContentLimit+256)
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, "bad response status code 500", newAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", common.LocalLogContentLimit+1))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", common.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)
	logBuffer, restoreWriter := captureErrorWriter(t)
	defer restoreWriter()

	body := strings.Repeat("e", common.LocalLogContentLimit+256)
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

func captureErrorWriter(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()

	return &logBuffer, func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	}
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()
	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() {
		common.DebugEnabled = oldDebug
	})
}
