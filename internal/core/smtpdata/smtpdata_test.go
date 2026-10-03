// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package smtpdata

import (
	"errors"
	"fmt"
	"testing"

	"github.com/emersion/go-smtp"
)

// Only a read that failed for a reason that may pass on retry is a 4xx.
func TestOnlyARealReadFailureIsTemporary(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"over the size limit", smtp.ErrDataTooLarge, 552},
		{"over the size limit, wrapped", fmt.Errorf("read: %w", smtp.ErrDataTooLarge), 552},
		{"a line over the limit", smtp.ErrTooLongLine, 554},
		{"a broken connection", errors.New("connection reset by peer"), 451},
	}

	for _, c := range cases {
		if got := ReadError(c.err); got.Code != c.want {
			t.Errorf("%s: %d, want %d", c.name, got.Code, c.want)
		}
	}
}
