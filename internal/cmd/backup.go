package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"

	"github.com/spf13/cobra"
)

func init() {
	backupCmd.Flags().StringVarP(&backupOutputFlag, "output", "o", "", "archive path (default: ./atm-backup-<timestamp>.tar.gz)")
	restoreCmd.Flags().BoolVar(&restoreYesFlag, "yes", false, "skip the confirmation prompt when existing data would be replaced")
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)
}

var (
	backupOutputFlag string
	restoreYesFlag   bool
)

// backupManifestName is read before anything is extracted, so it has to be the
// first entry the archive writer emits.
const backupManifestName = "manifest.json"

// backupPaths are the entries under ~/.atm a backup carries, relative to it.
// This is an allowlist rather than a "everything except caches" filter: a new
// cache file appearing in the data directory must not silently start riding
// along in every archive. The cost of the choice is that genuinely new records
// need a line here, so backupManifest records what was left behind (see
// unbackedEntries) and makes that omission visible instead of silent.
//
// atm.db is not listed: it is written from store.SnapshotOwnRecords rather than
// copied, because the live file is a WAL database and its -wal sibling.
var backupPaths = []string{
	"config.json",
	"connectors",
	"knowledge",
	"memory",
	"native-hosts",
	"pricing.json",
	"providers",
	"todos",
}

// transientEntries never belong in a backup and are not worth reporting as
// omissions either: sockets, caches, hook spool files and the -wal/-shm siblings
// of a snapshot that already contains their committed contents.
var transientEntries = []string{
	"atm.db",
	"atm.db-shm",
	"atm.db-wal",
	"exec",
	// Diagnostic output, not a record: it describes what went wrong on this
	// machine and means nothing after a restore elsewhere.
	"logs",
	"notch.sock",
}

type backupManifest struct {
	ATMVersion    string   `json:"atm_version"`
	SchemaVersion int      `json:"schema_version"`
	CreatedAt     string   `json:"created_at"`
	Database      string   `json:"database"`
	Contents      []string `json:"contents"`
	// EmptiedTables says which tables the database in this archive carries with
	// no rows, so a restore can tell the user what `atm sync` still has to refill.
	EmptiedTables []string `json:"emptied_tables"`
	// UnbackedEntries lists what existed under ~/.atm but is not in the archive.
	// Anything here is either transient or an allowlist gap; recording it is how
	// the gap becomes reviewable rather than invisible.
	UnbackedEntries []string `json:"unbacked_entries"`
}

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Archive the records ATM cannot rebuild",
	Long: `Archive the records ATM cannot rebuild from any other source.

Todos, memory, knowledge, the collection ledger and the review cursor are this
database's own records — nothing else holds a copy. The session mirror is left
out because ` + "`atm sync`" + ` rebuilds it from each agent's transcripts, which keeps
the archive small enough to take often.

Works on a database too old for this build to open normally, so it is also the
escape hatch when a schema is rejected as unupgradable.`,
	Args: cobra.NoArgs,
	RunE: runBackup,
}

var restoreCmd = &cobra.Command{
	Use:   "restore <archive>",
	Short: "Restore records from an archive created by atm backup",
	Long: `Restore records from an archive created by atm backup.

Existing data is moved aside rather than deleted: anything the archive replaces
is kept under ~/.atm/pre-restore-<timestamp>/ so a wrong restore is reversible.

The session mirror is not in the archive; run ` + "`atm sync`" + ` afterwards to rebuild it.`,
	Args: cobra.ExactArgs(1),
	RunE: runRestore,
}

func runBackup(cmd *cobra.Command, args []string) error {
	// A missing database is not a reason to refuse. Knowledge and todo documents
	// are plain files that exist independently of it, and telling someone who
	// asked for a backup to run `atm sync` first would be advice for a different
	// problem. Archive whatever is actually there.
	schemaVersion, err := store.ReadSchemaVersionAt(config.AtmDB)
	databaseMissing := errors.Is(err, store.ErrDatabaseMissing)
	if err != nil && !databaseMissing {
		return fmt.Errorf("read schema version: %w", err)
	}

	now := time.Now()
	target := backupOutputFlag
	if target == "" {
		target = fmt.Sprintf("atm-backup-%s.tar.gz", now.In(config.Loc).Format("20060102-150405"))
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("refusing to overwrite %s: pass --output to choose another path", target)
	} else if !os.IsNotExist(err) {
		return err
	}

	// The snapshot is a real file because VACUUM INTO writes one; putting it next
	// to the archive rather than in the data directory keeps a failed backup from
	// leaving debris where ATM looks for its own files.
	staging, err := os.MkdirTemp(filepath.Dir(mustAbs(target)), ".atm-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	snapshot := ""
	if !databaseMissing {
		snapshot = filepath.Join(staging, "atm.db")
		if err := store.SnapshotOwnRecords(snapshot); err != nil {
			return err
		}
	}

	unbacked, err := unbackedEntries()
	if err != nil {
		return err
	}
	manifest := backupManifest{
		ATMVersion:      rootCmd.Version,
		SchemaVersion:   schemaVersion,
		CreatedAt:       now.UTC().Format(time.RFC3339),
		UnbackedEntries: unbacked,
	}
	if snapshot != "" {
		manifest.Database = "atm.db"
		manifest.EmptiedTables = store.RebuildableTables()
	}

	written, err := writeBackupArchive(target, snapshot, &manifest)
	if err != nil {
		os.Remove(target)
		return err
	}

	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if jsonOutput {
		output.JSON(map[string]any{
			"archive":  target,
			"bytes":    info.Size(),
			"manifest": manifest,
		})
		return nil
	}
	fmt.Printf("Backed up to %s (%s)\n", target, formatBytes(info.Size()))
	if manifest.Database == "" {
		fmt.Printf("  database: none at %s — files only\n", config.AtmDB)
	} else {
		fmt.Printf("  schema: v%d\n", manifest.SchemaVersion)
	}
	fmt.Printf("  contents: %s\n", strings.Join(written, ", "))
	if manifest.Database != "" {
		fmt.Printf("  carried empty: session mirror (%s) — `atm sync` refills it\n",
			strings.Join(manifest.EmptiedTables, ", "))
	}
	if len(unbacked) > 0 {
		fmt.Printf("  left in place: %s\n", strings.Join(unbacked, ", "))
	}
	return nil
}

// writeBackupArchive streams the snapshot and every backupPaths entry into a
// gzipped tar. The manifest goes first so a reader can validate before
// extracting, but its Contents are only known once everything has been walked,
// so the entries are collected first and the manifest is written from that.
func writeBackupArchive(target, snapshot string, manifest *backupManifest) ([]string, error) {
	var contents []string
	for _, name := range backupPaths {
		if _, err := os.Stat(filepath.Join(config.AtmDir, name)); err == nil {
			contents = append(contents, name)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	manifest.Contents = contents
	if manifest.Database == "" && len(contents) == 0 {
		return nil, fmt.Errorf("nothing to back up: %s holds no ATM records", config.AtmDir)
	}

	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	archive := tar.NewWriter(gz)

	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	manifestJSON = append(manifestJSON, '\n')
	if err := archive.WriteHeader(&tar.Header{
		Name: backupManifestName,
		Mode: 0600,
		Size: int64(len(manifestJSON)),
	}); err != nil {
		return nil, err
	}
	if _, err := archive.Write(manifestJSON); err != nil {
		return nil, err
	}

	if snapshot != "" {
		if err := addFileToArchive(archive, snapshot, manifest.Database); err != nil {
			return nil, err
		}
	}
	for _, name := range contents {
		if err := addTreeToArchive(archive, filepath.Join(config.AtmDir, name), name); err != nil {
			return nil, err
		}
	}

	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	// The archive is the deliverable; a lost write here would produce a truncated
	// backup that only fails when someone tries to restore it.
	if err := file.Sync(); err != nil {
		return nil, err
	}
	return contents, nil
}

func addTreeToArchive(archive *tar.Writer, root, prefix string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := prefix
		if relative != "." {
			name = filepath.ToSlash(filepath.Join(prefix, relative))
		}
		switch {
		case info.IsDir():
			return archive.WriteHeader(&tar.Header{
				Name:     name + "/",
				Mode:     0700,
				Typeflag: tar.TypeDir,
			})
		case info.Mode().IsRegular():
			return addFileToArchive(archive, path, name)
		default:
			// Sockets and symlinks are either runtime state or point outside the
			// data directory; neither is a record worth carrying.
			return nil
		}
	})
}

func addFileToArchive(archive *tar.Writer, path, name string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if err := archive.WriteHeader(&tar.Header{
		Name:    name,
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	copied, err := io.Copy(archive, file)
	if err != nil {
		return err
	}
	// tar headers carry a size that must match the payload exactly. A file
	// growing or shrinking mid-walk would otherwise produce an archive that only
	// fails at extraction time.
	if copied != info.Size() {
		return fmt.Errorf("%s changed while being archived (%d of %d bytes)", path, copied, info.Size())
	}
	return nil
}

// unbackedEntries reports what sits in the data directory but is not carried,
// minus the transient files nobody would want back.
func unbackedEntries() ([]string, error) {
	entries, err := os.ReadDir(config.AtmDir)
	if err != nil {
		// No data directory at all is not an error here: the caller decides
		// whether an empty backup is worth refusing, and it produces a better
		// message than a bare ENOENT on a path the user never mentioned.
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	carried := make(map[string]bool, len(backupPaths)+len(transientEntries))
	for _, name := range backupPaths {
		carried[name] = true
	}
	for _, name := range transientEntries {
		carried[name] = true
	}
	var out []string
	for _, entry := range entries {
		name := entry.Name()
		if carried[name] || strings.HasPrefix(name, ".atm-backup-") || strings.HasPrefix(name, "pre-restore-") {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func runRestore(cmd *cobra.Command, args []string) error {
	archivePath := args[0]
	manifest, err := readBackupManifest(archivePath)
	if err != nil {
		return err
	}
	// A newer archive may hold columns this build cannot interpret. Extracting it
	// would produce a database that reads as valid and answers wrongly, which is
	// worse than refusing.
	if manifest.SchemaVersion > store.SchemaVersion {
		return fmt.Errorf("archive holds schema v%d but this atm build supports v%d: upgrade atm before restoring",
			manifest.SchemaVersion, store.SchemaVersion)
	}

	if err := os.MkdirAll(config.AtmDir, 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(config.AtmDir, ".atm-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	extracted, err := extractBackupArchive(archivePath, staging)
	if err != nil {
		return err
	}
	if len(extracted) == 0 {
		return fmt.Errorf("%s contains no restorable entries", archivePath)
	}

	var conflicts []string
	for _, name := range extracted {
		if _, err := os.Stat(filepath.Join(config.AtmDir, name)); err == nil {
			conflicts = append(conflicts, name)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if len(conflicts) > 0 {
		confirmed, err := confirmDestructive(cmd, restoreYesFlag,
			fmt.Sprintf("Restore will replace %s in %s (moved to pre-restore-<timestamp>/, not deleted). Continue?",
				strings.Join(conflicts, ", "), config.AtmDir))
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	aside := ""
	if len(conflicts) > 0 {
		aside = filepath.Join(config.AtmDir, "pre-restore-"+time.Now().In(config.Loc).Format("20060102-150405"))
		if err := os.MkdirAll(aside, 0700); err != nil {
			return err
		}
		for _, name := range conflicts {
			if err := os.Rename(filepath.Join(config.AtmDir, name), filepath.Join(aside, name)); err != nil {
				return fmt.Errorf("move %s aside: %w", name, err)
			}
		}
	}

	for _, name := range extracted {
		if err := os.Rename(filepath.Join(staging, name), filepath.Join(config.AtmDir, name)); err != nil {
			return fmt.Errorf("restore %s: %w", name, err)
		}
	}

	if jsonOutput {
		output.JSON(map[string]any{
			"restored":       extracted,
			"schema_version": manifest.SchemaVersion,
			"replaced":       conflicts,
			"moved_aside":    aside,
			"next":           "run `atm sync` to rebuild the session mirror",
		})
		return nil
	}
	fmt.Printf("Restored %s from %s\n", strings.Join(extracted, ", "), archivePath)
	fmt.Printf("  schema: v%d (this build: v%d)\n", manifest.SchemaVersion, store.SchemaVersion)
	if aside != "" {
		fmt.Printf("  replaced data moved to %s\n", aside)
	}
	fmt.Println("  run `atm sync` to rebuild the session mirror")
	return nil
}

func readBackupManifest(archivePath string) (backupManifest, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return backupManifest{}, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return backupManifest{}, fmt.Errorf("%s is not a gzip archive: %w", archivePath, err)
	}
	defer gz.Close()
	archive := tar.NewReader(gz)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return backupManifest{}, err
		}
		if header.Name != backupManifestName {
			continue
		}
		var manifest backupManifest
		// Bounded because the manifest is read before anything is trusted; a
		// hostile or corrupt archive must not be able to make this allocate.
		if err := json.NewDecoder(io.LimitReader(archive, 1<<20)).Decode(&manifest); err != nil {
			return backupManifest{}, fmt.Errorf("read %s: %w", backupManifestName, err)
		}
		return manifest, nil
	}
	return backupManifest{}, fmt.Errorf("%s has no %s: it was not created by `atm backup`", archivePath, backupManifestName)
}

// extractBackupArchive unpacks into staging and returns the top-level names it
// wrote, so the caller can move exactly those into place.
func extractBackupArchive(archivePath, staging string) ([]string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	seen := map[string]bool{}
	var top []string
	archive := tar.NewReader(gz)
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Name == backupManifestName {
			continue
		}
		relative, err := safeExtractPath(header.Name)
		if err != nil {
			return nil, err
		}
		destination := filepath.Join(staging, relative)
		// Confirmed here, in the function that does the writing, rather than
		// trusted from the helper: this is the guard that has to hold for every
		// MkdirAll and OpenFile below, and a reader (or an analyser) checking
		// whether extraction can escape should not have to leave this loop to find
		// out. safeExtractPath decides whether the entry is a plain relative path;
		// this decides whether the path it produced landed inside our own root.
		if destination != staging && !strings.HasPrefix(destination, staging+string(os.PathSeparator)) {
			return nil, fmt.Errorf("archive entry %q escapes the extraction directory", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destination, 0700); err != nil {
				return nil, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
				return nil, err
			}
			if err := writeExtractedFile(destination, archive, header); err != nil {
				return nil, err
			}
		default:
			// Symlinks and devices are the classic way an archive escapes its
			// extraction root, and a backup never needs them.
			continue
		}
		// Taken from the validated relative path, not from header.Name again. These
		// names are what the caller moves into ~/.atm, so parsing the raw name a
		// second time would mean two pieces of code deciding what an entry is
		// called, with only one of them having been checked.
		name := strings.SplitN(filepath.ToSlash(relative), "/", 2)[0]
		if name != "" && name != "." && !seen[name] {
			seen[name] = true
			top = append(top, name)
		}
	}
	sort.Strings(top)
	return top, nil
}

func writeExtractedFile(destination string, source io.Reader, header *tar.Header) error {
	mode := os.FileMode(header.Mode).Perm()
	if mode == 0 {
		mode = 0600
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer file.Close()
	// Bounded by the header's own size so a corrupt or hostile archive cannot
	// stream unbounded data into the data directory.
	written, err := io.Copy(file, io.LimitReader(source, header.Size))
	if err != nil {
		return err
	}
	if written != header.Size {
		return fmt.Errorf("%s is truncated: got %d of %d bytes", header.Name, written, header.Size)
	}
	return nil
}

// safeExtractPath normalises an archive entry into the relative path it may be
// written to, and refuses anything that would land outside the extraction tree —
// absolute paths, .. traversal, or a name that normalises out of the tree.
//
// It returns the relative path rather than a resolved one so that the caller
// joins it against its own root and checks the result there: the guard that
// matters belongs next to the writes it protects, not one call away from them.
func safeExtractPath(name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry %q escapes the extraction directory", name)
	}
	return cleaned, nil
}

func mustAbs(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return absolute
}

func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"KB", "MB", "GB", "TB"}
	for _, suffix := range units {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f PB", value/unit)
}
