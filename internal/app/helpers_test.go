package app_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/linkvt/hyperctl/internal/app"
)

const (
	testPullSecret = `{"auths":{"example.com":{"auth":"Zm9vOmJhcg=="}}}`
	testIAMUserARN = "arn:aws:iam::123456789012:user/tester"
)

// fakeOC records its arguments and "extracts" the fake openshift-install
// into the --to directory.
const fakeOC = `#!/bin/sh
d="$HYPERCTL_TEST_LOG/oc-$(ls "$HYPERCTL_TEST_LOG" | grep -c '^oc-')"
mkdir -p "$d"
echo "$*" > "$d/args"
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
printf '%s' "$OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE" > "$d/release-override"
printf '%s' "$OPENSHIFT_INSTALL_EXPERIMENTAL_DISABLE_IMAGE_POLICY" > "$d/disable-image-policy"
dir="$4"
mv "$dir/install-config.yaml" "$d/install-config.yaml"
mkdir -p "$dir/auth"
echo "fake kubeconfig" > "$dir/auth/kubeconfig"
echo '{}' > "$dir/metadata.json"
[ -z "$HYPERCTL_TEST_INSTALLER_FAIL" ] || exit 1
`

// testEnv isolates a hyperctl run: temp HOME, fake binaries on PATH and a
// fake AWS identity.
type testEnv struct {
	home          string
	logDir        string
	pullSecret    string
	callerARN     string
	defaultRegion string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	root := t.TempDir()
	e := &testEnv{
		home:       filepath.Join(root, "home"),
		logDir:     filepath.Join(root, "log"),
		pullSecret: filepath.Join(root, "pull-secret.json"),
		callerARN:  testIAMUserARN,
	}
	binDir := filepath.Join(root, "bin")
	fakeInstallerPath := filepath.Join(root, "fake-openshift-install")
	for _, dir := range []string{e.home, e.logDir, binDir} {
		mustMkdir(t, dir)
	}
	writeFile(t, filepath.Join(binDir, "oc"), fakeOC, 0o755)
	writeFile(t, fakeInstallerPath, fakeInstaller, 0o755)
	writeFile(t, e.pullSecret, testPullSecret, 0o600)

	t.Setenv("HOME", e.home)
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, "")
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
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
		STS:           fakeSTS{arn: e.callerARN},
		DefaultRegion: e.defaultRegion,
		Stdout:        &out,
	})
	err := cmd.Run(context.Background(), append([]string{"hyperctl"}, args...))
	return out.String(), err
}

type fakeSTS struct {
	arn string
}

func (f fakeSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	return &sts.GetCallerIdentityOutput{Arn: aws.String(f.arn)}, nil
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
