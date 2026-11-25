package skill

import (
	"reflect"
	"testing"
)

func TestEntriesListsSkillMdFirstThenByPath(t *testing.T) {
	got := Entries(map[string]string{
		"references/b.md": "12345678",
		"SKILL.md":        "1234",
		"references/a.md": "",
		"LICENSE":         "123456789",
	})
	want := []Entry{
		{Path: "SKILL.md", Tokens: 1},
		{Path: "LICENSE", Tokens: 3},
		{Path: "references/a.md", Tokens: 1},
		{Path: "references/b.md", Tokens: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Entries() = %v, want %v", got, want)
	}
}

func TestEntriesOfNoFilesIsEmpty(t *testing.T) {
	if got := Entries(nil); len(got) != 0 {
		t.Fatalf("Entries(nil) = %v, want empty", got)
	}
}
