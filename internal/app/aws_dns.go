package app

import (
	"context"
	"fmt"
	"strings"
)

// deleteDNSLeftovers deletes DNS entries that clusters with the given names
// left behind, e.g. after their instances were pruned. It asks for
// confirmation unless yes is set.
func deleteDNSLeftovers(ctx context.Context, p progress, deps Deps, yes bool, baseDomain string, names ...string) error {
	if len(names) == 0 {
		return nil
	}
	var withLeftovers []string
	var lines []string
	err := p.wait(ctx, "Checking DNS leftovers of "+strings.Join(names, ", "), func(ctx context.Context) error {
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
				lines = append(lines, "    "+entry)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(withLeftovers) == 0 {
		p.done("No DNS leftovers of %s", strings.Join(names, ", "))
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
	err = p.wait(ctx, "Deleting DNS entries of "+strings.Join(withLeftovers, ", "), func(ctx context.Context) error {
		for _, name := range withLeftovers {
			if err := deps.AWS.DeleteClusterDNS(ctx, baseDomain, name); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.done("Deleted DNS entries:\n%s", list)
	return nil
}
