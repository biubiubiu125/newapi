package service

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

// MaxResponseBodyBytes is the shared ceiling for limited HTTP response reads
// used by task/plugin polling and submit flows.
const MaxResponseBodyBytes int64 = 1 << 20

func ReadResponseBodyLimited(resp *http.Response, maxBytes int64) ([]byte, error) {
	if resp == nil {
		return nil, errors.New("nil response")
	}
	if resp.Body == nil {
		return nil, errors.New("nil response body")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("invalid response body limit %d", maxBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return body, err
	}
	if int64(len(body)) > maxBytes {
		return body[:maxBytes], fmt.Errorf("response body exceeds %d bytes", maxBytes)
	}
	return body, nil
}
