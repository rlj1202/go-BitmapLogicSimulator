package gobls

import (
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"time"
)

const (
	BRIGHT_MIN = 57087 // 223(0xDF) for uint8, 57087(0xDEFF) for uint32

	TIME_RAISE  = 0.5
	TIME_FALL   = 0.5
	TIME_RANDOM = 0.5
)

type Simulator struct {
	prevImage image.Image
	curImage  image.Image

	width  int
	height int

	wireMap   [][]int
	wireRemap []int

	states []bool // wire states

	gates    []*gate // not gates
	gatePerm []int   // permutation for not gates
}

func NewSimulator() *Simulator {
	simulator := new(Simulator)

	return simulator
}

func (simulator *Simulator) LoadImage(img image.Image) {
	simulator.prevImage = simulator.curImage
	simulator.curImage = img

	width := img.Bounds().Max.X
	height := img.Bounds().Max.Y

	// search wires horizontally
	wireMap := make([][]int, height)
	for i := range wireMap {
		wireMap[i] = make([]int, width)
	}

	wireIdx := -1
	for y := 0; y < height; y++ {
		pixel := img.At(0, y)

		if isConductive(pixel) {
			wireIdx++

			wireMap[y][0] = wireIdx
		} else {
			wireMap[y][0] = -1
		}

		for x := 1; x < width; x++ {
			prevPixel := img.At(x-1, y)
			curPixel := img.At(x, y)

			if isConductive(curPixel) {
				if !isConductive(prevPixel) {
					wireIdx++
				}

				wireMap[y][x] = wireIdx
			} else {
				wireMap[y][x] = -1
			}
		}
	}

	// remap wires
	wireRemap := make([]int, wireIdx+1)
	for i := range wireRemap {
		wireRemap[i] = i
	}

	for y := 1; y < height; y++ {
		for x := 0; x < width; x++ {
			upperWire := wireMap[y-1][x]
			lowerWire := wireMap[y][x]

			if upperWire >= 0 && lowerWire >= 0 {
				upperIdx := wireRemap[upperWire]
				lowerIdx := wireRemap[lowerWire]
				if upperIdx != lowerIdx {
					// connect two wire
					for i, v := range wireRemap {
						if v == lowerIdx {
							wireRemap[i] = upperIdx
						}
					}
				}
			}
		}
	}

	// search crossing wires and not gates
	gates := make([]*gate, 0)
	for y := 1; y < height-1; y++ {
		for x := 1; x < width-1; x++ {
			if wireMap[y][x] < 0 && wireMap[y][x-1] >= 0 && wireMap[y][x+1] >= 0 && wireMap[y-1][x] >= 0 && wireMap[y+1][x] >= 0 {
				flag := 0

				if wireMap[y-1][x-1] >= 0 {
					flag |= 1 << 0
				}
				if wireMap[y-1][x+1] >= 0 {
					flag |= 1 << 1
				}
				if wireMap[y+1][x+1] >= 0 {
					flag |= 1 << 2
				}
				if wireMap[y+1][x-1] >= 0 {
					flag |= 1 << 3
				}

				switch flag {
				case 0: // crossing wire
					// connect up, down wire and left, right wire
					upperIdx := wireRemap[wireMap[y-1][x]]
					lowerIdx := wireRemap[wireMap[y+1][x]]
					leftIdx := wireRemap[wireMap[y][x-1]]
					rightIdx := wireRemap[wireMap[y][x+1]]

					for i, v := range wireRemap {
						if v == lowerIdx {
							wireRemap[i] = upperIdx
						}
					}
					for i, v := range wireRemap {
						if v == rightIdx {
							wireRemap[i] = leftIdx
						}
					}
				case 1 + 2: // not gate down
					gates = append(gates, &gate{in: point{x, y - 1}, out: point{x, y + 1}})
				case 2 + 4: // not gate left
					gates = append(gates, &gate{in: point{x + 1, y}, out: point{x - 1, y}})
				case 4 + 8: // not gate up
					gates = append(gates, &gate{in: point{x, y + 1}, out: point{x, y - 1}})
				case 8 + 1: // not gate right
					gates = append(gates, &gate{in: point{x - 1, y}, out: point{x + 1, y}})
				}
			}
		}
	}

	// resolve gate in, out idx
	for _, gate := range gates {
		gate.inIdx = wireRemap[wireMap[gate.in.y][gate.in.x]]
		gate.outIdx = wireRemap[wireMap[gate.out.y][gate.out.x]]
	}

	// find input gates
	for _, gate := range gates {
		gate.inGates = make([]int, 0)
		for gateIdx, inputGate := range gates {
			if gate.inIdx == inputGate.outIdx {
				gate.inGates = append(gate.inGates, gateIdx)
			}
		}
	}

	// gate permutation
	gatePerm := rand.Perm(len(gates))

	// init wire state
	states := make([]bool, len(wireRemap))
	for i := range states {
		states[i] = false
	}

	// load previous states
	if simulator.prevImage != nil {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				if wireMap[y][x] != 0 && isConductive(simulator.prevImage.At(x, y)) {
					states[wireRemap[wireMap[y][x]]] = true
				}
			}
		}
	}

	simulator.width = width
	simulator.height = height
	simulator.wireMap = wireMap
	simulator.wireRemap = wireRemap
	simulator.gates = gates
	simulator.states = states
	simulator.gatePerm = gatePerm

	simulator.Simulate()

	simulator.test()
}

func (simulator *Simulator) test() {
	// wire remapping test image
	wireRemapImgFile, err := os.Create("wireMap.png")
	if err != nil {
		panic(err)
	}
	wireRemapImg := image.NewRGBA(simulator.curImage.Bounds())

	rand.Seed(time.Now().UTC().UnixNano())
	randomRColor := rand.Perm(200)
	randomGColor := rand.Perm(200)
	randomBColor := rand.Perm(200)

	for y := 0; y < simulator.height; y++ {
		for x := 0; x < simulator.width; x++ {
			wire := simulator.wireMap[y][x]
			if wire >= 0 {
				idx := simulator.wireRemap[wire]

				wireRemapImg.Set(x, y, color.RGBA{
					uint8(randomRColor[idx%200] + 55),
					uint8(randomGColor[idx%200] + 55),
					uint8(randomBColor[idx%200] + 55),
					255,
				})
			} else {
				wireRemapImg.Set(x, y, color.RGBA{0, 0, 0, 255})
			}
		}
	}

	png.Encode(wireRemapImgFile, wireRemapImg)

	// gates test image
	gateImgFile, err := os.Create("gate.png")
	if err != nil {
		panic(err)
	}
	gateImg := image.NewRGBA(simulator.curImage.Bounds())

	for y := 0; y < simulator.height; y++ {
		for x := 0; x < simulator.width; x++ {
			gateImg.Set(x, y, color.RGBA{0, 0, 0, 0})
		}
	}
	for _, gate := range simulator.gates {
		gateImg.Set(gate.in.x, gate.in.y, color.RGBA{255, 0, 0, 255})
		gateImg.Set(gate.out.x, gate.out.y, color.RGBA{0, 255, 0, 255})
	}

	png.Encode(gateImgFile, gateImg)
}

func (simulator *Simulator) Simulate() {
	for i := range simulator.gates {
		g := simulator.gates[simulator.gatePerm[i]]
		newState := !simulator.gateInput(g)
		g.updateState(newState)
	}

	simulator.storeGateStatesToWires()
}

func (simulator *Simulator) Set(x, y int, state bool) bool {
	wire := simulator.wireMap[y][x]

	if wire >= 0 {
		wireIdx := simulator.wireRemap[wire]
		simulator.states[wireIdx] = state

		return true
	}

	return false
}

func (simulator *Simulator) Get(x, y int) bool {
	wire := simulator.wireMap[y][x]

	if wire >= 0 {
		wireIdx := simulator.wireRemap[wire]
		state := simulator.states[wireIdx]

		return state
	}

	return false
}

func (simulator *Simulator) Size() (int, int) {
	return simulator.width, simulator.height
}

func (simulator *Simulator) PerPixel(f func(int, int, bool)) {
	for y := 0; y < simulator.height; y++ {
		for x := 0; x < simulator.width; x++ {
			wire := simulator.wireMap[y][x]
			state := false
			if wire != -1 {
				state = simulator.states[simulator.wireRemap[wire]]
			}
			f(x, y, state)
		}
	}
}

func isConductive(pixel color.Color) bool {
	r, g, b, _ := pixel.RGBA()

	return r > BRIGHT_MIN || g > BRIGHT_MIN || b > BRIGHT_MIN
}

func (simulator *Simulator) gateInput(g *gate) bool {
	if g.inGates == nil || len(g.inGates) == 0 {
		return simulator.states[g.inIdx]
	} else {
		for _, inGate := range g.inGates {
			if simulator.gates[inGate].state {
				return true
			}
		}

		return false
	}
}

func (simulator *Simulator) loadGateStatesFromWire() {
	for _, g := range simulator.gates {
		g.setState(simulator.states[g.outIdx])
	}
}

func (simulator *Simulator) storeGateStatesToWires() {
	for _, g := range simulator.gates {
		simulator.states[g.outIdx] = false
	}

	for _, g := range simulator.gates {
		if g.state {
			simulator.states[g.outIdx] = true
		}
	}
}
