package application

import (
	"testing"

	"github.com/PerishCode/open-cut/product/domain"
)

func TestInvocationPolicySnapshotAppliesExplicitOverride(t *testing.T) {
	revision, _ := domain.NewRevision(7)
	output := OutputJSON
	wait := uint32(750)
	snapshot, err := NewInvocationPolicySnapshot(InvocationPolicySettings{
		Revision: revision,
		Policy:   InvocationPolicy{Output: OutputHuman, WaitMilliseconds: 5_000},
	}, InvocationPolicyOverride{Output: &output, WaitMilliseconds: &wait})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SettingsRevision != revision || snapshot.Persisted.Output != OutputHuman ||
		snapshot.Effective.Output != OutputJSON || snapshot.Effective.WaitMilliseconds != wait {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestInvocationPolicyRejectsOutOfBoundsOverride(t *testing.T) {
	revision, _ := domain.NewRevision(1)
	wait := MaximumWaitMilliseconds + 1
	_, err := NewInvocationPolicySnapshot(InvocationPolicySettings{
		Revision: revision, Policy: DefaultInvocationPolicy(),
	}, InvocationPolicyOverride{WaitMilliseconds: &wait})
	if err == nil {
		t.Fatal("out-of-bounds wait was accepted")
	}
}
