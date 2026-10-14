package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanFunctionParametersDropsNonStringEnums(t *testing.T) {
	cleaned := CleanFunctionParameters(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"flag": map[string]interface{}{"type": "boolean", "enum": []interface{}{true}},
			"name": map[string]interface{}{"type": "string", "enum": []interface{}{"a"}},
			"bad":  map[string]interface{}{"type": "string", "enum": "nope"},
		},
	}).(map[string]interface{})

	props := cleaned["properties"].(map[string]interface{})
	flag := props["flag"].(map[string]interface{})
	_, hasEnum := flag["enum"]
	assert.False(t, hasEnum)
	assert.Equal(t, "BOOLEAN", flag["type"])

	name := props["name"].(map[string]interface{})
	assert.Equal(t, []interface{}{"a"}, name["enum"])

	bad := props["bad"].(map[string]interface{})
	_, hasEnum = bad["enum"]
	assert.False(t, hasEnum)
	assert.Equal(t, "STRING", bad["type"])
}

func TestShallowCleanerDropsNonStringEnums(t *testing.T) {
	cleaned := cleanGeminiFunctionParametersShallow(map[string]interface{}{
		"type":       "boolean",
		"enum":       []interface{}{true, false},
		"properties": map[string]interface{}{"x": true},
	}).(map[string]interface{})

	_, hasEnum := cleaned["enum"]
	assert.False(t, hasEnum)
	assert.Equal(t, "BOOLEAN", cleaned["type"])
	_, hasProperties := cleaned["properties"]
	assert.False(t, hasProperties)
}
