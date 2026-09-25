package ocp

// Release is an OCP release payload.
type Release struct {
	Version string
}

// PullSpec returns the release image pull spec.
func (r Release) PullSpec() string {
	return "quay.io/openshift-release-dev/ocp-release:" + r.Version + "-x86_64"
}
