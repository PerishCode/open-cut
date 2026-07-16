package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	MaximumAgentPresentationSubscriberBytes = 1024 * 1024
	MaximumAgentPresentationSubscribers     = 32
	agentPresentationQueueCapacity          = 8192
)

var ErrAgentPresentationUnavailable = errors.New("Agent presentation stream is unavailable")

type AgentPresentationEnvelope struct {
	RunID    domain.RunID          `json:"runId" format:"uuid"`
	TurnID   domain.TurnID         `json:"turnId" format:"uuid"`
	Sequence domain.Cursor         `json:"sequence" format:"uint64-decimal" pattern:"^[1-9][0-9]*$"`
	Kind     AgentPresentationKind `json:"kind" enum:"turn-started,context-rebuilt,tool-started,tool-completed,message-completed,turn-completed,turn-failed"`
	Tool     AgentPresentationTool `json:"tool,omitempty" enum:"command,file-change,reasoning,web-search,plan"`
}

type AgentPresentationBus interface {
	AgentPresentationPublisher
	SubscribeAgentPresentation(domain.RunID, domain.TurnID) (*AgentPresentationSubscription, error)
}

type agentPresentationQueued struct {
	event AgentPresentationEnvelope
	size  int
}

type agentPresentationSubscriber struct {
	id           uint64
	queue        chan agentPresentationQueued
	pendingBytes int
	closed       bool
}

type agentPresentationStream struct {
	sequence    uint64
	subscribers map[uint64]*agentPresentationSubscriber
}

type AgentPresentationHub struct {
	mu            sync.Mutex
	nextID        uint64
	streams       map[string]*agentPresentationStream
	subscriptions map[uint64]*AgentPresentationSubscription
}

type AgentPresentationSubscription struct {
	hub        *AgentPresentationHub
	key        string
	subscriber *agentPresentationSubscriber
	once       sync.Once
}

func NewAgentPresentationHub() *AgentPresentationHub {
	return &AgentPresentationHub{
		streams:       make(map[string]*agentPresentationStream),
		subscriptions: make(map[uint64]*AgentPresentationSubscription),
	}
}

func (hub *AgentPresentationHub) PublishAgentPresentation(
	runID domain.RunID,
	turnID domain.TurnID,
	event AgentPresentationEvent,
) error {
	if hub == nil || runID.IsZero() || turnID.IsZero() || !validAgentPresentationEvent(event) {
		return ErrAgentPresentationUnavailable
	}
	key := agentTurnKey(runID, turnID)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	stream := hub.streams[key]
	if stream == nil {
		stream = &agentPresentationStream{subscribers: make(map[uint64]*agentPresentationSubscriber)}
		hub.streams[key] = stream
	}
	stream.sequence++
	sequence, err := domain.NewCursor(stream.sequence)
	if err != nil {
		return ErrAgentPresentationUnavailable
	}
	envelope := AgentPresentationEnvelope{
		RunID: runID, TurnID: turnID, Sequence: sequence, Kind: event.Kind, Tool: event.Tool,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return ErrAgentPresentationUnavailable
	}
	queued := agentPresentationQueued{event: envelope, size: len(encoded)}
	terminal := event.Kind == AgentPresentationTurnCompleted || event.Kind == AgentPresentationTurnFailed
	for id, subscriber := range stream.subscribers {
		if subscriber.closed || subscriber.pendingBytes+queued.size > MaximumAgentPresentationSubscriberBytes {
			hub.closeSubscriberLocked(key, id, subscriber)
			continue
		}
		select {
		case subscriber.queue <- queued:
			subscriber.pendingBytes += queued.size
			if terminal {
				hub.closeSubscriberLocked(key, id, subscriber)
			}
		default:
			hub.closeSubscriberLocked(key, id, subscriber)
		}
	}
	if terminal {
		delete(hub.streams, key)
	}
	return nil
}

func (hub *AgentPresentationHub) SubscribeAgentPresentation(
	runID domain.RunID,
	turnID domain.TurnID,
) (*AgentPresentationSubscription, error) {
	if hub == nil || runID.IsZero() || turnID.IsZero() {
		return nil, ErrAgentPresentationUnavailable
	}
	key := agentTurnKey(runID, turnID)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	stream := hub.streams[key]
	if stream == nil {
		stream = &agentPresentationStream{subscribers: make(map[uint64]*agentPresentationSubscriber)}
		hub.streams[key] = stream
	}
	if len(stream.subscribers) >= MaximumAgentPresentationSubscribers {
		return nil, ErrAgentPresentationUnavailable
	}
	hub.nextID++
	subscriber := &agentPresentationSubscriber{
		id: hub.nextID, queue: make(chan agentPresentationQueued, agentPresentationQueueCapacity),
	}
	subscription := &AgentPresentationSubscription{hub: hub, key: key, subscriber: subscriber}
	stream.subscribers[subscriber.id] = subscriber
	hub.subscriptions[subscriber.id] = subscription
	return subscription, nil
}

func (subscription *AgentPresentationSubscription) Next(
	ctx context.Context,
) (AgentPresentationEnvelope, bool) {
	if subscription == nil || subscription.subscriber == nil {
		return AgentPresentationEnvelope{}, false
	}
	select {
	case <-ctx.Done():
		return AgentPresentationEnvelope{}, false
	case queued, ok := <-subscription.subscriber.queue:
		if !ok {
			return AgentPresentationEnvelope{}, false
		}
		subscription.hub.mu.Lock()
		subscription.subscriber.pendingBytes -= queued.size
		subscription.hub.mu.Unlock()
		return queued.event, true
	}
}

func (subscription *AgentPresentationSubscription) Close() {
	if subscription == nil || subscription.hub == nil || subscription.subscriber == nil {
		return
	}
	subscription.once.Do(func() {
		hub := subscription.hub
		hub.mu.Lock()
		defer hub.mu.Unlock()
		hub.closeSubscriberLocked(subscription.key, subscription.subscriber.id, subscription.subscriber)
	})
}

func (hub *AgentPresentationHub) closeSubscriberLocked(
	key string,
	id uint64,
	subscriber *agentPresentationSubscriber,
) {
	if subscriber == nil || subscriber.closed {
		return
	}
	subscriber.closed = true
	close(subscriber.queue)
	delete(hub.subscriptions, id)
	if stream := hub.streams[key]; stream != nil {
		delete(stream.subscribers, id)
		if len(stream.subscribers) == 0 && stream.sequence == 0 {
			delete(hub.streams, key)
		}
	}
}

func validAgentPresentationEvent(event AgentPresentationEvent) bool {
	switch event.Kind {
	case AgentPresentationTurnStarted, AgentPresentationContextRebuilt, AgentPresentationMessage,
		AgentPresentationTurnCompleted, AgentPresentationTurnFailed:
		return event.Tool == ""
	case AgentPresentationToolStarted, AgentPresentationToolCompleted:
		switch event.Tool {
		case AgentPresentationCommand, AgentPresentationFileChange, AgentPresentationReasoning,
			AgentPresentationWebSearch, AgentPresentationPlan:
			return true
		}
	}
	return false
}
