package target

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// A compiled plugin says what it can through its method set; a data-driven one
// carries every method and has to confirm each capability. The helpers below
// must tell the two apart, or a manifest without an inspect block would grow a
// check that can only fail.

type bareSystem struct{}

func (bareSystem) Name() string                                 { return "bare" }
func (bareSystem) ActionSubject(string, json.RawMessage) string { return "" }
func (bareSystem) Execute(context.Context, string, json.RawMessage, Credential) (any, error) {
	return nil, nil
}
func (bareSystem) PromptDoc() string { return "" }

type lifetimeSystem struct{ bareSystem }

func (lifetimeSystem) Inspect(context.Context, Credential) (CredentialInfo, error) {
	return CredentialInfo{Identity: "me"}, nil
}
func (lifetimeSystem) Rotate(context.Context, Credential) (Credential, CredentialInfo, error) {
	return Credential{}, CredentialInfo{}, nil
}
func (lifetimeSystem) Revoke(context.Context, Credential, string) error { return nil }

type declaredSystem struct {
	lifetimeSystem
	caps map[string]bool
}

func (d declaredSystem) Supports(c string) bool { return d.caps[c] }

func TestInspectsAndRotatesFollowTheMethodSet(t *testing.T) {
	if _, ok := Inspects(bareSystem{}); ok {
		t.Fatal("a system without Inspect must not inspect")
	}
	if _, ok := Rotates(bareSystem{}); ok {
		t.Fatal("a system without Rotate must not rotate")
	}
	if _, ok := Inspects(lifetimeSystem{}); !ok {
		t.Fatal("a compiled system with Inspect inspects")
	}
	if _, ok := Rotates(lifetimeSystem{}); !ok {
		t.Fatal("a compiled system with Rotate rotates")
	}
}

func TestADataDrivenSystemHasToConfirmEachCapability(t *testing.T) {
	sys := declaredSystem{caps: map[string]bool{CapInspect: true}}
	if _, ok := Inspects(sys); !ok {
		t.Fatal("inspect was declared")
	}
	if _, ok := Rotates(sys); ok {
		t.Fatal("rotate was not declared — the method alone does not count")
	}
}

func TestCredentialRejectedIsFoundAnywhereInTheChain(t *testing.T) {
	inner := &CredentialRejectedError{Status: 401, Err: errors.New("Unauthorized")}
	wrapped := fmt.Errorf("jira GET /myself: %w", inner)
	if !IsCredentialRejected(wrapped) {
		t.Fatal("the rejection is wrapped, and still a rejection")
	}
	if IsCredentialRejected(errors.New("HTTP 401")) {
		t.Fatal("a plain error is not a rejection — that reading belongs to the host, not the contract")
	}
	if got := inner.Error(); got != "credential rejected: Unauthorized" {
		t.Fatalf("Error() = %q", got)
	}
	if got := (&CredentialRejectedError{Status: 401}).Error(); got != "credential rejected (HTTP 401)" {
		t.Fatalf("Error() without cause = %q", got)
	}
}
