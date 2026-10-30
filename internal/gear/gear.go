// Package gear holds the domain types shared across Glimpse.
package gear

type (
	Class string
	Slot  string
	Stat  string
	Stats map[Stat]float64
)

type Item struct {
	ID      int
	Name    string
	Tier    int
	Enchant int
	Stats   Stats
	Sockets []Stats
	// Known is false when gamedata had no entry for ID.
	Known bool
}

type Character struct {
	Name  string
	Class Class
	Level int
	Items map[Slot]Item
}
