package xdg_test

import (
	"testing"

	"github.com/linkvt/hyperctl/internal/xdg"
)

func TestResolve(t *testing.T) {
	tests := map[string]struct {
		env  map[string]string
		want xdg.Dirs
	}{
		"defaults below home": {
			env: map[string]string{"HOME": "/home/u"},
			want: xdg.Dirs{
				Config: "/home/u/.config/app",
				Data:   "/home/u/.local/share/app",
				State:  "/home/u/.local/state/app",
				Cache:  "/home/u/.cache/app",
			},
		},
		"XDG variables override defaults": {
			env: map[string]string{
				"HOME":            "/home/u",
				"XDG_CONFIG_HOME": "/xdg/config",
				"XDG_DATA_HOME":   "/xdg/data",
				"XDG_STATE_HOME":  "/xdg/state",
				"XDG_CACHE_HOME":  "/xdg/cache",
			},
			want: xdg.Dirs{
				Config: "/xdg/config/app",
				Data:   "/xdg/data/app",
				State:  "/xdg/state/app",
				Cache:  "/xdg/cache/app",
			},
		},
		"relative XDG paths are ignored": {
			env: map[string]string{
				"HOME":            "/home/u",
				"XDG_CONFIG_HOME": "relative/config",
			},
			want: xdg.Dirs{
				Config: "/home/u/.config/app",
				Data:   "/home/u/.local/share/app",
				State:  "/home/u/.local/state/app",
				Cache:  "/home/u/.cache/app",
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
				t.Setenv(v, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			got, err := xdg.Resolve("app")
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
