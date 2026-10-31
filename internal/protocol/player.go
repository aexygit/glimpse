package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/aexygit/glimpse/internal/gear"
)

// slotNames maps the 1-based wire slot index to a slot. The array is always exactly 15
// records and 12 to 15 were empty on all 22 captured characters, so this is complete for
// Player; SubHand, Belt, Ring1/2, Bracelet1/2, Amulet and wing were never seen at any index.
// Earring1 versus Earring2 is unproven: six characters wore two different earrings and
// index 10 always held the equal or higher grade. An index missing from the table surfaces
// as "Slot<N>" (see slotName), never dropped.
var slotNames = map[int]gear.Slot{
	1: "MainHand", 2: "Helmet", 3: "Shoulder", 4: "Torso", 5: "Pants", 6: "Gloves",
	7: "Boots", 8: "Cape", 9: "Necklace", 10: "Earring1", 11: "Earring2",
}

func slotName(idx int) gear.Slot {
	if s, ok := slotNames[idx]; ok {
		return s
	}
	return gear.Slot(fmt.Sprintf("Slot%d", idx))
}

// Gear array scan parameters.
const (
	recHead = 15 // bytes read to validate a record: header, id, enchant, index, 8 zeros
	minGap  = 21 // an empty record is 21 or 22 bytes, an occupied one 33 or 34
	// maxGap is the outer bound of the gap lattice (see onLattice). Observed up to 147 across
	// 22 characters, all level 36 or below; the lattice admits the next steps (175, 176, ...).
	maxGap = 256
	minRun = 6 // fewer consecutive records is noise, not an equipment array
	// maxRecs bounds how many records a run can have. The array is exactly 15 records in all
	// 22 captures; the slack tolerates a future slot, and a hostile payload cannot make us
	// emit (and the app fetch) hundreds of items.
	maxRecs = 32
)

type gearRec struct {
	off, idx, enchant int
	id                uint32
}

var errNoGear = errors.New("player: no equipment array")

func decodePlayer(p []byte) (InspectResult, error) {
	r := reader{p: p}
	var res InspectResult
	res.Entity = int(r.varint())
	r.skip(4) // mask
	hasName := r.u8()&1 != 0
	name := r.take(int(r.u8()))
	level := r.u32()
	if r.bad {
		return InspectResult{}, fmt.Errorf("player: truncated header at byte %d of %d", r.i, len(p))
	}
	if !hasName || len(name) == 0 || !utf8.Valid(name) {
		return InspectResult{}, errors.New("player: no valid name")
	}
	res.Name = string(name)
	// The u32 after the name is level on the evidence of 22 captures; reject nonsense.
	if level >= 1 && level <= 99 {
		res.Level = int(level)
	}

	recs := scanGear(p[r.i:])
	if recs == nil {
		return InspectResult{}, errNoGear
	}
	for _, g := range recs {
		if g.id == 0 {
			continue // empty slot
		}
		res.Items = append(res.Items, InspectItem{Slot: slotName(g.idx), ID: int(g.id), Enchant: g.enchant})
	}
	return res, nil
}

// scanGear finds the longest run of records whose slot index counts 1, 2, 3, ...
// The array has no fixed offset (the pre-gear skill list sets it), and the skill list
// in the same id band never counts up, so the index is what tells them apart.
func scanGear(p []byte) []gearRec {
	var best []gearRec
	for q := 0; q+recHead <= len(p); q++ {
		if !isRec(p, q, 1, true) {
			continue
		}
		run, last := chain(p, q)
		if len(run) > len(best) {
			best = run
		}
		if len(run) >= minRun {
			q = last // starts inside a run only yield shorter runs
		}
	}
	if len(best) < minRun {
		return nil
	}
	return best
}

// chain follows records idx 1, 2, ... from q. It returns the run and the offset of its last record.
func chain(p []byte, q int) ([]gearRec, int) {
	run := []gearRec{readRec(p, q)}
	for idx := 2; idx <= maxRecs; idx++ {
		next := nextRec(p, q, idx)
		if next < 0 {
			break
		}
		q = next
		run = append(run, readRec(p, q))
	}
	return run, q
}

// nextRec returns the offset of the record with index idx following the one at q, or -1.
// Only gaps on the lattice below are tried, nearest first; anything off it is not a successor.
func nextRec(p []byte, q, idx int) int {
	occupied := binary.LittleEndian.Uint32(p[q+1:]) != 0
	for g := minGap; g <= maxGap; g++ {
		if onLattice(occupied, g) && isRec(p, q+g, idx, false) {
			return q + g
		}
	}
	return -1
}

// The gap from a record to the next is not free. Over 308 gaps in 22 characters, zero exceptions:
// after an empty record 21 or 22; after an occupied one 33 or 34 (238 samples), plus an extra block
// of 28 or 29 bytes per step (62, 90, 91, 118, 119, 147 seen). Widen a tolerance here to admit more.
const (
	emptyGapMin, emptyGapMax = 21, 22
	occGapMin, occGapMax     = 33, 34
	blockMin, blockMax       = 28, 29
)

// onLattice reports whether g is a plausible gap after a record that is occupied or empty.
func onLattice(occupied bool, g int) bool {
	if !occupied {
		return g >= emptyGapMin && g <= emptyGapMax
	}
	for k := 0; occGapMin+k*blockMin <= g; k++ { // k extra blocks
		if g <= occGapMax+k*blockMax {
			return true
		}
	}
	return false
}

// isRec reports whether a record with slot index idx starts at q: the index byte matches,
// the eight bytes after the enchant are zero, and the id is empty or nine digits. The first
// record must also be in the 110/210/310 families (a skill list lookalike in the same band
// is what the family gate keeps out); later ones are accepted in any family so an unseen
// item family cannot silently end the run.
func isRec(p []byte, q, idx int, first bool) bool {
	if q < 0 || q+recHead > len(p) || int(p[q+6]) != idx {
		return false
	}
	id := binary.LittleEndian.Uint32(p[q+1:])
	if id != 0 {
		fam := id / 1_000_000
		if first && fam != 110 && fam != 210 && fam != 310 {
			return false
		}
		if !first && (id < 100_000_000 || id > 999_999_999) {
			return false
		}
	}
	for _, b := range p[q+7 : q+recHead] {
		if b != 0 {
			return false
		}
	}
	return true
}

// readRec reads a record already accepted by isRec.
func readRec(p []byte, q int) gearRec {
	return gearRec{off: q, idx: int(p[q+6]), enchant: int(p[q+5]), id: binary.LittleEndian.Uint32(p[q+1:])}
}

// reader is a sticky-error cursor: the first short read sets bad and every later read
// returns zero, so callers check bad once.
type reader struct {
	p   []byte
	i   int
	bad bool
}

func (r *reader) take(n int) []byte {
	if r.bad || n < 0 || n > len(r.p)-r.i {
		r.bad = true
		return nil
	}
	b := r.p[r.i : r.i+n]
	r.i += n
	return b
}

func (r *reader) skip(n int) { r.take(n) }

func (r *reader) u8() byte {
	if b := r.take(1); b != nil {
		return b[0]
	}
	return 0
}

func (r *reader) u32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

// varint reads a LEB128 value that fits a u32, like a2kit's entity reader.
func (r *reader) varint() uint32 {
	if r.bad {
		return 0
	}
	v, n := binary.Uvarint(r.p[r.i:min(len(r.p), r.i+5)])
	if n <= 0 || v > math.MaxUint32 {
		r.bad = true
		return 0
	}
	r.i += n
	return uint32(v)
}
