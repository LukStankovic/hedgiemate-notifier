package state

import (
	"os"
	"path/filepath"
	"testing"
)

func carStore(path string) *jsonStore[map[string]CachedCar] {
	return &jsonStore[map[string]CachedCar]{
		path:  path,
		empty: func() map[string]CachedCar { return map[string]CachedCar{} },
	}
}

// TestNameCacheRoundTrip locks the user-81 "Car 1" fix: model must persist, not
// just display_name. Multi-car: each carID independent.
func TestNameCacheRoundTrip(t *testing.T) {
	c := carStore(filepath.Join(t.TempDir(), "car_names.json"))

	c.Save(map[string]CachedCar{
		"1": {Model: "Y"},
		"2": {DisplayName: "Daily"},
		"3": {DisplayName: "X", Model: "X"},
	})

	got := c.Load()
	if got["1"].Model != "Y" || got["1"].DisplayName != "" {
		t.Errorf("car 1 = %+v, want model Y, no name", got["1"])
	}
	if got["2"].DisplayName != "Daily" {
		t.Errorf("car 2 = %+v, want name Daily", got["2"])
	}
	if got["3"].DisplayName != "X" || got["3"].Model != "X" {
		t.Errorf("car 3 = %+v, want name+model X", got["3"])
	}
}

// TestNameCacheClearsRemovedName: removing the Tesla name publishes an empty
// display_name, which must overwrite the cached name so naming falls back to
// the model ("Model Y") instead of keeping the stale old name across a restart.
func TestNameCacheClearsRemovedName(t *testing.T) {
	c := carStore(filepath.Join(t.TempDir(), "car_names.json"))

	// Car was named "Daily" with model Y.
	c.Save(map[string]CachedCar{"1": {DisplayName: "Daily", Model: "Y"}})

	// Name removed → empty display_name arrives → cache updated (mirrors the
	// FieldDisplayName handler: load, set, save).
	cars := c.Load()
	cc := cars["1"]
	cc.DisplayName = ""
	cars["1"] = cc
	c.Save(cars)

	got := c.Load()["1"]
	if got.DisplayName != "" || got.Model != "Y" {
		t.Fatalf("after name removal = %+v, want empty name, model Y", got)
	}
	if name := displayNameForCar("1", &CarState{DisplayName: got.DisplayName, Model: got.Model}); name != "Model Y" {
		t.Errorf("displayNameForCar = %q, want Model Y", name)
	}
}

// TestNameCacheLegacyFormat: the old map[string]string file degrades to an
// empty map instead of erroring — rebuilt from live MQTT.
func TestNameCacheLegacyFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "car_names.json")
	if err := os.WriteFile(path, []byte(`{"1":"Daily"}`), 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if got := carStore(path).Load(); len(got) != 0 {
		t.Errorf("legacy load = %+v, want empty map", got)
	}
}

// TestDisplayNameModelFallback covers the resolution the cached model restores.
func TestDisplayNameModelFallback(t *testing.T) {
	cases := []struct {
		name, display, model, want string
	}{
		{"user name wins", "Daily", "Y", "Daily"},
		{"model fallback", "", "Y", "Model Y"},
		{"non-letter model verbatim", "", "Roadster", "Roadster"},
		{"nothing → Car id", "", "", "Car 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			car := &CarState{DisplayName: tc.display, Model: tc.model}
			if got := displayNameForCar("1", car); got != tc.want {
				t.Errorf("displayNameForCar = %q, want %q", got, tc.want)
			}
		})
	}
}
