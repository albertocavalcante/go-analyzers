package stringcopytest

type text string

// copy supports strings, but slices.Clone does not. None of these valid
// string-to-byte conversions should receive a slices.Clone suggestion.
func copyString(s string) []byte {
	dst := make([]byte, len(s))
	copy(dst, s)
	return dst
}

func copyNamedString(s text) []byte {
	dst := make([]byte, len(s))
	copy(dst, s)
	return dst
}

func copyStringSuffix(s string, start int) []byte {
	dst := make([]byte, len(s)-start)
	copy(dst, s[start:])
	return dst
}

func copyStringSlice(s string, start, end int) []byte {
	dst := make([]byte, len(s[start:end]))
	copy(dst, s[start:end])
	return dst
}
