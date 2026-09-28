package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/linkvt/hyperctl/internal/app"
)

const (
	testPullSecret = `{"auths":{"example.com":{"auth":"Zm9vOmJhcg=="}}}`
	testIAMUserARN = "arn:aws:iam::123456789012:user/tester"
)

// fakeOC records its arguments and "extracts" the fake openshift-install
// into the --to directory. For status checks, the API is down when
// HYPERCTL_TEST_API_DOWN is set and the hosted cluster's Available condition
// is HYPERCTL_TEST_HOSTED_AVAILABLE.
const fakeOC = `#!/bin/sh
d="$HYPERCTL_TEST_LOG/oc-$(ls "$HYPERCTL_TEST_LOG" | grep -c '^oc-')"
mkdir -p "$d"
echo "$*" > "$d/args"
case "$*" in
  *"get --raw /readyz"*) [ -z "$HYPERCTL_TEST_API_DOWN" ] || exit 1; echo ok; exit 0 ;;
  *"get hostedcluster"*) printf '%s' "$HYPERCTL_TEST_HOSTED_AVAILABLE"; exit 0 ;;
esac
for a in "$@"; do
  case "$a" in --to=*) to="${a#--to=}" ;; esac
done
mkdir -p "$to"
cp "$HYPERCTL_TEST_FAKE_INSTALLER" "$to/openshift-install"
chmod +x "$to/openshift-install"
`

// fakeInstaller records its arguments, relevant env vars and the consumed
// install-config, then writes the files the real installer leaves behind.
const fakeInstaller = `#!/bin/sh
d="$HYPERCTL_TEST_LOG/openshift-install-$(ls "$HYPERCTL_TEST_LOG" | grep -c '^openshift-install-')"
mkdir -p "$d"
echo "$*" > "$d/args"
[ "$1" = "destroy" ] && exit 0
echo "level=info msg=fake install" >&2
printf '%s' "$OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE" > "$d/release-override"
printf '%s' "$OPENSHIFT_INSTALL_EXPERIMENTAL_DISABLE_IMAGE_POLICY" > "$d/disable-image-policy"
dir="$4"
mv "$dir/install-config.yaml" "$d/install-config.yaml"
mkdir -p "$dir/auth"
echo "fake kubeconfig" > "$dir/auth/kubeconfig"
echo '{}' > "$dir/metadata.json"
[ -z "$HYPERCTL_TEST_INSTALLER_FAIL" ] || exit 1
`

// fakeHypershift records its arguments and the kubeconfig it runs against.
// "create kubeconfig" prints a fake hosted cluster kubeconfig. It fails for
// the subcommand in HYPERCTL_TEST_HYPERSHIFT_FAIL.
const fakeHypershift = `#!/bin/sh
d="$HYPERCTL_TEST_LOG/hypershift-$(ls "$HYPERCTL_TEST_LOG" | grep -c '^hypershift-')"
mkdir -p "$d"
echo "$*" > "$d/args"
printf '%s' "$KUBECONFIG" > "$d/kubeconfig"
[ -n "$HYPERCTL_TEST_HYPERSHIFT_FAIL" ] && [ "$1" = "$HYPERCTL_TEST_HYPERSHIFT_FAIL" ] && exit 1
case "$1 $2" in
  "create kubeconfig") echo "fake hosted kubeconfig" ;;
  *) echo "fake $1 output" ;;
esac
`

// testEnv isolates a hyperctl run: temp HOME, fake binaries on PATH and a
// fake AWS identity.
type testEnv struct {
	binDir        string
	home          string
	logDir        string
	pullSecret    string
	aws           *fakeAWS
	defaultRegion string
	releasesURL   string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	e := &testEnv{
		home:       filepath.Join(root, "home"),
		logDir:     filepath.Join(root, "log"),
		pullSecret: filepath.Join(root, "pull-secret.json"),
		aws:        &fakeAWS{callerARN: testIAMUserARN, publicBuckets: map[string]string{}, dns: map[string][]string{}},
	}
	binDir := filepath.Join(root, "bin")
	e.binDir = binDir
	fakeInstallerPath := filepath.Join(root, "fake-openshift-install")
	for _, dir := range []string{e.home, e.logDir, binDir} {
		mustMkdir(t, dir)
	}
	writeFile(t, filepath.Join(binDir, "oc"), fakeOC, 0o755)
	writeFile(t, filepath.Join(binDir, "hypershift"), fakeHypershift, 0o755)
	mustMkdir(t, filepath.Join(e.home, ".aws"))
	writeFile(t, filepath.Join(e.home, ".aws", "credentials"), "[default]\n", 0o600)
	writeFile(t, fakeInstallerPath, fakeInstaller, 0o755)
	writeFile(t, e.pullSecret, testPullSecret, 0o600)

	t.Setenv("HOME", e.home)
	t.Setenv("USER", "tester")
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, "")
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "")
	t.Setenv("HYPERCTL_TEST_LOG", e.logDir)
	t.Setenv("HYPERCTL_TEST_FAKE_INSTALLER", fakeInstallerPath)
	return e
}

// moveHypershiftOutOfPath moves the fake hypershift out of PATH and returns
// its new location.
func (e *testEnv) moveHypershiftOutOfPath(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(filepath.Dir(e.binDir), "hypershift-bin")
	mustMkdir(t, dir)
	path := filepath.Join(dir, "hypershift")
	if err := os.Rename(filepath.Join(e.binDir, "hypershift"), path); err != nil {
		t.Fatal(err)
	}
	return path
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
	var out bytes.Buffer
	cmd := app.New(app.Deps{
		AWS:           e.aws,
		DefaultRegion: e.defaultRegion,
		ReleasesURL:   e.releasesURL,
		Stdout:        &out,
	})
	err := cmd.Run(context.Background(), append([]string{"hyperctl"}, args...))
	return out.String(), err
}

// hypershiftCall is one recorded invocation of the fake hypershift.
type hypershiftCall struct {
	args       string
	kubeconfig string
}

// hypershiftCalls reads the invocations recorded by the fake hypershift.
func (e *testEnv) hypershiftCalls(t *testing.T) []hypershiftCall {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(e.logDir, "hypershift-*"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []hypershiftCall
	for _, dir := range dirs {
		calls = append(calls, hypershiftCall{
			args:       readFile(t, filepath.Join(dir, "args")),
			kubeconfig: readFile(t, filepath.Join(dir, "kubeconfig")),
		})
	}
	return calls
}

// hypershiftCall returns the recorded hypershift invocation whose arguments
// start with prefix.
func (e *testEnv) hypershiftCall(t *testing.T, prefix string) hypershiftCall {
	t.Helper()
	for _, call := range e.hypershiftCalls(t) {
		if strings.HasPrefix(call.args, prefix) {
			return call
		}
	}
	t.Fatalf("no hypershift call with prefix %q in %v", prefix, e.hypershiftCalls(t))
	return hypershiftCall{}
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
	return f.dns[name], nil
}

func (f *fakeAWS) DeleteClusterDNS(_ context.Context, _, name string) error {
	delete(f.dns, name)
	return nil
}
