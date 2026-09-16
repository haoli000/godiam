// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package types

import "testing"

func TestAVPTypeStringNamesKnownValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		avp  AVPType
		want string
	}{
		{name: "octet string", avp: AVPTypeOctetString, want: "OctetString"},
		{name: "address", avp: AVPTypeAddress, want: "Address"},
		{name: "time", avp: AVPTypeTime, want: "Time"},
		{name: "diameter identity", avp: AVPTypeDiameterIdentity, want: "DiameterIdentity"},
		{name: "grouped", avp: AVPTypeGrouped, want: "Grouped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.avp.String(); got != tt.want {
				t.Fatalf("AVPType(%d).String() = %q, want %q", tt.avp, got, tt.want)
			}
		})
	}
}

func TestAVPTypeStringFormatsPositiveUnknownValue(t *testing.T) {
	t.Parallel()

	if got, want := AVPType(99).String(), "Unknown(99)"; got != want {
		t.Fatalf("AVPType(99).String() = %q, want %q", got, want)
	}
}

func TestAVPTypeStringFormatsNegativeUnknownValue(t *testing.T) {
	t.Parallel()

	// String is commonly called from logging and debug dumps; panicking there hides the failure being diagnosed.
	if got, want := AVPType(-1).String(), "Unknown(-1)"; got != want {
		t.Fatalf("AVPType(-1).String() = %q, want %q", got, want)
	}
}
