package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/store"
)

const (
	ConnectorProtocolVersion = 1
	connectorOutputLimit     = 16 << 20
	connectorErrorLimit      = 64 << 10
)

type cappedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *cappedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = true
		return written, nil
	}
	if len(data) > remaining {
		buffer.exceeded = true
		data = data[:remaining]
	}
	_, _ = buffer.buffer.Write(data)
	return written, nil
}

// CommandConnector runs an external connector executable. The operation is
// appended to Args and the request is sent as one JSON object on stdin. The
// response is one JSON object on stdout. No shell is involved.
type CommandConnector struct {
	ConnectorID string
	Command     string
	Args        []string
	Timeout     time.Duration
}

type commandConnectorRequest struct {
	Version   int                    `json:"version"`
	Operation string                 `json:"operation"`
	Source    store.CollectionSource `json:"source,omitempty"`
	Since     int64                  `json:"since,omitempty"`
	Kind      string                 `json:"kind,omitempty"`
	Keyword   string                 `json:"keyword,omitempty"`
	Limit     int                    `json:"limit,omitempty"`
}

type commandConnectorResponse struct {
	Messages   []Message   `json:"messages"`
	Cursor     int64       `json:"cursor,omitempty"`
	Candidates []Candidate `json:"candidates"`
	Error      string      `json:"error,omitempty"`
}

func (connector CommandConnector) ID() string { return connector.ConnectorID }

func (connector CommandConnector) Fetch(ctx context.Context, source store.CollectionSource,
	since int64) ([]Message, int64, error) {
	response, err := connector.call(ctx, commandConnectorRequest{
		Version: ConnectorProtocolVersion, Operation: "fetch", Source: source, Since: since,
	})
	if err != nil {
		return nil, since, err
	}
	if err := validateConnectorMessages(connector.ID(), "fetch", response.Messages); err != nil {
		return nil, since, err
	}
	cursor := response.Cursor
	for _, message := range response.Messages {
		if message.CreatedAt > cursor {
			cursor = message.CreatedAt
		}
	}
	return response.Messages, cursor, nil
}

func (connector CommandConnector) History(ctx context.Context, source store.CollectionSource,
	options HistoryOptions) ([]Message, error) {
	since := int64(0)
	if !options.Since.IsZero() {
		since = options.Since.Unix()
	}
	response, err := connector.call(ctx, commandConnectorRequest{
		Version: ConnectorProtocolVersion, Operation: "history", Source: source,
		Since: since, Limit: options.Limit,
	})
	if err == nil {
		err = validateConnectorMessages(connector.ID(), "history", response.Messages)
	}
	return response.Messages, err
}

func (connector CommandConnector) Search(ctx context.Context, kind, keyword string,
	limit int) ([]Candidate, error) {
	response, err := connector.call(ctx, commandConnectorRequest{
		Version: ConnectorProtocolVersion, Operation: "search",
		Kind: kind, Keyword: keyword, Limit: limit,
	})
	if err == nil {
		for index, candidate := range response.Candidates {
			if !collectionConnectorToken(candidate.Kind) || strings.TrimSpace(candidate.ExternalID) == "" {
				err = fmt.Errorf("collection connector %s search returned invalid candidate %d", connector.ID(), index)
				break
			}
		}
	}
	return response.Candidates, err
}

func validateConnectorMessages(connectorID, operation string, messages []Message) error {
	for index, message := range messages {
		if strings.TrimSpace(message.ID) == "" || strings.TrimSpace(message.ConversationID) == "" ||
			message.CreatedAt <= 0 {
			return fmt.Errorf("collection connector %s %s returned invalid message %d: id, conversation_id, and created_at are required",
				connectorID, operation, index)
		}
		for stateIndex, state := range message.ExternalStates {
			if !collectionConnectorToken(state.Kind) || strings.TrimSpace(state.Reference) == "" ||
				strings.TrimSpace(state.State) == "" || state.CheckedAt <= 0 {
				return fmt.Errorf("collection connector %s %s returned invalid external state %d for message %d: kind, reference, state, and checked_at are required",
					connectorID, operation, stateIndex, index)
			}
			switch state.Disposition {
			case ExternalDispositionActionable, ExternalDispositionSettled, ExternalDispositionUnknown:
			default:
				return fmt.Errorf("collection connector %s %s returned invalid external disposition %q for message %d",
					connectorID, operation, state.Disposition, index)
			}
		}
		if message.ExternalStatesCoverMessage && len(message.ExternalStates) == 0 {
			return fmt.Errorf("collection connector %s %s returned message %d covered by no external states",
				connectorID, operation, index)
		}
	}
	return nil
}

func collectionConnectorToken(value string) bool {
	return connectorIDPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}

func (connector CommandConnector) call(ctx context.Context,
	request commandConnectorRequest) (commandConnectorResponse, error) {
	commandPath := strings.TrimSpace(connector.Command)
	if commandPath == "" {
		return commandConnectorResponse{}, fmt.Errorf("collection connector %s has no command", connector.ID())
	}
	timeout := connector.Timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload, err := json.Marshal(request)
	if err != nil {
		return commandConnectorResponse{}, err
	}
	args := append(append([]string{}, connector.Args...), request.Operation)
	command := exec.CommandContext(commandCtx, commandPath, args...)
	command.Stdin = bytes.NewReader(append(payload, '\n'))
	stdout := cappedBuffer{limit: connectorOutputLimit}
	stderr := cappedBuffer{limit: connectorErrorLimit}
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()
	if commandCtx.Err() == context.DeadlineExceeded {
		return commandConnectorResponse{}, fmt.Errorf("collection connector %s timed out after %s", connector.ID(), timeout)
	}
	if stdout.exceeded {
		return commandConnectorResponse{}, fmt.Errorf("collection connector %s output exceeds %d bytes",
			connector.ID(), connectorOutputLimit)
	}
	if err != nil {
		message := strings.TrimSpace(stderr.buffer.String())
		if stderr.exceeded {
			message += "…"
		}
		if message == "" {
			message = err.Error()
		}
		return commandConnectorResponse{}, fmt.Errorf("collection connector %s %s failed: %s",
			connector.ID(), request.Operation, message)
	}
	var response commandConnectorResponse
	if err := json.Unmarshal(stdout.buffer.Bytes(), &response); err != nil {
		return response, fmt.Errorf("decode collection connector %s %s response: %w",
			connector.ID(), request.Operation, err)
	}
	if message := strings.TrimSpace(response.Error); message != "" {
		return response, fmt.Errorf("collection connector %s %s: %s",
			connector.ID(), request.Operation, message)
	}
	return response, nil
}
