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
)

// promptCreateOptions asks for every value not given as a flag, prefilled
// with the current value, and for the SSH key if askSSHKey is set.
func promptCreateOptions(ctx context.Context, cmd *cli.Command, opts *createOptions, askSSHKey bool) error {
	var fields []huh.Field
	for _, input := range []struct {
		flag  string
		title string
		value *string
	}{
		{"release", "Release (e.g. 5.1.0-ec.0)", &opts.Release},
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
	if !cmd.IsSet("hosted-name") {
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
	return confirmCreateOptions(ctx, *opts)
}

// confirmCreateOptions shows a summary and fails unless the user confirms.
func confirmCreateOptions(ctx context.Context, opts createOptions) error {
	sshKey := opts.SSHKey
	if sshKey == "" {
		sshKey = "none"
	}
	hostedName := opts.HostedName
	if hostedName == "" {
		hostedName = opts.Name + "-workload"
	}
	summary := fmt.Sprintf("Release:        %s\nRegion:         %s\nBase domain:    %s\nMgmt cluster:   %s\nHosted cluster: %s\nPull secret:    %s\nSSH key:        %s\nOIDC bucket:    %s",
		opts.Release, opts.Region, opts.BaseDomain, opts.Name, hostedName, opts.PullSecret, sshKey, opts.OIDCBucket)
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
