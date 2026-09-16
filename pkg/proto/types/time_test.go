// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package types

import (
	"testing"
	"time"
)

func TestNewDiameterTimeUsesNineteenHundredEpoch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		when time.Time
		want DiameterTime
	}{
		{
			name: "unix epoch",
			when: time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC),
			want: 2208988800,
		},
		{
			name: "known y2k value",
			when: time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC),
			want: 3155673600,
		},
		{
			name: "one second before unix epoch",
			when: time.Date(1969, time.December, 31, 23, 59, 59, 0, time.UTC),
			want: 2208988799,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := NewDiameterTime(tt.when); got != tt.want {
				t.Fatalf("NewDiameterTime(%s) = %d, want %d", tt.when.Format(time.RFC3339), got, tt.want)
			}
		})
	}
}

func TestDiameterTimeToTimeUsesNineteenHundredEpoch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dt   DiameterTime
		want time.Time
	}{
		{
			name: "zero",
			dt:   0,
			want: time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "unix epoch",
			dt:   2208988800,
			want: time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "last uint32 second before rollover",
			dt:   0xffffffff,
			want: time.Date(2036, time.February, 7, 6, 28, 15, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.dt.ToTime(); !got.Equal(tt.want) {
				t.Fatalf("DiameterTime(%d).ToTime() = %s, want %s", tt.dt, got.Format(time.RFC3339), tt.want.Format(time.RFC3339))
			}
		})
	}
}

func TestDiameterTimeRoundTripsRepresentableTimes(t *testing.T) {
	t.Parallel()

	tests := []time.Time{
		time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, time.September, 16, 12, 7, 2, 0, time.UTC),
		time.Date(2036, time.February, 7, 6, 28, 15, 0, time.UTC),
	}

	for _, want := range tests {
		t.Run(want.Format(time.RFC3339), func(t *testing.T) {
			t.Parallel()

			if got := NewDiameterTime(want).ToTime(); !got.Equal(want) {
				t.Fatalf("round trip = %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
			}
		})
	}
}

func TestDiameterTimeNowIsCloseToCurrentClock(t *testing.T) {
	t.Parallel()

	before := time.Now().Add(-time.Second)
	got := DiameterTimeNow().ToTime()
	after := time.Now().Add(time.Second)

	if got.Before(before) || got.After(after) {
		t.Fatalf("DiameterTimeNow() = %s, outside [%s, %s]", got.Format(time.RFC3339), before.Format(time.RFC3339), after.Format(time.RFC3339))
	}
}
