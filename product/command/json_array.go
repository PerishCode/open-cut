package command

// jsonArray copies a collection into a field the schema declares
// nullable:"false". Appending to a nil slice returns nil for an empty source,
// and a nil slice marshals to null, so the obvious `append([]T(nil), src...)`
// silently emits a payload that contradicts the declared contract - and it
// does so only when the collection happens to be empty, which is exactly the
// case least likely to be exercised. Starting from an empty slice keeps an
// empty collection encoded as [].
func jsonArray[Element any](source []Element) []Element {
	return append(make([]Element, 0, len(source)), source...)
}
