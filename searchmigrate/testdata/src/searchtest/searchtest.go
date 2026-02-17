package searchtest

import "sort"

func example() {
	s := []int{1, 2, 3, 4, 5}

	// Should be flagged.
	_ = sort.Search(len(s), func(i int) bool { return s[i] >= 3 }) // want "sort.Search can potentially be replaced with slices.BinarySearch"

	// Should be flagged.
	target := 4
	_ = sort.Search(len(s), func(i int) bool { return s[i] >= target }) // want "sort.Search can potentially be replaced with slices.BinarySearch"
}

func noMatch() {
	// Custom search function, not sort.Search — should NOT be flagged.
	Search := func(n int, f func(int) bool) int { return 0 }
	_ = Search(10, func(i int) bool { return i >= 5 })

	// sort.Search with wrong arg count — should NOT be flagged.
	// (Can't actually call sort.Search with 1 arg — it won't compile.
	// This is just to document the analyzer only checks 2-arg calls.)
}
