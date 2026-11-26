package app

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/ocp"
	"github.com/linkvt/hyperctl/internal/releases"
	"github.com/linkvt/hyperctl/internal/xdg"
)

// createOptions are the values needed to create the clusters on every
// platform, the platformCreate holds the others.
type createOptions struct {
	Release        string
	Region         string
	BaseDomain     string
	Name           string
	PullSecret     string
	SSHKey         string
	HostedName     string
	ManagementOnly bool
	// StandaloneKarpenterOperator installs the HyperShift operator so that
	// hosted clusters run the standalone karpenter-operator instead of the one
	// embedded in the HyperShift image.
	StandaloneKarpenterOperator bool
}

func createCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create the management cluster",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "release", Usage: "OCP release version, e.g. 5.1.0-ec.0, or release image pull spec (default: last used)"},
			&cli.StringFlag{Name: "platform", Usage: "Cloud platform of the clusters, aws or azure (default: last used, or aws)"},
			&cli.StringFlag{Name: "region", Usage: "AWS region or Azure location, e.g. westus3 or germanywestcentral (default: last used, AWS profile region or " + defaultAzureLocation + ")"},
			&cli.StringFlag{Name: "base-domain", Usage: "Base domain with a public DNS zone (default: " + defaultBaseDomain + ", on Azure " + defaultAzureBaseDomain + ")"},
			&cli.StringFlag{Name: "name", Usage: "Management cluster name (default: $USER-karpenter)"},
			&cli.StringFlag{Name: "pull-secret", Usage: "Path to the pull secret file (default: ~/.openshift/pull-secret-latest.json)"},
			&cli.StringFlag{Name: "ssh-key", Usage: "Path to an SSH public key for node access (optional)"},
			&cli.BoolFlag{Name: "yes", Usage: "Do not ask anything: use the defaults for values not given as flags, skip the confirmation and delete DNS leftovers of previous clusters"},
			&cli.BoolFlag{Name: "management-only", Usage: "Create the management cluster and install the HyperShift operator without creating a hosted cluster"},
			&cli.BoolFlag{Name: "standalone-karpenter-operator", Value: true, Usage: "Run the standalone karpenter-operator in hosted clusters, =false (AWS only) runs the one embedded in the HyperShift operator image"},
			&cli.StringFlag{Name: "hosted-name", Usage: "Hosted cluster name (default: <name>-workload)"},
			&cli.StringFlag{Name: "instance-type", Usage: "EC2 instance type or Azure VM size of the management cluster nodes (default: " + defaultSize.InstanceType + ", on Azure " + defaultAzureVMSize + ")"},
			&cli.IntFlag{Name: "workers", Usage: "Worker nodes of the management cluster, on Azure the number of AKS nodes (default: 3; on AWS 0 or 1 runs a compact cluster in one zone)"},
			&cli.StringFlag{Name: "hypershift", Usage: "Path to the hypershift binary (default: from PATH)"},
			&cli.StringFlag{Name: "oidc-bucket", Usage: "S3 bucket for OIDC discovery documents on AWS, created if missing (default: $USER-hypershift-oidc)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runCreate(ctx, cmd, deps)
		},
	}
}

func runCreate(ctx context.Context, cmd *cli.Command, deps Deps) error {
	p := progress{w: cmd.Root().Writer, interactive: deps.Interactive}
	p.step("Checking prerequisites")
	dirs, err := xdg.Resolve("hyperctl")
	if err != nil {
		return err
	}
	st, err := loadState(dirs.State)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(dirs.Config)
	if err != nil {
		return err
	}
	clusters := newClusterStore(dirs)
	platformName, err := resolvePlatform(ctx, p, cmd, deps, clusters, st)
	if err != nil {
		return err
	}
	platform, err := newPlatform(platformName, deps, dirs, clusters)
	if err != nil {
		return err
	}
	run, err := platform.newCreate(cmd, st, cfg)
	if err != nil {
		return err
	}

	if err := run.checkAccount(ctx, p); err != nil {
		return err
	}
	hypershiftPath, err := resolveHypershift(ctx, cmd, deps, dirs, cfg)
	if err != nil {
		return err
	}
	p.done("hypershift %s", hypershiftPath)

	opts, err := resolveCreateOptions(ctx, p, cmd, deps, dirs, clusters, st, cfg, platformName, run)
	if err != nil {
		return err
	}
	if err := requireTrackedClusters(st, opts); err != nil {
		return err
	}
	st.LastRelease = opts.Release
	st.ManagementOnly = opts.ManagementOnly
	run.rememberRegion(&st, opts.Region)
	if err := saveState(dirs.State, st); err != nil {
		return err
	}

	if err := run.prepare(ctx, p, opts); err != nil {
		return err
	}

	// Track the clusters before creating them, so that incomplete ones can
	// be destroyed.
	st.MgmtCluster = opts.Name
	if err := saveState(dirs.State, st); err != nil {
		return err
	}
	p.step("Management cluster %s", opts.Name)
	if err := run.createMgmt(ctx, p, opts); err != nil {
		return err
	}

	p.step("HyperShift operator")
	hs := deps.Hypershift(hypershiftPath, clusters.kubeconfig(managementCluster(opts.Name)), p.tool("hypershift"))
	if err := run.installOperator(ctx, p, hs, opts); err != nil {
		return err
	}

	if !opts.ManagementOnly {
		st.HostedCluster = opts.HostedName
		if err := saveState(dirs.State, st); err != nil {
			return err
		}
		p.step("Hosted cluster %s", opts.HostedName)
		if err := run.createHosted(ctx, p, hs, opts); err != nil {
			return err
		}
	}

	p.step("Done, use the cluster kubeconfig with:")
	p.info("export KUBECONFIG=$(hyperctl kubeconfig)")
	return nil
}

// requireTrackedClusters fails if create would replace a tracked cluster, as
// only one management and one hosted cluster are supported.
func requireTrackedClusters(st state, opts createOptions) error {
	if st.MgmtCluster != "" && st.MgmtCluster != opts.Name {
		return fmt.Errorf("management cluster %s exists, destroy it first", st.MgmtCluster)
	}
	if !opts.ManagementOnly && st.HostedCluster != "" && st.HostedCluster != opts.HostedName {
		return fmt.Errorf("hosted cluster %s exists, destroy it first", st.HostedCluster)
	}
	return nil
}

// resolveCreateOptions takes values from flags, prompts for missing ones if it
// may prompt and falls back to defaults otherwise. The SSH key is only asked
// once and then saved to the config file.
func resolveCreateOptions(ctx context.Context, p progress, cmd *cli.Command, deps Deps, dirs xdg.Dirs, clusters clusterStore, st state, cfg config, platformName string, run platformCreate) (createOptions, error) {
	defaults, err := defaultCreateOptions(clusters, st, cfg, run)
	if err != nil {
		return createOptions{}, err
	}
	opts := createOptions{
		Release:        flagOr(cmd, "release", defaults.Release),
		Region:         flagOr(cmd, "region", defaults.Region),
		BaseDomain:     flagOr(cmd, "base-domain", defaults.BaseDomain),
		Name:           flagOr(cmd, "name", defaults.Name),
		PullSecret:     flagOr(cmd, "pull-secret", defaults.PullSecret),
		SSHKey:         flagOr(cmd, "ssh-key", defaults.SSHKey),
		HostedName:     flagOr(cmd, "hosted-name", defaults.HostedName),
		ManagementOnly: defaults.ManagementOnly,

		StandaloneKarpenterOperator: cmd.Bool("standalone-karpenter-operator"),
	}
	if cmd.IsSet("management-only") {
		opts.ManagementOnly = cmd.Bool("management-only")
	}
	if prompts(cmd, deps) {
		askSSHKey := !cmd.IsSet("ssh-key") && cfg.SSHKey == nil
		if err := promptCreateOptions(ctx, p, cmd, releases.Client{BaseURL: deps.ReleasesURL}, run, &opts, askSSHKey); err != nil {
			return createOptions{}, err
		}
		if err := confirmCreateOptions(ctx, platformName, run, opts); err != nil {
			return createOptions{}, err
		}
		if askSSHKey {
			cfg.SSHKey = &opts.SSHKey
			if err := saveConfig(dirs.Config, cfg); err != nil {
				return createOptions{}, err
			}
		}
	}
	if !opts.ManagementOnly && opts.HostedName == "" && opts.Name != "" {
		opts.HostedName = opts.Name + "-workload"
	}
	// Paths from the config file or prompts are not expanded by a shell.
	if opts.PullSecret, err = expandHome(opts.PullSecret); err != nil {
		return createOptions{}, err
	}
	if opts.SSHKey, err = expandHome(opts.SSHKey); err != nil {
		return createOptions{}, err
	}

	required := []struct{ flag, value string }{
		{"--release", opts.Release},
		{"--region", opts.Region},
		{"--base-domain", opts.BaseDomain},
		{"--name", opts.Name},
		{"--pull-secret", opts.PullSecret},
	}
	var missing []string
	for _, r := range required {
		if r.value == "" {
			missing = append(missing, r.flag)
		}
	}
	missing = append(missing, run.missing()...)
	if len(missing) > 0 {
		return createOptions{}, fmt.Errorf("missing required values: %s", strings.Join(missing, ", "))
	}
	if err := checkReleasePullable(ctx, p, deps.ReleaseImages, opts); err != nil {
		return createOptions{}, err
	}
	return opts, nil
}

// pullCheckTimeout bounds the release image pull check.
const pullCheckTimeout = 30 * time.Second

// ciRegistry is the registry of CI release images, its credentials come from
// an oc login token of the CI cluster.
const ciRegistry = "registry.ci.openshift.org"

// ciLoginURL is where to copy an oc login command with a token for the CI
// cluster from, which logs in to ciServer.
const (
	ciLoginURL = "https://oauth-openshift.apps.ci.l2s4.p1.openshiftapps.com/oauth/token/request"
	ciServer   = "https://api.ci.l2s4.p1.openshiftapps.com:6443"
)

// checkReleasePullable fails if the release image cannot be read with the pull
// secret, instead of failing when the image is first pulled.
func checkReleasePullable(ctx context.Context, p progress, images ReleaseImages, opts createOptions) error {
	release := ocp.ParseRelease(opts.Release)
	err := p.wait(ctx, "Checking access to "+release.PullSpec, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, pullCheckTimeout)
		defer cancel()
		return images.CheckPullable(ctx, release, opts.PullSecret)
	})
	if err != nil {
		if strings.Contains(err.Error(), "unauthorized") {
			return fmt.Errorf("%w\n%s", err, loginHint(release.Registry(), opts.PullSecret))
		}
		return err
	}
	p.done("%s is pullable", release.PullSpec)
	return nil
}

// loginHint explains how to add credentials for the registry to the pull
// secret.
func loginHint(registry, pullSecret string) string {
	if registry == ciRegistry {
		return fmt.Sprintf("log in to the CI cluster and save its token to the pull secret:\n  1. copy the oc login command (--server=%s) from %s and run it\n  2. oc registry login --registry %s --to %s",
			ciServer, ciLoginURL, registry, pullSecret)
	}
	return fmt.Sprintf("add credentials for %s to the pull secret with: oc registry login --registry %s --auth-basic=USER:PASSWORD --to %s",
		registry, registry, pullSecret)
}

// defaultCreateOptions returns the built-in defaults, overridden by the
// config file, the last used release and the tracked clusters.
func defaultCreateOptions(clusters clusterStore, st state, cfg config, run platformCreate) (createOptions, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return createOptions{}, err
	}
	opts := createOptions{
		ManagementOnly: st.ManagementOnly,
		Release:        st.LastRelease,
		Region:         run.defaultRegion(),
		BaseDomain:     run.defaultBaseDomain(),
		PullSecret:     cmp.Or(cfg.PullSecret, filepath.Join(home, ".openshift", "pull-secret-latest.json")),
	}
	if user := os.Getenv("USER"); user != "" {
		opts.Name = user + "-karpenter"
	}
	if cfg.SSHKey != nil {
		opts.SSHKey = *cfg.SSHKey
	}
	if st.MgmtCluster == "" {
		return opts, nil
	}

	// A tracked management cluster determines the settings of its hosted
	// cluster.
	opts.Name = st.MgmtCluster
	opts.HostedName = st.HostedCluster
	record, found, err := clusters.load(managementCluster(st.MgmtCluster))
	if err != nil || !found {
		return opts, err
	}
	opts.Release = record.Release
	opts.Region = record.Region
	opts.BaseDomain = record.BaseDomain
	opts.PullSecret = record.PullSecret
	return opts, nil
}

func flagOr(cmd *cli.Command, name, fallback string) string {
	if cmd.IsSet(name) {
		return cmd.String(name)
	}
	return fallback
}

// expandHome replaces a leading "~/" with the home directory.
func expandHome(path string) (string, error) {
	rest, ok := strings.CutPrefix(path, "~/")
	if !ok {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, rest), nil
}
