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

// resolveSetup returns the setup from the flag, or asks for it if it may
// prompt, prefilled with the last used one.
func resolveSetup(ctx context.Context, cmd *cli.Command, deps Deps, st state) (setup, error) {
	if cmd.IsSet("setup") {
		s := setup(cmd.String("setup"))
		switch s {
		case setupStandalone, setupManagement, setupHosted:
			return s, nil
		default:
			return "", fmt.Errorf("unknown setup %q, expected %s, %s or %s", s, setupStandalone, setupManagement, setupHosted)
		}
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
