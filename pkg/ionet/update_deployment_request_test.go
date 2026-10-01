package ionet

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateDeploymentRequestKeepsExplicitEmptyClears(t *testing.T) {
	var req UpdateDeploymentRequest
	require.NoError(t, json.Unmarshal([]byte(`{"image_url":"example.io/app","entrypoint":[],"args":[],"env_variables":{}}`), &req))

	encoded, err := json.Marshal(req)
	require.NoError(t, err)
	assert.JSONEq(t, `{"image_url":"example.io/app","entrypoint":[],"args":[],"env_variables":{}}`, string(encoded))
}

func TestUpdateDeploymentRequestOmitsFieldsTheClientDidNotSend(t *testing.T) {
	var req UpdateDeploymentRequest
	require.NoError(t, json.Unmarshal([]byte(`{"image_url":"example.io/app"}`), &req))

	encoded, err := json.Marshal(req)
	require.NoError(t, err)
	assert.JSONEq(t, `{"image_url":"example.io/app"}`, string(encoded))
}
