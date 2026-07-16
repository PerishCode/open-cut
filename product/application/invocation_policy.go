package application

import (
	"errors"

	"github.com/PerishCode/open-cut/product/domain"
)

const (
	MinimumWaitMilliseconds uint32 = 250
	MaximumWaitMilliseconds uint32 = 30_000
	DefaultWaitMilliseconds uint32 = 10_000
)

var ErrInvalidInvocationPolicy = errors.New("invalid invocation policy")

type OutputMode string

const (
	OutputJSON  OutputMode = "json"
	OutputHuman OutputMode = "human"
)

type InvocationPolicy struct {
	Output           OutputMode `json:"output" enum:"json,human"`
	WaitMilliseconds uint32     `json:"waitMilliseconds" minimum:"250" maximum:"30000"`
}

func DefaultInvocationPolicy() InvocationPolicy {
	return InvocationPolicy{Output: OutputJSON, WaitMilliseconds: DefaultWaitMilliseconds}
}

func (policy InvocationPolicy) Validate() error {
	if (policy.Output != OutputJSON && policy.Output != OutputHuman) ||
		policy.WaitMilliseconds < MinimumWaitMilliseconds || policy.WaitMilliseconds > MaximumWaitMilliseconds {
		return ErrInvalidInvocationPolicy
	}
	return nil
}

type InvocationPolicyOverride struct {
	Output           *OutputMode `json:"output,omitempty" enum:"json,human"`
	WaitMilliseconds *uint32     `json:"waitMilliseconds,omitempty" minimum:"250" maximum:"30000"`
}

func (override InvocationPolicyOverride) Apply(base InvocationPolicy) (InvocationPolicy, error) {
	if err := base.Validate(); err != nil {
		return InvocationPolicy{}, err
	}
	result := base
	if override.Output != nil {
		result.Output = *override.Output
	}
	if override.WaitMilliseconds != nil {
		result.WaitMilliseconds = *override.WaitMilliseconds
	}
	if err := result.Validate(); err != nil {
		return InvocationPolicy{}, err
	}
	return result, nil
}

type InvocationPolicySettings struct {
	Revision domain.Revision  `json:"revision" minimum:"1"`
	Policy   InvocationPolicy `json:"policy"`
}

func (settings InvocationPolicySettings) Validate() error {
	if settings.Revision.Value() < 1 {
		return ErrInvalidInvocationPolicy
	}
	return settings.Policy.Validate()
}

type InvocationPolicySnapshot struct {
	SettingsRevision domain.Revision  `json:"settingsRevision" minimum:"1"`
	Persisted        InvocationPolicy `json:"persisted"`
	Effective        InvocationPolicy `json:"effective"`
}

func NewInvocationPolicySnapshot(
	settings InvocationPolicySettings,
	override InvocationPolicyOverride,
) (InvocationPolicySnapshot, error) {
	if err := settings.Validate(); err != nil {
		return InvocationPolicySnapshot{}, err
	}
	effective, err := override.Apply(settings.Policy)
	if err != nil {
		return InvocationPolicySnapshot{}, err
	}
	return InvocationPolicySnapshot{
		SettingsRevision: settings.Revision,
		Persisted:        settings.Policy,
		Effective:        effective,
	}, nil
}

func (snapshot InvocationPolicySnapshot) Validate() error {
	if snapshot.SettingsRevision.Value() < 1 {
		return ErrInvalidInvocationPolicy
	}
	if err := snapshot.Persisted.Validate(); err != nil {
		return err
	}
	return snapshot.Effective.Validate()
}
