package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const cacheFileName = "car_names.json"

// nameCache persists car display names to a JSON file so they survive restarts.
// The file is stored next to the binary or in /data if available (Docker volume).
type nameCache struct {
	path string
}

func newNameCache() *nameCache {
	dir := "/data"
	if _, err := os.Stat(dir); err != nil {
		dir = "."
	}
	return &nameCache{path: filepath.Join(dir, cacheFileName)}
}

// Load reads cached display names from disk. Returns empty map on any error.
func (c *nameCache) Load() map[string]string {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return map[string]string{}
	}
	var names map[string]string
	if err := json.Unmarshal(data, &names); err != nil {
		return map[string]string{}
	}
	return names
}

// Save persists the car ID → display name mapping to disk.
func (c *nameCache) Save(names map[string]string) {
	data, err := json.Marshal(names)
	if err != nil {
		return
	}
	_ = os.WriteFile(c.path, data, 0644)
}
