package state

// CachedCar is the per-car naming state persisted across restarts. Both fields
// are kept: TeslaMate omits display_name for unnamed cars and doesn't reliably
// re-publish model after a reconnect, so without caching the model a restart
// drops back to "Car {id}".
type CachedCar struct {
	DisplayName string `json:"display_name,omitempty"`
	Model       string `json:"model,omitempty"`
}

func newCarNameStore() *jsonStore[map[string]CachedCar] {
	return newJSONStore("car_names.json", func() map[string]CachedCar {
		return map[string]CachedCar{}
	})
}
