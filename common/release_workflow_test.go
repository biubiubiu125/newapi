package common

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReleaseWorkflowParsesAndPinsBun(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}

	on, _ := doc["on"].(map[string]any)
	push, _ := on["push"].(map[string]any)
	tags, _ := push["tags"].([]any)
	seenV := false
	for _, tag := range tags {
		switch tag {
		case "v*":
			seenV = true
		case "*":
			t.Fatal("release workflow still triggers on every tag")
		}
	}
	if !seenV {
		t.Fatal("release workflow no longer triggers on v*")
	}

	jobs, _ := doc["jobs"].(map[string]any)
	for _, name := range []string{"linux", "macos", "windows"} {
		job, _ := jobs[name].(map[string]any)
		steps, _ := job["steps"].([]any)
		if len(steps) == 0 {
			t.Fatalf("%s has no steps", name)
		}
		foundFrontend := false
		foundBun := false
		for _, raw := range steps {
			step, _ := raw.(map[string]any)
			if step["name"] == "Build Frontend (default)" {
				t.Fatalf("%s still has the broken frontend step", name)
			}
			if step["name"] == "Build Frontend" {
				if _, ok := step["run"].(string); !ok {
					t.Fatalf("%s Build Frontend has no run script", name)
				}
				foundFrontend = true
			}
			if uses, _ := step["uses"].(string); strings.HasPrefix(uses, "oven-sh/setup-bun@") {
				with, _ := step["with"].(map[string]any)
				if with["bun-version"] != "1.4.0" {
					t.Fatalf("%s bun-version = %#v", name, with["bun-version"])
				}
				foundBun = true
			}
		}
		if !foundFrontend {
			t.Fatalf("%s is missing Build Frontend", name)
		}
		if !foundBun {
			t.Fatalf("%s is missing the pinned Bun setup", name)
		}
	}
}
