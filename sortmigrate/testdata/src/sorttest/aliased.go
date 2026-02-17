package sorttest

import s "sort"

func aliasedImport() {
	strs := []string{"c", "a", "b"}

	// Should be flagged even with aliased import.
	s.Strings(strs) // want `sort\.Strings can be replaced with slices\.Sort`

	_ = strs
}
