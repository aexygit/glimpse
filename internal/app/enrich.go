package app

import (
	"fmt"
	"maps"

	"github.com/aexygit/glimpse/internal/gamedata"
	"github.com/aexygit/glimpse/internal/gear"
	"github.com/aexygit/glimpse/internal/protocol"
)

// enrich merges captured items with cached catalog data.
func enrich(r protocol.InspectResult, store *gamedata.Store) gear.Character {
	c := gear.Character{Name: r.Name, Class: r.Class, Level: r.Level, Items: map[gear.Slot]gear.Item{}}
	for _, in := range r.Items {
		it := gear.Item{ID: in.ID, Enchant: in.Enchant, Stats: gear.Stats{}}
		if e, ok := store.Get(in.ID); ok {
			it.Name, it.Tier, it.Known = e.Name, e.Tier, true
			maps.Copy(it.Stats, e.Stats)
		} else {
			it.Name = fmt.Sprintf("Unknown item #%d", in.ID)
		}
		c.Items[in.Slot] = it
	}
	return c
}
