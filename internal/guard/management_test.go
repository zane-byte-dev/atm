package guard

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/zane-byte-dev/atm/internal/application"
)

type fakeShimInfrastructure struct {
	supported     bool
	executable    string
	executableErr error
	resolved      map[string]string
	resolveErr    map[string]error
	states        map[string]ShimState
	statusErr     map[string]error
	installErr    map[string]error
	uninstallErr  map[string]error
	calls         []string
}

func (fake *fakeShimInfrastructure) Supported() bool { return fake.supported }

func (fake *fakeShimInfrastructure) ExecutablePath() (string, error) {
	fake.calls = append(fake.calls, "executable")
	return fake.executable, fake.executableErr
}

func (fake *fakeShimInfrastructure) Resolve(tool, override string) (string, error) {
	fake.calls = append(fake.calls, "resolve:"+tool+":"+override)
	if err := fake.resolveErr[tool]; err != nil {
		return "", err
	}
	if override != "" {
		return override, nil
	}
	if path := fake.resolved[tool]; path != "" {
		return path, nil
	}
	return "/bin/" + tool, nil
}

func (fake *fakeShimInfrastructure) Status(tool, binPath string) (ShimState, error) {
	fake.calls = append(fake.calls, "status:"+tool+":"+binPath)
	state := fake.states[tool]
	if state.Tool == "" {
		state = ShimState{Tool: tool, BinPath: binPath, BinExists: true}
	}
	return state, fake.statusErr[tool]
}

func (fake *fakeShimInfrastructure) Install(tool, binPath, atmPath string) (ShimState, error) {
	fake.calls = append(fake.calls, "install:"+tool+":"+binPath+":"+atmPath)
	state := fake.states[tool]
	if state.Tool == "" {
		state = ShimState{Tool: tool, BinPath: binPath, Installed: true, BinExists: true}
	}
	return state, fake.installErr[tool]
}

func (fake *fakeShimInfrastructure) Uninstall(tool, binPath string) (ShimState, error) {
	fake.calls = append(fake.calls, "uninstall:"+tool+":"+binPath)
	state := fake.states[tool]
	if state.Tool == "" {
		state = ShimState{Tool: tool, BinPath: binPath, BinExists: true}
	}
	return state, fake.uninstallErr[tool]
}

type fakeManagementRepository struct {
	tools         []string
	views         map[string][]RuleView
	savedBins     map[string]string
	savedRules    []SetRuleInput
	removedRules  []RemoveRuleInput
	removedTools  []string
	saveBinErr    error
	saveRuleErr   error
	removeRuleErr error
	removeToolErr error
}

func (fake *fakeManagementRepository) ToolNames() []string {
	return append([]string{}, fake.tools...)
}

func (fake *fakeManagementRepository) RuleViews(tool string) []RuleView {
	return append([]RuleView{}, fake.views[tool]...)
}

func (fake *fakeManagementRepository) SaveToolBin(tool, binPath string) error {
	if fake.saveBinErr != nil {
		return fake.saveBinErr
	}
	if fake.savedBins == nil {
		fake.savedBins = map[string]string{}
	}
	fake.savedBins[tool] = binPath
	return nil
}

func (fake *fakeManagementRepository) SaveRule(tool string, rule Rule) error {
	if fake.saveRuleErr != nil {
		return fake.saveRuleErr
	}
	fake.savedRules = append(fake.savedRules, SetRuleInput{Tool: tool, Rule: rule})
	return nil
}

func (fake *fakeManagementRepository) RemoveRule(tool, ruleID string) error {
	if fake.removeRuleErr != nil {
		return fake.removeRuleErr
	}
	fake.removedRules = append(fake.removedRules, RemoveRuleInput{Tool: tool, RuleID: ruleID})
	return nil
}

func (fake *fakeManagementRepository) RemoveTool(tool string) error {
	if fake.removeToolErr != nil {
		return fake.removeToolErr
	}
	fake.removedTools = append(fake.removedTools, tool)
	return nil
}

func managementService(shims *fakeShimInfrastructure, repository *fakeManagementRepository) Service {
	if shims == nil {
		shims = &fakeShimInfrastructure{supported: true}
	}
	if repository == nil {
		repository = &fakeManagementRepository{views: map[string][]RuleView{}}
	}
	if shims.resolved == nil {
		shims.resolved = map[string]string{}
	}
	if shims.resolveErr == nil {
		shims.resolveErr = map[string]error{}
	}
	if shims.states == nil {
		shims.states = map[string]ShimState{}
	}
	if shims.statusErr == nil {
		shims.statusErr = map[string]error{}
	}
	if shims.installErr == nil {
		shims.installErr = map[string]error{}
	}
	if shims.uninstallErr == nil {
		shims.uninstallErr = map[string]error{}
	}
	if repository.views == nil {
		repository.views = map[string][]RuleView{}
	}
	return NewService(ServiceOptions{Shims: shims, Config: repository})
}

func TestManagementMutationsRequireHumanCLIAndDoNotReachPorts(t *testing.T) {
	mutations := []struct {
		name string
		run  func(Service, application.Call) error
	}{
		{"install", func(service Service, call application.Call) error {
			_, err := service.InstallTools(context.Background(), call, ShimInput{Tools: []string{"dws"}})
			return err
		}},
		{"uninstall", func(service Service, call application.Call) error {
			_, err := service.UninstallTools(context.Background(), call, ShimInput{Tools: []string{"dws"}})
			return err
		}},
		{"set rule", func(service Service, call application.Call) error {
			_, err := service.SetRule(context.Background(), call, SetRuleInput{
				Tool: "dws", Rule: Rule{ID: "send", Path: []string{"send"}},
			})
			return err
		}},
		{"remove rule", func(service Service, call application.Call) error {
			_, err := service.RemoveRule(context.Background(), call, RemoveRuleInput{Tool: "dws", RuleID: "send"})
			return err
		}},
		{"forget", func(service Service, call application.Call) error {
			_, err := service.ForgetTool(context.Background(), call, ForgetToolInput{Tool: "dws"})
			return err
		}},
	}

	for _, actor := range []application.Call{
		guardServiceCall(application.ActorAgent),
		{
			RequestID: "human-web",
			Actor:     application.Actor{Kind: application.ActorHuman, Origin: application.OriginWeb},
		},
	} {
		for _, mutation := range mutations {
			t.Run(fmt.Sprintf("%s/%s-%s", mutation.name, actor.Actor.Kind, actor.Actor.Origin), func(t *testing.T) {
				shims := &fakeShimInfrastructure{supported: true}
				repository := &fakeManagementRepository{views: map[string][]RuleView{}}
				service := managementService(shims, repository)
				if err := mutation.run(service, actor); !errors.Is(err, application.ErrForbidden) {
					t.Fatalf("error = %v, want forbidden", err)
				}
				if len(shims.calls) != 0 || len(repository.savedBins) != 0 || len(repository.savedRules) != 0 ||
					len(repository.removedRules) != 0 || len(repository.removedTools) != 0 {
					t.Fatalf("unauthorized mutation reached ports: shims=%v repository=%#v", shims.calls, repository)
				}
			})
		}
	}
}

func TestManagementStatusRemainsReadableByAgent(t *testing.T) {
	shims := &fakeShimInfrastructure{
		supported: true,
		states: map[string]ShimState{
			"dws": {Tool: "dws", BinPath: "/bin/dws", Installed: true, Rules: 1},
		},
	}
	repository := &fakeManagementRepository{
		tools: []string{"dws"},
		views: map[string][]RuleView{"dws": {{Tool: "dws", ID: "send", Enabled: true}}},
	}
	service := managementService(shims, repository)
	result, err := service.StatusTools(context.Background(), guardServiceCall(application.ActorAgent), ShimInput{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(result.States) != 1 || !result.States[0].Installed {
		t.Fatalf("states = %#v", result.States)
	}
}

func TestInstallToolsOwnsDefaultSelectionPartialFailureAndPathPersistence(t *testing.T) {
	shims := &fakeShimInfrastructure{
		supported:  true,
		executable: "/usr/local/bin/atm",
		resolveErr: map[string]error{"zeta": errors.New("not on PATH")},
		states: map[string]ShimState{
			"alpha": {Tool: "alpha", BinPath: "/bin/alpha", Installed: true, Rules: 1},
		},
	}
	repository := &fakeManagementRepository{
		tools: []string{"zeta", "empty", "alpha"},
		views: map[string][]RuleView{
			"alpha": {{Tool: "alpha", ID: "send", Enabled: true}},
			"zeta":  {{Tool: "zeta", ID: "push", Enabled: true}},
		},
	}
	service := managementService(shims, repository)
	result, err := service.InstallTools(
		context.Background(), guardServiceCall(application.ActorHuman), ShimInput{},
	)
	if !errors.Is(err, application.ErrUnavailable) {
		t.Fatalf("error = %v, want unavailable partial failure", err)
	}
	if len(result.States) != 1 || result.States[0].Tool != "alpha" {
		t.Fatalf("states = %#v, want successful alpha only", result.States)
	}
	if got := repository.savedBins["alpha"]; got != "/bin/alpha" {
		t.Fatalf("saved bin = %q", got)
	}
	wantCalls := []string{
		"executable",
		"resolve:alpha:",
		"install:alpha:/bin/alpha:/usr/local/bin/atm",
		"resolve:zeta:",
	}
	if !reflect.DeepEqual(shims.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", shims.calls, wantCalls)
	}
}

func TestStatusToolsReportsUnresolvedToolAndCountsOnlyEnabledRules(t *testing.T) {
	shims := &fakeShimInfrastructure{
		supported:  true,
		resolveErr: map[string]error{"dws": errors.New("not installed")},
	}
	repository := &fakeManagementRepository{
		views: map[string][]RuleView{"dws": {
			{Tool: "dws", ID: "one", Enabled: true},
			{Tool: "dws", ID: "two", Enabled: false},
		}},
	}
	service := managementService(shims, repository)
	result, err := service.StatusTools(
		context.Background(), guardServiceCall(application.ActorHuman), ShimInput{Tools: []string{"dws"}},
	)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(result.States) != 1 || result.States[0].Tool != "dws" || result.States[0].Rules != 1 || result.States[0].BinPath != "" {
		t.Fatalf("state = %#v", result.States)
	}
}

func TestShimInputValidationHappensBeforeExternalOperations(t *testing.T) {
	shims := &fakeShimInfrastructure{supported: true}
	repository := &fakeManagementRepository{
		tools: []string{"a", "b"},
		views: map[string][]RuleView{
			"a": {{Tool: "a", ID: "one", Enabled: true}},
			"b": {{Tool: "b", ID: "two", Enabled: true}},
		},
	}
	service := managementService(shims, repository)
	_, err := service.InstallTools(context.Background(), guardServiceCall(application.ActorHuman), ShimInput{Bin: "/custom/tool"})
	if !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("multi-tool --bin error = %v, want invalid_argument", err)
	}
	if len(shims.calls) != 0 {
		t.Fatalf("validation reached shim port: %v", shims.calls)
	}
}

func TestRuleManagementValidatesMatchersAndBuiltinSemantics(t *testing.T) {
	repository := &fakeManagementRepository{
		tools: []string{"dws"},
		views: map[string][]RuleView{"dws": {
			{Tool: "dws", ID: "builtin-send", Enabled: true, Builtin: true},
		}},
	}
	service := managementService(nil, repository)
	call := guardServiceCall(application.ActorHuman)

	if _, err := service.SetRule(context.Background(), call, SetRuleInput{
		Tool: "dws", Rule: Rule{ID: "builtin-send", Enabled: boolPointer(false)},
	}); err != nil {
		t.Fatalf("patch builtin: %v", err)
	}
	if len(repository.savedRules) != 1 || repository.savedRules[0].Rule.ID != "builtin-send" {
		t.Fatalf("saved rules = %#v", repository.savedRules)
	}

	if _, err := service.SetRule(context.Background(), call, SetRuleInput{
		Tool: "dws", Rule: Rule{ID: "unknown-patch", Enabled: boolPointer(false)},
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("matcherless custom rule error = %v, want invalid_argument", err)
	}
	if _, err := service.SetRule(context.Background(), call, SetRuleInput{
		Tool: "dws", Rule: Rule{ID: "broken", ArgvPattern: "^("},
	}); !errors.Is(err, application.ErrInvalidArgument) {
		t.Fatalf("invalid regexp error = %v, want invalid_argument", err)
	}
	if len(repository.savedRules) != 1 {
		t.Fatalf("invalid rules reached repository: %#v", repository.savedRules)
	}

	if _, err := service.RemoveRule(context.Background(), call, RemoveRuleInput{
		Tool: "dws", RuleID: "builtin-send",
	}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("remove builtin error = %v, want conflict", err)
	}
	if len(repository.removedRules) != 0 {
		t.Fatalf("builtin removal reached repository: %#v", repository.removedRules)
	}
}

func TestForgetToolRefusesInstalledShimAndOtherwiseRemovesConfig(t *testing.T) {
	repository := &fakeManagementRepository{views: map[string][]RuleView{}}
	shims := &fakeShimInfrastructure{
		supported: true,
		states: map[string]ShimState{
			"dws": {Tool: "dws", BinPath: "/bin/dws", Installed: true},
		},
	}
	service := managementService(shims, repository)
	call := guardServiceCall(application.ActorHuman)
	if _, err := service.ForgetTool(context.Background(), call, ForgetToolInput{Tool: "dws"}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("forget installed error = %v, want conflict", err)
	}
	if len(repository.removedTools) != 0 {
		t.Fatalf("installed tool was forgotten: %v", repository.removedTools)
	}

	shims.states["dws"] = ShimState{Tool: "dws", BinPath: "/bin/dws", Installed: false}
	result, err := service.ForgetTool(context.Background(), call, ForgetToolInput{Tool: " dws "})
	if err != nil {
		t.Fatalf("forget uninstalled: %v", err)
	}
	if !result.Forgotten || result.Tool != "dws" || !reflect.DeepEqual(repository.removedTools, []string{"dws"}) {
		t.Fatalf("result = %#v removed = %#v", result, repository.removedTools)
	}
}

func boolPointer(value bool) *bool { return &value }
