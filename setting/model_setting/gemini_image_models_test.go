package model_setting

import "testing"

func TestGeminiGAImageModelsSupportImagine(t *testing.T) {
	for _, model := range []string{
		"gemini-3-pro-image",
		"gemini-3.1-flash-image",
	} {
		if !IsGeminiModelSupportImagine(model) {
			t.Fatalf("expected %q to support image generation", model)
		}
	}
}
