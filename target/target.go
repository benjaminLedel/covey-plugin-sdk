// Package target is the contract between Covey and a target-system plugin: the
// interfaces a plugin implements, the registry it enters itself into, and the
// handful of helpers it needs along the way.
//
// It is its own module, with no dependencies beyond the standard library, for
// one reason: a plugin author should depend on the CONTRACT, not on Covey. The
// platform's own tree — Postgres, a browser driver, a wasm runtime — has no
// business in somebody's plugin.
//
// A plugin is one package that calls Register in init(). Whoever links it in
// gets it; Covey's own binary links in what it ships, and anybody building
// their own Covey adds or leaves out whatever they like. The same registry is
// read by the control plane (webhook intake, prompt docs, UI) and by the
// sandbox daemon (action execution) — there is no hardcoded list anywhere.
//
// Beyond compiled plugins, Covey can load plugins that need no rebuild at all:
// a declarative JSON manifest, an MCP server, or a WebAssembly module. Those
// live in Covey, not here, but they satisfy exactly the System interface below
// — broker, guard rails and recording apply identically no matter where a
// plugin came from.
package target

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// System is a connected target system. It bundles the integration surfaces
// (spec/13): actions and prompt docs. The webhook intake is optional (see
// Webhooker) — a target system that only takes in work via polling/heartbeat
// (e.g. GitLab) does not implement it.
type System interface {
	Name() string

	// ActionSubject maps action+params onto the guard-rail subject
	// (e.g. reply with internal=false → "zammad:reply_external").
	ActionSubject(action string, params json.RawMessage) string

	// Execute runs an agent action with brokered credentials (daemon side).
	// The credentials come from the broker per call — they are never
	// persisted.
	Execute(ctx context.Context, action string, params json.RawMessage, cred Credential) (any, error)

	// PromptDoc describes the available actions for the agent's system prompt
	// (it is compiled into the platform protocol section).
	PromptDoc() string
}

// ScopedDocSystem is an optional plugin interface for target systems whose
// prompt doc can be narrowed to the scopes granted in ACCESS.md. It pays off
// where a system carries procedures for several roles: GitLab describes both
// the developer and the QA/reviewer loop, and an agent without the merge scope
// dragged the reviewer part through every turn without ever being able to act
// on it — the doc stands in the context of EVERY turn, so what is unusable
// there is not paid for once but on every one.
//
// Systems without this interface keep delivering their full PromptDoc; the
// store falls back to it (fail-open). A plugin that implements it has to answer
// the full doc for an empty scope list for the same reason — a missing entry
// must never silently take a capability away from an agent.
type ScopedDocSystem interface {
	PromptDocForScopes(scopes []string) string
}

// Webhooker is an optional plugin interface for target systems with an
// incoming webhook (Zammad, manifest plugins). The event router
// (httpapi.handleTargetWebhook) checks for it; a system without Webhooker
// answers webhook posts fail-closed with 404. Target systems that only take in
// work via polling leave this interface out — then there is no incoming
// traffic, no public URL and no webhook secret.
type Webhooker interface {
	// VerifyWebhook checks the integrity of a raw webhook payload (e.g. an
	// HMAC signature). Empty secret = verification disabled (dev).
	VerifyWebhook(secret string, body []byte, header http.Header) bool

	// ParseWebhook turns the payload into the wake event for the orchestrator.
	ParseWebhook(body []byte) (WebhookEvent, error)
}

// Credential is the brokered access to a target system: where it is, how to
// authenticate against it, and — where the endpoint is not signed by a publicly
// trusted authority — who to trust for it.
type Credential struct {
	BaseURL string
	Token   string
	// CA is the PEM certificate (or chain) that signs the target system's
	// endpoint, brokered from the optional secret <system>_ca. Empty means the
	// system roots apply, which is the normal case for anything on the public
	// internet; an internal Kubernetes API server or an appliance behind a
	// company CA is the case this exists for.
	//
	// It belongs in the credential and not in an action's parameters, however
	// tempting the latter is. A parameter travels through the model's call, the
	// guard rails and the recording of every single action — the trust anchor
	// of an endpoint has nothing to do with any of them, and repeating it there
	// makes every call carry a certificate. Here it is brokered once, per call,
	// like the token beside it.
	//
	// A plugin that cannot make its own connection (a wasm module, a manifest)
	// never sees this either: the HOST builds the trust store from it. Only a
	// compiled plugin, which does its own dialling, reads the field.
	CA string
	// Login is the account the token acts as, when the HOST knows it and the
	// system cannot be asked. A GitHub App installation token is the case this
	// exists for: GET /user refuses such a token, yet everything that hangs off
	// "did I write that myself?" needs the bot's login ("name[bot]"). The host
	// minted the token, so it knows; a plugin that finds the field empty asks
	// the system as before. Never a secret, never part of an action's params.
	Login string
}

// WorkChecker is an optional plugin interface for systems without a webhook:
// a cheap upfront check by the Control Plane whether there is any work at all
// (e.g. unread mail via IMAP). Heartbeat entries with nur-wenn: <system> only
// fire if HasWork returns true — otherwise the run, and with it the (expensive)
// agent wake, is skipped. The check runs in the Control Plane; the credential
// never leaves it.
type WorkChecker interface {
	HasWork(ctx context.Context, cred Credential) (bool, error)
}

// KindWorkChecker refines WorkChecker for target systems with several kinds of
// work (e.g. GitLab: issues vs. merge request reviews). A heartbeat with
// nur-wenn: <system>:<kind> (say nur-wenn: gitlab:mr) calls
// HasWorkKind(…, kind) — that way each kind can be gated separately instead of
// letting both heartbeats fire off a shared boolean. Without a sub-scope
// (nur-wenn: <system>) HasWork still applies. An empty/unknown kind must not
// report less than HasWork (fail-open: when in doubt, assume there is work).
type KindWorkChecker interface {
	WorkChecker
	HasWorkKind(ctx context.Context, cred Credential, kind string) (bool, error)
}

// SignedWorkChecker refines WorkChecker with a signature of the work found — a
// short, stable description of WHAT the check responded to (with GitLab for
// instance "mr!9@n1234": merge request 9, newest note 1234).
//
// The reason: a nur-wenn: condition is level-triggered. It reports work as long
// as the state persists — "someone else wrote last in the thread" stays true
// until the agent writes itself. An agent that DELIBERATELY ends a run without
// a comment (the feedback was an approval, there is nothing to do) would
// therefore be woken again in the next interval and would end up commenting
// only to turn off its own alarm clock.
//
// The Control Plane therefore remembers the signature it last fired on and only
// fires again once it CHANGES. So the agent is still woken for every piece of
// news — including one it merely takes note of — but never twice for the same
// one. Whether feedback is work (defects) or not (approval) is thus decided by
// the agent, not by the gate.
//
// An empty signature switches the suppression off: then the heartbeat fires on
// every level as before (fail-open).
//
// Write the signature with WorkSignature, one entry per waiting item. Then a
// host can also tell WHICH items are waiting and which of them changed — what
// covey's workflows need to run one case per item, once the plugin says so
// with ItemSigner (see workentries.go).
type SignedWorkChecker interface {
	WorkChecker
	// HasWorkSigned checks like HasWorkKind (kind == "" → like HasWork) and
	// additionally returns the signature of the work found.
	HasWorkSigned(ctx context.Context, cred Credential, kind string) (bool, string, error)
}

// SignatureWriter says whether one of the system's OWN actions can move the
// work signature — the question the control plane has to answer after a run
// before it may write the state it now sees into the watermark.
//
// The background is a race the signature alone cannot resolve. The signature is
// remembered at dispatch, i.e. BEFORE the run; a run that comments changes it
// itself, so the control plane has to advance it afterwards, otherwise the
// agent's own comment wakes it again. But "the state after the run" also
// contains everything that arrived from outside DURING the run — under a shared
// target-system identity (no bot account per role) authorship cannot tell the
// two apart, and a foreign comment silently absorbed into the watermark is a
// piece of work nobody is ever woken for again.
//
// This interface narrows the race to the cases where the agent really wrote:
// did the run execute no signature-changing action at all, then every change
// since the dispatch comes from outside and the watermark must stay where it
// is. The subject is the one from ActionSubject (e.g. "gitlab:comment_external"),
// because that is what the recording carries.
//
// Systems without this interface keep the old behaviour: after a successful run
// the watermark is advanced unconditionally.
type SignatureWriter interface {
	SignedWorkChecker
	WritesWorkSignature(subject string) bool
}

// workdirKey carries the sandbox working directory through the context to
// Execute. Actions that materialize files in the sandbox (e.g. gitlab
// checkout) unpack them there — the daemon sets the value, because only it
// knows where the runtime's workspace lives.
type ctxKey int

const (
	workdirKey ctxKey = iota
	artifactSinkKey
	subAgentKey
	actorKey
)

// Actor is the agent on whose behalf an action runs. Several agents of one
// organisation can share a single identity on the target system (one GitHub
// App, one service account); the actor is how a plugin still says WHICH of
// them acted — as a commit author, a signature line or a hidden marker that
// lets the host route the answer back to the right agent.
type Actor struct {
	// Slug is the agent's stable handle within its organisation.
	Slug string
	// Name is the display name ("7OS Developer").
	Name string
	// Org is the organisation's display name, for signatures. May be empty.
	Org string
}

// WithActor attaches the acting agent to the context. The action proxy sets it
// per action.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey, a)
}

// ActorFrom reads the acting agent from the context. ok is false outside an
// agent's action (setup probes, the heartbeat pre-check).
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey).(Actor)
	return a, ok && a.Slug != ""
}

// Artifact is a binary side result of an action that belongs in the recording
// (e.g. a screenshot) but NOT in the action result handed to the runtime —
// otherwise it would end up in the LLM context. Plugins pass it through via
// EmitArtifact; the action proxy collects it separately from the returned
// result.
type Artifact struct {
	MIME  string
	Bytes []byte
}

// WithArtifactSink attaches a sink to the context that takes in a plugin's
// EmitArtifact calls. The action proxy sets it per action.
func WithArtifactSink(ctx context.Context, sink func(Artifact)) context.Context {
	return context.WithValue(ctx, artifactSinkKey, sink)
}

// EmitArtifact reports an artifact to the sink in the context (no-op without a
// sink).
func EmitArtifact(ctx context.Context, a Artifact) {
	if sink, ok := ctx.Value(artifactSinkKey).(func(Artifact)); ok && sink != nil {
		sink(a)
	}
}

// WithWorkdir attaches the sandbox working directory to the context.
func WithWorkdir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, workdirKey, dir)
}

// Workdir reads the sandbox working directory from the context. Empty if the
// action runs outside a sandbox (e.g. in Control Plane context).
func Workdir(ctx context.Context) string {
	dir, _ := ctx.Value(workdirKey).(string)
	return dir
}

// BaselineRef is the git tag a checkout puts on the freshly unpacked upstream
// state. It is the anchor between checkout and sub-run: the sub-run reports its
// work as a difference to that commit, not as a difference between two status
// snapshots. Only that way does the work stay visible when the sub-agent
// commits locally in the checkout — which many projects explicitly demand in
// their CLAUDE.md.
const BaselineRef = "covey-baseline"

// SubAgentRequest is a work order for a nested runtime run that starts IN the
// given directory — typically a project checkout. That way the project's own
// Claude Code harness applies there (CLAUDE.md, .claude/agents, skills,
// commands), which the outer run never sees from the agent home.
type SubAgentRequest struct {
	// Dir is the working directory of the sub-run (relative to the sandbox
	// home or absolute).
	Dir string
	// Task is the work order — the sub-agent's only input.
	Task string
	// Model and MaxTurns override the run's defaults (empty/0 = default).
	Model    string
	MaxTurns int
}

// SubAgentResult is the normalized result of a sub-run. ChangedFiles and
// Deleted are repo-relative paths and fit straight into the commit action.
type SubAgentResult struct {
	Result         string   `json:"result"`
	ChangedFiles   []string `json:"changed_files,omitempty"`
	Deleted        []string `json:"deleted,omitempty"`
	CostUSD        float64  `json:"cost_usd,omitempty"`
	TurnsExhausted bool     `json:"turns_exhausted,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// SubAgentRunner executes a sub-run. The daemon attaches it to the context per
// action — just like the workdir and the artifact sink, so that plugins can use
// the capability without importing the daemon.
type SubAgentRunner func(ctx context.Context, req SubAgentRequest) (SubAgentResult, error)

// WithSubAgent attaches the sub-agent runner to the context.
func WithSubAgent(ctx context.Context, run SubAgentRunner) context.Context {
	return context.WithValue(ctx, subAgentKey, run)
}

// SubAgent reads the runner from the context. nil if the action runs outside a
// sandbox (e.g. in Control Plane context) — then there is no runtime that could
// be nested.
func SubAgent(ctx context.Context) SubAgentRunner {
	run, _ := ctx.Value(subAgentKey).(SubAgentRunner)
	return run
}

// WebhookEvent is the normalized result of a webhook payload — everything the
// orchestrator needs for idempotency, correlation and task creation.
type WebhookEvent struct {
	// DedupKey makes processing idempotent (retries by the target system).
	DedupKey string
	// CorrelationKey wakes a blocked task (e.g. "zammad:ticket:42").
	CorrelationKey string
	// Title/TaskBody describe the new backlog task, in case no blocked task
	// correlates.
	Title    string
	TaskBody string
	// ResumeInput is the resume input for a correlated task.
	ResumeInput string
	// Wake: false → the event is registered (dedup) but does not wake —
	// e.g. the echo of the agent's own reply.
	Wake bool
	// CorrelateOnly: the event only wakes an already blocked task
	// (wake-on-correlation) but creates NO new one — e.g. the merge of an MR:
	// if nobody is waiting for it, it is not work.
	CorrelateOnly bool
}

// Descriptor is the plugin unit of a target system: metadata for the UI plus
// the implementation.
type Descriptor struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
	// Kind: "builtin" (compiled) or "custom" (manifest upload).
	Kind string `json:"kind"`
	// Category places the plugin in the store (constants Category…). The
	// plugin declares it itself — the UI derives its filters from the
	// categories that occur, without a list of its own. Empty = CategoryOther.
	Category string `json:"category,omitempty"`
	System   System `json:"-"`
	// NoCredentials: the system needs no brokered secrets (no
	// <name>_token/_url) — it works purely locally in the sandbox (e.g. the
	// dev plugin). ACCESS.md, activation and guard-rails still apply.
	NoCredentials bool `json:"-"`
	// CredentialsOptional: the system is usable WITHOUT a secret, and a stored
	// <name>_token only raises what it may do — e.g. an API key that lifts a
	// public rate limit (vulndb/NVD). The broker then resolves token and URL
	// best effort and grants even when nothing is stored, instead of denying.
	// The difference from NoCredentials: there the plugin never sees a
	// credential; here it sees one as soon as the organization stores one.
	CredentialsOptional bool `json:"-"`
	// BaseURLOptional: the plugin knows its target system's endpoint itself
	// (a fixed default, e.g. the Bot Framework token endpoint for Teams).
	// <name>_url is then an override for special cases, not a mandatory
	// secret — the broker does not refuse without it. A <name>_token stays
	// mandatory.
	BaseURLOptional bool `json:"-"`
	// SetupDoc is the setup guide for the UI (plain text, numbered steps).
	// Placeholders: {public_url} is replaced by the API with the configured
	// COVEY_PUBLIC_URL; <agent-slug> stays as it is and means the slug of the
	// responsible agent (the webhook endpoint accepts its ID instead).
	//
	// It describes what has to happen in the FOREIGN system — create a token,
	// hang in a trigger. What happens in Covey itself is not prose but the
	// fields below: the setup assistant acts on those, and a step it can carry
	// out itself has no business being an instruction.
	SetupDoc string `json:"setup_doc,omitempty"`
	// Env are the environment variables this plugin reads AT WORK — in the
	// sandbox, where the action proxy runs it, not in the control plane.
	//
	// It has to be declared because of where the two halves live. Operational
	// configuration (intake allowlists, size limits) is set on the control
	// plane, 12-factor style; the plugin that reads it runs somewhere else
	// entirely. A variable that does not make the journey is not merely
	// ineffective — an empty intake allowlist reads as "no restriction", so it
	// inverts into the widest setting, quietly. Covey therefore carries exactly
	// what is declared here into the sandbox.
	//
	// A plugin may only name variables in its OWN namespace: COVEY_<NAME>_…
	// with NAME being this plugin's Name, upper-cased. Anything else is
	// dropped, and that rule is what makes this safe to honour from a plugin
	// somebody else wrote — no declaration can reach COVEY_MASTER_KEY,
	// COVEY_DATABASE_URL or the daemon's own connection variables.
	//
	// Declare the name, never the value. Secrets do not belong here; they come
	// from the broker, per call.
	Env []string `json:"env,omitempty"`
	// Scopes are the access levels this plugin understands in ACCESS.md
	// (`- system: zammad scope: read,write,comment`). The plugin declares them
	// because only it knows them; the assistant offers exactly these instead of
	// letting somebody guess a word that is then silently ignored.
	//
	// Empty = the plugin does not distinguish (manifest and MCP plugins), and
	// the assistant leaves the scope out.
	Scopes []string `json:"scopes,omitempty"`
}

// Prober is an optional plugin interface: one cheap, read-only call that shows
// whether the stored credentials actually work — and as whom.
//
// It exists because "saved" and "works" are two different things, and until
// now the difference showed up at the first run of an agent, inside a
// recording, hours later. The returned string is the identity the target
// system reports back (a user name, a login, an address); it is displayed as
// is, so it should be short and recognisable.
//
// Read-only, by contract. A probe that changes anything in the foreign system
// would be a poor kind of test — nobody expects a connection test to leave
// traces.
type Prober interface {
	Probe(ctx context.Context, cred Credential) (string, error)
}

// CredentialInfo is what a plugin knows about a credential beyond "it works".
//
// It exists because a token has a lifetime and Probe cannot say so: an
// Atlassian API token dies after a year at most, a Data Center PAT after what
// the instance allows, and the first sign of either used to be a 401 in a
// recording. What the target system does not say stays empty — a guessed
// expiry is worse than none, because covey warns from it.
type CredentialInfo struct {
	// Identity is what Probe reports: the account the credential acts as.
	// Short and recognisable, shown as is.
	Identity string `json:"identity,omitempty"`
	// ID is the target system's own identifier for this credential, where it
	// has one (a PAT id). Rotate always fills it for the successor; Inspect
	// fills it only when the credential can be told apart from its siblings.
	ID string `json:"id,omitempty"`
	// ExpiresAt is when the target system will stop accepting the credential.
	// nil: the system does not say.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Rotatable says whether THIS credential can mint its successor through
	// Rotate. It sits on the info rather than on the plugin because it is a
	// property of the credential: the same Jira plugin holds Cloud API tokens
	// (never rotatable) and Data Center PATs (rotatable), told apart by the
	// token's shape.
	Rotatable bool `json:"rotatable,omitempty"`
}

// CredentialInspector is an optional plugin interface that extends Prober
// with what the target system knows about the credential's lifetime.
// Read-only, like Probe, and asked instead of it where present — a plugin
// that has Inspect need not repeat itself in Probe, but should keep Probe for
// hosts that only know that one.
type CredentialInspector interface {
	Inspect(ctx context.Context, cred Credential) (CredentialInfo, error)
}

// Rotator is an optional plugin interface for target systems that let a
// credential mint its own successor (Jira Data Center: a PAT creates a PAT).
//
// The control plane drives it, never a run: Rotate returns the successor and
// its lifetime, the caller verifies it with Inspect or Probe, stores it, and
// only then revokes the old one by the ID it kept from CredentialInfo. That
// order is the contract — a rotation that revoked first and then failed to
// store or verify would leave the agent without any credential at all. A plugin that cannot
// revoke (no API for it, no ID known) returns nil from Revoke and lets the old
// credential run out.
type Rotator interface {
	Rotate(ctx context.Context, cred Credential) (Credential, CredentialInfo, error)
	Revoke(ctx context.Context, cred Credential, id string) error
}

// CredentialRejectedError says that the target system refused the credential
// itself — not the request, not the permission: the token is expired, revoked
// or wrong. A plugin returns it (wrapping what the system said) so that the
// host can mark the stored secret instead of filing the failure under the
// action, where it reads as a permission problem three weeks later.
//
// 401 is the usual carrier. 403 is deliberately not: it is what a system
// answers to a valid credential that may not do this one thing, and marking
// the secret for that would be wrong in the way that matters.
type CredentialRejectedError struct {
	Status int   // the HTTP status the target system answered, if any
	Err    error // what it said
}

func (e *CredentialRejectedError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("credential rejected (HTTP %d)", e.Status)
	}
	return "credential rejected: " + e.Err.Error()
}

func (e *CredentialRejectedError) Unwrap() error { return e.Err }

// IsCredentialRejected reports whether err, anywhere in its chain, says the
// credential was refused. Hosts read it at the seam where a plugin's error
// leaves the plugin.
func IsCredentialRejected(err error) bool {
	var r *CredentialRejectedError
	return errors.As(err, &r)
}

// Optional capabilities a plugin may carry. A COMPILED plugin says so through
// its method set — it either has Probe or it does not, and a type assertion is
// the whole check. A plugin whose behaviour lives in DATA (a manifest) cannot:
// its Go type is the same generic engine either way, so the method is always
// there and the assertion always succeeds.
//
// That difference matters at the call sites. "Can this system be probed?"
// decides whether the UI offers a connection test at all, and a manifest
// without a probe block would grow a button that can only fail. So a
// data-driven plugin reports its capabilities explicitly, and the call sites ask
// through the helpers below instead of asserting directly.
const (
	CapProbe   = "probe"
	CapPoll    = "poll"
	CapInspect = "inspect"
	CapRotate  = "rotate"
)

// CapabilityReporter is implemented by plugins whose optional capabilities are
// declared in data rather than in their method set (see above). A plugin
// without it is taken at its method set — the compiled case.
type CapabilityReporter interface {
	Supports(capability string) bool
}

// supports answers whether sys carries a capability: the method set has to have
// it, and a data-driven plugin has to confirm it.
func supports(sys System, capability string) bool {
	if r, ok := sys.(CapabilityReporter); ok {
		return r.Supports(capability)
	}
	return true
}

// Probes returns the system's Prober if it really can probe. Use this instead
// of a bare type assertion on Prober.
func Probes(sys System) (Prober, bool) {
	p, ok := sys.(Prober)
	if !ok || !supports(sys, CapProbe) {
		return nil, false
	}
	return p, true
}

// Inspects returns the system's CredentialInspector if it really can inspect.
// Use this instead of a bare type assertion on CredentialInspector.
func Inspects(sys System) (CredentialInspector, bool) {
	i, ok := sys.(CredentialInspector)
	if !ok || !supports(sys, CapInspect) {
		return nil, false
	}
	return i, true
}

// Rotates returns the system's Rotator if it really can rotate. Whether a
// PARTICULAR credential can is CredentialInfo.Rotatable — this only says the
// plugin knows how.
func Rotates(sys System) (Rotator, bool) {
	r, ok := sys.(Rotator)
	if !ok || !supports(sys, CapRotate) {
		return nil, false
	}
	return r, true
}

// WorkChecks returns the system's WorkChecker if it really can check for work up
// front. Use this instead of a bare type assertion on WorkChecker.
func WorkChecks(sys System) (WorkChecker, bool) {
	c, ok := sys.(WorkChecker)
	if !ok || !supports(sys, CapPoll) {
		return nil, false
	}
	return c, true
}

// Categories for the target system store. Purely for placement in the UI —
// behavior never depends on them.
const (
	CategoryTicketing = "ticketing"     // helpdesk, service desk
	CategoryCode      = "code"          // repos, merge requests, CI
	CategoryComms     = "communication" // email, chat
	CategoryFiles     = "files"         // file shares, documents
	CategoryWeb       = "web"           // browser, web applications
	CategoryDev       = "dev"           // tools in the sandbox itself
	CategoryOther     = "other"
)

var (
	registry = map[string]Descriptor{}
	order    []string
)

// Register enters a target system plugin. Called from the respective
// subpackage's init(); the registration order is the display order.
func Register(d Descriptor) {
	if d.Kind == "" {
		d.Kind = "builtin"
	}
	if d.Category == "" {
		d.Category = CategoryOther
	}
	if _, ok := registry[d.Name]; !ok {
		order = append(order, d.Name)
	}
	registry[d.Name] = d
}

// Get returns the registered (compiled) target system for a name.
func Get(name string) (System, bool) {
	d, ok := registry[name]
	if !ok || d.System == nil {
		return nil, false
	}
	return d.System, true
}

// Describe returns the descriptor of a compiled target system — for Control
// Plane decisions that need the metadata rather than the implementation
// (e.g. NoCredentials in the broker).
func Describe(name string) (Descriptor, bool) {
	d, ok := registry[name]
	return d, ok
}

// All returns all registered descriptors in registration order.
func All() []Descriptor {
	out := make([]Descriptor, 0, len(order))
	for _, name := range order {
		out = append(out, registry[name])
	}
	return out
}

// Shutdown hooks: plugins with local state in the sandbox (e.g. the process
// supervisor of the dev plugin) register their cleanup code here. The daemon
// calls Shutdown() when shutting down — otherwise started processes (Chrome,
// dev servers) would outlive the sandbox.
var shutdownHooks []func()

// OnShutdown registers a cleanup hook (called from init() or on first use; not
// concurrently with the registration).
func OnShutdown(fn func()) { shutdownHooks = append(shutdownHooks, fn) }

// Shutdown runs all cleanup hooks (idempotent, in registration order).
func Shutdown() {
	for _, fn := range shutdownHooks {
		fn()
	}
}
