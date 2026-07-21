package server

import (
	"math/rand/v2"

	"github.com/heartlazyli/asciigame/internal/config"
)

// Map cell characters, mirroring map.h:12-20.
const (
	cellEmpty  = ' '
	cellWall   = '#'
	cellSpawn  = '$'
	cellHealth = '+'
	cellAttack = '^'
	cellShield = '*'
)

// gameMap is the fixed-size grid; index [y][x], row width MapWidth (+1 slot kept
// to match the C char[H][W+1] layout, unused as a NUL terminator here).
type gameMap [config.MapHeight][config.MapWidth + 1]byte

// mapTemplate is the predefined map, copied verbatim from map.c:13-34.
var mapTemplate = [config.MapHeight]string{
	"##################################################",
	"#                    $                           #",
	"#   ##    ##         $         ##    ##    $     #",
	"#   ##    ##    $              ##    ##          #",
	"#              ###        ###              $     #",
	"#   $          # $        $ #          $         #",
	"#              ###        ###                    #",
	"#       $                          $       ##    #",
	"#   ##              ####              ##         #",
	"#   ##     $        #  #        $     ##    $    #",
	"#          $        #  #        $                #",
	"#   ##     $        ####        $     ##         #",
	"#   ##                                ##    $    #",
	"#       $                          $             #",
	"#              ###        ###              $     #",
	"#   $          # $        $ #          $         #",
	"#              ###        ###                    #",
	"#   ##    ##    $              ##    ##          #",
	"#   ##    ##         $         ##    ##    $     #",
	"##################################################",
}

// mapGenerate copies the template into m, mirroring map_generate (map.c:46-56).
func mapGenerate(m *gameMap) {
	for y := 0; y < config.MapHeight; y++ {
		for x := 0; x < config.MapWidth; x++ {
			m[y][x] = mapTemplate[y][x]
		}
		m[y][config.MapWidth] = 0
	}
}

// mapIsWalkable mirrors map_is_walkable (map.c:58-73): only walls block; out of
// bounds is not walkable.
func mapIsWalkable(m *gameMap, x, y int) bool {
	if x < 0 || x >= config.MapWidth || y < 0 || y >= config.MapHeight {
		return false
	}
	return m[y][x] != cellWall
}

// mapCenter mirrors map_get_center (map.c:87-94).
func mapCenter() (int, int) { return config.MapWidth / 2, config.MapHeight / 2 }

// mapIsInPoison mirrors map_is_in_poison (map.c:75-85): Chebyshev distance from
// center greater than the radius means the cell is in the poison (dangerous).
func mapIsInPoison(x, y, radius int) bool {
	cx, cy := mapCenter()
	dx := abs(x - cx)
	dy := abs(y - cy)
	dist := dx
	if dy > dx {
		dist = dy
	}
	return dist > radius
}

// mapDistance mirrors map_distance (map.c:144-146): Manhattan distance.
func mapDistance(x1, y1, x2, y2 int) int { return abs(x1-x2) + abs(y1-y2) }

// mapInitialPoisonRadius mirrors map_get_initial_poison_radius (map.c:148-155).
func mapInitialPoisonRadius() int {
	cx, cy := config.MapWidth/2, config.MapHeight/2
	if cx > cy {
		return cx
	}
	return cy
}

// mapRandomPosition mirrors map_random_position (map.c:96-118): up to 1000 tries
// for a walkable cell in [1,W-2]x[1,H-2], falling back to the center.
func mapRandomPosition(m *gameMap) (int, int) {
	for i := 0; i < 1000; i++ {
		rx := rand.IntN(config.MapWidth-2) + 1
		ry := rand.IntN(config.MapHeight-2) + 1
		if mapIsWalkable(m, rx, ry) {
			return rx, ry
		}
	}
	return config.MapWidth / 2, config.MapHeight / 2
}

// mapRandomItemPosition mirrors map_random_item_position (map.c:120-142):
// prefers spawn points/empty cells, falling back to a random walkable cell.
func mapRandomItemPosition(m *gameMap) (int, int) {
	for i := 0; i < 1000; i++ {
		rx := rand.IntN(config.MapWidth-2) + 1
		ry := rand.IntN(config.MapHeight-2) + 1
		c := m[ry][rx]
		if c == cellSpawn || c == cellEmpty {
			return rx, ry
		}
	}
	return mapRandomPosition(m)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// randItemType returns a random spawnable item type (1..3), mirroring the C
// "rand() % 3 + 1" used for item generation.
func randItemType() ItemType { return ItemType(rand.IntN(3) + 1) }

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
