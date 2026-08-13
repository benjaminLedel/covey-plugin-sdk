# covey-plugin-sdk

The contract between [Covey](https://github.com/benjaminLedel/covey) and a **target-system plugin**: the interfaces a plugin implements, the registry it enters itself into, and the few helpers it needs along the way.

```go
import "github.com/benjaminLedel/covey-plugin-sdk/target"

func init() {
    target.Register(target.Descriptor{
        Name: "redmine", Label: "Redmine", Category: target.CategoryTicketing,
        System: System{}, Scopes: []string{"read", "comment"},
    })
}
```

## Why a module of its own

A plugin author depends on the **contract**, not on Covey. Covey's own tree carries Postgres, a browser driver, a wasm runtime and a web frontend; none of that has any business in somebody's plugin. This module has **no dependencies beyond the standard library**, and it is the only thing a plugin needs to compile.

It also keeps the graph acyclic: Covey depends on this, plugin repositories depend on this, and Covey's default build depends on the plugin pack. Nothing depends on Covey.

## What is in here

| | |
|---|---|
| `System` | what every plugin implements: name, actions, guard-rail subjects, prompt documentation |
| `Descriptor` + `Register` | how a plugin announces itself; the registry both the control plane and the sandbox daemon read |
| Optional interfaces | `Webhooker` (inbound events), `Prober` (connection test), `WorkChecker`/`SignedWorkChecker` (`nur-wenn:` gating), `ScopedDocSystem` (narrow the doc to an agent's scopes) |
| `Credential` | the brokered base URL and token — handed in per call, never stored by a plugin |
| Context helpers | `Workdir`, `EmitArtifact`, `SubAgent` — the sandbox capabilities a plugin may use when Covey grants them |
| `Client` | the HTTP client to use, so a plugin's traffic ends up in Covey's request log instead of being invisible |
| `StoreFile`, `StoreStream`, `PruneOldCheckouts` | the small helpers plugins kept reinventing |

## Writing a plugin

Compiled plugins are one way. A plugin can also be a **JSON manifest**, an **MCP server** or a **WebAssembly module** — those need no rebuild of Covey at all and are the usual choice for third parties. See [covey-plugins](https://github.com/benjaminLedel/covey-plugins) for the catalogue and [covey-plugin-template](https://github.com/benjaminLedel/covey-plugin-template) for a starting point.

Whichever you pick, the same enforcement applies: the secrets broker hands over credentials per call, the guard rails govern every action by its subject, and the recording sees all of it.

## Versioning

This module is the contract, so it changes carefully. Additions (new optional interfaces, new helpers) come in minor versions; a plugin that does not implement them keeps working, because every optional interface is checked and not assumed.

## Licence

MIT — see [LICENSE](LICENSE).
