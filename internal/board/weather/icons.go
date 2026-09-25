package weatherboard

import (
	"image"
	"image/color"

	"github.com/parandandrd/sportsmatrix/internal/weather"
)

// iconSize is the width and height of an icon on a 32-row panel. Taller panels
// scale it by whole pixels.
const iconSize = 12

// palette is what each letter of an icon's drawing lights up as. A dot is
// left dark.
var palette = map[byte]color.RGBA{
	'Y': {R: 255, G: 190, B: 0, A: 255},   // sun
	'O': {R: 255, G: 120, B: 0, A: 255},   // sun's edge
	'M': {R: 220, G: 220, B: 150, A: 255}, // moon
	'W': {R: 210, G: 210, B: 210, A: 255}, // cloud
	'G': {R: 110, G: 110, B: 120, A: 255}, // cloud's shadow, storm cloud
	'B': {R: 40, G: 110, B: 255, A: 255},  // rain
	'S': {R: 255, G: 255, B: 255, A: 255}, // snow
	'L': {R: 255, G: 230, B: 0, A: 255},   // lightning
	'F': {R: 140, G: 140, B: 150, A: 255}, // fog
	'C': {R: 120, G: 200, B: 255, A: 255}, // wind
}

var (
	iconSun = []string{
		".....YY.....",
		"..Y..YY..Y..",
		"...Y....Y...",
		"....OOOO....",
		"...OYYYYO...",
		"YY.OYYYYO.YY",
		"YY.OYYYYO.YY",
		"...OYYYYO...",
		"....OOOO....",
		"...Y....Y...",
		"..Y..YY..Y..",
		".....YY.....",
	}
	iconMoon = []string{
		"....MMMM....",
		"..MMMMM.....",
		".MMMMM......",
		".MMMM.......",
		"MMMM........",
		"MMMM........",
		"MMMM........",
		"MMMM........",
		".MMMM......M",
		".MMMMM...MM.",
		"..MMMMMMMMM.",
		"....MMMMM...",
	}
	iconPartlyDay = []string{
		"......Y..Y..",
		".......OO...",
		"...Y..OYYO.Y",
		"......OYYO..",
		"....WW.OO...",
		"...WWWW..Y..",
		".WWWWWWWW...",
		"WWWWWWWWWW..",
		"WWWWWWWWWW..",
		"GWWWWWWWWG..",
		".GGGGGGGG...",
		"............",
	}
	iconPartlyNight = []string{
		"........MMM.",
		".......MM...",
		"......MMM...",
		"......MMM..M",
		"....WW.MMMM.",
		"...WWWW.....",
		".WWWWWWWW...",
		"WWWWWWWWWW..",
		"WWWWWWWWWW..",
		"GWWWWWWWWG..",
		".GGGGGGGG...",
		"............",
	}
	iconCloud = []string{
		"............",
		"............",
		"............",
		".....WWW....",
		"...WWWWWW...",
		"..WWWWWWWWW.",
		".WWWWWWWWWWW",
		"WWWWWWWWWWWW",
		"WWWWWWWWWWWW",
		"GWWWWWWWWWWG",
		".GGGGGGGGGG.",
		"............",
	}
	iconRain = []string{
		".....WWW....",
		"...WWWWWW...",
		"..WWWWWWWWW.",
		".WWWWWWWWWWW",
		"WWWWWWWWWWWW",
		"GWWWWWWWWWWG",
		".GGGGGGGGGG.",
		"............",
		"..B...B...B.",
		".B...B...B..",
		"............",
		"B...B...B...",
	}
	iconThunder = []string{
		".....GGG....",
		"...GGGGGG...",
		"..GGGGGGGGG.",
		".GGGGGGGGGGG",
		"GGGGGGGGGGGG",
		".GGGGLLGGGG.",
		".....LL.....",
		"....LL......",
		"...LLLLL....",
		".....LL.....",
		"....LL......",
		"....L.......",
	}
	iconSnow = []string{
		".....WWW....",
		"...WWWWWW...",
		"..WWWWWWWWW.",
		".WWWWWWWWWWW",
		"WWWWWWWWWWWW",
		"GWWWWWWWWWWG",
		".GGGGGGGGGG.",
		"............",
		".S...S...S..",
		"............",
		"...S...S...S",
		"............",
	}
	iconSleet = []string{
		".....WWW....",
		"...WWWWWW...",
		"..WWWWWWWWW.",
		".WWWWWWWWWWW",
		"WWWWWWWWWWWW",
		"GWWWWWWWWWWG",
		".GGGGGGGGGG.",
		"............",
		".B...S...B..",
		"B.......B...",
		"...S...B...S",
		"..B.........",
	}
	iconFog = []string{
		"............",
		"............",
		".FFFFFFFFF..",
		"............",
		"...FFFFFFFFF",
		"............",
		"FFFFFFFFF...",
		"............",
		"..FFFFFFFFFF",
		"............",
		"FFFFFFF.....",
		"............",
	}
	iconWind = []string{
		"............",
		".......CC...",
		"..........C.",
		"CCCCCCCCCC..",
		"............",
		"CCCCCCCCCCC.",
		"...........C",
		"..........C.",
		"CCCCCC..CC..",
		"......C.....",
		".....C......",
		"............",
	}
)

// iconFor is the drawing for a kind of weather, by day or night.
func iconFor(kind weather.Kind, day bool) []string {
	switch kind {
	case weather.Clear:
		if day {
			return iconSun
		}
		return iconMoon
	case weather.PartlyCloudy:
		if day {
			return iconPartlyDay
		}
		return iconPartlyNight
	case weather.Cloudy:
		return iconCloud
	case weather.Rain:
		return iconRain
	case weather.Thunder:
		return iconThunder
	case weather.Snow:
		return iconSnow
	case weather.Sleet:
		return iconSleet
	case weather.Fog:
		return iconFog
	case weather.Wind:
		return iconWind
	}
	return iconCloud
}

// drawIcon draws an icon with its top left corner at pt, each of its pixels
// scale pixels square.
func drawIcon(img *image.RGBA, pt image.Point, icon []string, scale int) {
	if scale < 1 {
		scale = 1
	}
	for y, row := range icon {
		for x := 0; x < len(row); x++ {
			clr, ok := palette[row[x]]
			if !ok {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.SetRGBA(pt.X+x*scale+dx, pt.Y+y*scale+dy, clr)
				}
			}
		}
	}
}
