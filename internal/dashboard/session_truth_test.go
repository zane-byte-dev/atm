package dashboard

import (
	"testing"
	"time"

	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

func TestRangeSnapshotCarriesSessionTruthFields(t *testing.T) {
	parts := rangeParts{
		startTime: time.Unix(100, 0),
		endTime:   time.Unix(200, 0),
		sessions: []store.ListResult{{
			FullID: "rollout-child", ShortID: "child", Agent: "codex", Project: "atm",
			CreatedTS: 110, LastTS: 120, QCount: 0, ResumeID: "child-thread",
			RootSessionID: "root-thread", ParentSessionID: "parent-thread",
			AgentPath: "/root/child", AgentNickname: "Ada", SubagentDepth: 1,
			IsSubagent: true, ParserVersion: parser.CurrentSessionParserVersion,
			ContentState: parser.ContentStateAvailable, ResultStatus: parser.SessionResultCompleted,
			LatestProgress: "testing", FinalResult: "done",
		}},
	}
	got := parts.build()
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions = %#v", got.Sessions)
	}
	session := got.Sessions[0]
	if session.LocalUserTurnCount != 0 || session.ResumeID != "child-thread" ||
		session.RootSessionID != "root-thread" || session.ParentSessionID != "parent-thread" ||
		session.AgentPath != "/root/child" || session.AgentNickname != "Ada" ||
		!session.IsSubagent || session.ContentState != parser.ContentStateAvailable ||
		session.ResultStatus != parser.SessionResultCompleted || session.FinalResult != "done" {
		t.Fatalf("session = %#v", session)
	}
}
