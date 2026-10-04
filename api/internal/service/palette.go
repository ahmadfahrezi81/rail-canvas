package service

import "fmt"

type Palette struct {
	ID     string
	Colors []string // a cell's byte indexes this; 0 is the background
}

var palettes = map[string]Palette{
	// The 2017 r/place colors.
	"classic16": {ID: "classic16", Colors: []string{
		"#FFFFFF", "#E4E4E4", "#888888", "#222222",
		"#FFA7D1", "#E50000", "#E59500", "#A06A42",
		"#E5D900", "#94E044", "#02BE01", "#00D3DD",
		"#0083C7", "#0000EA", "#CF6EE4", "#820080",
	}},
}

func paletteByID(id string) (Palette, error) {
	p, ok := palettes[id]
	if !ok {
		return Palette{}, fmt.Errorf("unknown palette %q", id)
	}
	return p, nil
}
