package app

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/hypershift"
	"github.com/linkvt/hyperctl/internal/releases"
	"github.com/linkvt/hyperctl/internal/xdg"
)

const defaultBaseDomain = "autoscaling-dev.devcluster.openshift.com"

// createOptions are the values needed to create the clusters.
type createOptions struct {
	Release    string
	Region     string
	BaseDomain string
	Name       string
	PullSecret string
	SSHKey     string
	OIDCBucket string
	HostedName string
	// InstanceType and Workers size the management cluster, see mgmtSize.
	InstanceType string
	Workers      int
}

func createCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create the management cluster",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "release", Usage: "OCP release version, e.g. 5.1.0-ec.0, or release image pull spec (default: last used)"},
			&cli.StringFlag{Name: "region", Usage: "AWS region (default: region of the AWS profile)"},
			&cli.StringFlag{Name: "base-domain", Usage: "Route53 base domain (default: " + defaultBaseDomain + ")"},
			&cli.StringFlag{Name: "name", Usage: "Management cluster name (default: $USER-karpenter)"},
			&cli.StringFlag{Name: "pull-secret", Usage: "Path to the pull secret file (default: ~/.openshift/pull-secret-latest.json)"},
			&cli.StringFlag{Name: "ssh-key", Usage: "Path to an SSH public key for node access (optional)"},
			&cli.BoolFlag{Name: "yes", Usage: "Do not ask for confirmation, also not for deleting DNS leftovers of previous clusters"},
			&cli.StringFlag{Name: "hosted-name", Usage: "Hosted cluster name (default: <name>-workload)"},
			&cli.StringFlag{Name: "instance-type", Usage: "EC2 instance type of the management cluster nodes (default: " + defaultInstanceType + ")"},
			&cli.IntFlag{Name: "workers", Usage: "Worker nodes of the management cluster, 0 runs a compact cluster in a single zone (default: 0)"},
			&cli.StringFlag{Name: "hypershift", Usage: "Path to the hypershift binary (default: from PATH)"},
			&cli.StringFlag{Name: "oidc-bucket", Usage: "S3 bucket for OIDC discovery documents, created if missing (default: $USER-hypershift-oidc)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runCreate(ctx, cmd, deps)
		},
	}
}

func runCreate(ctx context.Context, cmd *cli.Command, deps Deps) error {
	p := progress{w: cmd.Root().Writer, interactive: deps.Interactive}
	p.step("Checking prerequisites")
	var identity string
	err := p.wait(ctx, "Checking the AWS identity", func(ctx context.Context) error {
		var err error
		identity, err = requireIAMUser(ctx, deps.AWS)
		return err
	})
	if err != nil {
		return err
	}
	p.done("AWS identity %s", identity)

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
	hypershiftPath, err := resolveHypershift(ctx, cmd, deps, dirs, cfg)
	if err != nil {
		return err
	}
	p.done("hypershift %s", hypershiftPath)
	credentialsFile, err := awsCredentialsFile()
	if err != nil {
		return err
	}
	p.done("AWS credentials file %s", credentialsFile)

	opts, err := resolveCreateOptions(ctx, p, cmd, deps, dirs, st, cfg)
	if err != nil {
		return err
	}
	if err := requireTrackedClusters(st, opts); err != nil {
		return err
	}
	st.LastRelease = opts.Release
	if err := saveState(dirs.State, st); err != nil {
		return err
	}

	p.step("Checking the AWS account")
	oidcBucketRegion, err := ensureAWSPrerequisites(ctx, p, deps.AWS, opts)
	if err != nil {
		return err
	}
	untracked, err := untrackedClusterNames(dirs, opts)
	if err != nil {
		return err
	}
	if err := deleteDNSLeftovers(ctx, p, deps, cmd.Bool("yes"), opts.BaseDomain, untracked...); err != nil {
		return err
	}

	// Track the clusters before creating them, so that incomplete ones can
	// be destroyed.
	st.MgmtCluster = opts.Name
	if err := saveState(dirs.State, st); err != nil {
		return err
	}
	p.step("Management cluster %s", opts.Name)
	if err := ensureMgmtCluster(ctx, p, deps.AWS, dirs, opts); err != nil {
		return err
	}

	p.step("HyperShift operator")
	hs := hypershift.CLI{
		Path:       hypershiftPath,
		Kubeconfig: mgmtKubeconfigPath(dirs, opts.Name),
		Output:     p.tool("hypershift"),
	}
	start := time.Now()
	if err := hs.Install(ctx, hypershift.InstallParams{
		OIDCBucket:         opts.OIDCBucket,
		OIDCBucketRegion:   oidcBucketRegion,
		AWSCredentialsFile: credentialsFile,
	}); err != nil {
		return err
	}
	p.done("Installed the HyperShift operator (%s)", since(start))

	st.HostedCluster = opts.HostedName
	if err := saveState(dirs.State, st); err != nil {
		return err
	}
	p.step("Hosted cluster %s", opts.HostedName)
	if err := ensureHostedCluster(ctx, p, hs, dirs, opts); err != nil {
		return err
	}

	p.step("Done, use the hosted cluster with:")
	p.info("export KUBECONFIG=$(hyperctl kubeconfig)")
	return nil
}

// requireTrackedClusters fails if create would replace a tracked cluster, as
// only one management and one hosted cluster are supported.
func requireTrackedClusters(st state, opts createOptions) error {
	if st.MgmtCluster != "" && st.MgmtCluster != opts.Name {
		return fmt.Errorf("management cluster %s exists, destroy it first", st.MgmtCluster)
	}
	if st.HostedCluster != "" && st.HostedCluster != opts.HostedName {
		return fmt.Errorf("hosted cluster %s exists, destroy it first", st.HostedCluster)
	}
	return nil
}

// untrackedClusterNames returns the names of the clusters to create that
// hyperctl has no record of. DNS entries with these names are leftovers.
func untrackedClusterNames(dirs xdg.Dirs, opts createOptions) ([]string, error) {
	var names []string
	for name, dir := range map[string]string{
		opts.Name:       clusterDir(dirs, opts.Name),
		opts.HostedName: hostedDir(dirs, opts.Name, opts.HostedName),
	} {
		_, found, err := loadClusterRecord(dir)
		if err != nil {
			return nil, err
		}
		if !found {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// awsCredentialsFile returns the AWS shared credentials file the HyperShift
// operator uses to write OIDC documents. The operator reads its default
// profile.
func awsCredentialsFile() (string, error) {
	path := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".aws", "credentials")
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("AWS credentials file for the HyperShift operator: %w", err)
	}
	return path, nil
}

// resolveCreateOptions takes values from flags, prompts for missing ones when
// interactive and falls back to defaults otherwise. The SSH key is only asked
// once and then saved to the config file.
func resolveCreateOptions(ctx context.Context, p progress, cmd *cli.Command, deps Deps, dirs xdg.Dirs, st state, cfg config) (createOptions, error) {
	defaults, err := defaultCreateOptions(deps, dirs, st, cfg)
	if err != nil {
		return createOptions{}, err
	}
	opts := createOptions{
		Release:      flagOr(cmd, "release", defaults.Release),
		Region:       flagOr(cmd, "region", defaults.Region),
		BaseDomain:   flagOr(cmd, "base-domain", defaults.BaseDomain),
		Name:         flagOr(cmd, "name", defaults.Name),
		PullSecret:   flagOr(cmd, "pull-secret", defaults.PullSecret),
		SSHKey:       flagOr(cmd, "ssh-key", defaults.SSHKey),
		OIDCBucket:   flagOr(cmd, "oidc-bucket", defaults.OIDCBucket),
		HostedName:   flagOr(cmd, "hosted-name", defaults.HostedName),
		InstanceType: flagOr(cmd, "instance-type", defaults.InstanceType),
		Workers:      defaults.Workers,
	}
	if cmd.IsSet("workers") {
		opts.Workers = cmd.Int("workers")
	}
	if deps.Interactive {
		askSSHKey := !cmd.IsSet("ssh-key") && cfg.SSHKey == nil
		if err := promptCreateOptions(ctx, p, cmd, releases.Client{BaseURL: deps.ReleasesURL}, &opts, askSSHKey, !cmd.Bool("yes")); err != nil {
			return createOptions{}, err
		}
		if askSSHKey {
			cfg.SSHKey = &opts.SSHKey
			if err := saveConfig(dirs.Config, cfg); err != nil {
				return createOptions{}, err
			}
		}
	}
	if opts.HostedName == "" && opts.Name != "" {
		opts.HostedName = opts.Name + "-workload"
	}
	// Paths from the config file or prompts are not expanded by a shell.
	if opts.PullSecret, err = expandHome(opts.PullSecret); err != nil {
		return createOptions{}, err
	}
	if opts.SSHKey, err = expandHome(opts.SSHKey); err != nil {
		return createOptions{}, err
	}

	var missing []string
	for _, required := range []struct{ flag, value string }{
		{"--release", opts.Release},
		{"--region", opts.Region},
		{"--base-domain", opts.BaseDomain},
		{"--name", opts.Name},
		{"--pull-secret", opts.PullSecret},
		{"--oidc-bucket", opts.OIDCBucket},
		{"--instance-type", opts.InstanceType},
	} {
		if required.value == "" {
			missing = append(missing, required.flag)
		}
	}
	if len(missing) > 0 {
		return createOptions{}, fmt.Errorf("missing required values: %s", strings.Join(missing, ", "))
	}
	return opts, nil
}

// defaultCreateOptions returns the built-in defaults, overridden by the
// config file, the last used release and the tracked clusters.
func defaultCreateOptions(deps Deps, dirs xdg.Dirs, st state, cfg config) (createOptions, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return createOptions{}, err
	}
	opts := createOptions{
		Release:      st.LastRelease,
		Region:       cmp.Or(cfg.Region, deps.DefaultRegion),
		BaseDomain:   cmp.Or(cfg.BaseDomain, defaultBaseDomain),
		PullSecret:   cmp.Or(cfg.PullSecret, filepath.Join(home, ".openshift", "pull-secret-latest.json")),
		OIDCBucket:   cfg.OIDCBucket,
		InstanceType: cmp.Or(cfg.MgmtInstanceType, defaultInstanceType),
	}
	if cfg.MgmtWorkers != nil {
		opts.Workers = *cfg.MgmtWorkers
	}
	if user := os.Getenv("USER"); user != "" {
		opts.Name = user + "-karpenter"
		opts.OIDCBucket = cmp.Or(opts.OIDCBucket, user+"-hypershift-oidc")
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
	record, found, err := loadClusterRecord(clusterDir(dirs, st.MgmtCluster))
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
