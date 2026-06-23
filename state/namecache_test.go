package state

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNameCacheRoundTrip locks the fix for the user-81 "Car 1" regression: the
// model must persist across restarts, not just the display_name.
func TestNameCacheRoundTrip(t *testing.T) {
	c := &nameCache{path: filepath.Join(t.TempDir(), cacheFileName)}

	c.Save(map[string]CachedCar{
		"1": {Model: "Y"},               // unnamed car — model only
		"2": {DisplayName: "Daily"},     // user-named
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

// TestNameCacheLegacyFormat ensures the old map[string]string file degrades
// gracefully (empty map) instead of erroring — it's rebuilt from live MQTT.
func TestNameCacheLegacyFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), cacheFileName)
	if err := os.WriteFile(path, []byte(`{"1":"Daily"}`), 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	c := &nameCache{path: path}
	if got := c.Load(); len(got) != 0 {
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
