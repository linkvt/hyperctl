package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/huh/v2"

	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// compactWorkerCount gives hosted control planes a worker node for management-cluster workloads.
const compactWorkerCount = 1

// openshiftSize is the size of an OpenShift management cluster on AWS.
type openshiftSize struct {
	// InstanceType is the EC2 instance type of all nodes.
	InstanceType string
	// Workers is the number of worker nodes, with 0 or compactWorkerCount
	// the cluster runs in a single zone.
	Workers int
}

// defaultSize is the size openshift-install uses by default on AWS.
var defaultSize = openshiftSize{InstanceType: "m6i.xlarge", Workers: 3}

// sizePreset is a cluster size offered in the prompt.
type sizePreset struct {
	label string
	size  openshiftSize
}

var sizePresets = []sizePreset{
	{"Standard: 3 control plane + 3 worker nodes m6i.xlarge in 3 zones", defaultSize},
	{"Compact: 3 control plane + 1 worker t3a.xlarge in 1 zone", openshiftSize{"t3a.xlarge", compactWorkerCount}},
	{"Compact: 3 control plane + 1 worker m6i.xlarge in 1 zone", openshiftSize{"m6i.xlarge", compactWorkerCount}},
}

// customSize marks the custom instance type option of the size prompt.
const customSize = -1

// ensureMgmtCluster installs the management cluster unless it is installed.
func ensureMgmtCluster(ctx context.Context, p progress, deps Deps, dirs xdg.Dirs, clusters clusterStore, opts createOptions, size openshiftSize) error {
	installed, err := clusters.check(managementCluster(opts.Name))
	if err != nil {
		return err
	}
	if installed {
		p.done("%s %s is already installed", opts.Setup.clusterTitle(), opts.Name)
		return nil
	}
	return installMgmtCluster(ctx, p, deps, dirs, clusters, opts, size)
}

// installMgmtCluster extracts the installer for the release and installs the
// management cluster, tracking progress in the cluster record.
func installMgmtCluster(ctx context.Context, p progress, deps Deps, dirs xdg.Dirs, clusters clusterStore, opts createOptions, size openshiftSize) error {
	release := ocp.ParseRelease(opts.Release)

	pullSecret, err := os.ReadFile(opts.PullSecret)
	if err != nil {
		return fmt.Errorf("reading pull secret: %w", err)
	}
	var sshKey []byte
	if opts.SSHKey != "" {
		if sshKey, err = os.ReadFile(opts.SSHKey); err != nil {
			return fmt.Errorf("reading SSH key: %w", err)
		}
	}

	installer, err := ensureInstaller(ctx, p, deps.ReleaseImages, dirs, release, opts.PullSecret)
	if err != nil {
		return err
	}

	var zone string
	if size.Workers == 0 || size.Workers == compactWorkerCount {
		err := p.wait(ctx, "Choosing an availability zone", func(ctx context.Context) error {
			var err error
			zone, err = deps.AWS.FirstZone(ctx, opts.Region)
			return err
		})
		if err != nil {
			return err
		}
		p.done("Compact cluster in availability zone %s", zone)
	}

	installConfig, err := ocp.RenderInstallConfig(ocp.InstallConfigParams{
		Name:         opts.Name,
		BaseDomain:   opts.BaseDomain,
		Region:       opts.Region,
		Zone:         zone,
		InstanceType: size.InstanceType,
		Workers:      size.Workers,
		PullSecret:   strings.TrimSpace(string(pullSecret)),
		SSHKey:       strings.TrimSpace(string(sshKey)),
	})
	if err != nil {
		return err
	}

	c := managementCluster(opts.Name)
	dir := clusters.dir(c)
	installDir := clusters.installDir(c)
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		return err
	}
	// The installer consumes install-config.yaml, keep a copy for reference.
	for _, d := range []string{dir, installDir} {
		if err := os.WriteFile(filepath.Join(d, "install-config.yaml"), installConfig, 0o600); err != nil {
			return err
		}
	}

	if err := clusters.begin(c, clusterRecord{
		Platform:   platformAWS,
		Release:    opts.Release,
		Region:     opts.Region,
		BaseDomain: opts.BaseDomain,
		PullSecret: opts.PullSecret,
	}); err != nil {
		return err
	}
	p.info("Installing, this takes about 45 minutes (log: %s)", filepath.Join(installDir, ".openshift_install.log"))
	start := time.Now()
	if err := deps.ReleaseImages.CreateCluster(ctx, installer, installDir, release, p.tool("openshift-install")); err != nil {
		return err
	}
	if err := clusters.markInstalled(c); err != nil {
		return err
	}
	p.done("Installed %s %s (%s)", opts.Setup.clusterNoun(), opts.Name, since(start))
	return nil
}

// ensureInstaller returns the openshift-install binary for the release from
// the cache, extracting it from the release image if needed.
func ensureInstaller(ctx context.Context, p progress, images ReleaseImages, dirs xdg.Dirs, release ocp.Release, pullSecret string) (ocp.Installer, error) {
	cacheDir := filepath.Join(dirs.Cache, "releases")
	if installer, found := images.CachedInstaller(cacheDir, release); found {
		p.done("Using cached openshift-install %s", release.Name)
		return installer, nil
	}
	var installer ocp.Installer
	start := time.Now()
	err := p.wait(ctx, "Extracting openshift-install "+release.Name+" from "+release.PullSpec, func(ctx context.Context) error {
		var err error
		installer, err = images.ExtractInstaller(ctx, cacheDir, release, pullSecret)
		return err
	})
	if err != nil {
		return ocp.Installer{}, err
	}
	p.done("Extracted openshift-install %s (%s)", release.Name, since(start))
	return installer, nil
}

// describe describes the size for the summary.
func (s openshiftSize) describe() string {
	if s.Workers == 0 {
		return fmt.Sprintf("3 nodes %s, compact in 1 zone", s.InstanceType)
	}
	if s.Workers == compactWorkerCount {
		return fmt.Sprintf("3 control plane + 1 worker %s, compact in 1 zone", s.InstanceType)
	}
	return fmt.Sprintf("3 control plane + %d worker nodes %s", s.Workers, s.InstanceType)
}

// promptOpenShiftSize lets the user pick a cluster size preset or a
// custom instance type for a compact cluster.
func promptOpenShiftSize(ctx context.Context, title string, size *openshiftSize) error {
	selected := customSize
	var options []huh.Option[int]
	for i, preset := range sizePresets {
		options = append(options, huh.NewOption(preset.label, i))
		if preset.size == *size {
			selected = i
		}
	}
	options = append(options, huh.NewOption("Compact with a custom instance type and 1 worker in 1 zone", customSize))
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[int]().Title(title).Options(options...).Value(&selected),
	)).RunWithContext(ctx); err != nil {
		return err
	}

	if selected != customSize {
		*size = sizePresets[selected].size
		return nil
	}
	size.Workers = compactWorkerCount
	return huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("EC2 instance type").
			Description("At least 4 vCPU and 16 GiB memory").
			Value(&size.InstanceType).
			Validate(requireValue),
	)).RunWithContext(ctx)
}

// destroyMgmtCluster destroys the management cluster with openshift-install.
func destroyMgmtCluster(ctx context.Context, p progress, yes bool, deps Deps, dirs xdg.Dirs, clusters clusterStore, name string) error {
	c := managementCluster(name)
	record, found, err := clusters.destroying(c)
	if err != nil {
		return err
	}
	if !found {
		p.done("Installation never started, nothing to destroy")
		return nil
	}
	installer, err := ensureInstaller(ctx, p, deps.ReleaseImages, dirs, ocp.ParseRelease(record.Release), record.PullSecret)
	if err != nil {
		return err
	}
	p.info("Destroying")
	start := time.Now()
	if err := deps.ReleaseImages.DestroyCluster(ctx, installer, clusters.installDir(c), p.tool("openshift-install")); err != nil {
		return err
	}
	p.done("Destroyed management cluster %s (%s)", name, since(start))
	if err := deleteDNSLeftovers(ctx, p, deps, yes, record.BaseDomain, name); err != nil {
		return err
	}
	return clusters.forget(c)
}
