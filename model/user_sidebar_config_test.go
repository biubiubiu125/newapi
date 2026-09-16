package model

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestGenerateDefaultSidebarConfigIncludesGptImage(t *testing.T) {
	raw := generateDefaultSidebarConfigForRole(common.RoleCommonUser)
	var config map[string]map[string]any
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatalf("unmarshal default sidebar config: %v", err)
	}
	console := config["console"]
	if console == nil {
		t.Fatal("missing console section")
	}
	for _, key := range []string{"gpt_image", "image_tasks", "model_check"} {
		value, ok := console[key].(bool)
		if !ok || !value {
			t.Errorf("console.%s = %v, want true", key, console[key])
		}
	}
}
