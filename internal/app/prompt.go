package app

import (
	"cmp"
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
// with the current value, and for the SSH key if askSSHKey is set.
func promptCreateOptions(ctx context.Context, p progress, cmd *cli.Command, client releases.Client, run platformCreate, opts *createOptions, askSSHKey bool) error {
	if run.needsRelease() && !cmd.IsSet("release") {
		release, err := promptRelease(ctx, p, client, opts.Release)
		if err != nil {
			return err
		}
		opts.Release = release
	}

	regionTitle, regionDescription := run.regionPrompt()
	var fields []huh.Field
	for _, input := range []struct {
		flag        string
		title       string
		description string
		value       *string
		// release marks the values only asked with needsRelease.
		release bool
	}{
		{"region", regionTitle, regionDescription, &opts.Region, false},
		{"base-domain", "Base domain", "", &opts.BaseDomain, true},
		{"name", opts.Setup.clusterTitle() + " name", "", &opts.Name, false},
		{"pull-secret", "Pull secret file", "", &opts.PullSecret, true},
	} {
		if cmd.IsSet(input.flag) || (input.release && !run.needsRelease()) {
			continue
		}
		field := huh.NewInput().Title(input.title).Value(input.value).Validate(requireValue)
		if input.description != "" {
			field = field.Description(input.description)
		}
		fields = append(fields, field)
	}
	if opts.Setup.hosted() && !cmd.IsSet("hosted-name") {
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
	return run.promptSize(ctx, cmd)
}

const (
	releaseLastUsed = "last-used"
	releaseBrowse   = "browse"
	releaseManual   = "manual"
)

type releaseFilter int

const (
	releaseFilterAcceptedNoFailures releaseFilter = iota
	releaseFilterAccepted
	releaseFilterAll
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

// browseReleases lets the user choose which release results to browse, then
// select a stream and one of its releases.
func browseReleases(ctx context.Context, p progress, client releases.Client) (string, error) {
	filter := releaseFilterAcceptedNoFailures
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[releaseFilter]().Title("Releases").Options(
			huh.NewOption("Accepted, no failed jobs", releaseFilterAcceptedNoFailures),
			huh.NewOption("Accepted (including failed jobs)", releaseFilterAccepted),
			huh.NewOption("All phases (also Ready and Rejected)", releaseFilterAll),
		).Value(&filter),
	)).RunWithContext(ctx); err != nil {
		return "", err
	}

	var streams []string
	err := p.wait(ctx, "Loading release streams", func(ctx context.Context) error {
		var err error
		streams, err = client.Streams(ctx, filter == releaseFilterAll)
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
	var failureCounts map[string]int
	err = p.wait(ctx, "Loading releases of "+stream, func(ctx context.Context) error {
		var err error
		tags, err = client.Tags(ctx, stream)
		if err != nil {
			return err
		}
		tags, failureCounts, err = filterBrowseTags(ctx, client, stream, tags, filter)
		return err
	})
	if err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", noReleasesError(stream, filter)
	}
	var options []huh.Option[string]
	for _, tag := range tags {
		options = append(options, huh.NewOption(releaseOptionLabel(tag, failureCounts[tag.Name]), tag.PullSpec))
	}
	var pullSpec string
	err = huh.NewForm(huh.NewGroup(
		tui.Select(huh.NewSelect[string]().Title("Release in " + stream).Options(options...).Height(15).Value(&pullSpec)),
	)).RunWithContext(ctx)
	return pullSpec, err
}

func noReleasesError(stream string, filter releaseFilter) error {
	switch filter {
	case releaseFilterAcceptedNoFailures:
		return fmt.Errorf("no Accepted releases without failed jobs in stream %s; try %q or %q", stream,
			"Accepted (including failed jobs)", "All phases (also Ready and Rejected)")
	case releaseFilterAccepted:
		return fmt.Errorf("no Accepted releases in stream %s; try %q", stream, "All phases (also Ready and Rejected)")
	default:
		return fmt.Errorf("no releases in stream %s", stream)
	}
}

func releaseOptionLabel(tag releases.Tag, failedJobs int) string {
	label := tag.Name + " (" + tag.Phase
	if failedJobs > 0 {
		label += fmt.Sprintf(" · %d failed", failedJobs)
	}
	return label + ")"
}

func filterBrowseTags(ctx context.Context, client releases.Client, stream string, tags []releases.Tag, filter releaseFilter) ([]releases.Tag, map[string]int, error) {
	if filter == releaseFilterAll {
		return tags, nil, nil
	}

	var filtered []releases.Tag
	var failureCounts map[string]int
	for _, tag := range tags {
		if tag.Phase != releases.PhaseAccepted {
			continue
		}
		details, err := client.TagDetails(ctx, stream, tag.Name)
		if err != nil {
			return nil, nil, fmt.Errorf("loading results for release %s: %w", tag.Name, err)
		}
		if details.Results == nil {
			return nil, nil, fmt.Errorf("release %s has no job results", tag.Name)
		}
		if filter == releaseFilterAcceptedNoFailures {
			if details.Results.HasFailures() {
				continue
			}
		} else if failedJobs := details.Results.FailedJobCount(); failedJobs > 0 {
			if failureCounts == nil {
				failureCounts = make(map[string]int)
			}
			failureCounts[tag.Name] = failedJobs
		}
		filtered = append(filtered, tag)
	}
	return filtered, failureCounts, nil
}

// confirmCreateOptions shows a summary and fails unless the user confirms.
func confirmCreateOptions(ctx context.Context, platformName string, run platformCreate, opts createOptions) error {
	setup := map[setup]string{
		setupStandalone: "Standalone cluster",
		setupManagement: "Management cluster and HyperShift operator",
		setupHosted:     "Management and hosted cluster",
	}[opts.Setup]
	lines := []string{
		"Platform:       " + platformName,
		"Setup:          " + setup,
	}
	if run.needsRelease() {
		lines = append(lines, "Release:        "+opts.Release)
	}
	lines = append(lines, "Region:         "+opts.Region)
	if run.needsRelease() {
		lines = append(lines, "Base domain:    "+opts.BaseDomain)
	}
	clusterLabel, sizeLabel := "Mgmt cluster:   ", "Mgmt size:      "
	if !opts.Setup.operator() {
		clusterLabel, sizeLabel = "Cluster:        ", "Cluster size:   "
	}
	lines = append(lines, clusterLabel+opts.Name, sizeLabel+run.describeSize())
	if opts.Setup.hosted() {
		lines = append(lines, "Hosted cluster: "+cmp.Or(opts.HostedName, opts.Name+"-workload"))
	}
	if run.needsRelease() {
		lines = append(lines, "Pull secret:    "+opts.PullSecret)
	}
	lines = append(lines, "SSH key:        "+cmp.Or(opts.SSHKey, "none"))
	lines = append(lines, run.summary()...)
	if opts.Setup.operator() {
		karpenterOperator := "embedded"
		if opts.StandaloneKarpenterOperator {
			karpenterOperator = "standalone"
		}
		lines = append(lines, "Karpenter op:   "+karpenterOperator)
	}
	summary := strings.Join(lines, "\n")
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
