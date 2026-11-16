package target

import (
	"os"
	"strings"
)

// ChildEnv is the environment for a process a plugin starts inside the
// sandbox: the daemon's own environment without its COVEY_* variables, except
// the ones named in keep.
//
// The daemon's variables are its credentials for the control plane:
// COVEY_WS_URL and COVEY_DAEMON_TOKEN let any process holding them open its
// own connection and ask the broker for the stored secret of every target
// system the agent may use. A process a plugin starts runs code nobody
// reviewed — an install script, a test suite, the application under
// development — so it must not inherit them. The host filters its own runtime
// children the same way; this is the one place both rules live, so the two
// cannot drift apart.
//
// keep names the variables a plugin hands on deliberately, such as
// COVEY_ACTION_PORT for processes that reach target systems through the action
// proxy.
func ChildEnv(keep ...string) []string {
	return FilterEnv(os.Environ(), keep...)
}

// FilterEnv applies ChildEnv's rule to an explicit environment.
func FilterEnv(environ []string, keep ...string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if strings.HasPrefix(kv, "COVEY_") && !kept(kv, keep) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func kept(kv string, keep []string) bool {
	name, _, _ := strings.Cut(kv, "=")
	for _, k := range keep {
		if name == k {
			return true
		}
	}
	return false
}
