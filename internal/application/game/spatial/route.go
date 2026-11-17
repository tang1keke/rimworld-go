package spatial

import "rimworld-go/internal/domain/ai/pathing"

var steps = [8][2]int{
	{0, -1}, {1, -1}, {1, 0}, {1, 1},
	{0, 1}, {-1, 1}, {-1, 0}, {-1, -1},
}

// Before breaks path ties in map row-major order.
func Before(ax, ay, bx, by int) bool {
	if ay != by {
		return ay < by
	}
	return ax < bx
}

// StepOpen checks a neighbour, blocked corners, and the movement index.
func StepOpen(q pathing.Query, o *Occupancy, fx, fy, tx, ty int) bool {
	dx, dy := tx-fx, ty-fy
	if dx < -1 || dx > 1 || dy < -1 || dy > 1 || dx == 0 && dy == 0 {
		return false
	}
	if !q.Walkable(tx, ty) || o.At(tx, ty) != 0 {
		return false
	}
	if dx != 0 && dy != 0 && (!q.Walkable(fx+dx, fy) || !q.Walkable(fx, fy+dy)) {
		return false
	}
	return true
}

// FreeNextTo selects the nearest one-step legal stop adjacent to work.
func FreeNextTo(fx, fy, wx, wy int, stop bool, free func(x, y int) bool) (x, y int, ok bool) {
	if pathing.Dist(fx, fy, wx, wy) <= 1 && stop {
		return fx, fy, true
	}
	bestD := 0
	for _, d := range steps {
		nx, ny := fx+d[0], fy+d[1]
		if pathing.Dist(nx, ny, wx, wy) > 1 || !free(nx, ny) {
			continue
		}
		dist := pathing.Octile(nx, ny, wx, wy)
		if !ok || dist < bestD || dist == bestD && Before(nx, ny, x, y) {
			x, y, bestD, ok = nx, ny, dist, true
		}
	}
	return
}

// NearestFreeStep prefers stopping on the current cell when tied.
func NearestFreeStep(fx, fy, tx, ty int, stop bool, free func(x, y int) bool) (x, y int, ok bool) {
	x, y, ok = fx, fy, stop
	bestD := pathing.Octile(fx, fy, tx, ty)
	for _, d := range steps {
		nx, ny := fx+d[0], fy+d[1]
		if !free(nx, ny) {
			continue
		}
		dist := pathing.Octile(nx, ny, tx, ty)
		if !ok || dist < bestD || dist == bestD && (x != fx || y != fy) && Before(nx, ny, x, y) {
			x, y, bestD, ok = nx, ny, dist, true
		}
	}
	return
}

// RouteCost measures a candidate path using its query's movement extras.
func RouteCost(q pathing.Query, fx, fy int, path []pathing.Point) int {
	c, _ := q.(pathing.Coster)
	cost := 0
	for _, step := range path {
		cost += pathing.Octile(fx, fy, step.X, step.Y)
		if c != nil {
			cost += max(c.ExtraCost(step.X, step.Y), 0)
		}
		fx, fy = step.X, step.Y
	}
	return cost
}

// AsideRoute searches a bounded ring for a reachable step off both paths.
// The callbacks carry only map rules; scratch and stable selection live here.
func AsideRoute(px, py, wx, wy int, working bool,
	stepOpen func(fx, fy, tx, ty int) bool,
	blocked func(x, y int) bool) ([]pathing.Point, bool) {
	const radius = 5
	const side = 2*radius + 1
	var parent [side * side]int16
	var depth [side * side]int8
	for i := range parent {
		parent[i] = -1
	}
	local := func(x, y int) (int, bool) {
		lx, ly := x-px+radius, y-py+radius
		if lx < 0 || ly < 0 || lx >= side || ly >= side {
			return 0, false
		}
		return ly*side + lx, true
	}
	start, _ := local(px, py)
	parent[start] = int16(start)
	queue := []pathing.Point{{X: px, Y: py}}
	best, found, bestDepth, bestKey := pathing.Point{}, false, int8(0), 0
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		ci, _ := local(cur.X, cur.Y)
		if found && depth[ci] >= bestDepth {
			break
		}
		for _, d := range steps {
			x, y := cur.X+d[0], cur.Y+d[1]
			li, in := local(x, y)
			if !in || parent[li] >= 0 || !stepOpen(cur.X, cur.Y, x, y) {
				continue
			}
			parent[li], depth[li] = int16(ci), depth[ci]+1
			queue = append(queue, pathing.Point{X: x, Y: y})
			if blocked(x, y) {
				continue
			}
			key := 0
			if working {
				key = pathing.Octile(x, y, wx, wy)
				if pathing.Dist(x, y, wx, wy) > 1 {
					key += 1 << 20
				}
			}
			if !found || key < bestKey || key == bestKey && Before(x, y, best.X, best.Y) {
				best, found, bestDepth, bestKey = pathing.Point{X: x, Y: y}, true, depth[li], key
			}
		}
	}
	if !found {
		return nil, false
	}
	var rev []pathing.Point
	for c := best; c.X != px || c.Y != py; {
		rev = append(rev, c)
		li, _ := local(c.X, c.Y)
		p := int(parent[li])
		c = pathing.Point{X: px + p%side - radius, Y: py + p/side - radius}
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev, true
}
