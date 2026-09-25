package app_test

import "testing"

func TestCacheCleanRemovesCachedInstallers(t *testing.T) {
	env := newTestEnv(t)
	env.createClusters(t)

	env.run(t, "cache", "clean")
	env.run(t, "destroy", "--yes")

	if got := env.ocCallCount(t); got != 2 {
		t.Errorf("expected the installer to be extracted again for destroy, got %d extractions", got)
	}
	if got := len(env.installerDestroyCalls(t)); got != 1 {
		t.Errorf("expected 1 installer destroy call, got %d", got)
	}
}
