package service

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadResponseBodyLimited(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader([]byte("hello")))}
	body, err := ReadResponseBodyLimited(resp, 5)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), body)
}

func TestReadResponseBodyLimitedTruncatesOversizeBody(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader([]byte("abcdef")))}
	body, err := ReadResponseBodyLimited(resp, 5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds")
	assert.Equal(t, []byte("abcde"), body)
}
