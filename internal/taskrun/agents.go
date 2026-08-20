package taskrun

import (
	"context"

	"github.com/zane-byte-dev/atm/internal/application"
)

type AgentInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Binary    string `json:"binary"`
	Available bool   `json:"available"`
	CostNote  string `json:"cost_note"`
}

const DispatchAgentID = "codex"

var dispatchAgent = AgentInfo{
	ID: DispatchAgentID, Name: "Codex", Binary: "codex",
	CostNote: "使用当前 Codex 套餐或 API 配额；每次执行和继续修改都会产生新的模型用量。",
}

func DispatchAgent() AgentInfo {
	return dispatchAgent
}

type AgentsInput struct{}

type AgentsResult struct {
	Agents []AgentInfo `json:"agents"`
}

func (service Service) Agents(ctx context.Context, call application.Call, _ AgentsInput) (AgentsResult, error) {
	if err := validateCall(ctx, call); err != nil {
		return AgentsResult{}, err
	}
	agent := dispatchAgent
	_, err := service.process.LookPath(agent.Binary)
	agent.Available = err == nil
	return AgentsResult{Agents: []AgentInfo{agent}}, nil
}
