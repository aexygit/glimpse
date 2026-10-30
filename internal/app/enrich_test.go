package app

import (
	"testing"

	"github.com/aexygit/glimpse/internal/gamedata"
	"github.com/aexygit/glimpse/internal/gear"
	"github.com/aexygit/glimpse/internal/protocol"
)

func openStore(t *testing.T) *gamedata.Store {
	t.Helper()
	s, err := gamedata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestEnrichKnown(t *testing.T) {
	s := openStore(t)
	if err := s.Put(gamedata.Entry{ID: 1, Name: "Sword", Tier: 5, Stats: gear.Stats{"attack": 10}}); err != nil {
		t.Fatal(err)
	}
	c := enrich(protocol.InspectResult{Items: []protocol.InspectItem{
		{Slot: "MainHand", ID: 1, Rolls: gear.Stats{"attack": 5, "crit": 2}},
	}}, s)
	it := c.Items["MainHand"]
	if it.Name != "Sword" || it.Tier != 5 || !it.Known || it.Stats["attack"] != 15 || it.Stats["crit"] != 2 {
		t.Errorf("item = %+v", it)
	}
	if e, _ := s.Get(1); e.Stats["attack"] != 10 {
		t.Errorf("store entry mutated: %v", e.Stats)
	}
}

func TestEnrichUnknownOffline(t *testing.T) {
	c := enrich(protocol.InspectResult{Items: []protocol.InspectItem{
		{Slot: "Ring1", ID: 42, Rolls: gear.Stats{"crit": 3}},
	}}, openStore(t))
	it := c.Items["Ring1"]
	if it.Name != "Unknown item #42" || it.Known || len(it.Stats) != 1 || it.Stats["crit"] != 3 {
		t.Errorf("item = %+v", it)
	}
}
