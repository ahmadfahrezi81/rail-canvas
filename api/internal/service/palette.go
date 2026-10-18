package service

import (
	"fmt"
	"strconv"
	"strings"
)

type Palette struct {
	ID     string
	Colors []string // a cell's byte indexes this; 0 is the background
	Names  []string // one per color: the words the bot accepts
}

var palettes = map[string]Palette{
	// The 2017 r/place colors.
	"classic16": {ID: "classic16", Colors: []string{
		"#FFFFFF", "#E4E4E4", "#888888", "#222222",
		"#FFA7D1", "#E50000", "#E59500", "#A06A42",
		"#E5D900", "#94E044", "#02BE01", "#00D3DD",
		"#0083C7", "#0000EA", "#CF6EE4", "#820080",
	}, Names: []string{
		"white", "lightgrey", "grey", "black",
		"pink", "red", "orange", "brown",
		"yellow", "lime", "green", "cyan",
		"blue", "darkblue", "magenta", "purple",
	}},
}

func paletteByID(id string) (Palette, error) {
	p, ok := palettes[id]
	if !ok {
		return Palette{}, fmt.Errorf("unknown palette %q", id)
	}
	return p, nil
}

// Color resolves a name ("red", "Dark Blue") or an index ("5"); false when neither.
func (p Palette) Color(word string) (int, bool) {
	w := strings.TrimSpace(word)
	if i, err := strconv.Atoi(w); err == nil {
		return i, i >= 0 && i < len(p.Colors)
	}
	w = strings.ToLower(strings.NewReplacer(" ", "", "-", "", "_", "").Replace(w))
	for i, name := range p.Names {
		if name == w || (w == "gray" && name == "grey") || (w == "lightgray" && name == "lightgrey") {
			return i, true
		}
	}
	return 0, false
}
