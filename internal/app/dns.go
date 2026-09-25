package app

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// deleteDNSLeftovers deletes DNS entries that clusters with the given names
// left behind, e.g. after their instances were pruned. It asks for
// confirmation unless yes is set.
func deleteDNSLeftovers(ctx context.Context, w io.Writer, deps Deps, yes bool, baseDomain string, names ...string) error {
	var withLeftovers []string
	var lines []string
	for _, name := range names {
		entries, err := deps.AWS.ClusterDNS(ctx, baseDomain, name)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			continue
		}
		withLeftovers = append(withLeftovers, name)
		for _, entry := range entries {
			lines = append(lines, "  "+entry)
		}
	}
	if len(withLeftovers) == 0 {
		return nil
	}

	list := strings.Join(lines, "\n")
	if !yes {
		if !deps.Interactive {
			return fmt.Errorf("found DNS entries of clusters named %s:\n%s\nconfirm deleting them with --yes", strings.Join(withLeftovers, ", "), list)
		}
		if err := confirmDNSDeletion(ctx, list); err != nil {
			return err
		}
	}
	for _, name := range withLeftovers {
		if err := deps.AWS.DeleteClusterDNS(ctx, baseDomain, name); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(w, "Deleted DNS entries:\n%s\n", list)
	return nil
}
