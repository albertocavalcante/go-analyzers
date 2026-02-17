package searchtest

import s "sort"

func aliasedSearch() {
	data := []int{1, 2, 3, 4, 5}

	// Should be flagged even with aliased sort import.
	_ = s.Search(len(data), func(i int) bool { return data[i] >= 3 }) // want "sort.Search can potentially be replaced with slices.BinarySearch"
}
