package runtime

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"testing"
)

// A permanent error fails the wave instead of letting it be claimed again as crashed (each claim
// repeats the whole agent run).
func TestPermanentErrorsAreTheOnesRetryingCannotFix(t *testing.T) {
	denied := &os.PathError{Op: "mkdir", Path: "/var/lib/easycode/repos/x.git", Err: syscall.EACCES}
	for _, err := range []error{denied, fmt.Errorf("commit: %w", denied), fs.ErrPermission, syscall.EROFS, fmt.Errorf("write: %w", syscall.ENOSPC)} {
		if !isPermanent(err) {
			t.Errorf("%v must be permanent", err)
		}
	}
	for _, err := range []error{errors.New("connection reset"), syscall.ECONNREFUSED, ErrLeaseLost} {
		if isPermanent(err) {
			t.Errorf("%v must not be permanent", err)
		}
	}
}
