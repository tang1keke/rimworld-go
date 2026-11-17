# Core Vanilla faction relationships and diplomacy: clean-room design

2026-11-17. Branch: `pr/vanilla-faction-core`, based on `pr/vanilla-visitor-system` 233359e96. DLC excluded.

## Sources and constraints

- [Factions](https://rimworldwiki.com/wiki/Factions): civilian and hostile outlanders, gentle/fierce/savage tribes and pirates; hidden ancient, insect and mechanoid groups; goodwill thresholds, natural range, trading bonus.
- [Tribes](https://rimworldwiki.com/wiki/Tribes), [Trade](https://rimworldwiki.com/wiki/Trade), [Comms console](https://rimworldwiki.com/wiki/Comms_console): tribal radio-aid restriction, 19 goodwill caravan request, 31 goodwill aid, cooldowns, requested traders and trade behavior.
- Different public articles quote 15 goodwill for called caravans; the detailed console reference currently specifies 19. This implementation explicitly chooses the latter.
- Code follows `.agents/specs/CLEAN_ROOM.md`: published mechanics and permitted numbers are references. No original source code, disassembly, XML, hidden game data, names, descriptions or assets have been copied. Names, colors, look and trade-price interpolation are authored for this project.

## Original code gaps

| Owner | Before | Implemented |
| --- | --- | --- |
| `domain/faction` | Four normal archetypes; -50/-10/+50/+30 relationships | Six human archetypes; -75/0/+75/0 thresholds with hysteresis |
| `domain/faction` | Goodwill drift 1 per day toward arbitrary baseline | Published natural bands, saved five-day 0.2 / -0.4 fractional phase |
| `domain/faction` | Gift cost fixed 40 per goodwill; no real deal relation reward | Goodwill-dependent 40..160 cost with independently interpolated 2,041/3,661/5,539 silver public benchmarks for +40/+60/+75, 25% base-bonus, and +1 per 500 traded silver |
| `game` + `maps` | Player hostility substituted for two factions' mutual war | Actual saved symmetric `Enemies` governs inter-faction combat and base destruction |
| `visit`, `comms` | Neutral callers accepted; tribal military aid accepted | Allied-only caravan calls, industrial-allied-only aid; neutral on-map visitors still eligible |
| `tradeview` + Ebiten dialogs | Gift previews lacked actual faction identity | Typed faction state carried to read model and preview, domain and UI agree |

## Migration contract

- Keep existing serialized type IDs 0..5 unchanged; append FierceClans, SavageClans, AncientQuiet, AncientHostile, MechHive. Only ordinary surface human types get regular settlements and visit/raid participation.
- Keep existing world save version. Only additive `Faction.DriftDay` and `Faction.TradeRemainder` fields. The old fields, faction IDs, map sites and their histories are preserved. `setupview.CloneFactions` now clones `Enemies` as well as histories, so faction war lists are independent after copying.
- Existing locally authored icons are reused for new archetypes: clan variants share `faction_clans`, old-kin shares `faction_settlers`, lost-kin shares `faction_freebooters` and mechanical host shares `faction_swarm`. Distinct map accent colors preserve visible differentiation without copied imagery.
- Legacy v12 worlds reconstruct their wars with the existing v17 `SetEnemies` migration. Changing the new rough/fierce/savage faction hostility rule changes that real migrated state. Restoring the previous war rule alone makes the old hash pass, proving cause. Original fixture bytes remain unchanged. Additive zero-valued counters alone do not affect hash.
- Wars remain deterministic, mutual, sorted and persisted. Dynamic faction wars, full hidden-NPC identity integration and original per-trade value conversion are future separate work.

## Test and architecture evidence

- New `game/faction_vanilla_core_test.go`: six archetypes, type-ID stability, visitor and raider selection, hidden filtering, war vs player relations, actual sale goodwill, saved 5-day and trade-residual replay.
- `domain/faction` and `trade` tests: exact goodwill thresholds, natural decay, permanent hostility, costs, aid eligibility, cooldown, player relationships, fractional payouts and deterministic world seed.
- Focused `go test -race` across domain, maps, visit, travel, tradeview, dialog and the affected game tests PASS. `go build ./...`, related `go vet`, `make arch` PASS.
- `internal/arch/solid_budget.txt` has two stale presence exceptions removed: mutable `init` registration and global `defs` mutation eliminated. No upward budget adjustment.

## Downstream boundary

S1 counterpart: `release/vanilla-faction-core-20261117`, `docs/vanilla-faction-system-design-20261117.md`. S1-specific ColonyManager radio gift ceiling (200 to 500 silver) and automated behavior tests stay downstream; upstream code has no account/AI policy. This is a local PR candidate after the visitor lifecycle branch. No remote push, PR, main merge or deployment.


## Follow-up review (2026-11-18)

- Fixed an original combat-fixture assumption: two hostile-to-player factions now receive explicit mutual Enemies before checking actual pawn-on-pawn fighting. A full upstream game test run exposed the fixture, while focused tests had passed.
- Replaced a brittle globe test demand for all five road classes in one random world with two independent assertions: the complete class legend and a generated path visible zoom behavior.
- Published wiki cumulative gift-cost examples are directly asserted: goodwill 0 to 40 for 2,041 silver, 0 to 60 for 3,661 and 0 to 75 for 5,539. The intermediate steps remain independently authored (not copied from any proprietary content). War-time trade does not credit goodwill or saved trade carry.
