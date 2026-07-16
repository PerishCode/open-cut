package service

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/PerishCode/open-cut/product/application"
)

type CodexJSONLDecoder struct {
	expectedSession string
	nativeSessionID string
	messageBytes    int
	messageCount    uint32
	terminal        bool
	failed          bool
	turnStarted     bool
	itemSeen        bool
}

type codexJSONLEvent struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Item     *codexJSONLItem `json:"item"`
}

type codexJSONLItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func NewCodexJSONLDecoder() *CodexJSONLDecoder { return &CodexJSONLDecoder{} }

func NewCodexResumeJSONLDecoder(expectedSession string) *CodexJSONLDecoder {
	return &CodexJSONLDecoder{expectedSession: expectedSession}
}

func (decoder *CodexJSONLDecoder) Consume(
	ctx context.Context,
	line []byte,
	observer AgentProcessObserver,
) error {
	if decoder == nil || observer == nil || decoder.terminal || len(line) == 0 || len(line) > MaximumAgentJSONLLineBytes {
		return ErrAgentProcessProtocol
	}
	var event codexJSONLEvent
	if err := json.Unmarshal(line, &event); err != nil || event.Type == "" {
		return ErrAgentProcessProtocol
	}
	switch event.Type {
	case "thread.started":
		if decoder.nativeSessionID != "" || !validOpaqueCodexSession(event.ThreadID) ||
			(decoder.expectedSession != "" && decoder.expectedSession != event.ThreadID) {
			return ErrAgentProcessProtocol
		}
		decoder.nativeSessionID = event.ThreadID
		return observer.ObserveNativeSession(ctx, event.ThreadID)
	case "turn.started":
		decoder.turnStarted = true
		return observer.ObserveAgentPresentation(ctx, AgentPresentationEvent{Kind: AgentPresentationTurnStarted})
	case "item.started", "item.updated", "item.completed":
		decoder.itemSeen = true
		return decoder.consumeItem(ctx, event.Type, event.Item, observer)
	case "turn.completed":
		if decoder.nativeSessionID == "" {
			return ErrAgentProcessProtocol
		}
		decoder.terminal = true
		return observer.ObserveAgentPresentation(ctx, AgentPresentationEvent{Kind: AgentPresentationTurnCompleted})
	case "turn.failed", "error":
		decoder.terminal, decoder.failed = true, true
		if err := observer.ObserveAgentPresentation(ctx, AgentPresentationEvent{Kind: AgentPresentationTurnFailed}); err != nil {
			return err
		}
		return ErrAgentProcessFailed
	default:
		return ErrAgentProcessProtocol
	}
}

func (decoder *CodexJSONLDecoder) FreshFallbackSafe() bool {
	return decoder != nil && decoder.expectedSession != "" && !decoder.turnStarted && !decoder.itemSeen &&
		decoder.messageCount == 0
}

func (decoder *CodexJSONLDecoder) Finish() (AgentProcessResult, error) {
	if decoder == nil || decoder.nativeSessionID == "" || !decoder.terminal || decoder.failed {
		return AgentProcessResult{}, ErrAgentProcessProtocol
	}
	return AgentProcessResult{NativeSessionID: decoder.nativeSessionID, MessageCount: decoder.messageCount}, nil
}

func (decoder *CodexJSONLDecoder) consumeItem(
	ctx context.Context,
	eventType string,
	item *codexJSONLItem,
	observer AgentProcessObserver,
) error {
	if item == nil {
		return ErrAgentProcessProtocol
	}
	tool, known := codexPresentationTool(item.Type)
	if !known {
		return ErrAgentProcessProtocol
	}
	if item.Type == "agent_message" {
		if eventType != "item.completed" {
			return nil
		}
		if item.Text == "" || !utf8.ValidString(item.Text) {
			return ErrAgentProcessProtocol
		}
		decoder.messageBytes += len([]byte(item.Text))
		if decoder.messageBytes > application.MaximumAgentTurnTextBytes {
			return ErrAgentProcessResourceLimit
		}
		if err := observer.ObserveAgentMessage(ctx, item.Text); err != nil {
			return err
		}
		decoder.messageCount++
		return observer.ObserveAgentPresentation(ctx, AgentPresentationEvent{Kind: AgentPresentationMessage})
	}
	kind := AgentPresentationToolStarted
	if eventType == "item.completed" {
		kind = AgentPresentationToolCompleted
	}
	if eventType == "item.updated" {
		return nil
	}
	return observer.ObserveAgentPresentation(ctx, AgentPresentationEvent{Kind: kind, Tool: tool})
}

func codexPresentationTool(value string) (AgentPresentationTool, bool) {
	switch value {
	case "command_execution":
		return AgentPresentationCommand, true
	case "file_change":
		return AgentPresentationFileChange, true
	case "reasoning":
		return AgentPresentationReasoning, true
	case "web_search":
		return AgentPresentationWebSearch, true
	case "todo_list", "plan_update":
		return AgentPresentationPlan, true
	case "agent_message":
		return "", true
	default:
		return "", false
	}
}

func validOpaqueCodexSession(value string) bool {
	if len(value) == 0 || len(value) > 256 || strings.TrimSpace(value) != value || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

var _ AgentStreamDecoder = (*CodexJSONLDecoder)(nil)
