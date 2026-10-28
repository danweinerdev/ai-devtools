package store

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

// tempMarker names aid's temp files: `.<target>.aid-tmp-<random>`.
const tempMarker = ".aid-tmp-"

// WriteAtomic replaces path with data so that a crash at any point leaves
// either the old file or the new one, never a partial one: write a temp file in
// the same directory, fsync it, rename it over the target, then fsync the
// directory so the rename itself survives a power loss.
//
// Callers hold the work directory's lock; WriteAtomic doesn't lock.
func WriteAtomic(path string, data []byte) error {
	return writeAtomicWith(path, data, replaceFile)
}

func writeAtomicWith(path string, data []byte, replace func(oldpath, newpath string) error) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+tempMarker+"*")
	if err != nil {
		return fmt.Errorf("creating a temp file in %s: %w", dir, err)
	}
	name := tmp.Name()
	fail := func(step string, err error) error {
		tmp.Close()
		os.Remove(name)
		return fmt.Errorf("%s %s: %w", step, path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail("writing", err)
	}
	if err := tmp.Sync(); err != nil {
		return fail("syncing", err)
	}
	if err := tmp.Close(); err != nil {
		return fail("closing", err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return fail("setting the mode of", err)
	}
	if err := replace(name, path); err != nil {
		os.Remove(name)
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("syncing directory %s: %w", dir, err)
	}
	return nil
}

// SweepTemps removes temp files a crashed writer left in dir and returns their
// names. Only call it under the lock: outside it, a live writer's temp file
// would look abandoned.
func SweepTemps(dir string) ([]string, error) {
	return sweepTemps(dir, 0)
}

// sweepTemps removes temp files in dir at least minAge old. A positive age
// suits a directory with no lock of its own (`.aid/` itself), where only age
// tells an abandoned temp file from a live writer's.
func sweepTemps(dir string, minAge time.Duration) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var swept []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), ".") || !strings.Contains(e.Name(), tempMarker) {
			continue
		}
		if minAge > 0 {
			info, err := e.Info()
			if err != nil || time.Since(info.ModTime()) < minAge {
				continue
			}
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !os.IsNotExist(err) {
			return swept, err
		}
		swept = append(swept, e.Name())
	}
	return swept, nil
}

// newOf returns a pointer to a fresh zero value of v's type (v may be a value
// or a pointer).
func newOf(v any) any {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return reflect.New(t).Interface()
}
