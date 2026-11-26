package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/linkvt/hyperctl/internal/app"
)

const (
	testPullSecret       = `{"auths":{"example.com":{"auth":"Zm9vOmJhcg=="}}}`
	testIAMUserARN       = "arn:aws:iam::123456789012:user/tester"
	testHostedKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: cluster
  cluster:
    server: https://api.hosted.fake:6443
contexts:
- name: admin
  context:
    cluster: cluster
    namespace: default
    user: admin
current-context: admin
users:
- name: admin
  user:
    token: hosted-token
`
)

// testEnv isolates a hyperctl run: temp HOME and XDG directories and fakes
// of the clouds and the wrapped binaries.
type testEnv struct {
	home          string
	pullSecret    string
	aws           *fakeAWS
	hypershift    *fakeHypershift
	images        *fakeReleaseImages
	cluster       *fakeCluster
	azure         *fakeAzure
	defaultRegion string
	releasesURL   string
	// interactive runs hyperctl as in a terminal. Prompts fail in tests, as
	// they find no terminal.
	interactive bool
	// stderr is the error output of the last run.
	stderr string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	cluster := newFakeCluster()
	azure := newFakeAzure()
	e := &testEnv{
		home:       filepath.Join(root, "home"),
		pullSecret: filepath.Join(root, "pull-secret.json"),
		aws:        &fakeAWS{callerARN: testIAMUserARN, publicBuckets: map[string]string{}, dns: map[string][]string{}},
		azure:      azure,
		hypershift: newFakeHypershift(cluster, azure),
		images:     &fakeReleaseImages{},
		cluster:    cluster,
	}
	mustMkdir(t, filepath.Join(e.home, ".aws"))
	writeFile(t, filepath.Join(e.home, ".aws", "credentials"), "[default]\n", 0o600)
	writeFile(t, e.pullSecret, testPullSecret, 0o600)

	t.Setenv("HOME", e.home)
	t.Setenv("USER", "tester")
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, "")
	}
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")
	return e
}

// run executes hyperctl with the given arguments and fails the test on error.
func (e *testEnv) run(t *testing.T, args ...string) string {
	t.Helper()
	out, err := e.runErr(t, args...)
	if err != nil {
		t.Fatalf("hyperctl %v: %v\noutput:\n%s", args, err, out)
	}
	return out
}

func (e *testEnv) runErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd := app.New(app.Deps{
		AWS:           e.aws,
		Azure:         e.azure,
		Hypershift:    e.hypershift.cli,
		LookPath:      e.hypershift.lookPath,
		ReleaseImages: e.images,
		Cluster:       e.cluster,
		DefaultRegion: e.defaultRegion,
		ReleasesURL:   e.releasesURL,
		Interactive:   e.interactive,
		Stdout:        &out,
		Stderr:        &stderr,
	})
	err := cmd.Run(context.Background(), append([]string{"hyperctl"}, args...))
	e.stderr = stderr.String()
	return out.String(), err
}

// fakeAWS is an in-memory AWS account.
type fakeAWS struct {
	callerARN       string
	spotRoleMissing bool
	// publicBuckets maps bucket names to their region.
	publicBuckets  map[string]string
	bucketsCreated int
	// dns maps cluster names to their DNS entries.
	dns map[string][]string
	// beforeClusterDNS runs before the DNS entries of a cluster are looked up.
	beforeClusterDNS func()
}

func (f *fakeAWS) CallerIdentity(context.Context) (string, error) {
	return f.callerARN, nil
}

func (f *fakeAWS) SpotRoleExists(context.Context) (bool, error) {
	return !f.spotRoleMissing, nil
}

func (f *fakeAWS) BucketRegion(_ context.Context, bucket string) (string, bool, error) {
	region, found := f.publicBuckets[bucket]
	return region, found, nil
}

func (f *fakeAWS) CreatePublicBucket(_ context.Context, bucket, region string) error {
	f.publicBuckets[bucket] = region
	f.bucketsCreated++
	return nil
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeAWS) ClusterDNS(_ context.Context, _, name string) ([]string, error) {
	if f.beforeClusterDNS != nil {
		f.beforeClusterDNS()
	}
	return f.dns[name], nil
}

func (f *fakeAWS) DeleteClusterDNS(_ context.Context, _, name string) error {
	delete(f.dns, name)
	return nil
}

func (f *fakeAWS) FirstZone(_ context.Context, region string) (string, error) {
	return region + "b", nil
}
