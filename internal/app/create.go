package app

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/ocp"
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
}

func createCommand(deps Deps) *cli.Command {
	return &cli.Command{
		Name:  "create",
		Usage: "Create the management cluster",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "release", Usage: "OCP release version, e.g. 5.1.0-ec.0"},
			&cli.StringFlag{Name: "region", Usage: "AWS region (default: region of the AWS profile)"},
			&cli.StringFlag{Name: "base-domain", Usage: "Route53 base domain (default: " + defaultBaseDomain + ")"},
			&cli.StringFlag{Name: "name", Usage: "Management cluster name (default: $USER-karpenter)"},
			&cli.StringFlag{Name: "pull-secret", Usage: "Path to the pull secret file (default: ~/.openshift/pull-secret-latest.json)"},
			&cli.StringFlag{Name: "ssh-key", Usage: "Path to an SSH public key for node access (optional)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runCreate(ctx, cmd, deps)
		},
	}
}

func runCreate(ctx context.Context, cmd *cli.Command, deps Deps) error {
	identity, err := requireIAMUser(ctx, deps.STS)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.Root().Writer, "AWS identity: %s\n", identity)

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
	opts, err := resolveCreateOptions(ctx, cmd, deps, dirs, st, cfg)
	if err != nil {
		return err
	}
	st.LastRelease = opts.Release
	if err := saveState(dirs.State, st); err != nil {
		return err
	}
	clusterDir := clusterDir(dirs, opts.Name)

	record, found, err := loadClusterRecord(clusterDir)
	if err != nil {
		return err
	}
	switch {
	case found && record.Phase == phaseInstalled:
		_, _ = fmt.Fprintf(cmd.Root().Writer, "Management cluster %s is already installed\n", opts.Name)
		st.MgmtCluster = opts.Name
		return saveState(dirs.State, st)
	case found:
		return fmt.Errorf("previous installation of management cluster %s did not complete, destroy it first", opts.Name)
	}

	if err := installMgmtCluster(ctx, cmd, dirs, opts); err != nil {
		return err
	}
	st.MgmtCluster = opts.Name
	return saveState(dirs.State, st)
}

// resolveCreateOptions takes values from flags, prompts for missing ones when
// interactive and falls back to defaults otherwise. The SSH key is only asked
// once and then saved to the config file.
func resolveCreateOptions(ctx context.Context, cmd *cli.Command, deps Deps, dirs xdg.Dirs, st state, cfg config) (createOptions, error) {
	defaults, err := defaultCreateOptions(deps, st, cfg)
	if err != nil {
		return createOptions{}, err
	}
	opts := createOptions{
		Release:    flagOr(cmd, "release", defaults.Release),
		Region:     flagOr(cmd, "region", defaults.Region),
		BaseDomain: flagOr(cmd, "base-domain", defaults.BaseDomain),
		Name:       flagOr(cmd, "name", defaults.Name),
		PullSecret: flagOr(cmd, "pull-secret", defaults.PullSecret),
		SSHKey:     flagOr(cmd, "ssh-key", defaults.SSHKey),
	}
	if deps.Interactive {
		askSSHKey := !cmd.IsSet("ssh-key") && cfg.SSHKey == nil
		if err := promptCreateOptions(ctx, cmd, &opts, askSSHKey); err != nil {
			return createOptions{}, err
		}
		if askSSHKey {
			cfg.SSHKey = &opts.SSHKey
			if err := saveConfig(dirs.Config, cfg); err != nil {
				return createOptions{}, err
			}
		}
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
// config file and the last used release.
func defaultCreateOptions(deps Deps, st state, cfg config) (createOptions, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return createOptions{}, err
	}
	opts := createOptions{
		Release:    st.LastRelease,
		Region:     cmp.Or(cfg.Region, deps.DefaultRegion),
		BaseDomain: cmp.Or(cfg.BaseDomain, defaultBaseDomain),
		PullSecret: cmp.Or(cfg.PullSecret, filepath.Join(home, ".openshift", "pull-secret-latest.json")),
	}
	if user := os.Getenv("USER"); user != "" {
		opts.Name = user + "-karpenter"
	}
	if cfg.SSHKey != nil {
		opts.SSHKey = *cfg.SSHKey
	}
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

// installMgmtCluster extracts the installer for the release and installs the
// management cluster, tracking progress in the cluster record.
func installMgmtCluster(ctx context.Context, cmd *cli.Command, dirs xdg.Dirs, opts createOptions) error {
	release := ocp.Release{Version: opts.Release}

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

	installer, err := ocp.EnsureInstaller(ctx, filepath.Join(dirs.Cache, "releases"), release, opts.PullSecret)
	if err != nil {
		return err
	}

	installConfig, err := ocp.RenderInstallConfig(ocp.InstallConfigParams{
		Name:       opts.Name,
		BaseDomain: opts.BaseDomain,
		Region:     opts.Region,
		PullSecret: strings.TrimSpace(string(pullSecret)),
		SSHKey:     strings.TrimSpace(string(sshKey)),
	})
	if err != nil {
		return err
	}

	clusterDir := clusterDir(dirs, opts.Name)
	installDir := installDir(dirs, opts.Name)
	if err := os.MkdirAll(installDir, 0o700); err != nil {
		return err
	}
	// The installer consumes install-config.yaml, keep a copy for reference.
	for _, dir := range []string{clusterDir, installDir} {
		if err := os.WriteFile(filepath.Join(dir, "install-config.yaml"), installConfig, 0o600); err != nil {
			return err
		}
	}

	if err := saveClusterRecord(clusterDir, clusterRecord{Platform: platformAWS, Phase: phaseInstalling}); err != nil {
		return err
	}
	if err := installer.CreateCluster(ctx, installDir, release, cmd.Root().Writer, cmd.Root().ErrWriter); err != nil {
		return err
	}
	return saveClusterRecord(clusterDir, clusterRecord{Platform: platformAWS, Phase: phaseInstalled})
}
