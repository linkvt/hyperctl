package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/releases"
	"github.com/linkvt/hyperctl/internal/tui"
)

// promptCreateOptions asks for every value not given as a flag, prefilled
// with the current value, and for the SSH key if askSSHKey is set. With
// confirm set, it asks for confirmation of the summary.
func promptCreateOptions(ctx context.Context, p progress, cmd *cli.Command, client releases.Client, opts *createOptions, askSSHKey, confirm bool) error {
	if !cmd.IsSet("management-only") {
		if err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[bool]().
				Title("Setup").
				Options(
					huh.NewOption("Management and hosted cluster with AutoNode", false),
					huh.NewOption("Management cluster and HyperShift operator only", true),
				).
				Value(&opts.ManagementOnly),
		)).RunWithContext(ctx); err != nil {
			return err
		}
	}
	if !cmd.IsSet("release") {
		release, err := promptRelease(ctx, p, client, opts.Release)
		if err != nil {
			return err
		}
		opts.Release = release
	}

	var fields []huh.Field
	for _, input := range []struct {
		flag  string
		title string
		value *string
	}{
		{"region", "AWS region", &opts.Region},
		{"base-domain", "Base domain", &opts.BaseDomain},
		{"name", "Management cluster name", &opts.Name},
		{"pull-secret", "Pull secret file", &opts.PullSecret},
	} {
		if cmd.IsSet(input.flag) {
			continue
		}
		fields = append(fields, huh.NewInput().Title(input.title).Value(input.value).Validate(requireValue))
	}
	if !opts.ManagementOnly && !cmd.IsSet("hosted-name") {
		fields = append(fields, huh.NewInput().
			Title("Hosted cluster name").
			Placeholder("<management cluster name>-workload").
			Value(&opts.HostedName))
	}
	if askSSHKey {
		sshKeys, err := sshPublicKeys()
		if err != nil {
			return err
		}
		options := []huh.Option[string]{huh.NewOption("none", "")}
		for _, key := range sshKeys {
			options = append(options, huh.NewOption(key, key))
		}
		fields = append(fields, huh.NewSelect[string]().
			Title("SSH public key for node access").
			Options(options...).
			Value(&opts.SSHKey))
	}
	if len(fields) > 0 {
		if err := huh.NewForm(huh.NewGroup(fields...)).RunWithContext(ctx); err != nil {
			return err
		}
	}
	if !cmd.IsSet("instance-type") && !cmd.IsSet("workers") {
		if err := promptSize(ctx, opts); err != nil {
			return err
		}
	}
	if !confirm {
		return nil
	}
	return confirmCreateOptions(ctx, *opts)
}

// customSize marks the custom instance type option of the size prompt.
const customSize = -1

// promptSize lets the user pick a management cluster size preset or a custom
// instance type for a compact cluster.
func promptSize(ctx context.Context, opts *createOptions) error {
	selected := customSize
	var options []huh.Option[int]
	for i, preset := range sizePresets {
		options = append(options, huh.NewOption(preset.label, i))
		if preset.instanceType == opts.InstanceType && preset.workers == opts.Workers {
			selected = i
		}
	}
	options = append(options, huh.NewOption("Compact with a custom instance type in 1 zone", customSize))
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[int]().Title("Management cluster size").Options(options...).Value(&selected),
	)).RunWithContext(ctx); err != nil {
		return err
	}

	if selected != customSize {
		opts.InstanceType = sizePresets[selected].instanceType
		opts.Workers = sizePresets[selected].workers
		return nil
	}
	opts.Workers = 0
	return huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("EC2 instance type").
			Description("At least 4 vCPU and 16 GiB memory").
			Value(&opts.InstanceType).
			Validate(requireValue),
	)).RunWithContext(ctx)
}

const (
	releaseLastUsed = "last-used"
	releaseBrowse   = "browse"
	releaseManual   = "manual"
)

// promptRelease lets the user keep the last used release, browse the release
// controller or enter a version or pull spec.
func promptRelease(ctx context.Context, p progress, client releases.Client, lastUsed string) (string, error) {
	source := releaseBrowse
	var options []huh.Option[string]
	if lastUsed != "" {
		source = releaseLastUsed
		options = append(options, huh.NewOption("Last used: "+lastUsed, releaseLastUsed))
	}
	options = append(options,
		huh.NewOption("Browse the release controller", releaseBrowse),
		huh.NewOption("Enter a version or pull spec", releaseManual),
	)
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Release").Options(options...).Value(&source),
	)).RunWithContext(ctx); err != nil {
		return "", err
	}

	switch source {
	case releaseLastUsed:
		return lastUsed, nil
	case releaseManual:
		release := lastUsed
		err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Release version (e.g. 5.1.0-ec.0) or pull spec").Value(&release).Validate(requireValue),
		)).RunWithContext(ctx)
		return release, err
	default:
		return browseReleases(ctx, p, client)
	}
}

// browseReleases lets the user pick a release: accepted ones only unless
// all phases are chosen, then a stream and one of its releases.
func browseReleases(ctx context.Context, p progress, client releases.Client) (string, error) {
	var all bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[bool]().Title("Releases").Options(
			huh.NewOption("Accepted only", false),
			huh.NewOption("All phases (also Ready and Rejected)", true),
		).Value(&all),
	)).RunWithContext(ctx); err != nil {
		return "", err
	}

	var streams []string
	err := p.wait(ctx, "Loading release streams", func(ctx context.Context) error {
		var err error
		streams, err = client.Streams(ctx, all)
		return err
	})
	if err != nil {
		return "", err
	}
	var stream string
	if err := huh.NewForm(huh.NewGroup(
		tui.Select(huh.NewSelect[string]().Title("Release stream").Options(huh.NewOptions(streams...)...).Height(15).Value(&stream)),
	)).RunWithContext(ctx); err != nil {
		return "", err
	}

	var tags []releases.Tag
	err = p.wait(ctx, "Loading releases of "+stream, func(ctx context.Context) error {
		var err error
		tags, err = client.Tags(ctx, stream)
		return err
	})
	if err != nil {
		return "", err
	}
	tags = filterTags(tags, all)
	if len(tags) == 0 {
		return "", fmt.Errorf("no releases in stream %s", stream)
	}
	var options []huh.Option[string]
	for _, tag := range tags {
		options = append(options, huh.NewOption(tag.Name+" ("+tag.Phase+")", tag.PullSpec))
	}
	var pullSpec string
	err = huh.NewForm(huh.NewGroup(
		tui.Select(huh.NewSelect[string]().Title("Release in " + stream).Options(options...).Height(15).Value(&pullSpec)),
	)).RunWithContext(ctx)
	return pullSpec, err
}

// confirmCreateOptions shows a summary and fails unless the user confirms.
func confirmCreateOptions(ctx context.Context, opts createOptions) error {
	sshKey := opts.SSHKey
	if sshKey == "" {
		sshKey = "none"
	}
	setup := "Management and hosted cluster"
	if opts.ManagementOnly {
		setup = "Management cluster only"
	}
	summary := fmt.Sprintf("Setup:          %s\nRelease:        %s\nRegion:         %s\nBase domain:    %s\nMgmt cluster:   %s\nMgmt size:      %s",
		setup, opts.Release, opts.Region, opts.BaseDomain, opts.Name, describeSize(opts))
	if !opts.ManagementOnly {
		hostedName := opts.HostedName
		if hostedName == "" {
			hostedName = opts.Name + "-workload"
		}
		summary += fmt.Sprintf("\nHosted cluster: %s", hostedName)
	}
	summary += fmt.Sprintf("\nPull secret:    %s\nSSH key:        %s\nOIDC bucket:    %s", opts.PullSecret, sshKey, opts.OIDCBucket)
	confirmed := true
	err := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("Summary").Description(summary),
		huh.NewConfirm().Title("Create the clusters?").Value(&confirmed),
	)).RunWithContext(ctx)
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("aborted")
	}
	return nil
}

func requireValue(s string) error {
	if s == "" {
		return errors.New("required")
	}
	return nil
}

// sshPublicKeys lists the public keys in ~/.ssh.
func sshPublicKeys() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return filepath.Glob(filepath.Join(home, ".ssh", "*.pub"))
}

// promptHypershiftPath asks for the path of the hypershift binary.
func promptHypershiftPath(ctx context.Context) (string, error) {
	var path string
	err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("hypershift binary").
			Description("Not found in PATH, e.g. ~/repos/openshift/hypershift/bin/hypershift").
			Value(&path).
			Validate(requireValue),
	)).RunWithContext(ctx)
	return path, err
}

// confirmDestroy fails unless the user confirms destroying the targets.
func confirmDestroy(ctx context.Context, targets []string) error {
	var confirmed bool
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Destroy " + strings.Join(targets, " and ") + "?").
			Value(&confirmed),
	)).RunWithContext(ctx)
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("aborted")
	}
	return nil
}

// confirmDNSDeletion fails unless the user confirms deleting the DNS entries.
func confirmDNSDeletion(ctx context.Context, list string) error {
	confirmed := true
	err := huh.NewForm(huh.NewGroup(
		huh.NewNote().Title("DNS entries of previous clusters").Description(list),
		huh.NewConfirm().Title("Delete these DNS entries?").Value(&confirmed),
	)).RunWithContext(ctx)
	if err != nil {
		return err
	}
	if !confirmed {
		return errors.New("aborted, the DNS entries conflict with the clusters to create")
	}
	return nil
}
