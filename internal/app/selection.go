package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// errNoClusters is returned by commands that act on an existing cluster
// when there is none.
var errNoClusters = errors.New("no clusters, create one with: hyperctl create")

// selection is the cluster a command acts on, with what it was selected
// from.
type selection struct {
	dirs     xdg.Dirs
	store    clusterStore
	clusters clusterList
	cluster  storedCluster
}

// clusterFlag selects the cluster of the commands that act on an existing
// cluster.
func clusterFlag() cli.Flag {
	return &cli.StringFlag{Name: "cluster", Usage: "Name of the cluster to act on (default: the current cluster, see hyperctl use)"}
}

// selectCluster returns the cluster named by --cluster, else the current
// cluster.
func selectCluster(cmd *cli.Command) (selection, error) {
	dirs, err := xdg.Resolve("hyperctl")
	if err != nil {
		return selection{}, err
	}
	st, err := loadState(dirs.State)
	if err != nil {
		return selection{}, err
	}
	store := newClusterStore(dirs)
	clusters, err := store.list()
	if err != nil {
		return selection{}, err
	}
	name := st.Current
	if cmd.IsSet("cluster") {
		name = cmd.String("cluster")
	}
	switch {
	case len(clusters) == 0:
		return selection{}, errNoClusters
	case name == "":
		return selection{}, fmt.Errorf("no current cluster, select one with: hyperctl use <name>\nclusters: %s", strings.Join(clusters.names(), ", "))
	}
	c, found := clusters.find(name)
	if !found {
		return selection{}, unknownClusterError(name, clusters)
	}
	return selection{dirs: dirs, store: store, clusters: clusters, cluster: c}, nil
}

// unknownClusterError lists the existing clusters.
func unknownClusterError(name string, clusters clusterList) error {
	if len(clusters) == 0 {
		return fmt.Errorf("cluster %s does not exist, %w", name, errNoClusters)
	}
	return fmt.Errorf("cluster %s does not exist, clusters: %s", name, strings.Join(clusters.names(), ", "))
}

// hosted returns the selected hosted cluster or the only hosted cluster of
// the selected management cluster.
func (s selection) hosted() (storedCluster, error) {
	if s.cluster.isHosted() {
		return s.cluster, nil
	}
	hosted := s.clusters.hosted(s.cluster.mgmt)
	switch len(hosted) {
	case 0:
		return storedCluster{}, fmt.Errorf("no hosted cluster on cluster %s, create one with: hyperctl create --name %s --setup hosted", s.cluster.mgmt, s.cluster.mgmt)
	case 1:
		return hosted[0], nil
	default:
		return storedCluster{}, fmt.Errorf("management cluster %s has several hosted clusters, select one with --cluster: %s", s.cluster.mgmt, strings.Join(hosted.names(), ", "))
	}
}

// management returns the management cluster of the selected cluster, or
// the selected cluster itself.
func (s selection) management() storedCluster {
	c, _ := s.clusters.find(s.cluster.mgmt)
	return c
}

// noArguments fails for positional arguments of commands that select the
// cluster with --cluster.
func noArguments(cmd *cli.Command) error {
	if cmd.Args().Present() {
		return fmt.Errorf("unexpected argument %q, select the cluster with --cluster <name>", cmd.Args().First())
	}
	return nil
}
