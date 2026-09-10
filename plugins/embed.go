package plugins

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

//go:embed tasks
var taskPlugins embed.FS

func init() {
	entries, err := fs.ReadDir(taskPlugins, "tasks")
	if err != nil {
		panic(fmt.Sprintf("read embedded task plugins: %v", err))
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := entry.Name()
		source, sourceErr := Source(key)
		if sourceErr != nil {
			panic(fmt.Sprintf("read embedded task plugin %s: %v", key, sourceErr))
		}
		if _, registerErr := jsplugin.DefaultRegistry.RegisterFactory(source, jsplugin.Options{Key: key}); registerErr != nil {
			panic(fmt.Sprintf("register embedded task plugin %s: %v", key, registerErr))
		}
	}
}

// Source returns the embedded factory source for a task plugin key.
func Source(key string) (string, error) {
	source, err := taskPlugins.ReadFile("tasks/" + key + "/plugin.js")
	if err != nil {
		return "", err
	}
	return string(source), nil
}

// Icon returns a factory sidecar logo when plugins/tasks/<key>/ ships
// icon.svg or icon.png. Missing artwork is not an error for registration.
func Icon(key string) (string, []byte, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, "/") || strings.Contains(key, "\\") {
		return "", nil, fmt.Errorf("plugin icon not found")
	}
	for _, name := range []string{"icon.svg", "icon.png"} {
		data, err := taskPlugins.ReadFile("tasks/" + key + "/" + name)
		if err != nil {
			continue
		}
		mediaType := "image/svg+xml"
		if strings.HasSuffix(name, ".png") {
			mediaType = "image/png"
		}
		if err := jsplugin.ValidateIconImage(mediaType, data); err != nil {
			return "", nil, err
		}
		return mediaType, data, nil
	}
	return "", nil, fmt.Errorf("plugin icon not found")
}
