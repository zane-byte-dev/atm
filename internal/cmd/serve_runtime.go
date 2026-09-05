package cmd

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/zane-byte-dev/atm/internal/apphost"
	"github.com/zane-byte-dev/atm/internal/background"
	"github.com/zane-byte-dev/atm/internal/presence"
	webapp "github.com/zane-byte-dev/atm/internal/web"
)

// Compose ownership while the HTTP instance lock is held. A socket owned by
// another ATM runtime fails startup before any scheduler is started or durable
// job is accepted.
func workspaceRuntime(parent context.Context, host *apphost.Host) func(webapp.Instance, func(...string)) (func(context.Context) error, error) {
	return func(info webapp.Instance, invalidate func(...string)) (func(context.Context) error, error) {
		ctx, cancel := context.WithCancel(parent)
		banners := make(chan presence.Notification, 32)
		receiver, err := presence.Start(presence.Options{DataDir: info.DataDir, InstanceID: info.InstanceID,
			OnChange: func() { invalidate("presence", "sessions") },
			Notify: func(notification presence.Notification) {
				if notification.Action == "post" {
					select {
					case banners <- notification:
					default:
					}
				}
			},
		})
		if err != nil {
			cancel()
			return nil, fmt.Errorf("claim Agent hooks (another ATM runtime still owns them): %w", err)
		}
		manager, err := background.New(background.Options{DataDir: info.DataDir, WithConfig: host.WithConfig, Refine: host.RefineOptions(), Schedule: true,
			OnChange: func(job background.Job) {
				invalidate("jobs")
				if job.Terminal() {
					publishCollectionRuntimeNotifications(receiver, job)
					// A completed runtime job may have added indexed or built-in model
					// usage. Do not let the menu companion keep its short scalar cache
					// after the Web domains have already been invalidated.
					host.InvalidateQuickUsage()
					switch job.Kind {
					case background.SessionSync:
						invalidate("sessions", "usage", "day")
					case background.CollectionRun, background.CollectionReprocess:
						invalidate("collection", "knowledge", "todos", "usage")
					case background.DayRebuild:
						invalidate("day", "usage")
					case background.QuotaRefresh:
						invalidate("usage")
					case background.TodoRefine:
						invalidate("todos", "usage")
					}
				}
			},
		})
		if err != nil {
			cancel()
			receiver.Close()
			return nil, err
		}
		if err := manager.Start(ctx); err != nil {
			cancel()
			manager.Close(context.Background())
			receiver.Close()
			return nil, err
		}
		host.AttachRuntime(manager, receiver)
		done := make(chan struct{})
		go func() {
			defer close(done)
			timer := time.NewTicker(8 * time.Second)
			defer timer.Stop()
			for {
				_ = host.RefreshConfig(ctx)
				_ = host.RefreshPresence(ctx)
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
				}
			}
		}()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case notification := <-banners:
					// Re-check at the process boundary so a banner queued just before
					// the Menu preference changed cannot escape through osascript.
					if receiver.ShouldDisplayFallback(notification) {
						displayRuntimeBanner(ctx, notification)
					}
				}
			}
		}()
		return func(shutdown context.Context) error {
			cancel()
			jobErr := manager.Close(shutdown)
			var scanErr error
			select {
			case <-done:
			case <-shutdown.Done():
				scanErr = shutdown.Err()
			}
			if jobErr != nil || scanErr != nil {
				// Preserve both owner locks until the failed serve command exits.
				return errors.Join(jobErr, scanErr)
			}
			host.AttachRuntime(nil, nil)
			return errors.Join(jobErr, scanErr, receiver.Close())
		}, nil
	}
}

// Fixed programs and an argument-only script prevent notification text from
// becoming executable code. The single bounded worker waits and reaps children.
func displayRuntimeBanner(parent context.Context, notification presence.Notification) {
	if skipLocalNotification() {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	var command *exec.Cmd
	const title, subtitle, body = "ATM", "有待处理事项", "打开 ATM 查看最新状态。"
	switch runtime.GOOS {
	case "darwin":
		command = exec.CommandContext(ctx, "/usr/bin/osascript", "-e", "on run argv\ndisplay notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv)\nend run", title, subtitle, body)
	case "linux":
		command = exec.CommandContext(ctx, "notify-send", "--", title, body)
	}
	if command != nil {
		_ = command.Run()
	}
}
