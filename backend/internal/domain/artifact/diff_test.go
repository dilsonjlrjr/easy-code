package artifact

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// US-27 CA-1: header fields added, removed and modified, by JSON pointer, items of lists by index.
func TestCompareHeadersPointsEachChangedField(t *testing.T) {
	before := json.RawMessage(`{"id":"US-01","versao":1,"titulo":"Reservar","criterios_aceite":[{"id":"CA-1","entao":"vejo"}],"rnf":["RNF-09"],"a/b":1}`)
	after := json.RawMessage(`{"id":"US-01","versao":2,"titulo":"Reservar sala","criterios_aceite":[{"id":"CA-1","entao":"vejo"},{"id":"CA-2","entao":"novo"}],"prioridade":"must","a/b":1}`)
	changes, err := CompareHeaders(before, after)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, c := range changes {
		got = append(got, c.Operation+" "+c.Path)
	}
	want := "added /criterios_aceite/1 added /prioridade removed /rnf modified /titulo modified /versao"
	if strings.Join(got, " ") != want {
		t.Fatalf("changes: %v", got)
	}
	if changes[3].Before != "Reservar" || changes[3].After != "Reservar sala" || changes[1].Before != nil {
		t.Fatalf("values: %+v", changes)
	}
	same, _ := CompareHeaders(before, before)
	if len(same) != 0 {
		t.Fatalf("equal headers: %v", same)
	}
	if _, err := CompareHeaders(json.RawMessage(`{`), after); err == nil {
		t.Fatal("bad header accepted")
	}
}

func render(changes []LineChange) string {
	var out []string
	for _, c := range changes {
		sign := map[string]string{OperationEqual: " ", OperationAdded: "+", OperationRemoved: "-"}[c.Operation]
		out = append(out, fmt.Sprintf("%s%d:%d %s", sign, c.LineFrom, c.LineTo, c.Text))
	}
	return strings.Join(out, "|")
}

// US-27 CA-1: body lines added, removed and kept, with the line of each side.
func TestCompareBodiesIsAUnifiedLineDiff(t *testing.T) {
	cases := []struct{ before, after, want string }{
		{"a\nb\nc\n", "a\nb\nc\n", " 1:1 a| 2:2 b| 3:3 c"},
		{"a\nb\nc", "a\nx\nc", " 1:1 a|-2:0 b|+0:2 x| 3:3 c"},
		{"a\nc", "a\nb\nc", " 1:1 a|+0:2 b| 2:3 c"},
		{"", "a", "+0:1 a"},
		{"a\nb", "", "-1:0 a|-2:0 b"},
		{"x\na\nb\ny", "a\nz\nb", "-1:0 x| 2:1 a|+0:2 z| 3:3 b|-4:0 y"},
	}
	for _, c := range cases {
		if got := render(CompareBodies(c.before, c.after)); got != c.want {
			t.Errorf("%q → %q:\n got %s\nwant %s", c.before, c.after, got, c.want)
		}
	}
}

// Applying the script to the first body gives the second one exactly, also for long bodies.
func TestCompareBodiesRebuildsBothSides(t *testing.T) {
	var a, b []string
	for i := 0; i < 3000; i++ {
		a = append(a, fmt.Sprintf("linha %d", i))
		if i%7 != 0 {
			b = append(b, fmt.Sprintf("linha %d", i))
		}
		if i%11 == 0 {
			b = append(b, fmt.Sprintf("nova %d", i))
		}
	}
	for _, pair := range [][2][]string{{a, b}, {a, []string{"tudo diferente"}}} {
		changes := CompareBodies(strings.Join(pair[0], "\n"), strings.Join(pair[1], "\n"))
		var from, to []string
		for _, c := range changes {
			if c.Operation != OperationAdded {
				from = append(from, c.Text)
			}
			if c.Operation != OperationRemoved {
				to = append(to, c.Text)
			}
		}
		if strings.Join(from, "\n") != strings.Join(pair[0], "\n") || strings.Join(to, "\n") != strings.Join(pair[1], "\n") {
			t.Fatal("the diff does not rebuild both bodies")
		}
	}
}
