package cmd

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/dashboard"
)

// loadDashboardLiveStatus adapts the existing OS/process discovery projection
// to the dashboard port. The service decides when this potentially expensive
// read runs and joins it with the durable snapshot. This is an explicit
// transition seam: process/transcript discovery still lives beside the legacy
// `session status` adapter and should move to its own read service separately;
// keeping it behind this port prevents that later move from changing Dashboard.
func loadDashboardLiveStatus(ctx context.Context, agent string) (dashboard.LiveStatus, error) {
	if err := ctx.Err(); err != nil {
		return dashboard.LiveStatus{}, err
	}
	view, err := buildStatusView(agent)
	if err != nil {
		return dashboard.LiveStatus{}, err
	}
	return dashboardLiveStatusFromView(view), nil
}

// dashboardLiveStatusFromView is deliberately pure so the transport mapping
// (including subagent lineage) can be regression-tested without discovering
// live processes or transcripts from the host.
func dashboardLiveStatusFromView(view statusView) dashboard.LiveStatus {
	result := dashboard.LiveStatus{
		GeneratedAt: view.GeneratedAt,
		Time:        view.Time,
		Sessions:    make([]dashboard.LiveSession, 0, len(view.Sessions)),
		Bindings:    make([]dashboard.BindingContext, 0, len(view.Bindings)),
	}
	for _, session := range view.Sessions {
		result.Sessions = append(result.Sessions, dashboard.LiveSession{
			Tool:            session.Tool,
			SessionID:       session.SessionID,
			ResumeID:        session.ResumeID,
			RootSessionID:   session.RootSessionID,
			ParentSessionID: session.ParentSessionID,
			AgentPath:       session.AgentPath,
			AgentNickname:   session.AgentNickname,
			SubagentDepth:   session.SubagentDepth,
			Project:         session.Project,
			Client:          session.Client,
			CWD:             session.CWD,
			Model:           session.Model,
			Summary:         session.Summary,
			AgeSeconds:      session.AgeSeconds,
			ActivityState:   session.ActivityState,
			BindingState:    session.BindingState,
			Binding:         session.Binding,
			Todo:            session.Todo,
			PID:             session.PID,
			TTY:             session.TTY,
			TerminalApp:     session.TerminalApp,
			FirstQ:          session.FirstQ,
			LastQ:           session.LastQ,
			LastA:           session.LastA,
			LatestResult:    session.LatestResult,
			Updates:         session.Updates,
			Tools:           session.Tools,
			Topics:          session.Topics,
		})
	}
	for _, binding := range view.Bindings {
		result.Bindings = append(result.Bindings, dashboard.BindingContext{
			State:             binding.State,
			Binding:           binding.Binding,
			Todo:              binding.Todo,
			Observed:          binding.Observed,
			ObservedSessionID: binding.ObservedSessionID,
		})
	}
	return result
}
