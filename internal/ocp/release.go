package ocp

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const (
	quayReleaseRepo = "quay.io/openshift-release-dev/ocp-release"
	quayArchSuffix  = "-x86_64"
)

// Release is an OCP release payload.
type Release struct {
	// Name identifies the release, e.g. 5.1.0-ec.0 or a nightly tag.
	Name     string
	PullSpec string
}

// ParseRelease parses a release reference, either a version of an official
// release on quay.io or the pull spec of any release image.
func ParseRelease(ref string) Release {
	repo, tag, isPullSpec := strings.Cut(ref, ":")
	if !isPullSpec || !strings.Contains(repo, "/") {
		return Release{Name: ref, PullSpec: quayReleaseRepo + ":" + ref + quayArchSuffix}
	}
	name := tag
	if repo == quayReleaseRepo {
		name = strings.TrimSuffix(tag, quayArchSuffix)
	}
	return Release{Name: name, PullSpec: ref}
}

// Registry returns the host of the registry serving the release image.
func (r Release) Registry() string {
	host, _, _ := strings.Cut(r.PullSpec, "/")
	return host
}

// CheckPullable verifies that the release image can be read with the pull
// secret.
func CheckPullable(ctx context.Context, release Release, pullSecretPath string) error {
	cmd := exec.CommandContext(ctx, "oc", "adm", "release", "info",
		"--registry-config="+pullSecretPath,
		release.PullSpec,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("the release image %s cannot be pulled with the pull secret %s:\n%s", release.PullSpec, pullSecretPath, detail)
	}
	return nil
}
