package web

import "github.com/zane-byte-dev/atm/internal/contract"

// workspaceMethodAccess is an explicit transport policy. Adding a page never
// opens an arbitrary command, config object, sync job, or model operation.
func workspaceMethodAccess(method string) (known, write bool) {
	spec, known := contract.LookupWorkspaceMethod(method)
	return known, spec.Write
}
