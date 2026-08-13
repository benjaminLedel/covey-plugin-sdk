package target

import (
	"net/http"
	"sync/atomic"
	"time"
)

// clientFactory is the host's hook. Written once at startup, read from every
// plugin call including concurrent ones — hence atomic.
var clientFactory atomic.Pointer[func(string, time.Duration) *http.Client]

// Client returns the HTTP client a plugin should use for its outbound calls.
//
// Plugins do not build their own client, and the reason is not tidiness. Covey
// records the requests its plugins make at the platform's edges — that is what
// makes "why did the agent do that" answerable hours later, and what an
// operator debugging somebody else's target system has to go on. A plugin with
// a client of its own is a plugin whose traffic is invisible.
//
// Outside Covey (a plugin's own tests, a command-line tool) the default
// applies: a plain client with the given timeout. Nothing breaks, nothing is
// recorded.
func Client(name string, timeout time.Duration) *http.Client {
	if f := clientFactory.Load(); f != nil {
		if c := (*f)(name, timeout); c != nil {
			return c
		}
	}
	return &http.Client{Timeout: timeout}
}

// SetClientFactory lets the host supply the client every plugin gets. Covey
// calls it once at startup, before any plugin runs; nil restores the default.
func SetClientFactory(f func(name string, timeout time.Duration) *http.Client) {
	if f == nil {
		clientFactory.Store(nil)
		return
	}
	clientFactory.Store(&f)
}
