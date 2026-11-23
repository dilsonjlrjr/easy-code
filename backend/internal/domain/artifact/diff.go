package artifact

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Operations of a comparison (ArtifactComparison, US-27 CA-1).
const (
	OperationAdded    = "added"
	OperationRemoved  = "removed"
	OperationModified = "modified"
	OperationEqual    = "equal"
)

// FieldChange is a header field added, removed or modified between two versions; Path is a JSON pointer.
type FieldChange struct {
	Path      string
	Operation string
	Before    any // nil when added
	After     any // nil when removed
}

// LineChange is one line of the unified diff of the bodies; LineFrom and LineTo are 1-based, 0 when the
// line is not on that side.
type LineChange struct {
	Operation string
	LineFrom  int
	LineTo    int
	Text      string
}

// CompareHeaders lists the fields of the two headers (JSON objects) that differ, in path order. Objects
// are compared key by key and arrays index by index, so a changed item of a list points at the item.
func CompareHeaders(before, after json.RawMessage) ([]FieldChange, error) {
	var from, to any
	if err := decodeHeader(before, &from); err != nil {
		return nil, err
	}
	if err := decodeHeader(after, &to); err != nil {
		return nil, err
	}
	changes := []FieldChange{}
	compareValues("", from, to, &changes)
	return changes, nil
}

func decodeHeader(raw json.RawMessage, out *any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		*out = map[string]any{}
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(out)
}

// pointerToken escapes a key for a JSON pointer (RFC 6901).
func pointerToken(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func compareValues(path string, from, to any, changes *[]FieldChange) {
	fromObject, fromIsObject := from.(map[string]any)
	toObject, toIsObject := to.(map[string]any)
	if fromIsObject && toIsObject {
		keys := make([]string, 0, len(fromObject)+len(toObject))
		for k := range fromObject {
			keys = append(keys, k)
		}
		for k := range toObject {
			if _, ok := fromObject[k]; !ok {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := path + "/" + pointerToken(k)
			before, inFrom := fromObject[k]
			after, inTo := toObject[k]
			switch {
			case !inFrom:
				*changes = append(*changes, FieldChange{Path: child, Operation: OperationAdded, After: after})
			case !inTo:
				*changes = append(*changes, FieldChange{Path: child, Operation: OperationRemoved, Before: before})
			default:
				compareValues(child, before, after, changes)
			}
		}
		return
	}
	fromList, fromIsList := from.([]any)
	toList, toIsList := to.([]any)
	if fromIsList && toIsList {
		for i := 0; i < max(len(fromList), len(toList)); i++ {
			child := path + "/" + strconv.Itoa(i)
			switch {
			case i >= len(fromList):
				*changes = append(*changes, FieldChange{Path: child, Operation: OperationAdded, After: toList[i]})
			case i >= len(toList):
				*changes = append(*changes, FieldChange{Path: child, Operation: OperationRemoved, Before: fromList[i]})
			default:
				compareValues(child, fromList[i], toList[i], changes)
			}
		}
		return
	}
	if !reflect.DeepEqual(from, to) {
		*changes = append(*changes, FieldChange{Path: path, Operation: OperationModified, Before: from, After: to})
	}
}

// maxEditDistance bounds the work of the line diff; beyond it the differing middle of the bodies is shown
// as removed then added, which is still exact (RNF-09: a comparison never takes unbounded time).
const maxEditDistance = 1000

// CompareBodies is the unified line diff of two bodies (Myers), with the equal lines kept for context.
func CompareBodies(before, after string) []LineChange {
	a, b := splitLines(before), splitLines(after)
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	out := make([]LineChange, 0, len(a)+len(b))
	for i := 0; i < prefix; i++ {
		out = append(out, LineChange{Operation: OperationEqual, LineFrom: i + 1, LineTo: i + 1, Text: a[i]})
	}
	middle := myers(a[prefix:len(a)-suffix], b[prefix:len(b)-suffix])
	for _, change := range middle {
		if change.LineFrom > 0 {
			change.LineFrom += prefix
		}
		if change.LineTo > 0 {
			change.LineTo += prefix
		}
		out = append(out, change)
	}
	for i := 0; i < suffix; i++ {
		from, to := len(a)-suffix+i, len(b)-suffix+i
		out = append(out, LineChange{Operation: OperationEqual, LineFrom: from + 1, LineTo: to + 1, Text: a[from]})
	}
	return out
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// myers returns the edit script of a into b (1-based line numbers relative to the slices).
func myers(a, b []string) []LineChange {
	n, m := len(a), len(b)
	if n == 0 || m == 0 || n+m > 64*maxEditDistance {
		return replaceAll(a, b)
	}
	limit := min(n+m, maxEditDistance)
	offset := limit
	v := make([]int, 2*limit+2)
	trace := make([][]int, 0, 16)
	found := false
	for d := 0; d <= limit && !found; d++ {
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
	}
	if !found {
		return replaceAll(a, b)
	}
	// Backtrack from (n, m) through the saved frontiers.
	var reversed []LineChange
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		frontier := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && frontier[offset+k-1] < frontier[offset+k+1]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := frontier[offset+prevK]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			reversed = append(reversed, LineChange{Operation: OperationEqual, LineFrom: x, LineTo: y, Text: a[x-1]})
			x--
			y--
		}
		if d == 0 {
			break
		}
		if x == prevX {
			reversed = append(reversed, LineChange{Operation: OperationAdded, LineTo: y, Text: b[y-1]})
		} else {
			reversed = append(reversed, LineChange{Operation: OperationRemoved, LineFrom: x, Text: a[x-1]})
		}
		x, y = prevX, prevY
	}
	out := make([]LineChange, len(reversed))
	for i, change := range reversed {
		out[len(reversed)-1-i] = change
	}
	return out
}

func replaceAll(a, b []string) []LineChange {
	out := make([]LineChange, 0, len(a)+len(b))
	for i, line := range a {
		out = append(out, LineChange{Operation: OperationRemoved, LineFrom: i + 1, Text: line})
	}
	for i, line := range b {
		out = append(out, LineChange{Operation: OperationAdded, LineTo: i + 1, Text: line})
	}
	return out
}
