// Package architecture locks in dependency directions that ATM has already
// established. The tests inspect source imports rather than relying on package
// initialization, so they cover platform-specific production files as well.
package architecture

import (
	"bufio"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const cobraImport = "github.com/spf13/cobra"

type sourceImport struct {
	file       string
	packageDir string
	path       string
}

func TestOnlyCommandAdapterImportsCommandPackage(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	commandImport := module + "/internal/cmd"

	assertNoImports(t, imports, func(item sourceImport) string {
		if withinPackageDir(item.packageDir, "cmd") {
			return ""
		}
		if importWithin(item.path, commandImport) {
			return fmt.Sprintf("%s imports command adapter %q", item.file, item.path)
		}
		return ""
	})
}

func TestCobraStaysAtCommandAdapterEdge(t *testing.T) {
	root, _ := repository(t)
	imports := scanProductionImports(t, root)

	assertNoImports(t, imports, func(item sourceImport) string {
		if withinPackageDir(item.packageDir, "cmd") {
			return ""
		}
		if importWithin(item.path, cobraImport) {
			return fmt.Sprintf("%s imports Cobra %q outside the command adapter", item.file, item.path)
		}
		return ""
	})
}

func TestCommandRenderingStaysAtCommandAdapterEdge(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	outputImport := module + "/internal/output"

	assertNoImports(t, imports, func(item sourceImport) string {
		if withinPackageDir(item.packageDir, "cmd") {
			return ""
		}
		if importWithin(item.path, outputImport) {
			return fmt.Sprintf("%s imports command renderer %q outside the command adapter", item.file, item.path)
		}
		return ""
	})
}

func TestApplicationPrimitivesRemainIndependent(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	internalImport := module + "/internal"

	assertNoImports(t, imports, func(item sourceImport) string {
		if !withinPackageDir(item.packageDir, "application") {
			return ""
		}
		switch {
		case importWithin(item.path, cobraImport):
			return fmt.Sprintf("%s imports Cobra %q", item.file, item.path)
		case importWithin(item.path, internalImport):
			return fmt.Sprintf("%s imports ATM transport, storage, or domain package %q", item.file, item.path)
		default:
			return ""
		}
	})
}

func TestIPCTransportDoesNotDependOnCobraOrCommands(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	commandImport := module + "/internal/cmd"

	assertNoImports(t, imports, func(item sourceImport) string {
		if !withinPackageDir(item.packageDir, "ipc") {
			return ""
		}
		switch {
		case importWithin(item.path, cobraImport):
			return fmt.Sprintf("%s imports Cobra %q", item.file, item.path)
		case importWithin(item.path, commandImport):
			return fmt.Sprintf("%s imports command adapter %q", item.file, item.path)
		default:
			return ""
		}
	})
}

func TestAppIPCCompositionDoesNotDependOnCommandAdapters(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	commandImport := module + "/internal/cmd"
	outputImport := module + "/internal/output"

	assertNoImports(t, imports, func(item sourceImport) string {
		if !withinPackageDir(item.packageDir, "appipc") {
			return ""
		}
		switch {
		case importWithin(item.path, cobraImport):
			return fmt.Sprintf("%s imports Cobra %q", item.file, item.path)
		case importWithin(item.path, commandImport):
			return fmt.Sprintf("%s imports command adapter %q", item.file, item.path)
		case importWithin(item.path, outputImport):
			return fmt.Sprintf("%s imports command renderer %q", item.file, item.path)
		default:
			return ""
		}
	})
}

// Migrated adapters are kept on an explicit allowlist while the rest of cmd is
// moved incrementally. Once a command file has a service boundary, importing
// SQLite or store again would silently put orchestration back in Cobra.
func TestMigratedCommandAdaptersDoNotOpenPersistence(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	storeImport := module + "/internal/store"
	migrated := map[string]bool{
		"internal/cmd/config.go":                   true,
		"internal/cmd/collect_source.go":           true,
		"internal/cmd/dashboard_adapter.go":        true,
		"internal/cmd/day.go":                      true,
		"internal/cmd/day_extra.go":                true,
		"internal/cmd/guard_decide.go":             true,
		"internal/cmd/guard_install.go":            true,
		"internal/cmd/guard_query.go":              true,
		"internal/cmd/guard_rule.go":               true,
		"internal/cmd/knowledge.go":                true,
		"internal/cmd/knowledge_bulk.go":           true,
		"internal/cmd/knowledge_collection.go":     true,
		"internal/cmd/list.go":                     true,
		"internal/cmd/search.go":                   true,
		"internal/cmd/session_binding.go":          true,
		"internal/cmd/show.go":                     true,
		"internal/cmd/stats.go":                    true,
		"internal/cmd/timeline.go":                 true,
		"internal/cmd/todo_metadata_adapter.go":    true,
		"internal/cmd/todo_bulk.go":                true,
		"internal/cmd/todo_dependency.go":          true,
		"internal/cmd/todo_link.go":                true,
		"internal/cmd/todo_lint.go":                true,
		"internal/cmd/todo_lifecycle_adapter.go":   true,
		"internal/cmd/todo_maintenance_adapter.go": true,
		"internal/cmd/todo_read_adapter.go":        true,
		"internal/cmd/todo_refine.go":              true,
		"internal/cmd/todo_retention_adapter.go":   true,
		"internal/cmd/todo_review_context.go":      true,
		"internal/cmd/todo_run_management.go":      true,
	}

	assertNoImports(t, imports, func(item sourceImport) string {
		if !migrated[item.file] {
			return ""
		}
		if item.path == "database/sql" || importWithin(item.path, storeImport) {
			return fmt.Sprintf("%s reaches persistence directly through %q instead of its application service", item.file, item.path)
		}
		return ""
	})
}

func TestTaskRunManagementAdapterDoesNotReachPersistenceProcessOrFilesystem(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	storeImport := module + "/internal/store"

	assertNoImports(t, imports, func(item sourceImport) string {
		if item.file != "internal/cmd/todo_run_management.go" {
			return ""
		}
		switch {
		case item.path == "database/sql" || importWithin(item.path, storeImport):
			return fmt.Sprintf("%s reaches task-run persistence directly through %q", item.file, item.path)
		case item.path == "os" || item.path == "os/exec" || item.path == "path/filepath" || item.path == "syscall":
			return fmt.Sprintf("%s reaches task-run process/filesystem infrastructure directly through %q", item.file, item.path)
		default:
			return ""
		}
	})
}

func TestTaskRunApplicationDoesNotDependOnCommandOrRenderingPackages(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	commandImport := module + "/internal/cmd"
	outputImport := module + "/internal/output"

	assertNoImports(t, imports, func(item sourceImport) string {
		if !withinPackageDir(item.packageDir, "taskrun") {
			return ""
		}
		switch {
		case importWithin(item.path, cobraImport):
			return fmt.Sprintf("%s imports Cobra %q", item.file, item.path)
		case importWithin(item.path, commandImport):
			return fmt.Sprintf("%s imports command adapter %q", item.file, item.path)
		case importWithin(item.path, outputImport):
			return fmt.Sprintf("%s imports command renderer %q", item.file, item.path)
		default:
			return ""
		}
	})
}

func TestTodoBulkAdapterDoesNotReachPersistenceClockOrFilesystem(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	storeImport := module + "/internal/store"
	configImport := module + "/internal/config"

	assertNoImports(t, imports, func(item sourceImport) string {
		if item.file != "internal/cmd/todo_bulk.go" {
			return ""
		}
		switch {
		case item.path == "database/sql" || importWithin(item.path, storeImport):
			return fmt.Sprintf("%s reaches persistence directly through %q", item.file, item.path)
		case importWithin(item.path, configImport) || item.path == "time":
			return fmt.Sprintf("%s owns application configuration or time through %q", item.file, item.path)
		case item.path == "os" || item.path == "os/exec" || item.path == "path/filepath":
			return fmt.Sprintf("%s reaches filesystem/process infrastructure directly through %q", item.file, item.path)
		default:
			return ""
		}
	})
}

func TestTodoMetadataAdapterDoesNotReachPersistence(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	storeImport := module + "/internal/store"

	assertNoImports(t, imports, func(item sourceImport) string {
		if item.file != "internal/cmd/todo_metadata_adapter.go" {
			return ""
		}
		if item.path == "database/sql" || importWithin(item.path, storeImport) {
			return fmt.Sprintf("%s reaches persistence directly through %q", item.file, item.path)
		}
		return ""
	})
}

func TestTodoReadAdaptersDoNotReachPersistenceOrFilesystem(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	storeImport := module + "/internal/store"
	readAdapters := map[string]bool{
		"internal/cmd/todo_read_adapter.go":   true,
		"internal/cmd/todo_review_context.go": true,
	}

	assertNoImports(t, imports, func(item sourceImport) string {
		if !readAdapters[item.file] {
			return ""
		}
		switch {
		case item.path == "database/sql" || importWithin(item.path, storeImport):
			return fmt.Sprintf("%s reaches persistence directly through %q", item.file, item.path)
		case item.path == "os" || item.path == "os/exec" || item.path == "path/filepath":
			return fmt.Sprintf("%s reaches the filesystem directly through %q", item.file, item.path)
		default:
			return ""
		}
	})
}

func TestGuardManagementAdaptersDoNotReachConfigOrFilesystem(t *testing.T) {
	root, module := repository(t)
	imports := scanProductionImports(t, root)
	configImport := module + "/internal/config"
	managementAdapters := map[string]bool{
		"internal/cmd/guard_install.go": true,
		"internal/cmd/guard_rule.go":    true,
	}

	assertNoImports(t, imports, func(item sourceImport) string {
		if !managementAdapters[item.file] {
			return ""
		}
		switch {
		case importWithin(item.path, configImport):
			return fmt.Sprintf("%s reaches Guard config directly through %q", item.file, item.path)
		case item.path == "os" || item.path == "os/exec" || item.path == "path/filepath" || item.path == "runtime":
			return fmt.Sprintf("%s reaches Guard filesystem/process infrastructure directly through %q", item.file, item.path)
		default:
			return ""
		}
	})
}

func assertNoImports(t *testing.T, imports []sourceImport, violation func(sourceImport) string) {
	t.Helper()
	var violations []string
	for _, item := range imports {
		if message := violation(item); message != "" {
			violations = append(violations, message)
		}
	}
	if len(violations) == 0 {
		return
	}
	sort.Strings(violations)
	t.Fatalf("architecture boundary violated:\n  %s", strings.Join(violations, "\n  "))
}

func scanProductionImports(t *testing.T, root string) []sourceImport {
	t.Helper()
	internalRoot := filepath.Join(root, "internal")
	var imports []sourceImport
	err := filepath.WalkDir(internalRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		relativeFile, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("make %s relative to repository: %w", path, err)
		}
		packageDir, err := filepath.Rel(internalRoot, filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("make package for %s relative to internal: %w", path, err)
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return fmt.Errorf("decode import in %s: %w", path, err)
			}
			imports = append(imports, sourceImport{
				file:       filepath.ToSlash(relativeFile),
				packageDir: filepath.ToSlash(packageDir),
				path:       importPath,
			})
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan production imports: %v", err)
	}
	return imports
}

func withinPackageDir(packageDir, root string) bool {
	return packageDir == root || strings.HasPrefix(packageDir, root+"/")
}

func importWithin(importPath, root string) bool {
	return importPath == root || strings.HasPrefix(importPath, root+"/")
}

// repository locates the module from this test's source path. os.Getwd is only
// a fallback for unusual builds that trim source paths; correctness does not
// depend on `go test` being launched from the repository root or package dir.
func repository(t *testing.T) (root, module string) {
	t.Helper()
	starts := []string{}
	if _, source, _, ok := runtime.Caller(0); ok {
		starts = append(starts, filepath.Dir(source))
	}
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}

	seen := map[string]bool{}
	for _, start := range starts {
		absolute, err := filepath.Abs(start)
		if err != nil {
			continue
		}
		for directory := filepath.Clean(absolute); ; directory = filepath.Dir(directory) {
			if !seen[directory] {
				seen[directory] = true
				if modulePath, ok := readModulePath(filepath.Join(directory, "go.mod")); ok {
					return directory, modulePath
				}
			}
			parent := filepath.Dir(directory)
			if parent == directory {
				break
			}
		}
	}
	t.Fatal("could not locate repository go.mod from test source path")
	return "", ""
}

func readModulePath(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], true
		}
	}
	return "", false
}
