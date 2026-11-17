# Trusted behavior providers (Phase 38, initial slice)

This is an **opt-in, in-process Go extension point for trusted application code**, not a plugin loader or sandbox. Neither arbitrary native Go plugins nor untrusted scripts are supported. The first slice is upstream-generic: there are no downstream colony-autonomy policies, new jobs, or custom content types.

## Contract and opt-in host

`internal/application/game/behavior` defines the value-only `Provider`, `Manifest`, `World`, `Intent`, `Proposal`, `Registry`, `Migrator`, and `StoredState` contracts. `APIMajor/APIMinor` are currently **1.0**. Each provider declares a namespaced ID (for example `example.builder`), supported API version, explicit capabilities, a positive `EveryTicks` cadence, and a positive `StateVersion`. Registration rejects invalid/duplicate IDs, unsupported API versions and capabilities, and malformed manifests; execution order is ascending provider ID, independent of registration order.

The application adapter `internal/application/behaviorhost` attaches a trusted provider explicitly to an existing game and translates permitted proposals into native game commands. The game does not discover providers, import the host package, or grant direct simulation access.

```go
// Trusted application wiring, before the simulation begins:
g := game.New(game.Config{Seed: 1, Width: 64, Height: 64})
runner, err := behaviorhost.Attach(g, myTrustedProvider)
if err != nil { return err }

// Save/loading is the ordinary game format; install providers after loading:
loaded, restoredRunner, info, err := behaviorhost.LoadWithProviders(reader, myTrustedProvider)
_ = runner
_ = loaded
_ = restoredRunner
_ = info
```

The `Runner` exposes `Export()` for current provider states, `Missing()` for preserved saved states whose providers are not installed, and `Failures()` for per-provider diagnostics. Reattaching with a different trusted provider set preserves the current live state; no loader, dynamic linking, script interpreter, or hot-reload lifecycle is provided.

## Read-only queries

Every due provider receives a **detached** value-only `World` snapshot and a copy of its own opaque state. `World.Maps` is ordered by map ID and covers all open maps. The first slice exposes colonist/pawn read models, building/frame/item stacks, stockpile cells, visible rock/ore, rooms and research. Nested cell slices and all entity slices are copied separately for each provider. Callers receive no authoritative entity-store or mutable job pointers. The public game read model does not yet expose movement regions, so `Map.ColonistRegions` is empty in this host and `ColonistRegionsAt` returns -1. This is **not** a general-purpose pathfinding query.

These are Go values, not a security boundary against malicious code running in the same process; trusted Go code can still access globals or OS APIs outside the contract.

## Capabilities and native commands

`world.read` and `intent.propose` are mandatory. Other explicit capabilities gate these intents:

| Intent | Required capability | Native command |
| --- | --- | --- |
| `place_frame` | `command.place_frame` | `DesignateBuild` (new frame) |
| `swap_material` | `command.swap_material` | `DesignateBuild` (same building, new allowed material) |
| `designate_mine` | `command.mine` | `DesignateMine` |
| `designate_deconstruct` | `command.deconstruct` | `DesignateDeconstruct` |
| `add_bill`, `set_bill` | `command.bill` | `AddBill`, `SetBill` |
| `mark_haul`, `set_haul_priority` | `command.haul` | `SetHaulMark`, `SetZonePriority` |

A proposal contains up to **64 intents** and up to **64 KiB of replacement state**. The registry checks permissions, and the host validates **all** intents (including map, target, coordinates and definitions) before queuing any of them. The native command phase applies the queued commands on the **next simulation tick**, using the current authoritative game state and its normal rejection rules; an accepted proposal is not a guarantee that a later native command succeeds. Providers do not directly complete construction, insert items, mutate game jobs, or consume the simulation RNG.

## Determinism, errors and persistence

After every map completes its simulation phases and pending map lifecycle operations finish, due providers run synchronously on the game writer in **ascending ID order**, before advancing the clock. `Tick % EveryTicks == 0` determines each due run. Providers due on one tick observe the same world snapshot; state and command execution from one provider does not mutate that snapshot for another. The host offers no independent clock, RNG, network, filesystem or background execution hook. Deterministic policy and canonical state encoding remain the provider author's responsibility.

Errors or panics in `Propose`, unauthorized actions, or failed host validation are recorded in `Runner.Failures()` with provider ID and tick. A failed proposal commits **neither** its replacement state **nor** its native commands and does not stop later providers. The diagnostics describe the most recent tick on which one or more providers ran; they are not a historical event log. There is **no instruction budget, memory isolation, or protection against arbitrary trusted-Go side effects**.

`saveState.BehaviorStates` is an **additive** gob field: namespaced ID, state schema version and opaque bytes, exported in sorted order with detached byte storage. The existing `SaveVersion`, older save fixtures and no-provider state hashes are preserved. `game.Load` retains unknown provider states without installing code; loading again without those providers does not discard the bytes. `behaviorhost.LoadWithProviders` restores saved data before registering providers; older versions require the provider's `Migrator`, and newer saved versions are rejected for that provider. Current runtime state is available through `Runner.Export()`; `game.BehaviorStates(g)` is a read-only snapshot of the *retained load-time state*, not live `Runner` state. The canonical game state hash includes nonempty provider state and does not vary merely because no provider was attached.

The save header's `Content.Plugins` still contains only `["core"]`. Mod discovery, plugin IDs/content hashes in save metadata, dependency ordering, missing-plugin UI, and compatibility prompts are **Phase 37** integration work.

## Verification and follow-ups

The targeted race tests cover manifest validation, provider ordering, detached snapshots, capability/validation atomicity, failure isolation, native frame commands on home and secondary maps, save/load/replay, provider migration, reattachment without state rollback, and unknown-provider preservation. The architecture guard verifies dependencies and package budgets. The wider upstream race suite is run separately by the integration owner and may include unrelated Ebiten allocation and graphical-environment tests.

Phase 38 still needs sandboxed scripting (Starlark/WASM), instruction/resource bounds, typed event and custom component hooks, richer query and command surfaces, plugin-facing diagnostics and UI; none are supplied by this trusted Go slice.
