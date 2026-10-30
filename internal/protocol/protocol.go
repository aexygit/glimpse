// Package protocol decodes game frames into Glimpse types.
package protocol

import (
	"github.com/nuriland/a2kit/wire"

	"github.com/aexygit/glimpse/internal/gear"
)

type InspectItem struct {
	Slot    gear.Slot
	ID      int
	Enchant int
	Rolls   gear.Stats
	Sockets []gear.Stats
}

type InspectResult struct {
	Name  string
	Class gear.Class
	Level int
	Items []InspectItem
}

// OpInspect is a placeholder until the inspect opcode is identified.
const OpInspect wire.Opcode = 0x0000

// Decode returns nil for every frame until the inspect decoder lands.
func Decode(f wire.Frame) (any, error) { return nil, nil }
