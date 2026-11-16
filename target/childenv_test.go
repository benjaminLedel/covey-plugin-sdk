package target

import (
	"slices"
	"testing"
)

// TestFilterEnvDropsTheDaemonCredentials: a process a plugin starts must not
// see the daemon's connection to the control plane, only what is handed on.
func TestFilterEnvDropsTheDaemonCredentials(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"HOME=/home/agent",
		"COVEY_WS_URL=wss://control.example.org/daemon",
		"COVEY_DAEMON_TOKEN=secret",
		"COVEY_AGENT_ID=0000",
		"COVEY_ACTION_PORT=41234",
		"COVEY_ACTION_PORTX=1",
	}
	got := FilterEnv(in, "COVEY_ACTION_PORT")
	want := []string{"PATH=/usr/bin", "HOME=/home/agent", "COVEY_ACTION_PORT=41234"}
	if !slices.Equal(got, want) {
		t.Fatalf("FilterEnv = %q, want %q", got, want)
	}
	if got := FilterEnv(in); slices.ContainsFunc(got, func(kv string) bool { return kv[:6] == "COVEY_" }) {
		t.Fatalf("FilterEnv without keep still holds a COVEY_ variable: %q", got)
	}
}

func TestChildEnvReadsTheProcessEnvironment(t *testing.T) {
	t.Setenv("COVEY_DAEMON_TOKEN", "secret")
	t.Setenv("COVEY_ACTION_PORT", "41234")
	env := ChildEnv("COVEY_ACTION_PORT")
	if slices.Contains(env, "COVEY_DAEMON_TOKEN=secret") {
		t.Fatal("ChildEnv kept COVEY_DAEMON_TOKEN")
	}
	if !slices.Contains(env, "COVEY_ACTION_PORT=41234") {
		t.Fatal("ChildEnv dropped the variable it was asked to keep")
	}
}
