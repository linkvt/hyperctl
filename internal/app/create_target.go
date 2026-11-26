package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/urfave/cli/v3"
)

// errCreateCluster rejects --cluster, which selects existing clusters.
var errCreateCluster = errors.New("create names the clusters with --name and --hosted-name, --cluster selects existing clusters for other commands")

// createTarget are the clusters a create run creates or continues: the
// standalone or management cluster and, for setup hosted, the hosted
// cluster. Each is new or exists, with a record unless its creation never
// started.
type createTarget struct {
	name       string
	hostedName string
	setup      setup
	mgmt       storedCluster
	hosted     storedCluster
}

// resolveCreateTarget returns the names from the flags, or asks for them
// if it may prompt, prefilled with defaults that continue the current
// cluster. It fails for names of other clusters.
func resolveCreateTarget(ctx context.Context, cmd *cli.Command, deps Deps, clusters clusterList, st state, setup setup) (createTarget, error) {
	current, currentFound := clusters.find(st.Current)
	name := ""
	switch {
	case currentFound:
		name = current.mgmt
	case os.Getenv("USER") != "":
		name = os.Getenv("USER") + "-karpenter"
	}
	name = flagOr(cmd, "name", name)
	if prompts(cmd, deps) && !cmd.IsSet("name") {
		if err := promptName(ctx, setup.clusterTitle()+" name", &name); err != nil {
			return createTarget{}, err
		}
	}
	t := createTarget{name: name, setup: setup}
	if c, found := clusters.find(name); found {
		if c.isHosted() {
			return createTarget{}, fmt.Errorf("%s is a hosted cluster of %s, choose another --name", name, c.mgmt)
		}
		t.mgmt = c
	}
	if !setup.hosted() || name == "" {
		return t, nil
	}

	hosted := clusters.hosted(name)
	switch {
	case currentFound && current.isHosted() && current.mgmt == name:
		t.hostedName = current.hosted
	case len(hosted) == 1:
		t.hostedName = hosted[0].hosted
	default:
		t.hostedName = name + "-workload"
	}
	t.hostedName = flagOr(cmd, "hosted-name", t.hostedName)
	if prompts(cmd, deps) && !cmd.IsSet("hosted-name") {
		if err := promptName(ctx, "Hosted cluster name", &t.hostedName); err != nil {
			return createTarget{}, err
		}
	}
	if t.hostedName == name {
		return createTarget{}, fmt.Errorf("--hosted-name %s equals --name, choose another --hosted-name", name)
	}
	if c, found := clusters.find(t.hostedName); found {
		switch {
		case !c.isHosted():
			return createTarget{}, fmt.Errorf("%s is a management cluster, choose another --hosted-name", t.hostedName)
		case c.mgmt != name:
			return createTarget{}, fmt.Errorf("%s is a hosted cluster of %s, choose another --hosted-name", t.hostedName, c.mgmt)
		}
		t.hosted = c
	}
	return t, nil
}

// promptName asks for the name of a cluster, prefilled with the default.
func promptName(ctx context.Context, title string, name *string) error {
	return huh.NewForm(huh.NewGroup(
		huh.NewInput().Title(title).Value(name).Validate(requireValue),
	)).RunWithContext(ctx)
}

// creates reports whether the run creates a cluster, which the release
// applies to. A cluster whose creation did not complete counts as created
// by the run, which fails for it.
func (t createTarget) creates() bool {
	return !t.mgmt.installed() || (t.setup.hosted() && !t.hosted.installed())
}

// checkFlags fails for flags that differ from the values the records of the
// existing clusters hold, and for --release if the run creates no cluster.
func (t createTarget) checkFlags(cmd *cli.Command) error {
	for _, c := range []storedCluster{t.mgmt, t.hosted} {
		if !c.recorded {
			continue
		}
		for _, f := range []struct{ flag, recorded string }{
			{"region", c.record.Region},
			{"base-domain", c.record.BaseDomain},
			{"pull-secret", c.record.PullSecret},
		} {
			if !cmd.IsSet(f.flag) || f.recorded == "" {
				continue
			}
			value, err := expandHome(cmd.String(f.flag))
			if err != nil {
				return err
			}
			if value != f.recorded {
				return fmt.Errorf("--%s %s differs from %s of %s", f.flag, cmd.String(f.flag), f.recorded, c.clusterRef)
			}
		}
	}
	if cmd.IsSet("release") && !t.creates() {
		if t.setup.hosted() {
			return fmt.Errorf("--release only applies to clusters this run creates, %s and %s exist", t.mgmt.clusterRef, t.hosted.clusterRef)
		}
		return fmt.Errorf("--release only applies to clusters this run creates, %s exists", t.mgmt.clusterRef)
	}
	return nil
}
