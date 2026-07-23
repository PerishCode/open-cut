package service

import (
	"context"
	"time"
)

type uiSessionBinding struct {
	sessionHash    string
	clientInstance string
	origin         string
	expiresAt      time.Time
	apiInstance    string
}

type uiLeaseBinding struct {
	clientInstance string
	origin         string
	apiInstance    string
}

type uiSessionBindingContextKey struct{}

type UISessionContextBinder interface {
	BindUISession(context.Context, string) (context.Context, error)
}

func (service *UISessionService) BindUISession(
	ctx context.Context,
	token string,
) (context.Context, error) {
	if ctx == nil || token == "" {
		return nil, ErrUnauthorized
	}
	now := service.clock.Now().UTC()
	hash := tokenHash(token)
	service.mu.Lock()
	service.cleanupLocked(now)
	session, exists := service.sessions[hash]
	service.mu.Unlock()
	if !exists || !now.Before(session.expiresAt) {
		return nil, ErrUnauthorized
	}
	binding := uiSessionBinding{
		sessionHash: hash, clientInstance: session.clientInstance, origin: session.origin,
		expiresAt: session.expiresAt, apiInstance: service.apiInstance,
	}
	return context.WithValue(ctx, uiSessionBindingContextKey{}, binding), nil
}

func uiSessionBindingFromContext(ctx context.Context) (uiSessionBinding, error) {
	binding, ok := ctx.Value(uiSessionBindingContextKey{}).(uiSessionBinding)
	if !ok || binding.sessionHash == "" || binding.clientInstance == "" || binding.origin == "" ||
		binding.expiresAt.IsZero() || binding.apiInstance == "" {
		return uiSessionBinding{}, ErrUnauthorized
	}
	return binding, nil
}

func (binding uiSessionBinding) leaseBinding() uiLeaseBinding {
	return uiLeaseBinding{
		clientInstance: binding.clientInstance,
		origin:         binding.origin,
		apiInstance:    binding.apiInstance,
	}
}

func (lease uiLeaseBinding) matches(binding uiSessionBinding) bool {
	return lease.clientInstance == binding.clientInstance &&
		lease.origin == binding.origin &&
		lease.apiInstance == binding.apiInstance
}
