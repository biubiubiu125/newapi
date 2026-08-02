package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageRequestPreservesImageURL(t *testing.T) {
	var request ImageRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"model": "gpt-image-1",
		"prompt": "edit this image",
		"image_url": "https://example.test/input.png"
	}`), &request))

	require.JSONEq(t, `"https://example.test/input.png"`, string(request.ImageUrl))

	data, err := json.Marshal(request)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"image_url":"https://example.test/input.png"`)
}
