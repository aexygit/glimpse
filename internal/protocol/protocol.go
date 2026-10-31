// Package protocol decodes game frames into Glimpse types.
package protocol

import (
	"github.com/nuriland/a2kit/wire"

	"github.com/aexygit/glimpse/internal/gear"
)

// InspectItem is everything the wire carries about one equipped item:
// no stat rolls and no sockets, the broadcast does not include them.
type InspectItem struct {
	Slot    gear.Slot
	ID      int
	Enchant int
}

type InspectResult struct {
	// Entity is the in-world entity id, stable while the character stays in view.
	// Consumers can use it to deduplicate repeated broadcasts.
	Entity int
	Name   string
	Class  gear.Class
	Level  int
	Items  []InspectItem
}

// OpPlayer is the proximity broadcast that carries a character's visible equipment.
const OpPlayer wire.Opcode = 0x3645

// Decode returns an InspectResult (by value, which is what app.Service.OnMessage
// asserts) for a Player frame from the server, and nil for every other frame.
func Decode(f wire.Frame) (any, error) {
	if f.Opcode != OpPlayer || f.Flags&wire.FromServer == 0 || f.Flags&wire.FromClient != 0 {
		return nil, nil
	}
	r, err := decodePlayer(f.Payload)
	if err != nil {
		return nil, err
	}
	return r, nil
}
