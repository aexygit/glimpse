The `player_NN.bin` files are Player (0x3645) payloads from two real captures of the game,
with the player names redacted. `player_NN.json` is the expected decode of each, written from an
independent script rather than from the Go decoder. Fixtures 01 to 04 come from the first capture;
05 to 07 are payloads 06, 16 and 17 of the second capture, kept as regression cases: 05 and 07 have
record gaps over 128 bytes, 06 has a chance empty record that shadows the real Shoulder.

Redaction overwrote text in place with same-length ASCII placeholders, so every length prefix
and the total payload length are unchanged:

| file | payload bytes | character name (len) | placeholder | other text |
|---|---|---|---|---|
| player_01.bin | 1623 | 9 bytes | `Tester001` | 5 byte guild string, now `Guild` |
| player_02.bin | 1601 | 9 bytes (3 CJK characters) | `Tester002` | 5 byte guild string, now `Guild` |
| player_03.bin | 1357 | 5 bytes | `Tstr3` | none |
| player_04.bin | 1547 | 11 bytes | `Tester00004` | 2 byte guild string, now `Gd` |
| player_05.bin | 1672 | 4 bytes | `Tst5` | none |
| player_06.bin | 1874 | 6 bytes | `Tstr06` | 9 byte guild string (one 2 byte UTF-8 character), now `GuildName` |
| player_07.bin | 1809 | 8 bytes (one 2 byte UTF-8 character) | `Tester07` | none |

Entity ids, levels, gear and the rest are untouched. The real names of fixtures 02 and 07 and the
guild string of 06 contained multi-byte characters; their placeholders are ASCII, so no fixture
exercises multi-byte names (a synthetic test does).
