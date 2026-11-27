package app

import (
	"cmp"
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/urfave/cli/v3"
)

// setup is what create sets up: a standalone cluster, a management cluster
// with the HyperShift operator, or a management cluster with a hosted
// cluster.
type setup string

const (
	setupStandalone setup = "standalone"
	setupManagement setup = "management"
	setupHosted     setup = "hosted"
)

// operator reports whether the setup installs the HyperShift operator.
func (s setup) operator() bool {
	return s != setupStandalone
}

// hosted reports whether the setup creates a hosted cluster.
func (s setup) hosted() bool {
	return s == setupHosted
}

// clusterTitle names the cluster that create installs first in prompts and
// progress, clusterNoun within sentences.
func (s setup) clusterTitle() string {
	if s.operator() {
		return "Management cluster"
	}
	return "Cluster"
}

func (s setup) clusterNoun() string {
	if s.operator() {
		return "management cluster"
	}
	return "cluster"
}

// resolveSetup returns the setup of new clusters from the flag, or asks for
// it if it may prompt, prefilled with the last used one.
func resolveSetup(ctx context.Context, cmd *cli.Command, deps Deps, st state) (setup, error) {
	if cmd.IsSet("setup") {
		return setupFlag(cmd)
	}
	s := cmp.Or(st.Setup, setupHosted)
	if !prompts(cmd, deps) {
		return s, nil
	}
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[setup]().
			Title("Setup").
			Options(
				huh.NewOption("Management and hosted cluster with AutoNode", setupHosted),
				huh.NewOption("Management cluster and HyperShift operator", setupManagement),
				huh.NewOption("Standalone cluster", setupStandalone),
			).
			Value(&s),
	)).RunWithContext(ctx)
	return s, err
}

// resolveAddSetup returns what to add to an existing cluster from the flag,
// or asks for it if it may prompt: setup hosted installs or updates the
// HyperShift operator and creates a hosted cluster, setup management only
// installs or updates the operator.
func resolveAddSetup(ctx context.Context, cmd *cli.Command, deps Deps) (setup, error) {
	if cmd.IsSet("setup") {
		s, err := setupFlag(cmd)
		if err == nil && s == setupStandalone {
			err = fmt.Errorf("--setup %s adds nothing to an existing cluster, expected %s or %s, or create a new cluster with --name", s, setupHosted, setupManagement)
		}
		return s, err
	}
	s := setupHosted
	if !prompts(cmd, deps) {
		return s, nil
	}
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[setup]().
			Title("Add").
			Options(
				huh.NewOption("Hosted cluster with AutoNode, installs or updates the HyperShift operator", setupHosted),
				huh.NewOption("Only install or update the HyperShift operator", setupManagement),
			).
			Value(&s),
	)).RunWithContext(ctx)
	return s, err
}

// setupFlag returns the setup of the --setup flag.
func setupFlag(cmd *cli.Command) (setup, error) {
	s := setup(cmd.String("setup"))
	switch s {
	case setupStandalone, setupManagement, setupHosted:
		return s, nil
	default:
		return "", fmt.Errorf("unknown setup %q, expected %s, %s or %s", s, setupStandalone, setupManagement, setupHosted)
	}
}

// checkSetupFlags fails for set flags that the setup does not use, before
// any further prompt or cloud operation. The platforms check their own
// flags.
func checkSetupFlags(cmd *cli.Command, s setup) error {
	var unused string
	switch {
	case !s.hosted() && cmd.IsSet("hosted-name"):
		unused = "--hosted-name"
	case !s.operator() && cmd.IsSet("hypershift"):
		unused = "--hypershift"
	case !s.operator() && !cmd.Bool("standalone-karpenter-operator"):
		unused = "--standalone-karpenter-operator=false"
	default:
		return nil
	}
	return fmt.Errorf("%s does not apply to setup %s", unused, s)
}
