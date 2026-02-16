package makecopytest

func example() {
	src := []int{1, 2, 3}

	// Should be flagged.
	dst := make([]int, len(src)) // want "make\\+copy can be simplified to dst := slices.Clone\\(src\\)"
	copy(dst, src)
	_ = dst

	// Should be flagged (string slice).
	names := []string{"a", "b"}
	result := make([]string, len(names)) // want "make\\+copy can be simplified to result := slices.Clone\\(names\\)"
	copy(result, names)
	_ = result
}

func noMatch() {
	src := []int{1, 2, 3}

	// Different length — should NOT be flagged.
	dst := make([]int, len(src)+1)
	copy(dst, src)
	_ = dst

	// Three-arg make — should NOT be flagged.
	dst2 := make([]int, len(src), 100)
	copy(dst2, src)
	_ = dst2

	// Copy target differs — should NOT be flagged.
	other := make([]int, 10)
	dst3 := make([]int, len(src))
	copy(dst3, other)
	_ = dst3

	// Not consecutive — should NOT be flagged.
	dst4 := make([]int, len(src))
	_ = 42
	copy(dst4, src)
	_ = dst4
}
