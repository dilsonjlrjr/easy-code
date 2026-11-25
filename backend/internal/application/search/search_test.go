package search_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"easycode/internal/application/search"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
)

type fakeStore struct {
	term  string
	limit int
	calls int
}

func (f *fakeStore) Find(_ context.Context, _ identity.Principal, term string, limit int) (search.Results, error) {
	f.term, f.limit = term, limit
	f.calls++
	return search.Results{}, nil
}

// US-56 CA-3: a term under 2 characters (or over 100) is refused before reaching the store.
func TestSearchRefusesTermsOutOfBounds(t *testing.T) {
	for _, term := range []string{"", " ", "a", "  é  ", strings.Repeat("x", 101)} {
		store := &fakeStore{}
		_, err := search.NewService(store).Search(context.Background(), identity.Principal{}, term)
		var f *failure.Error
		if !errors.As(err, &f) || f.Kind != failure.KindInvalidInput || len(f.Fields) != 1 || f.Fields[0].Field != "search" {
			t.Fatalf("%q: %v", term, err)
		}
		if store.calls != 0 {
			t.Fatalf("%q reached the store", term)
		}
	}
}

// US-56 RN-3: the store gets the trimmed term and at most 10 per group; multibyte terms count runes.
func TestSearchTrimsAndCapsEachGroup(t *testing.T) {
	for _, term := range []string{"  US-12 ", "çã", strings.Repeat("é", 100)} {
		store := &fakeStore{}
		if _, err := search.NewService(store).Search(context.Background(), identity.Principal{}, term); err != nil {
			t.Fatalf("%q: %v", term, err)
		}
		if store.term != strings.TrimSpace(term) || store.limit != search.PerGroup || search.PerGroup != 10 {
			t.Fatalf("%q: store got %q, %d", term, store.term, store.limit)
		}
	}
}
