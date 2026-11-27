package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/charmbracelet/huh"
	"github.com/urfave/cli/v3"
)

// errClusterAndName rejects --cluster together with --name, as one adds to
// an existing cluster and the other creates a new one.
var errClusterAndName = errors.New("--cluster adds to an existing cluster, --name names a new one, set only one")

// createTarget are the clusters a create run creates. A new run creates a
// standalone or management cluster and, for setup hosted, a hosted cluster.
// A run that adds to an existing cluster installs the HyperShift operator
// on it and, for setup hosted, creates a hosted cluster.
type createTarget struct {
	platform   string
	name       string
	hostedName string
	setup      setup
	// add marks a run that adds to the existing cluster mgmt.
	add  bool
	mgmt storedCluster
}

// resolveCreateTarget decides whether the run creates a new cluster or adds
// to an existing one, and resolves the platform, setup and names for it.
func resolveCreateTarget(ctx context.Context, p progress, cmd *cli.Command, deps Deps, clusters clusterList, st state) (createTarget, error) {
	if cmd.IsSet("cluster") && cmd.IsSet("name") {
		return createTarget{}, errClusterAndName
	}
	mgmt, add, err := resolveCreateMode(ctx, cmd, deps, clusters, st)
	if err != nil {
		return createTarget{}, err
	}
	if add {
		return resolveAddTarget(ctx, p, cmd, deps, clusters, mgmt)
	}
	return resolveNewTarget(ctx, cmd, deps, clusters, st)
}

// resolveCreateMode returns the management cluster to add to and true, or
// false to create a new cluster. --cluster adds to the named cluster,
// --name creates a new one. Otherwise the run adds to the current cluster
// if its management cluster is installed, which a prompt may change to a
// new cluster.
func resolveCreateMode(ctx context.Context, cmd *cli.Command, deps Deps, clusters clusterList, st state) (storedCluster, bool, error) {
	if cmd.IsSet("cluster") {
		name := cmd.String("cluster")
		if name == "" {
			return storedCluster{}, false, errors.New("--cluster must not be empty")
		}
		c, found := clusters.find(name)
		if !found {
			return storedCluster{}, false, unknownClusterError(name, clusters)
		}
		mgmt, _ := clusters.find(c.mgmt)
		if !mgmt.installed() {
			return storedCluster{}, false, incompleteError(mgmt.clusterRef)
		}
		return mgmt, true, nil
	}
	if cmd.IsSet("name") {
		return storedCluster{}, false, nil
	}
	current, found := clusters.find(st.Current)
	if !found {
		return storedCluster{}, false, nil
	}
	mgmt, _ := clusters.find(current.mgmt)
	if !mgmt.installed() {
		return storedCluster{}, false, nil
	}
	if !prompts(cmd, deps) {
		return mgmt, true, nil
	}
	add := true
	err := huh.NewForm(huh.NewGroup(
		// Options scrolls to the bound value, so Value comes first.
		huh.NewSelect[bool]().
			Title("Create").
			Value(&add).
			Options(
				huh.NewOption("Add to "+mgmt.clusterRef.String(), true),
				huh.NewOption("New cluster", false),
			),
	)).RunWithContext(ctx)
	return mgmt, add, err
}

// resolveAddTarget takes the platform from the record of the existing
// cluster and resolves what to add to it.
func resolveAddTarget(ctx context.Context, p progress, cmd *cli.Command, deps Deps, clusters clusterList, mgmt storedCluster) (createTarget, error) {
	platform := mgmt.record.Platform
	if cmd.IsSet("platform") && cmd.String("platform") != platform {
		return createTarget{}, fmt.Errorf("%s runs on %s, create a cluster on %s with --name", mgmt.clusterRef, platform, cmd.String("platform"))
	}
	p.done("Platform %s (%s)", platform, mgmt.clusterRef)
	s, err := resolveAddSetup(ctx, cmd, deps)
	if err != nil {
		return createTarget{}, err
	}
	if err := checkSetupFlags(cmd, s); err != nil {
		return createTarget{}, err
	}
	t := createTarget{platform: platform, name: mgmt.mgmt, setup: s, add: true, mgmt: mgmt}
	if !s.hosted() {
		return t, nil
	}
	t.hostedName, err = resolveHostedName(ctx, cmd, deps, clusters, t.name)
	return t, err
}

// resolveNewTarget resolves the platform, setup and names of new clusters.
func resolveNewTarget(ctx context.Context, cmd *cli.Command, deps Deps, clusters clusterList, st state) (createTarget, error) {
	platform, err := resolvePlatform(ctx, cmd, deps)
	if err != nil {
		return createTarget{}, err
	}
	s, err := resolveSetup(ctx, cmd, deps, st)
	if err != nil {
		return createTarget{}, err
	}
	if err := checkSetupFlags(cmd, s); err != nil {
		return createTarget{}, err
	}
	name := ""
	if user := os.Getenv("USER"); user != "" {
		name = user + "-karpenter"
	}
	name, err = resolveNewName(ctx, cmd, deps, clusters, "name", s.clusterTitle()+" name", managementCluster, name)
	if err != nil {
		return createTarget{}, err
	}
	t := createTarget{platform: platform, name: name, setup: s}
	if !s.hosted() {
		return t, nil
	}
	t.hostedName, err = resolveHostedName(ctx, cmd, deps, clusters, name)
	return t, err
}

// resolveHostedName returns the name of a new hosted cluster of the
// management cluster mgmt, by default the first free <mgmt>-workload.
func resolveHostedName(ctx context.Context, cmd *cli.Command, deps Deps, clusters clusterList, mgmt string) (string, error) {
	name := mgmt + "-workload"
	for i := 2; ; i++ {
		if _, found := clusters.find(name); !found {
			break
		}
		name = mgmt + "-workload-" + strconv.Itoa(i)
	}
	ref := func(name string) clusterRef { return hostedCluster(mgmt, name) }
	name, err := resolveNewName(ctx, cmd, deps, clusters, "hosted-name", "Hosted cluster name", ref, name)
	if err != nil {
		return "", err
	}
	if name == mgmt {
		return "", fmt.Errorf("--hosted-name %s equals --name, choose another --hosted-name", name)
	}
	return name, nil
}

// resolveNewName returns the name of a cluster to create from the flag, or
// asks for it if it may prompt, prefilled with the default. ref names the
// cluster to create. It fails for names of existing clusters.
func resolveNewName(ctx context.Context, cmd *cli.Command, deps Deps, clusters clusterList, flag, title string, ref func(string) clusterRef, name string) (string, error) {
	validate := func(name string) error {
		return checkNewName(clusters, ref(name), flag)
	}
	if cmd.IsSet(flag) {
		name = cmd.String(flag)
		if name == "" {
			return "", fmt.Errorf("--%s must not be empty", flag)
		}
		return name, validate(name)
	}
	if prompts(cmd, deps) {
		err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title(title).Value(&name).Validate(func(s string) error {
				if err := requireValue(s); err != nil {
					return err
				}
				return validate(s)
			}),
		)).RunWithContext(ctx)
		return name, err
	}
	return name, validate(name)
}

// checkNewName fails if a cluster with the name of c exists. A cluster
// whose creation never started has no record and may be created again.
func checkNewName(clusters clusterList, c clusterRef, flag string) error {
	existing, found := clusters.find(c.name())
	switch {
	case !found || (!existing.recorded && existing.clusterRef == c):
		return nil
	case !existing.installed():
		return incompleteError(existing.clusterRef)
	case flag == "name":
		return fmt.Errorf("%s exists, add to it with --cluster %s or choose another --name", c.name(), c.name())
	default:
		return fmt.Errorf("%s exists, choose another --%s", c.name(), flag)
	}
}

// creates reports whether the run creates a cluster, which the release
// applies to.
func (t createTarget) creates() bool {
	return !t.add || t.setup.hosted()
}

// checkFlags fails for flags that differ from the values the record of the
// existing cluster holds, for the size flags of the management cluster, and
// for --release if the run creates no cluster.
func (t createTarget) checkFlags(cmd *cli.Command) error {
	if !t.add {
		return nil
	}
	for _, flag := range []string{"instance-type", "workers"} {
		if cmd.IsSet(flag) {
			return fmt.Errorf("--%s only applies to new clusters, create one with --name", flag)
		}
	}
	for _, f := range []struct{ flag, recorded string }{
		{"region", t.mgmt.record.Region},
		{"base-domain", t.mgmt.record.BaseDomain},
		{"pull-secret", t.mgmt.record.PullSecret},
	} {
		if !cmd.IsSet(f.flag) || f.recorded == "" {
			continue
		}
		value, err := expandHome(cmd.String(f.flag))
		if err != nil {
			return err
		}
		if value != f.recorded {
			return fmt.Errorf("--%s %s differs from %s of %s", f.flag, cmd.String(f.flag), f.recorded, t.mgmt.clusterRef)
		}
	}
	if cmd.IsSet("release") && !t.creates() {
		return fmt.Errorf("--release only applies to clusters this run creates, %s exists", t.mgmt.clusterRef)
	}
	return nil
}
