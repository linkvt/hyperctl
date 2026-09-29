package app

import "fmt"

// defaultInstanceType sizes a compact management cluster, which only runs
// hosted control planes without much load.
const defaultInstanceType = "t3a.xlarge"

// sizePreset is a management cluster size offered in the prompt. Relative
// costs are of EC2 instances and NAT gateways in eu-central-1.
type sizePreset struct {
	label        string
	instanceType string
	workers      int
}

var sizePresets = []sizePreset{
	{"Compact: 3 nodes t3a.xlarge in 1 zone (~40% of Standard)", defaultInstanceType, 0},
	{"Compact: 3 nodes m6i.xlarge in 1 zone (~50% of Standard)", "m6i.xlarge", 0},
	{"Standard: 3 control plane + 3 worker nodes m6i.xlarge in 3 zones", "m6i.xlarge", 3},
}

// describeSize describes the management cluster size for the summary.
func describeSize(opts createOptions) string {
	if opts.Workers == 0 {
		return fmt.Sprintf("3 nodes %s, compact in 1 zone", opts.InstanceType)
	}
	return fmt.Sprintf("3 control plane + %d worker nodes %s", opts.Workers, opts.InstanceType)
}
