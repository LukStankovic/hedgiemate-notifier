package state

import (
	"encoding/json"
	"os"
)

// CachedCar is the per-car naming state persisted across restarts. Both fields
// are kept: TeslaMate publishes an empty display_name for unnamed cars and
// doesn't reliably re-publish either after a reconnect, so without caching the
// model an unnamed car drops back to "Car {id}".
type CachedCar struct {
	DisplayName string `json:"display_name,omitempty"`
	Model       string `json:"model,omitempty"`
}

// carNameCache persists car naming state, with a one-way migration from the
// pre-1.4.5 file format (map[carID]display_name) so an upgrade doesn't orphan
// an existing name.
type carNameCache struct {
	store *jsonStore[map[string]CachedCar]
}

func newCarNameCache() *carNameCache {
	return &carNameCache{
		store: newJSONStore("car_names.json", func() map[string]CachedCar {
			return map[string]CachedCar{}
		}),
	}
}

func (c *carNameCache) Load() map[string]CachedCar {
	cars := c.store.Load()
	if len(cars) > 0 {
		return cars
	}
	// New format empty/absent — try the legacy map[carID]display_name file.
	data, err := os.ReadFile(c.store.Path())
	if err != nil {
		return cars
	}
	var legacy map[string]string
	if err := json.Unmarshal(data, &legacy); err != nil {
		return cars
	}
	for id, name := range legacy {
		if name != "" {
			cars[id] = CachedCar{DisplayName: name}
		}
	}
	return cars
}

func (c *carNameCache) Save(cars map[string]CachedCar) {
	c.store.Save(cars)
}
