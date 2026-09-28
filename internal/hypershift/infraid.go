package hypershift

import (
	"math/rand/v2"
	"strings"
)

const (
	infraIDMaxLen    = 27
	infraIDRandomLen = 5
	infraIDAlphabet  = "abcdefghijklmnopqrstuvwxyz0123456789"
)

// NewInfraID returns an infra ID for a cluster name in the format hypershift
// generates: the name, shortened if needed, and a random suffix.
func NewInfraID(name string) string {
	base := name
	if maxBaseLen := infraIDMaxLen - infraIDRandomLen - 1; len(base) > maxBaseLen {
		base = strings.TrimRight(base[:maxBaseLen], "-")
	}
	suffix := make([]byte, infraIDRandomLen)
	for i := range suffix {
		suffix[i] = infraIDAlphabet[rand.IntN(len(infraIDAlphabet))]
	}
	return base + "-" + string(suffix)
}
