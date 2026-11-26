package app_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// fakeOC records its arguments and "extracts" the fake openshift-install
// into the --to directory. For status checks, the API is down when
// HYPERCTL_TEST_API_DOWN is set and the hosted cluster's Available condition
// is HYPERCTL_TEST_HOSTED_AVAILABLE. HostedCluster annotations are kept as
// files in $HYPERCTL_TEST_LOG/annotations. HYPERCTL_TEST_HOSTED_KUBECONFIG_SECRET
// names the hosted kubeconfig secret, whose content is read from the file at
// HYPERCTL_TEST_HOSTED_KUBECONFIG. HYPERCTL_TEST_KUBECONFIG_ERROR fails lookups.
// Waiting for a deleted namespace fails with HYPERCTL_TEST_WAIT_ERROR, and
// HYPERCTL_TEST_NAMESPACE_GONE makes namespace lookups find nothing.
const fakeOC = `#!/bin/sh
d="$HYPERCTL_TEST_LOG/oc-$(ls "$HYPERCTL_TEST_LOG" | grep -c '^oc-')"
mkdir -p "$d"
echo "$*" > "$d/args"
annotations="$HYPERCTL_TEST_LOG/annotations"
mkdir -p "$annotations"
# annotations set through the HostedControlPlane, which the standalone
# karpenter-operator reads
hcpAnnotations="$HYPERCTL_TEST_LOG/hcp-annotations"
mkdir -p "$hcpAnnotations"
case "$*" in
  *"adm release info"*)
    [ -z "$HYPERCTL_TEST_RELEASE_INFO_ERROR" ] || { printf '%s\n' "$HYPERCTL_TEST_RELEASE_INFO_ERROR" >&2; exit 1; }
    exit 0 ;;
  *"get --raw /readyz"*) [ -z "$HYPERCTL_TEST_API_DOWN" ] || exit 1; echo ok; exit 0 ;;
  *"get hostedcontrolplane"*"-o json")
    printf '{"metadata":{"annotations":{'
    sep=""
    for f in "$hcpAnnotations"/*; do
      [ -e "$f" ] || continue
      printf '%s"%s":"%s"' "$sep" "$(basename "$f" | tr '_' '/')" "$(cat "$f")"
      sep=","
    done
    printf '}}'
    printf '}'
    exit 0 ;;
  *"get hostedcluster"*"-o json")
    if [ -n "$HYPERCTL_TEST_KUBECONFIG_ERROR" ]; then
      printf '%s\n' "$HYPERCTL_TEST_KUBECONFIG_ERROR" >&2
      exit 1
    fi
    printf '{"metadata":{"annotations":{'
    sep=""
    for f in "$annotations"/*; do
      [ -e "$f" ] || continue
      printf '%s"%s":"%s"' "$sep" "$(basename "$f" | tr '_' '/')" "$(cat "$f")"
      sep=","
    done
    printf '}}'
    if [ -n "$HYPERCTL_TEST_HOSTED_KUBECONFIG_SECRET" ]; then
      printf ',"status":{"kubeconfig":{"name":"%s"}}' "$HYPERCTL_TEST_HOSTED_KUBECONFIG_SECRET"
    fi
    printf '}'
    exit 0 ;;
  *"get secret "*)
    [ -n "$HYPERCTL_TEST_HOSTED_KUBECONFIG" ] || exit 0
    printf '{"data":{"kubeconfig":"'
    base64 < "$HYPERCTL_TEST_HOSTED_KUBECONFIG" | tr -d '\n'
    printf '"}}'
    exit 0 ;;
  *"get hostedcluster"*) printf '%s' "$HYPERCTL_TEST_HOSTED_AVAILABLE"; exit 0 ;;
  *"annotate hostedcluster"*)
    for a in "$@"; do
      case "$a" in
        *=*) key="${a%%=*}"; printf '%s' "${a#*=}" > "$annotations/$(echo "$key" | tr '/' '_')" ;;
        *-) rm -f "$annotations/$(echo "${a%-}" | tr '/' '_')" ;;
      esac
    done
    exit 0 ;;
  *"annotate hostedcontrolplane"*)
    for a in "$@"; do
      case "$a" in
        *=*) key="${a%%=*}"; printf '%s' "${a#*=}" > "$hcpAnnotations/$(echo "$key" | tr '/' '_')" ;;
        *-) rm -f "$hcpAnnotations/$(echo "${a%-}" | tr '/' '_')" ;;
      esac
    done
    exit 0 ;;
  *"delete pod"*) exit 0 ;;
  *"apply --server-side"*) exit 0 ;;
  *"delete hostedcluster"*) exit 0 ;;
  *"wait --for=delete"*)
    if [ -n "$HYPERCTL_TEST_WAIT_ERROR" ]; then
      printf '%s\n' "$HYPERCTL_TEST_WAIT_ERROR" >&2
      exit 1
    fi
    exit 0 ;;
  *"get namespace "*)
    if [ -n "$HYPERCTL_TEST_NAMESPACE_GONE" ]; then exit 0; fi
    prev=""
    for a in "$@"; do
      if [ "$prev" = "namespace" ]; then printf 'namespace/%s\n' "$a"; exit 0; fi
      prev="$a"
    done
    exit 0 ;;
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
cat > "$dir/auth/kubeconfig" <<KUBECONFIG
apiVersion: v1
kind: Config
clusters:
- name: fake-mgmt
  cluster:
    server: https://api.mgmt.fake:6443
contexts:
- name: admin
  context:
    cluster: fake-mgmt
    user: admin
current-context: admin
users:
- name: admin
  user:
    token: mgmt-token
KUBECONFIG
echo '{}' > "$dir/metadata.json"
[ -z "$HYPERCTL_TEST_INSTALLER_FAIL" ] || exit 1
`

// testEnv isolates a hyperctl run: temp HOME, fake binaries on PATH and a
// fake AWS identity.
type testEnv struct {
	binDir        string
	home          string
	logDir        string
	pullSecret    string
	aws           *fakeAWS
	hypershift    *fakeHypershift
	azure         *fakeAzure
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
		azure:      newFakeAzure(),
		hypershift: newFakeHypershift(),
	}
	binDir := filepath.Join(root, "bin")
	e.binDir = binDir
	fakeInstallerPath := filepath.Join(root, "fake-openshift-install")
	for _, dir := range []string{e.home, e.logDir, binDir} {
		mustMkdir(t, dir)
	}
	writeFile(t, filepath.Join(binDir, "oc"), fakeOC, 0o755)
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
		Azure:         e.azure,
		Hypershift:    e.hypershift.cli,
		LookPath:      e.hypershift.lookPath,
		DefaultRegion: e.defaultRegion,
		ReleasesURL:   e.releasesURL,
		Stdout:        &out,
		Stderr:        io.Discard,
	})
	err := cmd.Run(context.Background(), append([]string{"hyperctl"}, args...))
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

func (f *fakeAWS) FirstZone(_ context.Context, region string) (string, error) {
	return region + "b", nil
}

// ocCalls returns the arguments of recorded oc invocations that contain
// the given text.
func (e *testEnv) ocCalls(t *testing.T, contains string) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(e.logDir, "oc-*"))
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, dir := range dirs {
		if args := readFile(t, filepath.Join(dir, "args")); strings.Contains(args, contains) {
			calls = append(calls, args)
		}
	}
	return calls
}
