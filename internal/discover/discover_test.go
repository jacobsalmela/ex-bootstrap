package discover

import (
	"testing"

	"bootstrap/internal/inventory"
)

func TestFindByXname(t *testing.T) {
	entries := []inventory.Entry{
		{Xname: "x1000c0s0b0n0", MAC: "aa:bb:cc:dd:ee:01", IP: "10.0.0.1"},
		{Xname: "x1000c0s1b0n0", MAC: "aa:bb:cc:dd:ee:02", IP: "10.0.0.2"},
		{Xname: "x1000c0s2b0n0", MAC: "aa:bb:cc:dd:ee:03", IP: "10.0.0.3"},
	}

	tests := []struct {
		name      string
		xname     string
		wantFound bool
		wantMAC   string
	}{
		{
			name:      "Found first entry",
			xname:     "x1000c0s0b0n0",
			wantFound: true,
			wantMAC:   "aa:bb:cc:dd:ee:01",
		},
		{
			name:      "Found middle entry",
			xname:     "x1000c0s1b0n0",
			wantFound: true,
			wantMAC:   "aa:bb:cc:dd:ee:02",
		},
		{
			name:      "Not found",
			xname:     "x1000c0s9b0n0",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findByXname(entries, tt.xname)
			if tt.wantFound {
				if result == nil {
					t.Fatalf("expected to find entry for %s, got nil", tt.xname)
				}
				if result.MAC != tt.wantMAC {
					t.Errorf("got MAC %s, want %s", result.MAC, tt.wantMAC)
				}
			} else {
				if result != nil {
					t.Errorf("expected nil, got entry: %+v", result)
				}
			}
		})
	}
}
