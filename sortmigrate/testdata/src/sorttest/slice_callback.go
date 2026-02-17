package sorttest

import (
	"io/fs"
	"sort"
)

type Item struct {
	Name  string
	Age   int
	Inner struct {
		Key string
	}
}

type Entry struct {
	name string
}

func (e Entry) GetName() string { return e.name }

// Field access: sort.Slice with struct field comparison.
func sliceFieldAccess() {
	items := []Item{{Name: "b"}, {Name: "a"}}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = items
}

// Method call: sort.Slice with method call comparison.
func sliceMethodCall() {
	entries := []Entry{{name: "b"}, {name: "a"}}
	sort.Slice(entries, func(i, j int) bool { return entries[i].GetName() < entries[j].GetName() }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = entries
}

// Reversed comparison: sort.Slice with > operator.
func sliceReversed() {
	s := []int{1, 2, 3}
	sort.Slice(s, func(i, j int) bool { return s[i] > s[j] }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = s
}

// Swapped params: s[j] < s[i] is another way to express descending sort.
func sliceSwappedParams() {
	s := []int{1, 2, 3}
	sort.Slice(s, func(i, j int) bool { return s[j] < s[i] }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = s
}

// Swapped params + > operator: double reversal = ascending.
func sliceSwappedParamsGTR() {
	s := []int{1, 2, 3}
	sort.Slice(s, func(i, j int) bool { return s[j] > s[i] }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = s
}

// Pointer element type: sort.Slice with pointer-to-struct slice.
func slicePointerElem() {
	items := []*Item{{Name: "b"}, {Name: "a"}}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = items
}

// Chained field access: sort.Slice with nested struct field.
func sliceChainedField() {
	items := []Item{{}, {}}
	sort.Slice(items, func(i, j int) bool { return items[i].Inner.Key < items[j].Inner.Key }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = items
}

// SliceStable with field access.
func sliceStableFieldAccess() {
	items := []Item{{Name: "b"}, {Name: "a"}}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name }) // want `sort\.SliceStable can be replaced with slices\.SortStableFunc`
	_ = items
}

// SliceIsSorted with method call.
func sliceIsSortedMethodCall() {
	entries := []Entry{{name: "b"}, {name: "a"}}
	_ = sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].GetName() < entries[j].GetName() }) // want `sort\.SliceIsSorted can be replaced with slices\.IsSortedFunc`
}

// Cross-package element type: fs.DirEntry with "io/fs" already imported — fixable.
func sliceCrossPackageImported(entries []fs.DirEntry) {
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = entries
}

// Multiline callback with single return — should still be fixable.
func sliceMultiline() {
	s := []int{3, 1, 2}
	sort.Slice(s, func(i, j int) bool { // want `sort\.Slice can be replaced with slices\.SortFunc`
		return s[i] < s[j]
	})
	_ = s
}

// Complex callback: multi-statement body — report-only, no auto-fix.
func sliceComplexCallback() {
	s := []int{3, 1, 2}
	sort.Slice(s, func(i, j int) bool { // want `sort\.Slice can be replaced with slices\.SortFunc`
		if s[i] == s[j] {
			return false
		}
		return s[i] < s[j]
	})
	_ = s
}

// Non-inline callback: variable reference — report-only, no auto-fix.
func sliceNonInlineCallback() {
	s := []int{3, 1, 2}
	less := func(i, j int) bool { return s[i] < s[j] }
	sort.Slice(s, less) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = s
	_ = less
}

// Mismatched chains — report-only (comparing different fields).
func sliceMismatchedChains() {
	items := []Item{{Name: "b"}, {Name: "a"}}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Inner.Key }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = items
}

// Different slice in callback — report-only (callback indexes a different slice).
func sliceDifferentSlice() {
	s := []int{3, 1, 2}
	other := []int{1, 2, 3}
	sort.Slice(s, func(i, j int) bool { return other[i] < other[j] }) // want `sort\.Slice can be replaced with slices\.SortFunc`
	_ = s
	_ = other
}
