package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const cacheFileName = "car_names.json"

// CachedCar is the per-car naming state persisted across restarts. Both the
// user-set display_name and the model are cached: TeslaMate omits display_name
// for unnamed cars and only (re)publishes model when it has fresh data, so a
// notifier restart while the car is asleep would otherwise lose the model and
// fall back to "Car {id}" instead of "Model Y".
type CachedCar struct {
	DisplayName string `json:"display_name,omitempty"`
	Model       string `json:"model,omitempty"`
}

// nameCache persists car naming state to a JSON file so it survives restarts.
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

// Load reads the cached car naming state from disk. Returns an empty map on any
// error — including the legacy map[string]string format, which fails to
// unmarshal into CachedCar and is harmlessly rebuilt from live MQTT.
func (c *nameCache) Load() map[string]CachedCar {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return map[string]CachedCar{}
	}
	var cars map[string]CachedCar
	if err := json.Unmarshal(data, &cars); err != nil {
		return map[string]CachedCar{}
	}
	return cars
}

// Save persists the car ID → naming state mapping to disk atomically: it
// writes a temp file in the same directory and renames it over the target, so a
// crash mid-write can never leave a truncated/corrupt JSON behind.
func (c *nameCache) Save(cars map[string]CachedCar) {
	data, err := json.Marshal(cars)
	if err != nil {
		return
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
	}
}
