// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

package campaign

import (
	"testing"
	"time"

	cmodel "github.com/yousysadmin/mailyard/internal/models/campaign"
	submodel "github.com/yousysadmin/mailyard/internal/models/subscriber"
)

// 09:00+01:00 delivers at 09:00 in every subscriber's timezone: the
// wall clock is the one written, whatever offset it carried.
func TestLocalDeliveryUsesTheWallClockAsWritten(t *testing.T) {
	written, err := time.Parse(time.RFC3339, "2026-11-02T09:00:00+01:00")
	if err != nil {
		t.Fatal(err)
	}

	utc := written.UTC()
	offset := 3600
	c := &cmodel.Campaign{SendAtLocalTime: true, ScheduledAt: &utc, ScheduledOffset: &offset}

	for _, tz := range []string{"Europe/Lisbon", "Africa/Lagos", "America/New_York", "Asia/Tokyo"} {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Skipf("no zoneinfo for %s", tz)
		}

		got := localDeliverAt(c, &submodel.Subscriber{Timezone: tz})
		if got == nil {
			t.Fatalf("%s: no delivery time", tz)
		}

		if h, m, _ := got.In(loc).Clock(); h != 9 || m != 0 {
			t.Errorf("%s: delivers at %02d:%02d local, want 09:00", tz, h, m)
		}
	}

	if got := localDeliverAt(c, &submodel.Subscriber{}); got == nil || !got.Equal(utc) {
		t.Errorf("no timezone: %v, want the instant as written", got)
	}
}

// A campaign scheduled before the offset was stored keeps reading its
// wall clock in UTC: 09:00+01:00 is 08:00Z, so 08:00 local everywhere.
func TestACampaignWithNoStoredOffsetReadsUTC(t *testing.T) {
	written, err := time.Parse(time.RFC3339, "2026-11-02T09:00:00+01:00")
	if err != nil {
		t.Fatal(err)
	}

	utc := written.UTC()
	c := &cmodel.Campaign{SendAtLocalTime: true, ScheduledAt: &utc}

	for _, tz := range []string{"America/New_York", "Asia/Tokyo"} {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Skipf("no zoneinfo for %s", tz)
		}

		got := localDeliverAt(c, &submodel.Subscriber{Timezone: tz})
		if got == nil {
			t.Fatalf("%s: no delivery time", tz)
		}

		if h, m, _ := got.In(loc).Clock(); h != 8 || m != 0 {
			t.Errorf("%s: delivers at %02d:%02d local, want 08:00", tz, h, m)
		}
	}
}
