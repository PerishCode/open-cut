package protocol

import "time"

const Version = "sidecar.v1"

type Capability string

const (
	CapabilityObserve          Capability = "observe"
	CapabilityRuntimeReady     Capability = "runtime-ready"
	CapabilityLifecycle        Capability = "lifecycle"
	CapabilityUpdateTransition Capability = "update-transition"
	CapabilityDelegateSidecar  Capability = "delegate-sidecar"
)

type Role string

const (
	RoleOwner    Role = "owner"
	RoleRuntime  Role = "runtime"
	RoleSidecar  Role = "sidecar"
	RoleObserver Role = "observer"
)

type ControlDescriptor struct {
	Schema     int       `json:"schema"`
	Protocol   string    `json:"protocol"`
	Address    string    `json:"address"`
	PID        int       `json:"pid"`
	SessionID  string    `json:"sessionId"`
	Generation uint64    `json:"generation"`
	StartedAt  time.Time `json:"startedAt"`
}

type Health struct {
	Schema     int    `json:"schema"`
	Protocol   string `json:"protocol"`
	SessionID  string `json:"sessionId"`
	Generation uint64 `json:"generation"`
}

type Endpoint struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type SessionStatus struct {
	Subject       string     `json:"subject"`
	App           string     `json:"app"`
	Mode          string     `json:"mode"`
	Source        string     `json:"source"`
	Ready         bool       `json:"ready"`
	ConnectedAt   time.Time  `json:"connectedAt"`
	LastHeartbeat time.Time  `json:"lastHeartbeat"`
	Endpoints     []Endpoint `json:"endpoints,omitempty"`
}

type Status struct {
	Schema     int             `json:"schema"`
	Channel    string          `json:"channel"`
	Namespace  string          `json:"namespace"`
	SessionID  string          `json:"sessionId"`
	Generation uint64          `json:"generation"`
	Sessions   []SessionStatus `json:"sessions"`
}

type ClientEvent struct {
	Type       string `json:"type"`
	Channel    string `json:"channel,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	SessionID  string `json:"sessionId,omitempty"`
	Generation uint64 `json:"generation,omitempty"`
	App        string `json:"app,omitempty"`
	Mode       string `json:"mode,omitempty"`
	Source     string `json:"source,omitempty"`
	Name       string `json:"name,omitempty"`
	URL        string `json:"url,omitempty"`
	Code       int    `json:"code,omitempty"`
}

type ServerEvent struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
}

type ControlRequest struct {
	Command string `json:"command"`
}

type ControlResponse struct {
	Accepted int `json:"accepted"`
}

type DelegateRequest struct {
	Subject    string `json:"subject"`
	TTLSeconds int64  `json:"ttlSeconds,omitempty"`
}

type DelegateResponse struct {
	Subject   string    `json:"subject"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type UpdateTransitionRequest struct {
	Action string `json:"action"`
}

type UpdateTransitionResponse struct {
	Status          string `json:"status"`
	Version         string `json:"version"`
	RestartRequired bool   `json:"restartRequired"`
}
