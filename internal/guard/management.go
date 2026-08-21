package guard

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
)

// Rule is the Guard-owned name for the durable rule representation. Keeping the
// alias here means adapters speak to Guard even while config owns the on-disk
// shape shared by the gate's hot path.
type Rule = config.GuardRule

// ShimInfrastructure is the narrow process/filesystem port for management use
// cases. The application service owns selection, policy, partial failure and
// config orchestration; this port only performs one local operation.
type ShimInfrastructure interface {
	Supported() bool
	ExecutablePath() (string, error)
	Resolve(tool, override string) (string, error)
	Status(tool, binPath string) (ShimState, error)
	Install(tool, binPath, atmPath string) (ShimState, error)
	Uninstall(tool, binPath string) (ShimState, error)
}

// ManagementRepository is the durable configuration port used by Guard's
// management plane. Implementations must make each individual mutation atomic;
// a physical shim installation and recording its path remain deliberately
// observable as two operations so a persistence failure can be reported rather
// than hidden.
type ManagementRepository interface {
	ToolNames() []string
	RuleViews(tool string) []RuleView
	SaveToolBin(tool, binPath string) error
	SaveRule(tool string, rule Rule) error
	RemoveRule(tool, ruleID string) error
	RemoveTool(tool string) error
}

// LocalShimInfrastructure performs management against this machine. It is kept
// separate from Cobra so the service can be tested without moving real binaries.
type LocalShimInfrastructure struct{}

func (LocalShimInfrastructure) Supported() bool { return runtime.GOOS != "windows" }

func (LocalShimInfrastructure) ExecutablePath() (string, error) {
	return config.ExecutablePath()
}

func (LocalShimInfrastructure) Resolve(tool, override string) (string, error) {
	return Resolve(tool, override)
}

func (LocalShimInfrastructure) Status(tool, binPath string) (ShimState, error) {
	return Status(tool, binPath)
}

func (LocalShimInfrastructure) Install(tool, binPath, atmPath string) (ShimState, error) {
	return Install(tool, binPath, atmPath)
}

func (LocalShimInfrastructure) Uninstall(tool, binPath string) (ShimState, error) {
	return Uninstall(tool, binPath)
}

// LocalManagementRepository preserves config.json fields it does not own by
// delegating to config's atomic mutation helpers.
type LocalManagementRepository struct{}

func (LocalManagementRepository) ToolNames() []string { return ToolNames() }
func (LocalManagementRepository) RuleViews(tool string) []RuleView {
	return RuleViews(tool)
}
func (LocalManagementRepository) SaveToolBin(tool, binPath string) error {
	return config.SaveGuardToolBin(tool, binPath)
}
func (LocalManagementRepository) SaveRule(tool string, rule Rule) error {
	return config.SaveGuardRule(tool, rule)
}
func (LocalManagementRepository) RemoveRule(tool, ruleID string) error {
	return config.RemoveGuardRule(tool, ruleID)
}
func (LocalManagementRepository) RemoveTool(tool string) error {
	return config.RemoveGuardTool(tool)
}

type ShimInput struct {
	Tools []string `json:"tools,omitempty"`
	Bin   string   `json:"bin,omitempty"`
}

type ShimResult struct {
	States []ShimState `json:"states"`
}

type ListRulesInput struct {
	// Nil means every registered tool. A pointer distinguishes that from an
	// explicitly supplied empty tool name, which is invalid.
	Tool *string `json:"tool,omitempty"`
}

type ListRulesResult struct {
	Rules []RuleView `json:"rules"`
}

type SetRuleInput struct {
	Tool string `json:"tool"`
	Rule Rule   `json:"rule"`
}

type RemoveRuleInput struct {
	Tool   string `json:"tool"`
	RuleID string `json:"rule_id"`
}

type ForgetToolInput struct {
	Tool string `json:"tool"`
}

type ForgetToolResult struct {
	Tool      string `json:"tool"`
	Forgotten bool   `json:"forgotten"`
}

func (service Service) StatusTools(ctx context.Context, call application.Call, input ShimInput) (ShimResult, error) {
	if err := validateGuardCall(ctx, call); err != nil {
		return ShimResult{}, err
	}
	return service.manageShims(ctx, input, shimStatus)
}

func (service Service) InstallTools(ctx context.Context, call application.Call, input ShimInput) (ShimResult, error) {
	if err := requireGuardManagementHuman(ctx, call); err != nil {
		return ShimResult{}, err
	}
	return service.manageShims(ctx, input, shimInstall)
}

func (service Service) UninstallTools(ctx context.Context, call application.Call, input ShimInput) (ShimResult, error) {
	if err := requireGuardManagementHuman(ctx, call); err != nil {
		return ShimResult{}, err
	}
	return service.manageShims(ctx, input, shimUninstall)
}

type shimOperation string

const (
	shimStatus    shimOperation = "status"
	shimInstall   shimOperation = "install"
	shimUninstall shimOperation = "uninstall"
)

func (service Service) manageShims(ctx context.Context, input ShimInput, operation shimOperation) (ShimResult, error) {
	if !service.shims.Supported() {
		err := application.NewError(application.CodeUnavailable, "the outbound action gate is not supported on this platform")
		err.Details = map[string]any{"operation": operation}
		return ShimResult{}, err
	}

	tools, err := service.managementTools(input.Tools)
	if err != nil {
		return ShimResult{}, err
	}
	if input.Bin != "" && len(tools) != 1 {
		return ShimResult{}, invalidGuardArgument("--bin applies to one tool at a time", "bin", input.Bin)
	}

	atmPath := ""
	if operation == shimInstall {
		atmPath, err = service.shims.ExecutablePath()
		if err != nil {
			return ShimResult{}, unavailableGuard("resolve ATM executable", err)
		}
	}

	result := ShimResult{States: []ShimState{}}
	failures := []string{}
	for _, tool := range tools {
		if err := guardContextError(ctx); err != nil {
			return result, err
		}
		binPath, resolveErr := service.shims.Resolve(tool, input.Bin)
		if resolveErr != nil {
			if operation == shimStatus {
				result.States = append(result.States, ShimState{Tool: tool, Rules: service.activeRuleCount(tool)})
				continue
			}
			failures = append(failures, fmt.Sprintf("%s: %v", tool, resolveErr))
			continue
		}

		var state ShimState
		switch operation {
		case shimInstall:
			state, err = service.shims.Install(tool, binPath, atmPath)
		case shimUninstall:
			state, err = service.shims.Uninstall(tool, binPath)
		default:
			state, err = service.shims.Status(tool, binPath)
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", tool, err))
			continue
		}
		if operation == shimInstall && state.Installed {
			if saveErr := service.config.SaveToolBin(tool, binPath); saveErr != nil {
				failures = append(failures, fmt.Sprintf(
					"%s: gate installed, but its installation path could not be recorded: %v", tool, saveErr))
			}
		}
		result.States = append(result.States, state)
	}
	if len(failures) > 0 {
		appErr := application.NewError(application.CodeUnavailable, strings.Join(failures, "; "))
		appErr.Retryable = true
		appErr.Details = map[string]any{"operation": operation, "failures": failures}
		return result, appErr
	}
	return result, nil
}

func (service Service) managementTools(explicit []string) ([]string, error) {
	if len(explicit) > 0 {
		tools := make([]string, 0, len(explicit))
		for index, candidate := range explicit {
			tool := strings.TrimSpace(candidate)
			if tool == "" {
				return nil, invalidGuardArgument("tool name is required", fmt.Sprintf("tools[%d]", index), candidate)
			}
			tools = append(tools, tool)
		}
		return tools, nil
	}

	tools := []string{}
	for _, tool := range service.config.ToolNames() {
		if len(service.config.RuleViews(tool)) > 0 {
			tools = append(tools, tool)
		}
	}
	sort.Strings(tools)
	if len(tools) == 0 {
		return nil, invalidGuardArgument("no tools have guard rules", "tools", explicit)
	}
	return tools, nil
}

func (service Service) ListRules(ctx context.Context, call application.Call, input ListRulesInput) (ListRulesResult, error) {
	if err := validateGuardCall(ctx, call); err != nil {
		return ListRulesResult{}, err
	}
	tools := service.config.ToolNames()
	if input.Tool != nil {
		tool, err := validateManagementTool(*input.Tool)
		if err != nil {
			return ListRulesResult{}, err
		}
		tools = []string{tool}
	} else {
		sort.Strings(tools)
	}
	result := ListRulesResult{Rules: []RuleView{}}
	for _, tool := range tools {
		result.Rules = append(result.Rules, service.config.RuleViews(tool)...)
	}
	return result, nil
}

func (service Service) SetRule(ctx context.Context, call application.Call, input SetRuleInput) (ListRulesResult, error) {
	if err := requireGuardManagementHuman(ctx, call); err != nil {
		return ListRulesResult{}, err
	}
	tool, err := validateManagementTool(input.Tool)
	if err != nil {
		return ListRulesResult{}, err
	}
	input.Rule.ID = strings.TrimSpace(input.Rule.ID)
	if input.Rule.ID == "" {
		return ListRulesResult{}, invalidGuardArgument("rule needs an id", "rule.id", input.Rule.ID)
	}
	for index, token := range input.Rule.Path {
		if strings.TrimSpace(token) == "" {
			return ListRulesResult{}, invalidGuardArgument("rule path tokens cannot be empty", fmt.Sprintf("rule.path[%d]", index), token)
		}
	}
	if pattern := strings.TrimSpace(input.Rule.ArgvPattern); pattern != "" {
		if _, compileErr := regexp.Compile(pattern); compileErr != nil {
			return ListRulesResult{}, invalidGuardArgument(
				fmt.Sprintf("rule %q has an invalid argv_pattern: %v", input.Rule.ID, compileErr),
				"rule.argv_pattern", input.Rule.ArgvPattern)
		}
	}
	if !input.Rule.HasMatcher() && !service.isBuiltinRule(tool, input.Rule.ID) {
		return ListRulesResult{}, invalidGuardArgument(fmt.Sprintf(
			"rule %q has no path or argv_pattern, and there is no built-in %q to patch",
			input.Rule.ID, input.Rule.ID), "rule", input.Rule.ID)
	}
	if err := service.config.SaveRule(tool, input.Rule); err != nil {
		return ListRulesResult{}, guardManagementInfrastructureError("save Guard rule", err)
	}
	return ListRulesResult{Rules: service.config.RuleViews(tool)}, nil
}

func (service Service) RemoveRule(ctx context.Context, call application.Call, input RemoveRuleInput) (ListRulesResult, error) {
	if err := requireGuardManagementHuman(ctx, call); err != nil {
		return ListRulesResult{}, err
	}
	tool, err := validateManagementTool(input.Tool)
	if err != nil {
		return ListRulesResult{}, err
	}
	ruleID := strings.TrimSpace(input.RuleID)
	if ruleID == "" {
		return ListRulesResult{}, invalidGuardArgument("rule id is required", "rule_id", input.RuleID)
	}
	for _, view := range service.config.RuleViews(tool) {
		if view.ID == ruleID && view.Builtin && !view.Overridden {
			appErr := application.NewError(application.CodeConflict, fmt.Sprintf(
				"%s/%s is built in and cannot be removed; switch it off instead: "+
					`echo '{"id":%q,"enabled":false}' | atm guard rule set %s`,
				tool, ruleID, ruleID, tool))
			appErr.Details = map[string]any{"tool": tool, "rule_id": ruleID, "builtin": true}
			return ListRulesResult{}, appErr
		}
	}
	if err := service.config.RemoveRule(tool, ruleID); err != nil {
		return ListRulesResult{}, guardManagementInfrastructureError("remove Guard rule", err)
	}
	return ListRulesResult{Rules: service.config.RuleViews(tool)}, nil
}

func (service Service) ForgetTool(ctx context.Context, call application.Call, input ForgetToolInput) (ForgetToolResult, error) {
	if err := requireGuardManagementHuman(ctx, call); err != nil {
		return ForgetToolResult{}, err
	}
	tool, err := validateManagementTool(input.Tool)
	if err != nil {
		return ForgetToolResult{}, err
	}
	if binPath, resolveErr := service.shims.Resolve(tool, ""); resolveErr == nil {
		if state, statusErr := service.shims.Status(tool, binPath); statusErr == nil && state.Installed {
			appErr := application.NewError(application.CodeConflict, fmt.Sprintf(
				"%s still has a shim at %s; run `atm guard uninstall %s` first",
				tool, state.BinPath, tool))
			appErr.Details = map[string]any{"tool": tool, "bin_path": state.BinPath, "installed": true}
			return ForgetToolResult{}, appErr
		}
	}
	if err := service.config.RemoveTool(tool); err != nil {
		return ForgetToolResult{}, guardManagementInfrastructureError("forget Guard tool", err)
	}
	return ForgetToolResult{Tool: tool, Forgotten: true}, nil
}

func (service Service) activeRuleCount(tool string) int {
	count := 0
	for _, view := range service.config.RuleViews(tool) {
		if view.Enabled {
			count++
		}
	}
	return count
}

func (service Service) isBuiltinRule(tool, ruleID string) bool {
	for _, view := range service.config.RuleViews(tool) {
		if view.ID == ruleID && view.Builtin {
			return true
		}
	}
	return false
}

func validateManagementTool(value string) (string, error) {
	tool := strings.TrimSpace(value)
	if tool == "" {
		return "", invalidGuardArgument("tool name is required", "tool", value)
	}
	return tool, nil
}

func requireGuardManagementHuman(ctx context.Context, call application.Call) error {
	if err := validateGuardCall(ctx, call); err != nil {
		return err
	}
	// This policy blocks accidental management by an identified Agent. The CLI's
	// actor comes from ambient environment and can be cleared or forged, so it is
	// explicitly not adversarial human-presence authentication. Do not reuse this
	// boundary for replayable Guard decisions or remote access.
	if call.Actor.Kind != application.ActorHuman || call.Actor.Origin != application.OriginCLI {
		err := application.NewError(application.CodeForbidden,
			"only a human at the Guard CLI edge may change Guard installation or rules")
		err.Details = map[string]any{"actor_kind": call.Actor.Kind, "origin": call.Actor.Origin}
		return err
	}
	return nil
}

func guardManagementInfrastructureError(action string, cause error) error {
	var appErr *application.Error
	if errors.As(cause, &appErr) {
		return appErr
	}
	return unavailableGuard(action, cause)
}
