package common

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseMultipartFormReusableReadsFromBodyStorage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-1"))
	part, err := writer.CreateFormFile("image", "image.txt")
	require.NoError(t, err)
	_, err = part.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
	ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())

	storage, err := CreateBodyStorage(body.Bytes())
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(KeyBodyStorage, storage)

	form, err := ParseMultipartFormReusable(ctx)
	require.NoError(t, err)
	defer form.RemoveAll()

	require.Equal(t, []string{"gpt-image-1"}, form.Value["model"])
	require.Len(t, form.File["image"], 1)
	file, err := form.File["image"][0].Open()
	require.NoError(t, err)
	defer file.Close()
	content, err := io.ReadAll(file)
	require.NoError(t, err)
	require.Equal(t, []byte("image-bytes"), content)
}

func TestUnmarshalBodyReusableMultipartWithoutBoundaryFallsBackToJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	ctx.Request.Header.Set("Content-Type", "multipart/form-data")

	storage, err := CreateBodyStorage([]byte(`{"model":"gpt-image-1"}`))
	require.NoError(t, err)
	defer storage.Close()
	ctx.Set(KeyBodyStorage, storage)

	var req struct {
		Model string `json:"model"`
	}
	require.NoError(t, UnmarshalBodyReusable(ctx, &req))
	require.Equal(t, "gpt-image-1", req.Model)
}
