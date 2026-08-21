package cmd

import (
	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/appipc"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/dashboard"
	doctorapp "github.com/zane-byte-dev/atm/internal/doctor"
	"github.com/zane-byte-dev/atm/internal/guard"
	"github.com/zane-byte-dev/atm/internal/knowledge"
	quotaapp "github.com/zane-byte-dev/atm/internal/quota"
	sessionapp "github.com/zane-byte-dev/atm/internal/session"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

// `_ipc` is a hidden Cobra transport for the desktop bridge. Method ownership,
// typed JSON decoding and domain dispatch live in appipc; cmd only supplies the
// executable-specific ports and forwards stdin/stdout.
func init() {
	rootCmd.AddCommand(ipcCmd)
}

var ipcServer = newAppIPCServer()

// refreshAppIPCServer composes config-backed dependencies after main has loaded
// ~/.atm/config.json. Package variables are initialized before main runs, so
// keeping the first composition would permanently capture an empty connector
// registry for desktop requests even though ordinary CLI commands see it.
func refreshAppIPCServer() {
	ipcServer = newAppIPCServer()
}

func newAppIPCServer() *appipc.Server {
	return appipc.New(appipc.Dependencies{
		Config:    config.Default,
		AIDay:     aiday.Default,
		Dashboard: dashboard.NewService(loadDashboardLiveStatus),
		Doctor:    doctorapp.Default,
		Guard:     guard.Default,
		Quota:     quotaapp.Default,
		// Read at composition time, which refreshAppIPCServer redoes after main
		// loads ~/.atm/config.json.
		QuotaLiveBilling: config.GrokLiveQuota,
		Knowledge:        knowledge.NewService(knowledge.ServiceOptions{DataDir: config.AtmDir}),
		Session:          sessionapp.NewService(sessionapp.ServiceOptions{Location: config.Loc}),
		Work:             workapp.Default,
		WorkEffects:      localWorkEffectExecutor{},
		Collector:        defaultCollectorService(),
	})
}

var ipcCmd = &cobra.Command{
	Use:    "_ipc <verb>",
	Short:  "Answer one desktop app request (not a human interface)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runIPC,
}

func runIPC(cmd *cobra.Command, args []string) error {
	if err := ipcServer.Serve(cmd.Context(), args[0], cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
		// The structured envelope is already on stdout. Preserve the non-zero
		// status and wrapped cause without logging or printing a second copy.
		return exitError{code: 1, err: err}
	}
	return nil
}
