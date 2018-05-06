package gobls

import (
	"math/rand"
)

type point struct {
	x, y int
}

type gate struct {
	state     bool
	slowState float32

	in, out       point
	inIdx, outIdx int
	inGates       []int
}

func (g *gate) setState(newState bool) {
	g.state = newState

	if newState {
		g.slowState = 1
	} else {
		g.slowState = 0
	}
}

func (g *gate) updateState(newState bool) {
	if newState {
		if g.state && g.slowState >= 1 {
			return
		}

		g.slowState += TIME_RAISE * rand.Float32()

		if g.slowState >= 1 {
			g.slowState = 1
			g.state = true
		}
	} else {
		if !g.state && g.slowState <= 0 {
			return
		}

		g.slowState -= TIME_FALL * rand.Float32()

		if g.slowState <= 0 {
			g.slowState = 0
			g.state = false
		}
	}
}
