// Package xdg resolves hyperctl's config, data, state and cache directories
// following the XDG Base Directory Specification on all platforms.
package xdg

import (
	"os"
	"path/filepath"
)

// Dirs are the per-application base directories.
type Dirs struct {
	Config string
	Data   string
	State  string
	Cache  string
}

// Resolve returns the base directories for the given application name.
// XDG_* variables are honored if set to absolute paths, as required by the
// specification.
func Resolve(app string) (Dirs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Dirs{}, err
	}
	return Dirs{
		Config: filepath.Join(baseDir("XDG_CONFIG_HOME", home, ".config"), app),
		Data:   filepath.Join(baseDir("XDG_DATA_HOME", home, ".local", "share"), app),
		State:  filepath.Join(baseDir("XDG_STATE_HOME", home, ".local", "state"), app),
		Cache:  filepath.Join(baseDir("XDG_CACHE_HOME", home, ".cache"), app),
	}, nil
}

func baseDir(envVar, home string, defaultElems ...string) string {
	if dir := os.Getenv(envVar); filepath.IsAbs(dir) {
		return dir
	}
	return filepath.Join(append([]string{home}, defaultElems...)...)
}
