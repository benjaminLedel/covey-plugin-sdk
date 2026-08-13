package target

import (
	"strings"
	"testing"
)

// TestDeclaredEnvStaysInTheOwnNamespace: the rule that makes it safe to honour
// an environment declaration written by somebody else. A plugin may name its
// own variables and nothing else — no declaration can reach the master key,
// the database URL or the daemon's connection variables.
//
// The rule is enforced by the host when it collects the declarations; this
// test pins the shape every plugin is expected to follow, so a plugin whose
// entries would be silently dropped fails here instead.
func TestDeclaredEnvStaysInTheOwnNamespace(t *testing.T) {
	for _, d := range All() {
		prefix := "COVEY_" + strings.ToUpper(strings.ReplaceAll(d.Name, "-", "_")) + "_"
		for _, key := range d.Env {
			if !strings.HasPrefix(key, prefix) {
				t.Errorf("plugin %q declares %q, which is outside its namespace %q — "+
					"the host will drop it", d.Name, key, prefix)
			}
		}
	}
}
