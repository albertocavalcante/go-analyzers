package clamptest

func example() {
	x := 50
	lo := 0
	hi := 100

	// Should be flagged.
	if x < lo { // want "clamp pattern can be simplified"
		x = lo
	} else if x > hi {
		x = hi
	}
	_ = x
}

func reversed() {
	x := 50
	lo := 0
	hi := 100

	// Should be flagged (reversed order).
	if x > hi { // want "clamp pattern can be simplified"
		x = hi
	} else if x < lo {
		x = lo
	}
	_ = x
}

func noMatch() {
	x := 50

	// Not a clamp — different variables assigned.
	y := 0
	if x < 0 {
		y = 0
	} else if x > 100 {
		x = 100
	}
	_ = x
	_ = y

	// Not a clamp — has else clause.
	if x < 0 {
		x = 0
	} else if x > 100 {
		x = 100
	} else {
		x = x + 1
	}
	_ = x
}
