package cmd

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/zane-byte-dev/atm/internal/logging"
	"github.com/zane-byte-dev/atm/internal/store"
	"github.com/zane-byte-dev/atm/internal/textmodel"
)

// ATM 自己的内置文本模型调用（收集判定、任务整理、连接自检）在进程内先攒着，命令结束
// 时作为**一个会话**落进和各 Agent 完全相同的 usage / usage_events 里，agent 名是
// `store.BuiltinAgent`。这样「哪个 client 烧了多少 token」只有一套统计逻辑。
//
// 为什么不是「调用发生时立刻写库」：这些调用发生在两条毫不相干的路上——收集走
// collector 的 service，任务整理走文件态的 workapp，后者压根不开数据库。挂在 withDB
// 上只能盖住一半（第一版就是这么错的：refine 一行都没记上）。而且立刻写要新开一条
// 连接，可能撞上命令自己正持有的写事务。
//
// 攒的代价是进程被强杀会丢掉这一轮的账；每次调用另有一行日志兜底，那个是即时写的。
// CLI 一条命令一个进程、没有常驻循环，所以攒的窗口就是一条命令的时长。
var builtinModelCalls struct {
	sync.Mutex
	calls []store.BuiltinModelCall
}

// bufferBuiltinModelCalls installs the sink for this process. Called once from
// Execute, before any command runs.
func bufferBuiltinModelCalls() {
	textmodel.Sink = func(call textmodel.Call) {
		logBuiltinModelCall(call)

		startedAt := call.StartedAt
		if startedAt.IsZero() {
			startedAt = time.Now()
		}
		builtinModelCalls.Lock()
		defer builtinModelCalls.Unlock()
		builtinModelCalls.calls = append(builtinModelCalls.calls, store.BuiltinModelCall{
			Task:           call.Task,
			Model:          call.Model,
			InputTokens:    call.Usage.InputTokens,
			OutputTokens:   call.Usage.OutputTokens,
			CacheHitTokens: call.Usage.CacheHitTokens,
			DurationMS:     call.DurationMS,
			TS:             startedAt.Unix(),
			OK:             call.Err == "",
		})
	}
}

// logBuiltinModelCall records the call in the CLI log, whether or not it ends up
// in the usage tables.
//
// 日志和统计是两件事：统计只收报了 token 的调用，而一次超时、一次 429、一次连不上
// 恰恰是排查时最想看到的东西，它们在统计里是零、在日志里是一行。
func logBuiltinModelCall(call textmodel.Call) {
	fields := map[string]any{
		"task":        call.Task,
		"model":       call.Model,
		"duration_ms": call.DurationMS,
		"input":       call.Usage.InputTokens,
		"output":      call.Usage.OutputTokens,
		"cache_hit":   call.Usage.CacheHitTokens,
	}
	if call.Err == "" {
		logging.Lifecycle("builtin_model_call", fields)
		return
	}
	logging.Failure("builtin_model_call", failedCommandPath(), fmt.Errorf("%s", call.Err), fields)
}

// flushBuiltinModelCalls writes what this process spent, if anything.
//
// 一次调用都没有就一行不写、连库都不开：绝大多数命令根本不碰模型，不该为记账付一次
// 打开数据库的代价。写失败只记日志——这些调用已经发生过了，丢一笔账不值得让一次收集
// 或整理失败。
func flushBuiltinModelCalls() {
	builtinModelCalls.Lock()
	pending := builtinModelCalls.calls
	builtinModelCalls.calls = nil
	builtinModelCalls.Unlock()
	if len(pending) == 0 {
		return
	}

	db, err := store.Open()
	if err != nil {
		logging.Failure("builtin_model_usage_flush", "", err, map[string]any{
			"dropped": len(pending),
		})
		return
	}
	defer db.Close()
	if err := store.RecordBuiltinUsage(db, builtinUsageSessionID(pending), pending); err != nil {
		logging.Failure("builtin_model_usage_flush", "", err, map[string]any{
			"dropped": len(pending),
		})
	}
}

// builtinUsageSessionID names this process's session. 一条命令一个会话：这是 ATM 自己
// 「一次工作」的天然粒度，而 pid + 首次调用时间在一台机器上不会撞。
func builtinUsageSessionID(calls []store.BuiltinModelCall) string {
	started := time.Now().Unix()
	for _, call := range calls {
		if call.TS > 0 && call.TS < started {
			started = call.TS
		}
	}
	return fmt.Sprintf("atm-%d-%d", started, os.Getpid())
}
