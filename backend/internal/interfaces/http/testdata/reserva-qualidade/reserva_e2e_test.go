package e2e

import (
	"testing"

	"example.com/reserva"
)

// CT-02 (US-01 CA-2): a booking that starts before another and ends during it is a conflict.
func TestConflictStartingBefore(t *testing.T) {
	var b reserva.Book
	_ = b.Reserve(reserva.Booking{Room: "A", Start: 600, End: 660})
	if err := b.Reserve(reserva.Booking{Room: "A", Start: 570, End: 630}); err != reserva.ErrConflict {
		t.Fatalf("want conflict, got %v", err)
	}
}
