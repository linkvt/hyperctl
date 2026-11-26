package app_test

import "testing"

func TestCacheCleanRemovesCachedInstallers(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "cache", "clean")
	env.run(t, "destroy", "--yes")

	if got := env.images.extractions; got != 2 {
		t.Errorf("expected the installer to be extracted again for destroy, got %d extractions", got)
	}
	if got := len(env.images.destroys); got != 1 {
		t.Errorf("expected 1 installer destroy call, got %d", got)
	}
}
