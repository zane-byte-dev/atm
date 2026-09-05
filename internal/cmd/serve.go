package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	webassets "github.com/zane-byte-dev/atm/app/web"
	"github.com/zane-byte-dev/atm/internal/apphost"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/executionlock"
	"github.com/zane-byte-dev/atm/internal/store"
	webapp "github.com/zane-byte-dev/atm/internal/web"
)

var (
	servePort    int
	serveOpen    bool
	serveDataDir string
	serveDevUI   string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Open a local browser workspace for ATM",
	Long: `Serve ATM's browser workspace on this Mac. The CLI continues to work independently.

The workspace serves tasks, collection, Agent sessions, knowledge, usage,
AI Day, and settings. Go owns background sync, collection, Agent hooks, and
notification routing.

Use --open to establish a browser connection. An existing workspace for the
same data directory is reused. --data-dir selects an isolated ATM data directory
without changing HOME or Agent source directories.

A stale database is rejected at startup. Use atm serve migrate to create a
backup and explicitly upgrade it before opening the workspace.`,
	Args: noSubcommandArgs,
	RunE: runServe,
}

func init() {
	serveCmd.PersistentFlags().IntVar(&servePort, "port", 47321, "loopback port (0 selects an available port)")
	serveCmd.PersistentFlags().BoolVar(&serveOpen, "open", false, "open an authenticated browser page")
	serveCmd.PersistentFlags().StringVar(&serveDataDir, "data-dir", "", "ATM data directory (default: configured directory)")
	serveCmd.Flags().StringVar(&serveDevUI, "dev-ui", "", "proxy a local Vite server (http://127.0.0.1:5173) for frontend HMR")
	serveCmd.AddCommand(&cobra.Command{Use: "status", Short: "Show this data directory's running workspace", Args: cobra.NoArgs, RunE: runServeStatus})
	serveCmd.AddCommand(&cobra.Command{Use: "stop", Short: "Gracefully stop this data directory's workspace", Args: cobra.NoArgs, RunE: runServeStop})
	serveCmd.AddCommand(&cobra.Command{
		Use: "migrate", Short: "Back up and explicitly upgrade an existing workspace database", Args: cobra.NoArgs, RunE: runServeMigrate,
		Long: `Back up an existing ATM data directory, then upgrade its database for this build.

Stop the workspace with atm serve stop first and pause other ATM writers while
upgrading.

A normal atm backup archive is created in <data-dir>/backups before any schema
change. If the backup fails, the database is not migrated. Keep the archive for
recovery with atm restore. Use --data-dir to select the intended data directory.`,
	})
	rootCmd.AddCommand(serveCmd)
}

func runServe(command *cobra.Command, args []string) error {
	if servePort < 0 || servePort > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}
	if err := apphost.ConfigureDataDir(serveDataDir); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(commandContext(command), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	// A CLI-only binary can still open a complete instance that is already
	// serving pages; asset availability matters only when starting a new one.
	if status, err := webapp.ReadStatus(ctx, config.AtmDir); err == nil && status.Running {
		if serveOpen {
			return openExistingWorkspace(ctx, config.AtmDir)
		}
		fmt.Fprintf(command.OutOrStdout(), "ATM workspace is already running at %s\n", status.Instance.Origin)
		return nil
	}
	database, err := readWorkspaceDatabase()
	if err != nil {
		return err
	}
	if !database.Missing && database.Version < store.SchemaVersion {
		return fmt.Errorf("database schema v%d must be upgraded to v%d before serving; run `atm serve migrate --data-dir %s`", database.Version, store.SchemaVersion, config.AtmDir)
	}
	assets, err := webassets.Assets()
	if err != nil && serveDevUI == "" {
		return err
	}
	host := apphost.New(rootCmd.Version)
	host.SetWorkEffects(localWorkEffectExecutor{NotifyTodo: notifyTodoEvent})
	host.SetPresenceLoader(loadDashboardLiveStatus)
	tracker := apphost.NewChangeTracker(config.AtmDir)
	defer tracker.Close()
	options := webapp.Options{DataDir: config.AtmDir, Version: rootCmd.Version, Port: servePort, Assets: assets, Dispatch: host.Call, Attachment: host.Attachment,
		DevUI:        serveDevUI,
		Fingerprints: tracker.Fingerprints, Capabilities: host.RuntimeCapabilities, Companion: host.Companion, NativeControl: host.NativeControl,
		Upload: func(ctx context.Context, call application.Call, id, etag, name string, data []byte) (any, error) {
			return host.UploadTodoImage(ctx, call, apphost.UploadImageInput{TodoID: id, ExpectedETag: etag, Name: name, Data: data})
		},
	}
	options.StartRuntime = workspaceRuntime(ctx, host)
	server, err := webapp.Start(options)
	if errors.Is(err, webapp.ErrAlreadyRunning) && serveOpen {
		return openExistingWorkspace(ctx, config.AtmDir)
	}
	if err != nil {
		return err
	}
	defer server.Close()
	cliCommandLongRunning.Store(true)
	fmt.Fprintf(command.OutOrStdout(), "ATM workspace: %s\n", server.Info().Origin)
	fmt.Fprintln(command.OutOrStdout(), "Mode: Go runtime; background jobs, Agent hooks, and notifications are owned by this process.")
	if serveOpen {
		browserURL, err := server.BrowserURL()
		if err != nil {
			return err
		}
		if err := launchWorkspaceBrowser(ctx, browserURL); err != nil {
			fmt.Fprintln(command.ErrOrStderr(), "The workspace is running, but the browser could not open. Retry with atm serve --open.")
		}
	}
	return server.Wait(ctx)
}

func runServeStatus(command *cobra.Command, args []string) error {
	if err := apphost.ConfigureDataDir(serveDataDir); err != nil {
		return err
	}
	status, err := webapp.ReadStatus(commandContext(command), config.AtmDir)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(status)
	}
	if !status.Running {
		fmt.Fprintln(command.OutOrStdout(), "ATM workspace is not running.")
		return nil
	}
	fmt.Fprintf(command.OutOrStdout(), "ATM workspace: %s (pid %d, %s, mode %s)\n", status.Instance.Origin, status.Instance.PID, status.Instance.Version, status.Instance.Mode)
	return nil
}

func runServeStop(command *cobra.Command, args []string) error {
	if err := apphost.ConfigureDataDir(serveDataDir); err != nil {
		return err
	}
	managed, err := stopManagedWorkspace(commandContext(command), config.AtmDir)
	if err != nil {
		return err
	}
	if !managed {
		if err := webapp.Stop(commandContext(command), config.AtmDir); err != nil {
			return err
		}
	}
	if jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(map[string]bool{"stopping": true})
	}
	fmt.Fprintln(command.OutOrStdout(), "ATM workspace stop requested.")
	return nil
}

type workspaceDatabase struct {
	Version int
	Missing bool
}

// Startup inspects schema without migrating it. A stale workspace must cross
// serve migrate's stopped-instance lock and pre-migration backup boundary.
func readWorkspaceDatabase() (workspaceDatabase, error) {
	version, err := store.ReadSchemaVersionAt(config.AtmDB)
	if errors.Is(err, store.ErrDatabaseMissing) {
		return workspaceDatabase{Missing: true}, nil
	}
	if err != nil {
		return workspaceDatabase{}, fmt.Errorf("read workspace database schema: %w", err)
	}
	if version <= 0 {
		return workspaceDatabase{}, fmt.Errorf("database at %s has no ATM schema; refusing to initialize an existing unrecognized file", config.AtmDB)
	}
	if version > store.SchemaVersion {
		return workspaceDatabase{}, fmt.Errorf("database schema v%d is newer than this ATM build (v%d); update ATM before opening this workspace", version, store.SchemaVersion)
	}
	if version < store.MinUpgradableVersion {
		return workspaceDatabase{}, fmt.Errorf("database schema v%d is no longer supported by this ATM build (minimum v%d); use a release that supports this schema to create a backup or upgrade it", version, store.MinUpgradableVersion)
	}
	return workspaceDatabase{Version: version}, nil
}

type serveMigrationResult struct {
	Database   string `json:"database"`
	FromSchema int    `json:"from_schema"`
	ToSchema   int    `json:"to_schema"`
	Migrated   bool   `json:"migrated"`
	Archive    string `json:"archive,omitempty"`
}

func runServeMigrate(command *cobra.Command, args []string) error {
	if err := apphost.ConfigureDataDir(serveDataDir); err != nil {
		return err
	}
	database, err := readWorkspaceDatabase()
	if err != nil {
		return err
	}
	if database.Missing {
		return fmt.Errorf("no existing ATM database at %s; a new workspace does not need migration", config.AtmDB)
	}
	result := serveMigrationResult{Database: config.AtmDB, ToSchema: store.SchemaVersion}
	err = webapp.WithStoppedInstance(config.AtmDir, func() error {
		// New CLI invocations use the same execution locks. Holding them keeps
		// a complete sync/collection batch outside the backup/upgrade boundary.
		syncLock, err := executionlock.Acquire(commandContext(command), config.AtmDir, "sync")
		if err != nil {
			return err
		}
		defer syncLock.Close()
		collectionLock, err := executionlock.Acquire(commandContext(command), config.AtmDir, "collection")
		if err != nil {
			return err
		}
		defer collectionLock.Close()
		// Read again under the instance lock: another migration may have completed
		// since the initial check, or the database may have been replaced.
		database, err := readWorkspaceDatabase()
		if err != nil {
			return err
		}
		if database.Missing {
			return fmt.Errorf("ATM database disappeared before migration: %s", config.AtmDB)
		}
		result.FromSchema = database.Version
		if database.Version == store.SchemaVersion {
			return nil
		}
		fmt.Fprintln(command.ErrOrStderr(), "Before upgrading, pause other ATM writers using this data directory.")
		result.Archive, err = backupBeforeServeMigration(database.Version)
		if err != nil {
			return fmt.Errorf("pre-upgrade backup failed; database was not migrated: %w", err)
		}
		fmt.Fprintf(command.ErrOrStderr(), "Pre-upgrade backup: %s\n", result.Archive)
		db, err := store.Open()
		if err != nil {
			return fmt.Errorf("migration failed; pre-upgrade backup is at %s: %w", result.Archive, err)
		}
		if err := db.Close(); err != nil {
			return err
		}
		version, err := store.ReadSchemaVersionAt(config.AtmDB)
		if err != nil {
			return err
		}
		if version != store.SchemaVersion {
			return fmt.Errorf("migration ended at schema v%d, expected v%d; backup: %s", version, store.SchemaVersion, result.Archive)
		}
		result.Migrated = true
		return nil
	})
	if errors.Is(err, webapp.ErrAlreadyRunning) {
		return fmt.Errorf("stop this workspace with atm serve stop before running atm serve migrate: %w", err)
	}
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(result)
	}
	if !result.Migrated {
		fmt.Fprintf(command.OutOrStdout(), "ATM database already uses schema v%d; no migration was needed.\n", result.ToSchema)
		return nil
	}
	fmt.Fprintf(command.OutOrStdout(), "ATM database upgraded from v%d to v%d.\nBackup: %s\nStart the workspace again with atm serve --open.\n", result.FromSchema, result.ToSchema, result.Archive)
	return nil
}

// Reuse atm backup's archive, manifest and restore contract. The snapshot API
// reads the old database without migration, including committed WAL records.
func backupBeforeServeMigration(version int) (string, error) {
	backupDir := filepath.Join(config.AtmDir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(backupDir, 0o700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(backupDir, ".atm-migration-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	snapshot := filepath.Join(staging, "atm.db")
	if err := store.SnapshotOwnRecords(snapshot); err != nil {
		return "", err
	}
	unbacked, err := unbackedEntries()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	manifest := backupManifest{ATMVersion: rootCmd.Version, SchemaVersion: version, CreatedAt: now.Format(time.RFC3339), Database: "atm.db", EmptiedTables: store.RebuildableTables(), UnbackedEntries: unbacked}
	stagedArchive := filepath.Join(staging, "backup.tar.gz")
	if _, err := writeBackupArchive(stagedArchive, snapshot, &manifest); err != nil {
		return "", err
	}
	archive := filepath.Join(backupDir, fmt.Sprintf("atm-before-schema-v%d-to-v%d-%s.tar.gz", version, store.SchemaVersion, now.Format("20060102-150405.000000000")))
	// Linking publishes the fully written archive without overwriting any
	// existing backup. Both paths are deliberately on the same filesystem.
	if err := os.Link(stagedArchive, archive); err != nil {
		return "", err
	}
	return filepath.Abs(archive)
}

func openExistingWorkspace(ctx context.Context, dataDir string) error {
	url, err := webapp.OpenExisting(ctx, dataDir)
	if err != nil {
		return err
	}
	return launchWorkspaceBrowser(ctx, url)
}

func launchWorkspaceBrowser(ctx context.Context, url string) error {
	program := "open"
	if runtime.GOOS == "linux" {
		program = "xdg-open"
	}
	return exec.CommandContext(ctx, program, url).Run()
}
