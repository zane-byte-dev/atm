package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	webassets "github.com/zane-byte-dev/atm/app/web"
	"github.com/zane-byte-dev/atm/internal/apphost"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/launchservice"
	webapp "github.com/zane-byte-dev/atm/internal/web"
)

type workspaceServiceOptions = launchservice.Options
type workspaceServicePlan = launchservice.Plan

const workspaceServiceOwner = launchservice.Owner

var (
	canonicalServicePath      = launchservice.Canonical
	makeWorkspaceServicePlan  = launchservice.MakePlan
	renderWorkspaceService    = launchservice.Render
	ownedWorkspaceService     = launchservice.Owned
	installWorkspaceService   = launchservice.Install
	uninstallWorkspaceService = launchservice.Uninstall
	stopWorkspaceService      = launchservice.Stop
)

var serveServicePrint, serveServiceDryRun, serveUninstallDryRun bool

func init() {
	install := &cobra.Command{Use: "install", Short: "Install and start this complete CLI as a macOS login service", Args: cobra.NoArgs, RunE: runServeInstall,
		Long: "Install this running CLI as a user LaunchAgent, then start its Web workspace and background workers. The binary must include Web assets. Its absolute path is recorded without copying it. Use --print to review the plist before installation. Existing unrelated LaunchAgents are never replaced."}
	install.Flags().BoolVar(&serveServicePrint, "print", false, "print the LaunchAgent plist without writing files or running launchctl")
	install.Flags().BoolVar(&serveServiceDryRun, "dry-run", false, "same as --print")
	uninstall := &cobra.Command{Use: "uninstall", Short: "Stop and remove this workspace's login service, keeping all data", Args: cobra.NoArgs, RunE: runServeUninstall,
		Long: "Unload and remove only ATM's managed LaunchAgent for the selected data directory. Data, logs and the Go presence ownership marker remain. The old macOS App is not restarted or re-enabled."}
	uninstall.Flags().BoolVar(&serveUninstallDryRun, "dry-run", false, "show the service target without unloading or removing it")
	serveCmd.AddCommand(install, uninstall)
}

func defaultWorkspaceServiceOptions(dataDir string) (workspaceServiceOptions, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return workspaceServiceOptions{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return workspaceServiceOptions{}, err
	}
	return workspaceServiceOptions{Home: home, DataDir: dataDir, Executable: executable, Path: os.Getenv("PATH"), GOOS: runtime.GOOS, UID: os.Getuid(), Port: servePort,
		CheckAssets: func() error {
			assets, err := webassets.Assets()
			if err != nil {
				return err
			}
			index, err := fs.ReadFile(assets, "index.html")
			if err != nil || len(index) == 0 {
				return errors.New("this executable does not contain a complete Web workspace; build the full CLI first")
			}
			return nil
		},
		Run: func(ctx context.Context, args ...string) ([]byte, error) {
			bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			return exec.CommandContext(bounded, "/bin/launchctl", args...).CombinedOutput()
		},
		WorkspaceRunning: func(ctx context.Context, dataDir string) (bool, error) {
			status, err := webapp.ReadStatus(ctx, dataDir)
			return status.Running, err
		},
	}, nil
}

func runServeInstall(command *cobra.Command, _ []string) error {
	if err := apphost.ConfigureDataDir(serveDataDir); err != nil {
		return err
	}
	opts, err := defaultWorkspaceServiceOptions(config.AtmDir)
	if err != nil {
		return err
	}
	plan, err := makeWorkspaceServicePlan(opts, true)
	if err != nil {
		return err
	}
	plist, err := renderWorkspaceService(plan)
	if err != nil {
		return err
	}
	if serveServicePrint || serveServiceDryRun {
		_, err = command.OutOrStdout().Write(plist)
		return err
	}
	if err := installWorkspaceService(commandContext(command), opts, plan, plist); err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(plan)
	}
	fmt.Fprintf(command.OutOrStdout(), "ATM login service installed: %s\nWorkspace: http://127.0.0.1:%d\nLogs: %s\nOpen with atm serve --open.\n", plan.PlistPath, opts.Port, filepath.Dir(plan.Stdout))
	return nil
}

func runServeUninstall(command *cobra.Command, _ []string) error {
	if err := apphost.ConfigureDataDir(serveDataDir); err != nil {
		return err
	}
	opts, err := defaultWorkspaceServiceOptions(config.AtmDir)
	if err != nil {
		return err
	}
	plan, err := makeWorkspaceServicePlan(opts, false)
	if err != nil {
		return err
	}
	if serveUninstallDryRun {
		return json.NewEncoder(command.OutOrStdout()).Encode(plan)
	}
	if err := uninstallWorkspaceService(commandContext(command), opts, plan); err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(command.OutOrStdout()).Encode(map[string]any{"uninstalled": true, "label": plan.Label, "data_preserved": true})
	}
	fmt.Fprintf(command.OutOrStdout(), "ATM login service removed: %s\nData and Go presence ownership are preserved.\n", plan.Label)
	return nil
}

// stopManagedWorkspace unloads a verified launchd job before serve stop sends
// a control request. Keeping its plist enables it again at the next login.
func stopManagedWorkspace(ctx context.Context, dataDir string) (bool, error) {
	if runtime.GOOS != "darwin" {
		return false, nil
	}
	opts, err := defaultWorkspaceServiceOptions(dataDir)
	if err != nil {
		return false, err
	}
	plan, err := makeWorkspaceServicePlan(opts, false)
	if err != nil {
		return false, err
	}
	return stopWorkspaceService(ctx, opts, plan)
}
