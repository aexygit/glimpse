# Protocol research (Task 5 Step 1)

Source read: github.com/nuriland/a2kit v0.5.0 (MIT) from the module cache. Citations are `file:line` inside that module. This document feeds Task 5b (the inspect decoder); it decodes nothing itself.

Status: the frame layout and reader conventions are read straight from source and solid. Section 4 is a hypothesis list, unverified. Section 5 is the runbook for the user's recording (Step 2).

## 1. Frame layout

- `wire.Frame` (wire/frame.go:26-33): `Time`, `Src`/`Dst` (`netip.AddrPort`), `Opcode`, `Flags`, `IfIndex`, `Payload []byte`. The payload is a copy the caller owns (frame.go:25).
- `Opcode` is a `uint16`: the first two bytes of the frame body read little-endian, so wire bytes `04 38` are `0x3804` (wire/opcode.go:9,13). Its `String()` prints wire order `"04 38"` (opcode.go:19). The high byte is the "family".
- `Payload` is everything after those two opcode bytes: the decoder does `Opcode: opcode(body)`, `Payload: keep(body[2:])` (wire/decoder.go:183,200). Payload offset 0 is the first byte after the opcode.
- `Flags` is a `uint32` bit set (wire/frame.go:15-21): `FromServer`, `FromClient`, `WasLZ4`, `WasBundled`, `Resynced`. The direction flags are spelled `FromServer` ("sent by the locked server") and `FromClient`. Exactly one is set on a locked stream. Client bodies are encrypted and dropped unless `Config.EmitClient` is set (wire/decoder.go:17,188-190), so inspect decoding only ever sees `FromServer` frames. `WasLZ4`/`WasBundled` mean the frame came out of an `FF FF | u32 size | LZ4 block` bundle that `wire` already unwrapped (wire/framer.go:341-366); the payload is plain either way.
- On the wire a frame is a LEB128 length varint, then the body (opcode + payload). The varint value `v` is `len(body)+4`: the frame occupies `head + v - 4` bytes and `v < 6` is rejected, no room for an opcode (wire/framer.go:183-192). `maxFrame` is 1 MiB (framer.go:13). Some streams add a `u32le` envelope header in front of frames; `wire` strips it (wire/envelope.go:7-13). A decoder in `internal/protocol` never sees any of this.
- Integer encodings. `wire/varint.go` holds only the unexported frame-length reader: LEB128, at most 5 bytes, 32 bits, must be minimal (varint.go:3-24). The payload encodings are in `game/reader.go`, all little-endian:
  - fixed width: `u8` (reader.go:50), `u16` (58), `u32` (66), `u64` (74), `f32` (82), `pos` = three `f32` (85)
  - varint: `varint` is LEB128 via `binary.Uvarint`, at most 5 bytes, must fit `uint32` (reader.go:88-99); `entity` is a varint (102); `small` is a varint that must be <= 255 (111-117)
  - strings: `name` is a `u8` length then that many bytes, must be valid UTF-8 (reader.go:120-126)
  - `skill` and `npc` ids are fixed `u32` (reader.go:105,108); stat rows use `u16` id + `u32` value (game/parse.go:279-280)
- `reader.varint` is looser than the frame-length varint: it uses `binary.Uvarint`, so it does not insist on a minimal encoding.

## 2. Reader conventions

- `game.Parse(f)` returns `ErrUnread` for any `FromClient` frame and for an opcode with no reader, builds `reader{p: f.Payload}`, calls the handler, and turns `r.bad` into `ErrLayout` "at byte i of n" (game/parse.go:16-33, game/errors.go:5-13). The handler returns an `Event` and never errors itself.
- `reader` (game/reader.go:12-16) holds `p`, `i`, `bad`. The first failed read sets `bad` and freezes `i`; every later read returns zero. A short read is therefore silent mid-handler and only surfaces once, after the handler, as `r.bad`. `take(n)` checks `n > left()` before slicing (reader.go:28-36), `peek` returns nil when short (42-47), `end()` sets `bad` if bytes remain (21-25).
- Handler shape (all in game/parse.go): read fields in order into a struct, set `r.bad = true` when a value contradicts the layout, call `r.end()` unless the tail is open-ended, return the struct.
  - `gearScore` (parse.go:137-144): `GearScore{Now: r.u32(), Best: r.u32()}`, `bad` if `Best < Now`, `r.end()`. Payload is exactly 8 bytes. Type: game/stats.go:109-112.
  - `combatPower` (147-154): same with `u64`, 16 bytes. Type: stats.go:114-117.
  - `sheet` (259-264): `u16` that must be 0, then `rows(r, Stats{Full: true})`. `sheetUpdate` (267) reads an entity varint first instead.
  - `rows` (270-288) is the best model for a list: read the count as a varint, then guard it against the bytes left (`n > uint32(r.left()/6)`, six bytes a row) before allocating or looping, read each row, check the trailer (base HP repeated, then a zero `u32`), then `r.end()`. Inspect will be a list of items, so it should look like this.
  - `kill` (157-165) and `characters` (410-440) show strings (`name`) and sub-records inside a list, with a length byte read before the loop.
- Convention for `internal/protocol` (Task 5b), copied from the above:
  1. Return nil, nil unless `f.Opcode == OpInspect` and `f.Flags&wire.FromServer != 0`.
  2. Read through a small sticky-error reader, same contract as a2kit's: first failure sets `bad`, later reads return zero, one check at the end.
  3. Bounds-check every `take`/`peek` against remaining bytes, and check every count against the bytes left (divide by the minimum record size) before allocating a slice.
  4. Reject, do not guess: a value outside the known layout sets `bad`; finish with an `end()` check that fails on trailing bytes.
  5. Never panic on short input. The plan's truncation test cuts each fixture at every length, so a short buffer must give an error, not an index panic.
  - a2kit's `reader` is unexported, so Task 5b writes its own (about 30 lines). It copies the idea, not the code, which is MIT and credited in the README either way.

## 3. Opcode landscape

The table is `game/opcode.go:14-60` (`var opcodes = map[wire.Opcode]op`), grouped by comment exactly as a2kit groups it. Format: `value Name (reader)`; `nil` means named but not read.

- login, world, clocks (opcode.go:15-27): `0x3600 Tick (tick)`, `0x3603 Ping (ping)`, `0x3611 Handshake (nil)`, `0x3615 Login (nil)`, `0x361A NameCheck (nameCheck)`, `0x3623 Zone (zone)`, `0x3633 Self (self)`, `0x3641 Spawn (spawn)`, `0x3642 Death (death)`, `0x3645 Player (player)`, `0x3649 Stats (sheet)`, `0x364A StatsUpdate (sheetUpdate)`
- lobby (29-36): `0x3901 LobbyHandshake (nil)`, `0x3903 LobbyPing`, `0x3906 Account`, `0x3909 Servers`, `0x390B Characters`, `0x390D Join (nil)`, `0x390F Redirect`
- movement (38-42): `0x371A Move (moveA)`, `0x371B Move (moveB)`, `0x371C Move (moveB)`, `0x371D Move (nil)`
- skills (44-49): `0x3802 Cast`, `0x3804 Hit`, `0x3805 DoT (nil)`, `0x3806 CastEnd`, `0x3817 Fall`
- the inventory and the gear (51-53): `0x561D GearScore (gearScore)`, `0x3656 CombatPower (combatPower)`
- ungrouped tail (55-59): `0x8D00 Gauges`, `0x8D04 Kill`, `0x8D2F Notice (nil)`, `0x921B HP (nil)`, `0x9702 Party (nil)`

There is no inspect opcode in a2kit v0.5.0. Nothing in the table is an inspect, an equipment list, or an item. `OpInspect` has to be found from a recording; it cannot be read off a2kit.

Named but not read (`read == nil`, opcode.go): `0x3611 Handshake`, `0x3615 Login`, `0x371D Move`, `0x3805 DoT`, `0x3901 LobbyHandshake`, `0x390D Join`, `0x8D2F Notice`, `0x921B HP`, `0x9702 Party`. Seven carry a source comment saying what they are (login handshake, login time and account, a Move sibling, lobby handshake, server picked, text to players, HP unsure); `0x3805 DoT` (opcode.go:47) and `0x9702 Party` (opcode.go:59) have none, only their names. None of the comments or names describes gear.

Gear-adjacent opcodes:
- `0x561D GearScore`: own gear score, `u32 Now`, `u32 Best` (parse.go:137-144)
- `0x3656 CombatPower`: `u64 Now`, `u64 Best` (parse.go:147-154)
- `0x3649 Stats`: the player's full stat table, "only visible to the player" (game/stats.go:9); `0x364A StatsUpdate` carries an entity id so it can describe other entities (parse.go:267)
- `0x3645 Player`: another player entering view, entity id and name only (parse.go:168-182)

Opcodes `wire` knows for its lock but `game` does not name (wire/opcode.go:49-58 minus the table): `0x3822`, `0x382A`, `0x382B`, `0x382C`, `0x3835`, `0x3847`, `0x3640`, `0x3646`, `0x3647`, `0x8D21`, `0x9200`, `0x920D`. "Known" means the lock counts them, so they are things the game sends often.

Families `wire` treats as plausible when it hunts (wire/opcode.go:62-68): high bytes `36 37 38 39 56 8A 8D 92 96 97`. A frame outside these is still accepted once a stream is aligned and trusted, because the plausibility check only runs for untrusted streams (wire/framer.go:197-203); and Glimpse does not set `KnownOnly` (internal/capture/capture.go:60,137), so an unknown inspect opcode is not filtered.

## 4. Candidate narrowing (unverified)

Hypotheses only. Nothing here is a finding until a recording confirms it. Ordering is by how well the hint fits, not by evidence.

1. Family `0x56`, siblings of `0x561D` (`0x56xx`). It is the only family whose sole named member is gear, and a2kit's own group comment lumps `0x561D` under "the inventory and the gear" (opcode.go:51). An inspect response is an inventory-shaped list of items, so the nearest unnamed `0x56xx` opcode is the best first guess.
2. Unnamed `0x36xx` next to the player/stat cluster: `0x3646`, `0x3647` sit between `Player 0x3645` and `Stats 0x3649`, and `0x3640` precedes `Spawn`. Weaker, because they are in `known` (wire/opcode.go:51), which means they arrive in ordinary traffic and an on-demand inspect should not be constant background. An inspect may also arrive as several frames: a `0x3645` for the inspected entity plus a `0x364A` stats update for that entity (Stats.Entity is non-zero for other entities, stats.go:9) alongside the item list.
3. Unmapped families `0x8A` and `0x96`: `wire` accepts them as plausible (opcode.go:64) but `game` names nothing in them. Nothing points at gear, so low, but cheap to rule in or out.
4. The named-but-unread set (list above). Low: every entry is already described as something else. `0x9702 Party` is the only one that concerns other players, and a party member list is not an inspect.
5. A structural filter that does not depend on guessing the number: inspect is on demand, once per inspect action, and large (up to ~18 item records, each with an id, enchant, rolls and sockets, plus name, class, level), so expect a payload of hundreds of bytes, `decoded` = 0, `idle/s` near 0 and a count equal to the number of inspects. This is an inference from the item shape, not from a2kit.

The inspect request itself is invisible: client frames are encrypted (wire/decoder.go:17). `a2k stats -actions` still lists the moments the client sent an unusual-size packet and what the server answered (section 5), which is the practical way to tie a server opcode to an inspect click.

## 5. Step 2 runbook for the user

All of this was checked against `cmd/a2k/` v0.5.0. The plan mentions `a2k stats` listing "unread opcodes"; the real command lists every type with a `decoded` count, so read it as below. There is no `a2k stream` command (there is a `-stream` flag on `show`/`export`, for bare server streams), and `record` has no `-o`.

Install (a2kit README "CLI" section): `go install github.com/nuriland/a2kit/cmd/a2k@v0.5.0` (a2kit needs Go 1.26, same as Glimpse), or download a build from github.com/nuriland/a2kit/releases. Pinning v0.5.0 matches the decoder Glimpse links. `a2k help` lists the commands.

Capture. Open the terminal as Administrator: live capture uses pktmon and needs it (capture/live_windows.go:49-50; the `record` help says "Needs admin (root) privileges", cmd/a2k/record.go:17). No Npcap is needed.
- `a2k adapters` lists capturable adapters (cmd/a2k/adapters.go:10-14). The default on Windows is `nics`: every adapter at once, VPN tunnels included, capturing all TCP traffic on the machine (options.go:65, capture/live_windows.go:125-133). The pcap holds everything, not just the game, so a long `-for 30m` session can get large.
- `a2k record -log inspect.jsonl inspect.pcap` records both (record.go:17-21,24,34-35). `FILE` is positional and writes a pcap of all TCP traffic; `-log FILE` writes a log of the game's messages only. At least one is required. Use both: `stats` needs the pcap, the log is the shareable one.
- Stop with Ctrl-C, or pass `-for 30m` (options.go:65-66). `-adapter NAME` picks an adapter. It prints `a2k: found the game at <ip:port>` when it locks (record.go:77).
- Start recording before you enter the world (ideally before launching Aion 2), so the connection is seen from its first bytes. The lock needs 3 known frames or 2 combat frames (wire/decoder.go:259-262), after which frames seen earlier are queued in. Not tested here.
- Inspect several characters, one at a time, and write down the wall-clock time of each inspect plus each target's visible gear. Leave a few quiet seconds between inspects.
- The pcap holds your account traffic: do not share it. Anything meant for the repo goes through `a2k export` (below), which drops Login, Account and Characters unless `-account` is passed (cmd/a2k/export.go:14-19, options.go:56).

Find the candidate with `a2k stats`:
- `a2k stats inspect.pcap` prints one row per server message type (cmd/a2k/stats.go:160): `type` (wire-order bytes, `1D 56` for 0x561D), `name` (`-` if a2kit has none), `count`, `share`, `size` (payload bytes after the opcode, one value or a range), `decoded` (messages a2kit's reader accepted), `failed` (reader rejected the layout), `note` (`known` = the lock uses it, `background` = sent whether or not the player acts). It needs a pcap/pcapng recording, not a log (analyze.go:42-43).
- `a2k stats -at 21.05s,1m13s inspect.pcap` adds `marks` (how many of your marked moments the type followed, as `n/N`), `within` (messages inside any mark window) and `idle/s` (rate outside the windows). Marks are offsets from the first message, as `a2k show` prints them, or local clock times like `10:53:41`. `-window` (default 1s) is how long after a mark counts as the answer. An inspect opcode should have `name` `-`, `decoded` 0, `marks` `N/N`, and a low `idle/s`.
- `a2k stats -actions inspect.pcap` lists when the client sent an unusual packet and which server types followed (stats.go:44).
- `a2k stats -type "1D 56" inspect.pcap` dumps one type: per-byte value histogram plus the first 8 messages, each cut to 32 bytes (stats.go:226-228,232-266). The argument is wire order (`parseOpcode`, stats.go:345-351), so 0x561D is `1D 56`.

Dump full payloads for one opcode:
- `a2k export inspect.pcap -o inspect.jsonl` writes an a2log file (account opcodes removed). Each line has `opcode` (`"1D 56"`), `flags`, and `payload` as base64 (a2kit README, a2log section). With jq: `jq -r 'select(.opcode == "1D 56") | .payload' inspect.jsonl`, base64-decode each line to get the bytes. Without jq, in PowerShell, this writes each matching payload to `payload_1.bin`, `payload_2.bin`, ...: `$i=0; Get-Content inspect.jsonl | ForEach-Object { $_ | ConvertFrom-Json } | Where-Object opcode -eq '1D 56' | ForEach-Object { [IO.File]::WriteAllBytes("$PWD\payload_$((++$i)).bin", [Convert]::FromBase64String($_.payload)) }`. Use `-o`, not `>`: on Windows `>` writes UTF-16 (options.go:44-46).
- Without jq: `a2k show -json -o inspect.json inspect.pcap` writes one JSON object per message with `type` (`"1D 56"`) and `payload` as spaced hex, present only for messages a2kit did not decode (print.go:158-167,180). Filter it with PowerShell `Get-Content inspect.json | ConvertFrom-Json | Where-Object type -eq '1D 56'`. `-wire` adds length per message without bytes (print.go:54).
- `a2k show inspect.pcap` prints one human line per message, undecoded ones as `(not decoded, N bytes)` (print.go:98-111).

## 6. Slot and stat key tables

Our decoder maps numeric positions onto the NC ids the site API returns (plan, Task 5; docs/research/item-source.md). Copied verbatim:

- Slots: `MainHand`, `SubHand`, `Helmet`, `Shoulder`, `Torso`, `Pants`, `Gloves`, `Boots`, `Cape`, `Belt`, `Necklace`, `Earring1`, `Earring2`, `Ring1`, `Ring2`, `Bracelet1`, `Bracelet2`, `Amulet`, plus wing.
- Stat ids seen so far: `WeaponFixingDamage`, `WeaponAccuracy`, `Critical`, `Block`, `STR`, `MPMax`.

### a2kit's numeric stat ids

a2kit has the only numeric stat-id space we can read: `statNames` in game/stats.go:24-107 maps a `Stat` (`uint16`, stats.go:15) to a snake_case name. It lists the ids the `0x3649 Stats` sheet carries, for example `1` might, `3` intelligence, `4` constitution, `38` critical_attack (stats.go:38), `255` block (stats.go:55), and four genus pages of eight rows generated from id 393 up (stats.go:101-105). Rows are read as `u16` id then `u32` value (game/parse.go:279-280).

If the inspect payload stores item stats with the same row shape and id space, Task 5b can map numeric ids to our NC keys (`Critical`, `Block`, ...) from this table instead of guessing. That is unverified: a2kit's names are the client sheet's, not the NC site's, so each id still needs checking against a known item in Step 3. Only the shape (`u16` + `u32` rows behind a varint count, guarded by `r.left()/6`) is a convention to reuse.

Note: the wing slot has no seen key spelling yet, so Task 5b should confirm it. The numeric-position-to-name table (which byte or index in the inspect payload is which slot) cannot come from a2kit and is Step 3's job, once the payload is diffed against known gear.

## 7. Decided layout (Task 5b, supersedes the hypotheses in section 4)

Section 4 and the `OpInspect` convention in section 2 are obsolete: there is no inspect opcode. A
17 second capture showed that visible equipment rides inside the `Player` (0x3645) proximity
broadcast, which a2kit parses only up to the name. Findings below come from two real captures (22
characters). Implemented in `internal/protocol/player.go`; fixtures in `internal/protocol/testdata/`
(names redacted).

### Header

`varint entity` (2 bytes in all captures), 4 byte mask (skipped; it differs between same-class characters, so it is not a class), `u8
flags` (bit 0 set: a name follows), `u8 name length`, UTF-8 name, `u32 LE` which reads 36, 32, 16, 36
across the first four fixtures. It is taken as the character level: it matches the skill id tier in the
region before the gear (147x at 16, 177x at 32, 187x at 36), but nothing in the capture states a level,
so the decoder drops values outside 1..99.

### Gear array

15 fixed-width little-endian records, slot index 1..15, found by scanning (its offset was 480, 670, 671
and 747 and tracks the size of the skill region before it):

```
+0  u8   header byte, varies (0f, 0e, 06, 0a), meaning unresolved
+1  u32  item id, 0 for an empty slot
+5  u8   enchant level, 0..18 observed
+6  u8   slot index, 1-based, increments by 1 record to record
+7  8 zero bytes
+15 optional byte, then u32 instance uid, then 00 03, then bytes up to the record end; the
    uid layout varies per record and +21..+32 is nonzero in 66 of 238 occupied records (unexplained)
```

The gap from one record to the next is a lattice, measured over 308 gaps in 22 characters with no
exception. After an empty record it is 21 or 22. After an occupied record it is 33 or 34 (238 samples),
plus one extra block of 28 or 29 bytes per step: 62, 90, 91, 118, 119 and 147 were seen, and the lattice
admits 175/176 and beyond up to `maxGap` (256). The decoder only accepts a next record at a lattice gap,
nearest first, so a record-looking fragment at any other distance is ignored and the failure mode for a
legitimate off-lattice gap is a short gear list, not a fabricated item. Widen the `blockMin`/`blockMax`
or base constants in `player.go` to admit more. A valid record has the next index, eight zero bytes after
the enchant, and an id that is 0 or nine digits. Only the first record (index 1) must be in the
110/210/310 families; later ones accept any nine-digit id, so an unseen item family cannot end the run.
Known limit: a fragment at gap 33 or 34 is on the lattice and nearer than a real successor in an extra
block, so it would win; no capture has one. The longest chain starting at index 1 wins, at most 32
records are read (the array is always 15), and fewer than 6 records is rejected. There are no stat rolls or sockets in a
record, so grading falls back to catalog base stats.

### The skill list trap

Before the gear sits a list of nine-digit ids, each followed by eight 0xff bytes, in the same numeric
band as the gear. They are skill ids (they match the `Cast` 0x3802 id space) and their tier tracks level;
the two level-36 fixtures share 10 of 11 while differing in 7 of 11 gear slots. A scan by id band alone
reads skills as gear. What separates them is the incrementing slot index at +6 and the zero run at +7.

### Slot table (complete for Player)

| index | slot | confidence |
|---|---|---|
| 1 | MainHand | family varies by weapon type, only slot with header 0f |
| 2 to 8 | Helmet, Shoulder, Torso, Pants, Gloves, Boots, Cape | confirmed by NC item categories, identical across 22 characters |
| 9 | Necklace | confirmed |
| 10, 11 | Earring1, Earring2 | both earrings; which is which is unproven (six characters wore two different earrings and index 10 always held the equal or higher grade) |
| 12 to 15 | none | empty on all 22 characters |

The array is always exactly 15 records, 11 of them can be occupied, and no payload has a record past
index 15. SubHand, Belt, Ring1, Ring2, Bracelet1, Bracelet2, Amulet and wing never appeared at any index,
so they are probably not carried in Player. The slot comes from the wire
index, never from the id family. The table is the `slotNames` map; an index not in it is reported as
`Slot<N>` with its id and enchant (kept as a fallback). Empty records are not reported.

### Not decoded

- `Class`: not found. The mask is not it (fixtures 1 and 4 share 10 of 11 skills but have different masks).
  Even a field that separates classes would need labelled characters to map to class names.
- 0x561C, equipment change: 7 byte header (`c9 30 1c 09 00 00 01` or `92 32 xx xx 00 00 01`, unresolved),
  then the old slot record, then the new slot record, then a `0e` terminator. Each record has the
  Player gear layout above. An unequip has the new id 0, an equip the old id 0. It may only fire for the
  user's own gear. Not decoded in v1.
