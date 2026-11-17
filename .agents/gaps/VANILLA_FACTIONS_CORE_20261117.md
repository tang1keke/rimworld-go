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

## Hidden factions: actor identity follow-up (2026-11-18)

### Evidence

- Public [Factions](https://rimworldwiki.com/wiki/Factions), [Mechanoid hive](https://rimworldwiki.com/wiki/Mechanoid_hive) and [Ancients](https://rimworldwiki.com/wiki/Ancients) describe hidden mechanoid, insect and ancient affiliations. Only observable mechanics and allowed public values are used.
- Current `raid/mech.go` mechanoids, `raid/hive.go` insects, vault defenders and casket insects are spawned into the authoritative animal store. Their immutable species already identifies their group, including after save/reload. `combat/engage.go` separately hardcodes Mech/Hive hostility.
- A hostile sleeper-casket human is spawned with `person.Faction=0` in `casket.go`, and has no persistent foreign origin when captured or exported; `bio_view.go` already displays factions for ordinary foreign humans and the colony emblem for owned animals.
- `personSave` serializes full biographies and faction refs. A small append-only `Hidden` faction type field can carry an ancient's original origin after their status changes, without inserting a fake world faction into `ng.factions` and without revising 1-based faction IDs.

### Contract and boundaries

- Canonical pure classification `faction.Creature(species)` returns `(Swarm,true)` for any hive species, `(MechHive,true)` for any mech species, and `(Player,false)` for ordinary wildlife. This is derived from immutable saved `animal.Species`, so introducing an additional creature faction field or migration would duplicate authority.
- The combat auto-engagement rule queries this classification. The existing machine and insect threat AI, aggro timings, EMP stun, hive breeding and loot/corpse behavior remain unchanged.
- Animal snapshot species facts and Dossier show the original project's own label/emblem for their hidden affiliation. A dormant hidden defender keeps its identity; dormancy still suppresses active threats. Normal wildlife remains `RelationWild`, and colony animals remain `RelationOwn`.
- A hostile sleeper casket spawns with a persisted `person.Hidden=AncientHostile` (zero = no hidden faction). The existing `person.Faction` and raid group remain 0. Capture/return/death preserve origin, while recruitment into the colony clears the old origin. The player UI uses the normal Dossier faction label/emblem. Friendly sleepers who join the colony immediately are owned by the player.
- No hidden group enters the world faction roster, world settlements, diplomacy, trader/caravan selection or goodwill. No new battle simulation or parallel AI path. Existing raid and animal species IDs, save version and old golden fixtures remain intact. Only an additive `personSave.Hidden` field is required.
- Faction-vs-faction dynamic war declarations remain a distinct decision. The cited Core public pages establish player-facing goodwill and faction-dependent encounters; behavior and persistence of NPC-to-NPC declarations requires a dedicated design with proven gameplay effects. Do not invent periodic random wars.

### Tests and PR

- Tests: domain species classification; actual raid mech, hive swarm, vault dormant/hive casket and hostile casket human origin; Dossier and snapshot identity; save/load/captive retention and zero defaults for older saves; existing combat auto-threat, raids, injury and save hash; upstream `make arch`; S1 affected game and same-base regression.
- Single worker owns the domain mapping, read-model projection, ancient person persistence, upstream tests, downstream port and docs. Clean Room applies throughout; no imported proprietary game identity names or art.

## Hidden faction cross-combat (2026-11-18)

### Public reference and observed gaps

- Core information: [Mechanoid hive](https://rimworldwiki.com/wiki/Mechanoid_hive) says hive mechanoids are permanently hostile to every other non-mech-hive faction. [Insectoids](https://rimworldwiki.com/wiki/Insectoids) says infestation insects are hostile to every other faction. The original game's proprietary code, XML, data and art are excluded by CLEAN_ROOM.md.
- In the present application, `raid.Raiders.target` sees only colonists/allies and human rival raiders; `raid.Mechs.pick` sees only colony/ally pawns and turrets; `rage.isFoe` sees people and tame animals, not the other hostile animal faction. Thus two hostile factions standing next to one another leave without fighting if the colony is farther away.
- `Game.creatures().grid` and `Game.nearestWhere` are existing bounded spatial indexes. Existing `raid.Tactics.Colonists` already obtains the pawn candidates. `raid.tactics.target`, `raid.Mechs.pick` and `rage.ThinkManhunter` already own targeting cadence, weapon range, sight, memory, pursuit, and lock hysteresis.
- `raid.Senses.Foes` has no consumers (the only implementation is its own adapter). It can be removed as part of a single-purpose interface improvement.

### Contract

1. Add pure `faction.HiddenEnemies(a,b)`: different factions fight when at least one is `Swarm` or `MechHive`. Same hidden faction is never a target; ordinary wild animals have no opposing faction; tame animals follow colony rules. All existing human-versus-human `Faction.Enemies` continue to own their separate world-war relationship.
2. Evolve the existing `raid.Tactics.Colonists` to `Foes(source,x,y,r,buf)`, still one method within the eight-method Tactics limit. The same colony/ally targets remain, plus the nearest active foreign hostile within a bounded radius: human raiders vs mechs/insects; mechs vs human raiders and insects. Filter stale/dead/downed/dormant creatures and prisoners; require real line-of-sight for new candidates. Use the existing indexed nearestWhere; select deterministic minimum distance then ID.
3. Raiders add this foreign candidate into their already-paced threat-weighted targeting; human rivals still use `Enemies`. Mechs choose among the same nearby foreign candidate and their existing colony/turret targets; a current standing target wins near-ties to prevent oscillation. Guards honor their post leash; attacks keep existing collision, cooldown and projectile paths.
4. Infestation insects extend `rage.isFoe` only for themselves: mechanoid creatures of a different hidden faction also qualify, while regular wildlife/manhunters retain their existing rules. All human pawns are already valid manhunter targets via `StandingPawn`. Existing `rage` lock for 180 ticks and switching margin 6 tiles remain.
5. No extra per-creature persisted field or save migration. No new faction IDs, UI or artwork. Existing `AnimalView`/Dossier and combat traces expose the battle. Neither incidental animal death nor neutral human visitors should make fictitious diplomatic or goodwill changes.
6. Real game integration tests must first reproduce missing targeting, then verify the three directions of hostility, same-side filtering, active/inactive targets, LOS, save/replay, actual HP damage and no-flap. Race, whole upstream game, S1 regression comparison and arch stay mandatory. A single worker makes compatible upstream/S1 edits; no push/PR/merge/deploy.

### Implemented and observed

- Before the patch, five cross-battle tests failed: the human raider walked toward the colony instead of fighting a nearby machine, the machine ignored an insect, and the insect chose a more distant human. New range/LOS tests and direct damage observations failed as well.
- Domain faction.HiddenEnemies is the single permanent inter-faction rule for machines/insects. combat.Tactics.RaidFoes uses a bounded creature index query, a reusable target buffer and line-of-sight; game raidTactics.Foes stays a thin adapter. The Game package architecture line budget stays fixed.
- Raiders, mechanoids, and hive rage use the original target-job/attack/movement paths; only candidate eligibility changes. Neutral visitors are recognized as foreign human faction members by mechs even though they stay outside the hostile raider AI.
- The first broad upstream game test found an obsolete casket movement assertion: a hostile ancient now stands still to attack a nearby vault insect. The test verifies actual attack traces or movement, preserving the observable activity contract.
- Regression coverage now includes human versus machine/insect, machine versus raider/visitor/insect, insect versus machine, same-faction exclusions, dormancy/LOS, positive hit damage, no-flap and save/replay. No save version, RNG draw, new ID, map-wide tick scan, assets or DLC.
- Remaining scope: dynamic non-player faction wars and independent evidence for exact item gift valuation. The present hidden-faction hostility is permanent, as published.

### Final Core animal hostility coverage

- A further check against the public wiki Mental break article showed that enraged ordinary animals pursue hostile mechanoids alongside humans. The earlier rage filter only added hive insects. A red real-game test demonstrated the gap; rage.isFoe now accepts a mechanoid for any other enraged animal, keeping the existing rage lock and chase behavior.
- Permanent machine hostility also covers colony-tamed animals. A second red actual-game test caught the prior exclusion; combat.Tactics.RaidFoes maps a tame animal to the player faction for machine/hive target selection. Human raider-to-colony-pet behavior stays on its existing target rules.
- With several foreign fighters nearby, each eligible opponent reaches the raider threat scorer. A red test showed the previous nearest-only candidate omitted a farther ranged machine; the indexed candidate list now exposes all visible active enemies to the existing threat-weighted scorer, in deterministic distance/ID competition. Scratch buffers are reused.
- Evidence: https://rimworldwiki.com/wiki/Mental_break , https://rimworldwiki.com/wiki/Mechanoid_hive and https://rimworldwiki.com/wiki/Insectoids . These are behavior references, with no original data or balance tables extracted.

### Final verification for cross-combat

- Upstream: GOMAXPROCS=4 go test -race -p 2 -count=1 ./internal/application/game/... ./internal/domain/faction PASS. go build ./..., affected go vet and make arch PASS without budget changes.
- S1 counterpart: race-enabled faction, rage, raid and combat packages PASS; real-game hidden-faction, casket, mech/hive, save-schema and old fixture regression PASS; go build ./... and affected vet PASS.
- S1 full go test -short -count=1 ./internal/application/game/... still has eight previously present failing tests. The failures have exactly the same names as the baseline before this PR, with zero new failures.
- Logs: /tmp/faction-crosscombat-release-race-20261118.log and /tmp/faction-crosscombat-s1-release-20261118.log; names: /tmp/faction-crosscombat-s1-release-fails-20261118.txt and /tmp/faction-s1-hidden-fail-names.txt.
