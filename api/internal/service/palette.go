package service

import (
	"errors"
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
	// The 2022 r/place colors, white first: index 0 is the background.
	"place32": {ID: "place32", Colors: []string{
		"#FFFFFF", "#D4D7D9", "#898D90", "#515252",
		"#000000", "#6D001A", "#BE0039", "#FF4500",
		"#FFA800", "#FFD635", "#FFF8B8", "#00A368",
		"#00CC78", "#7EED56", "#00756F", "#009EAA",
		"#00CCC0", "#2450A4", "#3690EA", "#51E9F4",
		"#493AC1", "#6A5CFF", "#94B3FF", "#811E9F",
		"#B44AC0", "#E4ABFF", "#DE107F", "#FF3881",
		"#FF99AA", "#6D482F", "#9C6926", "#FFB470",
	}, Names: []string{
		"white", "lightgrey", "grey", "darkgrey",
		"black", "burgundy", "darkred", "red",
		"orange", "yellow", "paleyellow", "darkgreen",
		"green", "lightgreen", "darkteal", "teal",
		"lightteal", "darkblue", "blue", "lightblue",
		"indigo", "periwinkle", "lavender", "darkpurple",
		"purple", "palepurple", "magenta", "pink",
		"lightpink", "darkbrown", "brown", "beige",
	}},
}

var ErrUnknownPalette = errors.New("unknown palette")

// DefaultPalette matches the column default.
const DefaultPalette = "classic16"

// ValidPalette reports whether a canvas can be created with this palette id.
func ValidPalette(id string) bool {
	_, ok := palettes[id]
	return ok
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
	w = strings.ReplaceAll(w, "gray", "grey")
	for i, name := range p.Names {
		if name == w {
			return i, true
		}
	}
	return 0, false
}
