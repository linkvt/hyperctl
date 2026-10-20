package app

import "fmt"

// compactWorkerCount gives hosted control planes a worker node for management-cluster workloads.
const compactWorkerCount = 1

// defaultInstanceType sizes a compact management cluster.
const defaultInstanceType = "t3a.xlarge"

// sizePreset is a management cluster size offered in the prompt. Relative
// costs are of EC2 instances and NAT gateways in eu-central-1.
type sizePreset struct {
	label        string
	instanceType string
	workers      int
}

var sizePresets = []sizePreset{
	{"Compact: 3 control plane + 1 worker t3a.xlarge in 1 zone", defaultInstanceType, compactWorkerCount},
	{"Compact: 3 control plane + 1 worker m6i.xlarge in 1 zone", "m6i.xlarge", compactWorkerCount},
	{"Standard: 3 control plane + 3 worker nodes m6i.xlarge in 3 zones", "m6i.xlarge", 3},
}

// describeSize describes the management cluster size for the summary.
func describeSize(opts createOptions) string {
	if opts.Workers == 0 {
		return fmt.Sprintf("3 nodes %s, compact in 1 zone", opts.InstanceType)
	}
	if opts.Workers == compactWorkerCount {
		return fmt.Sprintf("3 control plane + 1 worker %s, compact in 1 zone", opts.InstanceType)
	}
	return fmt.Sprintf("3 control plane + %d worker nodes %s", opts.Workers, opts.InstanceType)
}
