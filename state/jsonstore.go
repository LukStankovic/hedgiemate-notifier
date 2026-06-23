package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// jsonStore is a small JSON file cache for any value type. Writes are atomic
// (temp file + rename) and reads fall back to empty() on any error.
type jsonStore[T any] struct {
	path  string
	empty func() T
}

// newJSONStore puts the file in /data (Docker volume) when present, else cwd.
func newJSONStore[T any](filename string, empty func() T) *jsonStore[T] {
	dir := "/data"
	if _, err := os.Stat(dir); err != nil {
		dir = "."
	}
	return &jsonStore[T]{path: filepath.Join(dir, filename), empty: empty}
}

func (s *jsonStore[T]) Path() string { return s.path }

func (s *jsonStore[T]) Load() T {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return s.empty()
	}
	v := s.empty()
	if err := json.Unmarshal(data, &v); err != nil {
		return s.empty()
	}
	return v
}

func (s *jsonStore[T]) Save(v T) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
	}
}
