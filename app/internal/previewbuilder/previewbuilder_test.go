package previewbuilder

import (
	"net"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
	tests := []struct {
		address string
		want    bool
	}{
		{address: "8.8.8.8", want: true},
		{address: "127.0.0.1", want: false},
		{address: "10.0.0.1", want: false},
		{address: "169.254.169.254", want: false},
		{address: "100.64.0.1", want: false},
		{address: "::1", want: false},
		{address: "fc00::1", want: false},
	}

	for _, test := range tests {
		if got := isPublicIP(net.ParseIP(test.address)); got != test.want {
			t.Errorf("isPublicIP(%q) = %t, want %t", test.address, got, test.want)
		}
	}
}
