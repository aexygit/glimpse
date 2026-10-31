package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nuriland/a2kit/wire"
)

// player_05 to player_07 are the second capture's failing cases: gaps over 128 bytes (05, 07)
// and a chance empty record shadowing the real one (06, 07).
var fixtures = []string{"player_01", "player_02", "player_03", "player_04", "player_05", "player_06", "player_07"}

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".bin"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func readExpected(t testing.TB, name string) InspectResult {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var r InspectResult
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func serverFrame(payload []byte) wire.Frame {
	return wire.Frame{Opcode: OpPlayer, Flags: wire.FromServer, Payload: payload}
}

func mustDecode(t testing.TB, payload []byte) InspectResult {
	t.Helper()
	got, err := Decode(serverFrame(payload))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	r, ok := got.(InspectResult)
	if !ok {
		t.Fatalf("Decode returned %T, want InspectResult by value", got)
	}
	return r
}

// rec is a synthetic gear record for hand-built payloads.
type rec struct {
	idx, enchant int
	id           uint32
	fill         byte    // byte written over +7..+14, zero in real data
	pad          int     // extra trailing bytes beyond the minimum record length
	patches      []patch // bytes overwritten inside the record, to form chance fragments
}

// patch writes b at offset off of a record.
type patch struct {
	off int
	b   []byte
}

// fragment is a 15 byte record-looking run: header, id, enchant, index, 8 zeros.
func fragment(id uint32, idx int) []byte {
	b := make([]byte, 15)
	b[0] = 0x0e
	binary.LittleEndian.PutUint32(b[1:], id)
	b[5], b[6] = 1, byte(idx)
	return b
}

func (r rec) bytes() []byte {
	n := 21
	if r.id != 0 {
		n = 33
	}
	b := make([]byte, n+r.pad)
	b[0] = 0x0e
	binary.LittleEndian.PutUint32(b[1:], r.id)
	b[5], b[6] = byte(r.enchant), byte(r.idx)
	for i := 7; i < 15; i++ {
		b[i] = r.fill
	}
	for _, pt := range r.patches {
		copy(b[pt.off:], pt.b)
	}
	if r.id != 0 {
		b[15], b[16], b[17], b[18] = 0xaa, 0xbb, 0xcc, 0x01
		b[19], b[20] = 0, 3
	}
	return b
}

// buildPayload assembles a Player payload: header, name, level, filler, then the records.
func buildPayload(name string, level uint32, filler []byte, recs ...rec) []byte {
	return buildPayloadEntity(6055, name, level, filler, recs...)
}

func buildPayloadEntity(entity uint64, name string, level uint32, filler []byte, recs ...rec) []byte {
	p := binary.AppendUvarint(nil, entity)
	p = append(p, 0x07, 0x30, 0xa0, 0x01) // mask
	p = append(p, 0x07, byte(len(name)))
	p = append(p, name...)
	p = binary.LittleEndian.AppendUint32(p, level)
	p = append(p, filler...)
	for _, r := range recs {
		p = append(p, r.bytes()...)
	}
	return p
}

// fullRecs returns records 1..n with increasing ids in the 210 family.
func fullRecs(n int) []rec {
	var rs []rec
	for i := 1; i <= n; i++ {
		rs = append(rs, rec{idx: i, enchant: i, id: 210130000 + uint32(i)})
	}
	return rs
}

func TestDecodePlayer(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			want := readExpected(t, name)
			got := mustDecode(t, readFixture(t, name))
			if got.Entity != want.Entity || got.Name != want.Name || got.Level != want.Level || got.Class != want.Class {
				t.Errorf("header = {%d %q %q %d}, want {%d %q %q %d}",
					got.Entity, got.Name, got.Class, got.Level, want.Entity, want.Name, want.Class, want.Level)
			}
			if len(got.Items) != len(want.Items) {
				t.Fatalf("got %d items, want %d: %+v", len(got.Items), len(want.Items), got.Items)
			}
			for i, w := range want.Items {
				g := got.Items[i]
				if g.Slot != w.Slot || g.ID != w.ID || g.Enchant != w.Enchant {
					t.Errorf("item %d = {%s %d +%d}, want {%s %d +%d}", i, g.Slot, g.ID, g.Enchant, w.Slot, w.ID, w.Enchant)
				}
			}
		})
	}
}

// The gear array sits behind a skill list in the same numeric band. Only the incrementing
// slot index tells them apart, so pin the real ids, not just the count.
func TestDecodeSkillListNotGear(t *testing.T) {
	want := []struct {
		slot    string
		id, enc int
	}{
		{"MainHand", 110830049, 11}, {"Helmet", 210330053, 11}, {"Shoulder", 210430052, 10},
		{"Torso", 210130053, 10}, {"Pants", 210230053, 10}, {"Gloves", 210530053, 10},
		{"Boots", 210630053, 11}, {"Cape", 210730053, 10}, {"Necklace", 310130050, 10},
		{"Earring1", 310230052, 10}, {"Earring2", 310230051, 10},
	}
	got := mustDecode(t, readFixture(t, "player_01"))
	if len(got.Items) != len(want) {
		t.Fatalf("got %d items, want %d", len(got.Items), len(want))
	}
	for i, w := range want {
		g := got.Items[i]
		if string(g.Slot) != w.slot || g.ID != w.id || g.Enchant != w.enc {
			t.Errorf("item %d = {%s %d +%d}, want {%s %d +%d}", i, g.Slot, g.ID, g.Enchant, w.slot, w.id, w.enc)
		}
	}

	t.Run("lookalike skill list", func(t *testing.T) {
		// Records in the gear id band with the skill list's 0xff sentinel and no slot index.
		var skills []byte
		for i := 0; i < 12; i++ {
			skills = append(skills, rec{idx: 0, id: 210999000 + uint32(i), fill: 0xff}.bytes()...)
		}
		// Same, but with an incrementing index and a non-zero run.
		for i := 1; i <= 8; i++ {
			skills = append(skills, rec{idx: i, id: 310999000 + uint32(i), fill: 0xff}.bytes()...)
		}
		r := mustDecode(t, buildPayload("Tester", 20, skills, fullRecs(15)...))
		if len(r.Items) != 15 {
			t.Fatalf("got %d items, want 15", len(r.Items))
		}
		for _, it := range r.Items {
			if it.ID < 210130000 || it.ID > 210130015 {
				t.Errorf("picked up non-gear id %d in slot %s", it.ID, it.Slot)
			}
		}
	})
}

func TestDecodeOtherOpcode(t *testing.T) {
	p := readFixture(t, "player_01")
	for _, op := range []wire.Opcode{0x0000, 0x3633, 0x3641, 0x561C, 0xFFFF} {
		got, err := Decode(wire.Frame{Opcode: op, Flags: wire.FromServer, Payload: p})
		if got != nil || err != nil {
			t.Errorf("opcode %v: got (%v, %v), want (nil, nil)", op, got, err)
		}
	}
}

func TestDecodeClientFrame(t *testing.T) {
	p := readFixture(t, "player_01")
	for name, fl := range map[string]wire.Flags{"client": wire.FromClient, "no direction": 0, "both": wire.FromServer | wire.FromClient} {
		t.Run(name, func(t *testing.T) {
			got, err := Decode(wire.Frame{Opcode: OpPlayer, Flags: fl, Payload: p})
			if got != nil || err != nil {
				t.Errorf("got (%v, %v), want (nil, nil)", got, err)
			}
		})
	}
}

func TestDecodeTruncated(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			full := readFixture(t, name)
			want := mustDecode(t, full)
			for n := 0; n <= len(full); n++ {
				got, err := Decode(serverFrame(full[:n]))
				if err != nil {
					if got != nil {
						t.Fatalf("len %d: result with error %v", n, err)
					}
					continue
				}
				r, ok := got.(InspectResult)
				if !ok {
					t.Fatalf("len %d: got %T", n, got)
				}
				if r.Name != want.Name || len(r.Items) > len(want.Items) {
					t.Fatalf("len %d: bad partial result %+v", n, r)
				}
				for i, it := range r.Items {
					if it.Slot != want.Items[i].Slot || it.ID != want.Items[i].ID || it.Enchant != want.Items[i].Enchant {
						t.Fatalf("len %d: item %d = %+v, want %+v", n, i, it, want.Items[i])
					}
				}
			}
		})
	}
}

func TestDecodeMalformed(t *testing.T) {
	good := buildPayload("Tester", 20, nil, fullRecs(15)...)
	patch := func(p []byte, i int, v byte) []byte {
		q := append([]byte(nil), p...)
		q[i] = v
		return q
	}
	tests := []struct {
		name string
		p    []byte
	}{
		{"nil", nil},
		{"entity varint never ends", []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}},
		{"entity overflows u32", []byte{0xff, 0xff, 0xff, 0xff, 0x7f, 0, 0, 0, 0, 7, 1, 'a', 20, 0, 0, 0}},
		{"no name flag", patch(good, 6, 0x06)},
		{"hostile name length", patch(good, 7, 0xff)},
		{"name length zero", []byte{0x01, 0, 0, 0, 0, 0x07, 0x00, 20, 0, 0, 0}},
		{"name not utf8", buildPayload("\xff\xfe\xfd", 20, nil, fullRecs(15)...)},
		{"no gear array", buildPayload("Tester", 20, make([]byte, 600))},
		{"too few records", buildPayload("Tester", 20, nil, fullRecs(3)...)},
		{"gear indices do not increment", buildPayload("Tester", 20, nil, rec{idx: 1, id: 210130001}, rec{idx: 3, id: 210130003}, rec{idx: 4, id: 210130004}, rec{idx: 5, id: 210130005}, rec{idx: 6, id: 210130006}, rec{idx: 7, id: 210130007})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode(serverFrame(tt.p))
			if err == nil || got != nil {
				t.Errorf("got (%v, %v), want an error", got, err)
			}
		})
	}
}

func TestDecodeUnknownSlotIndex(t *testing.T) {
	rs := fullRecs(15)
	rs[11] = rec{idx: 12, enchant: 7, id: 310330077} // unmapped index, occupied
	rs[12] = rec{idx: 13}                            // unmapped and empty
	r := mustDecode(t, buildPayload("Tester", 20, nil, rs...))
	var found bool
	for _, it := range r.Items {
		if it.Slot == "Slot12" {
			found = true
			if it.ID != 310330077 || it.Enchant != 7 {
				t.Errorf("Slot12 = %+v, want id 310330077 +7", it)
			}
		}
		if it.ID == 0 {
			t.Errorf("empty record surfaced as an item: %+v", it)
		}
	}
	if !found {
		t.Fatalf("slot index 12 was dropped: %+v", r.Items)
	}
	if len(r.Items) != 14 {
		t.Errorf("got %d items, want 14 (15 records minus one empty)", len(r.Items))
	}
}

func TestDecodeRecordGaps(t *testing.T) {
	// Real arrays mix 33/34 byte records, 21/22 byte empties and one 90 byte record.
	rs := fullRecs(11)
	rs[3].pad = 57
	rs[4].pad = 1
	rs = append(rs, rec{idx: 12}, rec{idx: 13, pad: 1}, rec{idx: 14})
	r := mustDecode(t, buildPayload("Tester", 20, []byte{1, 2, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 9}, rs...))
	if len(r.Items) != 11 {
		t.Fatalf("got %d items, want 11", len(r.Items))
	}
}

func TestSlotNames(t *testing.T) {
	if got := slotName(1); got != "MainHand" {
		t.Errorf("slotName(1) = %q", got)
	}
	if got := slotName(99); got != "Slot99" {
		t.Errorf("slotName(99) = %q", got)
	}
	seen := map[string]int{}
	for idx, name := range slotNames {
		if prev, dup := seen[string(name)]; dup {
			t.Errorf("name %q used by indices %d and %d", name, prev, idx)
		}
		seen[string(name)] = idx
	}
}

func FuzzDecode(f *testing.F) {
	for _, name := range fixtures {
		b := readFixture(f, name)
		f.Add(b)
		f.Add(b[:len(b)/2])
	}
	f.Add(buildPayload("Tester", 20, nil, fullRecs(15)...))
	f.Add(buildPayload("Tester", 0xffffffff, nil, fullRecs(15)...))
	f.Add(buildPayloadEntity(0xffffffff, "Tester", 20, nil, fullRecs(15)...))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, p []byte) {
		got, err := Decode(serverFrame(p))
		if err != nil {
			if got != nil {
				t.Fatalf("result alongside error %v", err)
			}
			return
		}
		r, ok := got.(InspectResult)
		if !ok {
			t.Fatalf("got %T", got)
		}
		if r.Name == "" || !utf8.ValidString(r.Name) {
			t.Fatalf("bad name %q", r.Name)
		}
		if r.Level < 0 || r.Level > 99 || r.Entity < 0 || len(r.Items) > maxRecs {
			t.Fatalf("bad header or too many items: level %d entity %d items %d", r.Level, r.Entity, len(r.Items))
		}
		for _, it := range r.Items {
			if it.ID < 100_000_000 || it.ID > 999_999_999 || it.Slot == "" || it.Enchant < 0 || strings.ContainsAny(string(it.Slot), " \t") {
				t.Fatalf("bad item %+v", it)
			}
		}
	})
}

// Records 12 to 15 are dropped from the result, so check the scan itself reaches 15.
func TestScanGearFindsAllFifteen(t *testing.T) {
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			recs := scanGear(readFixture(t, name))
			if len(recs) != 15 {
				t.Fatalf("scanned %d records, want 15", len(recs))
			}
			for i, r := range recs {
				if r.idx != i+1 {
					t.Errorf("record %d has index %d", i, r.idx)
				}
			}
		})
	}
}

// Regression pins for the second capture: player_06 lost its Shoulder to a chance empty
// record, player_05 and player_07 exceeded the old 128 byte gap window.
func TestDecodeSecondCaptureRegressions(t *testing.T) {
	r := mustDecode(t, readFixture(t, "player_06"))
	var shoulder *InspectItem
	for i := range r.Items {
		if r.Items[i].Slot == "Shoulder" {
			shoulder = &r.Items[i]
		}
	}
	if len(r.Items) != 11 || shoulder == nil || shoulder.ID != 210430053 || shoulder.Enchant != 12 {
		t.Errorf("player_06: %d items, shoulder %+v; want 11 items and Shoulder 210430053 +12", len(r.Items), shoulder)
	}
	for _, name := range []string{"player_05", "player_07"} {
		if n := len(mustDecode(t, readFixture(t, name)).Items); n != 11 {
			t.Errorf("%s: got %d items, want 11", name, n)
		}
	}
}

func TestDecodePhantomEmptyRecord(t *testing.T) {
	rs := fullRecs(15)
	rs[1].pad, rs[1].patches = 30, []patch{{31, []byte{3}}} // chance idx 3 empty record at +25, real one at +63
	r := mustDecode(t, buildPayload("Tester", 20, nil, rs...))
	if len(r.Items) != 15 || r.Items[2].Slot != "Shoulder" || r.Items[2].ID != 210130003 {
		t.Fatalf("phantom shadowed the real record: %+v", r.Items)
	}
}

// A continuation record with an unseen family or an over-long body must not end the run.
func TestDecodeMidChainTolerance(t *testing.T) {
	for _, at := range []int{5, 7, 12} {
		t.Run(fmt.Sprintf("unseen family at %d", at), func(t *testing.T) {
			rs := fullRecs(15)
			rs[at-1].id = 410130000 + uint32(at)
			r := mustDecode(t, buildPayload("Tester", 20, nil, rs...))
			if len(r.Items) != 15 || r.Items[at-1].ID != 410130000+at {
				t.Fatalf("got %d items: %+v", len(r.Items), r.Items)
			}
		})
	}
	t.Run("long record", func(t *testing.T) {
		rs := fullRecs(15)
		rs[6].pad = 85 // gap 118
		if r := mustDecode(t, buildPayload("Tester", 20, nil, rs...)); len(r.Items) != 15 {
			t.Fatalf("got %d items", len(r.Items))
		}
	})
	t.Run("gap of 147", func(t *testing.T) {
		rs := fullRecs(15)
		rs[2].pad = 114
		if r := mustDecode(t, buildPayload("Tester", 20, nil, rs...)); len(r.Items) != 15 {
			t.Fatalf("got %d items", len(r.Items))
		}
	})
}

func TestDecodeFirstRecordFamily(t *testing.T) {
	rs := fullRecs(15)
	rs[0].id = 410130001 // the gate applies to idx 1 only
	if got, err := Decode(serverFrame(buildPayload("Tester", 20, nil, rs...))); err == nil || got != nil {
		t.Errorf("non-family first record: got (%v, %v), want an error", got, err)
	}
}

func TestDecodeLevel(t *testing.T) {
	for _, tt := range []struct {
		in   uint32
		want int
	}{{0, 0}, {1, 1}, {99, 99}, {100, 0}, {0xffffffff, 0}} {
		t.Run(fmt.Sprint(tt.in), func(t *testing.T) {
			if got := mustDecode(t, buildPayload("Tester", tt.in, nil, fullRecs(15)...)).Level; got != tt.want {
				t.Errorf("level %d -> %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestDecodeRecordCap(t *testing.T) {
	var rs []rec
	for i := 1; i <= 300; i++ {
		rs = append(rs, rec{idx: i, id: 210130000 + uint32(i%100)})
	}
	if n := len(mustDecode(t, buildPayload("Tester", 20, nil, rs...)).Items); n != 32 {
		t.Errorf("emitted %d items, want exactly 32", n)
	}
}

func TestDecodeMultibyteName(t *testing.T) {
	const name = "Téstê"
	if got := mustDecode(t, buildPayload(name, 20, nil, fullRecs(15)...)).Name; got != name {
		t.Errorf("name = %q, want %q", got, name)
	}
}

// plantAt overwrites p at off with a 15 byte fragment.
func plantAt(p []byte, off int, id uint32, idx int) []byte {
	q := append([]byte(nil), p...)
	copy(q[off:], fragment(id, idx))
	return q
}

// A fragment with index 12 and a nine-digit id, after record 11, used to beat the real (empty)
// record 12 and fabricate an item. The same fragment is planted in three real payloads.
func TestDecodePlantedFragments(t *testing.T) {
	for _, name := range []string{"player_01", "player_05", "player_07"} {
		t.Run(name, func(t *testing.T) {
			p := readFixture(t, name)
			recs := scanGear(p)
			if len(recs) != 15 {
				t.Fatalf("unplanted scan found %d records", len(recs))
			}
			want := mustDecode(t, p)
			for _, gap := range []int{125, 130, 143, 148, 200} {
				off := recs[10].off + gap
				if off < recs[14].off+21 {
					t.Fatalf("plant at gap %d overlaps the real array", gap)
				}
				planted := plantAt(p, off, 123456789, 12)
				if got := scanGear(planted); len(got) != 15 || got[11].id != 0 {
					t.Errorf("gap %d: scanned %d records; want 15 with an empty record 12", gap, len(got))
				}
				if got := mustDecode(t, planted); !reflect.DeepEqual(got.Items, want.Items) {
					t.Errorf("gap %d: items changed: %+v", gap, got.Items)
				}
			}
		})
	}
}

// Gaps after an occupied record follow 33/34 plus 28/29 per extra block. 237 is the last lattice
// gap inside the window; 257 is the first one past it.
func TestDecodeLatticeGaps(t *testing.T) {
	for _, gap := range []int{62, 90, 91, 118, 119, 147, 175, 176, 237} {
		t.Run(fmt.Sprint(gap), func(t *testing.T) {
			rs := fullRecs(15)
			rs[3].pad = gap - 33
			if n := len(mustDecode(t, buildPayload("Tester", 20, nil, rs...)).Items); n != 15 {
				t.Errorf("gap %d: got %d items, want 15", gap, n)
			}
		})
	}
	rs := fullRecs(15)
	rs[3].pad = 257 - 33
	if recs := scanGear(buildPayload("Tester", 20, nil, rs...)); len(recs) != 0 {
		t.Errorf("gap 257: scanned %d records, expected the run to end at 4 and be rejected", len(recs))
	}
}

// A successor off the lattice is not taken, even when it looks like a record. The run ends there.
func TestScanGearOffLattice(t *testing.T) {
	for _, gap := range []int{35, 40, 60, 64, 88, 93, 116, 122, 144, 151, 200} {
		t.Run(fmt.Sprint(gap), func(t *testing.T) {
			rs := fullRecs(15)
			rs[8].pad = gap - 33 // record 10 sits at an off-lattice gap
			if recs := scanGear(buildPayload("Tester", 20, nil, rs...)); len(recs) != 9 {
				t.Errorf("gap %d: scanned %d records, want the run to end at 9", gap, len(recs))
			}
		})
	}
	t.Run("after an empty record", func(t *testing.T) {
		rs := fullRecs(11)
		rs = append(rs, rec{idx: 12, pad: 4}, rec{idx: 13}, rec{idx: 14}, rec{idx: 15}) // gap 25
		if recs := scanGear(buildPayload("Tester", 20, nil, rs...)); len(recs) != 12 {
			t.Errorf("scanned %d records, want the run to end at 12", len(recs))
		}
	})
}

// Continuation ids must be nine digits: an 8 or 10 digit id is a uid or noise, not an item.
func TestDecodeContinuationIDBounds(t *testing.T) {
	for _, id := range []uint32{12_345_678, 99_999_999, 1_000_000_000, 4_000_000_000} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			rs := fullRecs(15)
			// Real record 3 at gap 62; a fragment with the right index and a bad id at gap 33.
			rs[1].pad, rs[1].patches = 29, []patch{{33, fragment(id, 3)}}
			r := mustDecode(t, buildPayload("Tester", 20, nil, rs...))
			if len(r.Items) != 15 || r.Items[2].ID != 210130003 {
				t.Errorf("fragment id %d shadowed the real record: %d items, idx 3 = %+v", id, len(r.Items), r.Items[2])
			}
		})
	}
}

// Fragments after the array must not replace the genuine empty records 13 to 15.
func TestDecodePlantedAfterEmpty(t *testing.T) {
	for _, name := range []string{"player_01", "player_05", "player_07"} {
		t.Run(name, func(t *testing.T) {
			p := readFixture(t, name)
			recs := scanGear(p)
			if len(recs) != 15 {
				t.Fatalf("unplanted scan found %d records", len(recs))
			}
			want := mustDecode(t, p)
			end := recs[14].off + 21
			for idx := 13; idx <= 15; idx++ {
				for _, extra := range []int{0, 8, 40, 100} {
					planted := plantAt(p, end+extra, 123456789, idx)
					if got := scanGear(planted); len(got) != 15 || got[idx-1].id != 0 {
						t.Errorf("index %d planted at %d past the array: scanned %d records", idx, extra, len(got))
					}
					if got := mustDecode(t, planted); !reflect.DeepEqual(got.Items, want.Items) {
						t.Errorf("index %d planted at %d past the array: items changed", idx, extra)
					}
				}
			}
		})
	}
}

// An occupied record followed by an extra block has its real successor at 62 or more. A fragment
// at gap 21 or 22 is off the lattice and must not win. (A fragment at 33 or 34 is on it and,
// being nearer, does win: nothing local can tell it from a real record. No capture has one.)
func TestDecodePlantedBeforeExtraBlock(t *testing.T) {
	var tested int
	for _, name := range fixtures {
		p := readFixture(t, name)
		recs := scanGear(p)
		if len(recs) != 15 {
			t.Fatalf("%s: scanned %d records", name, len(recs))
		}
		for i := 0; i < 14; i++ {
			if recs[i].id == 0 || recs[i+1].off-recs[i].off < 62 {
				continue
			}
			for _, g := range []int{21, 22} {
				tested++
				got := scanGear(plantAt(p, recs[i].off+g, 123456789, i+2))
				if !reflect.DeepEqual(got, recs) {
					t.Errorf("%s: fragment %d bytes after record %d changed the scan", name, g, i+1)
				}
			}
		}
	}
	if tested < 10 {
		t.Fatalf("only %d plants ran; the fixtures should hold several extra-block records", tested)
	}
}
