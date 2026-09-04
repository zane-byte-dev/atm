package web

import "github.com/zane-byte-dev/atm/internal/contract"

// workspaceMethodAccess is an explicit transport policy. Adding a page never
// opens an arbitrary IPC, command, config object, sync job, or model operation.
// All mutations share the same database-upgrade gate before dispatch.
func workspaceMethodAccess(method string) (known, write bool) {
	spec, known := contract.LookupWorkspaceMethod(method)
	return known, spec.Write
}

// workspaceWriteDomains describes the smallest live-query surface changed by a
// successful browser mutation. Runtime work publishes any additional domains
// when that work completes; this mapping only covers the mutation accepted by
// the HTTP request itself.
func workspaceWriteDomains(method string) []string {
	spec, ok := contract.LookupWorkspaceMethod(method)
	if !ok || !spec.Write {
		return nil
	}
	return spec.Domains
}
