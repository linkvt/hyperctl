package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/linkvt/hyperctl/internal/xdg"
)

// clusterPhase is the lifecycle phase of a cluster.
type clusterPhase string

const (
	phaseInstalling clusterPhase = "Installing"
	phaseInstalled  clusterPhase = "Installed"
)

// clusterRecord is what hyperctl remembers about a management or hosted
// cluster, including what is needed to destroy it.
type clusterRecord struct {
	Platform   string       `json:"platform"`
	Phase      clusterPhase `json:"phase"`
	Release    string       `json:"release"`
	Region     string       `json:"region"`
	BaseDomain string       `json:"baseDomain"`
	// InfraID tags the cloud resources of a hosted cluster.
	InfraID string `json:"infraID,omitempty"`
	// Azure holds the Azure resources a hosted cluster depends on.
	Azure *azureRecord `json:"azure,omitempty"`
	// PullSecret is the pull secret path, needed to extract the installer
	// again for destroying a management cluster.
	PullSecret string `json:"pullSecret,omitempty"`
	// HypershiftOperator reports whether a management cluster has the
	// HyperShift operator, see hasOperator.
	HypershiftOperator *bool `json:"hypershiftOperator,omitempty"`
	// UpdatedAt is the time of the last change, set when saving the record.
	UpdatedAt time.Time `json:"updatedAt"`
}

// hasOperator reports whether a management cluster has the HyperShift
// operator. Records of earlier versions lack the field and have the
// operator, as every earlier create installed it.
func (r clusterRecord) hasOperator() bool {
	return r.HypershiftOperator == nil || *r.HypershiftOperator
}

// azureRecord are the shared Azure resources a hosted cluster was created
// with, needed to destroy it even if the configuration changed since.
type azureRecord struct {
	// ResourceGroup holds the workload identities and the DNS zone.
	ResourceGroup string `json:"resourceGroup"`
	// DNSZone is the External DNS zone.
	DNSZone string `json:"dnsZone"`
	// BaseDomainResourceGroup holds the DNS zone of the base domain.
	BaseDomainResourceGroup string `json:"baseDomainResourceGroup"`
}

// clusterRef names a management cluster or, with hosted set, a hosted cluster
// of the management cluster.
type clusterRef struct {
	mgmt   string
	hosted string
}

func managementCluster(name string) clusterRef {
	return clusterRef{mgmt: name}
}

func hostedCluster(mgmt, name string) clusterRef {
	return clusterRef{mgmt: mgmt, hosted: name}
}

func (c clusterRef) String() string {
	if c.hosted != "" {
		return "hosted cluster " + c.hosted
	}
	return "management cluster " + c.mgmt
}

// name is the name of the cluster, unique among all clusters.
func (c clusterRef) name() string {
	if c.hosted != "" {
		return c.hosted
	}
	return c.mgmt
}

func (c clusterRef) isHosted() bool {
	return c.hosted != ""
}

// storedCluster is a cluster in the store: one with a record, or one whose
// directory exists without record, as its creation never started.
type storedCluster struct {
	clusterRef
	record   clusterRecord
	recorded bool
}

// installed reports whether creating the cluster completed.
func (c storedCluster) installed() bool {
	return c.recorded && c.record.Phase == phaseInstalled
}

// clusterList are the clusters in the store, each management cluster
// followed by its hosted clusters, sorted by name.
type clusterList []storedCluster

// find returns the cluster with the name and whether it exists.
func (l clusterList) find(name string) (storedCluster, bool) {
	i := slices.IndexFunc(l, func(c storedCluster) bool { return c.name() == name })
	if i < 0 {
		return storedCluster{}, false
	}
	return l[i], true
}

// hosted returns the hosted clusters of a management cluster.
func (l clusterList) hosted(mgmt string) clusterList {
	var hosted clusterList
	for _, c := range l {
		if c.isHosted() && c.mgmt == mgmt {
			hosted = append(hosted, c)
		}
	}
	return hosted
}

func (l clusterList) names() []string {
	var names []string
	for _, c := range l {
		names = append(names, c.name())
	}
	return names
}

// clusterStore keeps the records and files of the clusters in the data
// directory. A record is saved before the first cloud resource of a cluster
// is created and removed after the last one is destroyed.
type clusterStore struct {
	root string
}

func newClusterStore(dirs xdg.Dirs) clusterStore {
	return clusterStore{root: filepath.Join(dirs.Data, "clusters")}
}

// check reports whether a cluster is installed. A cluster whose creation
// did not complete must be destroyed before creating it again.
func (s clusterStore) check(c clusterRef) (bool, error) {
	record, found, err := s.load(c)
	switch {
	case err != nil || !found:
		return false, err
	case record.Phase == phaseInstalled:
		return true, nil
	case c.hosted != "":
		return false, fmt.Errorf("previous creation of hosted cluster %s did not complete, destroy it first", c.hosted)
	default:
		return false, fmt.Errorf("previous installation of management cluster %s did not complete, destroy it first", c.mgmt)
	}
}

// begin saves the record of a new cluster as installing, a management
// cluster without the HyperShift operator. It requires what
// destroying the cluster needs: the pull secret of an OpenShift management
// cluster, the infra ID of a hosted cluster and the shared resources of an
// Azure hosted cluster.
func (s clusterStore) begin(c clusterRef, r clusterRecord) error {
	switch {
	case c.hosted == "" && r.Platform == platformAWS && r.PullSecret == "":
		return fmt.Errorf("%s needs the pull secret", c)
	case c.hosted != "" && r.InfraID == "":
		return fmt.Errorf("%s needs an infra ID", c)
	case c.hosted != "" && r.Platform == platformAzure && (r.Azure == nil || r.Azure.ResourceGroup == ""):
		return fmt.Errorf("%s needs the shared Azure resources", c)
	}
	_, found, err := s.load(c)
	if err != nil {
		return err
	}
	if found {
		return fmt.Errorf("%s already has a record", c)
	}
	r.Phase = phaseInstalling
	if c.hosted == "" {
		r.HypershiftOperator = new(false)
	}
	return s.save(c, r)
}

// markInstalled records that creating a cluster completed.
func (s clusterStore) markInstalled(c clusterRef) error {
	record, found, err := s.load(c)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s has no record", c)
	}
	record.Phase = phaseInstalled
	return s.save(c, record)
}

// markOperatorInstalled records that the HyperShift operator is installed on
// a management cluster, with the base domain and pull secret it uses, which
// a cluster created without the operator may not have recorded.
func (s clusterStore) markOperatorInstalled(c clusterRef, baseDomain, pullSecret string) error {
	record, found, err := s.load(c)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%s has no record", c)
	}
	record.HypershiftOperator = new(true)
	record.BaseDomain = baseDomain
	record.PullSecret = pullSecret
	return s.save(c, record)
}

// destroying returns the record of a cluster to destroy and whether it
// exists. Without a record, creating the cluster never started and its
// directory is removed.
func (s clusterStore) destroying(c clusterRef) (clusterRecord, bool, error) {
	record, found, err := s.load(c)
	if err != nil || found {
		return record, found, err
	}
	return clusterRecord{}, false, s.forget(c)
}

// forget removes everything kept for a destroyed cluster.
func (s clusterStore) forget(c clusterRef) error {
	return os.RemoveAll(s.dir(c))
}

// track creates the directory of a cluster before anything else, so that
// the cluster exists even if its creation stops before the record.
func (s clusterStore) track(c clusterRef) error {
	return os.MkdirAll(s.dir(c), 0o700)
}

// list returns the clusters with a record or a directory.
func (s clusterStore) list() (clusterList, error) {
	mgmts, err := s.listDir(s.root)
	if err != nil {
		return nil, err
	}
	var clusters clusterList
	for _, mgmt := range mgmts {
		hosteds, err := s.listDir(filepath.Join(s.root, mgmt, "hosted"))
		if err != nil {
			return nil, err
		}
		refs := []clusterRef{managementCluster(mgmt)}
		for _, hosted := range hosteds {
			refs = append(refs, hostedCluster(mgmt, hosted))
		}
		for _, c := range refs {
			record, found, err := s.load(c)
			if err != nil {
				return nil, err
			}
			clusters = append(clusters, storedCluster{clusterRef: c, record: record, recorded: found})
		}
	}
	return clusters, nil
}

// listDir returns the names of the directories in dir, sorted, and none if
// dir does not exist.
func (s clusterStore) listDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// load returns the record of a cluster and whether it exists.
func (s clusterStore) load(c clusterRef) (clusterRecord, bool, error) {
	var r clusterRecord
	found, err := readYAML(s.recordPath(c), &r)
	return r, found, err
}

func (s clusterStore) save(c clusterRef, r clusterRecord) error {
	r.UpdatedAt = time.Now()
	return writeYAML(s.recordPath(c), r)
}

func (s clusterStore) recordPath(c clusterRef) string {
	return filepath.Join(s.dir(c), "cluster.yaml")
}

// dir holds everything hyperctl keeps for a cluster. The directories of
// hosted clusters are inside the one of their management cluster.
func (s clusterStore) dir(c clusterRef) string {
	dir := filepath.Join(s.root, c.mgmt)
	if c.hosted != "" {
		dir = filepath.Join(dir, "hosted", c.hosted)
	}
	return dir
}

// installDir is the openshift-install asset directory of a management
// cluster, needed for destroy.
func (s clusterStore) installDir(c clusterRef) string {
	return filepath.Join(s.dir(c), "install")
}

// kubeconfig is the admin kubeconfig of a cluster. The installer writes the
// one of OpenShift management clusters, hyperctl writes it to the same
// place for AKS clusters.
func (s clusterStore) kubeconfig(c clusterRef) string {
	if c.hosted != "" {
		return filepath.Join(s.dir(c), "kubeconfig")
	}
	return filepath.Join(s.installDir(c), "auth", "kubeconfig")
}
