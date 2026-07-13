package api

import "testing"

func TestValidateDiscoveryTargetInput(t *testing.T) {
	ok := DiscoveryTargetInput{Name: "LAN", CIDR: "192.168.1.0/24", Community: "public"}
	if err := validateDiscoveryTargetInput(ok); err != nil {
		t.Errorf("valid input rejected: %v", err)
	}

	cases := []struct {
		name string
		in   DiscoveryTargetInput
	}{
		{"missing name", DiscoveryTargetInput{CIDR: "192.168.1.0/24", Community: "public"}},
		{"missing cidr", DiscoveryTargetInput{Name: "LAN", Community: "public"}},
		{"missing community", DiscoveryTargetInput{Name: "LAN", CIDR: "192.168.1.0/24"}},
		{"bad port", DiscoveryTargetInput{Name: "LAN", CIDR: "192.168.1.0/24", Community: "public", Port: 99999}},
		{"bad version", DiscoveryTargetInput{Name: "LAN", CIDR: "192.168.1.0/24", Community: "public", Version: "3"}},
		{"invalid cidr", DiscoveryTargetInput{Name: "LAN", CIDR: "nope", Community: "public"}},
		{"ipv6 cidr", DiscoveryTargetInput{Name: "LAN", CIDR: "2001:db8::/64", Community: "public"}},
		{"oversized cidr", DiscoveryTargetInput{Name: "LAN", CIDR: "10.0.0.0/8", Community: "public"}},
	}
	for _, c := range cases {
		if err := validateDiscoveryTargetInput(c.in); err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

func TestCIDRHostCount(t *testing.T) {
	cases := []struct {
		cidr string
		want int
	}{
		{"192.168.1.0/24", 256},
		{"10.0.0.0/30", 4},
		{"172.16.0.0/20", 4096},
	}
	for _, c := range cases {
		got, err := cidrHostCount(c.cidr)
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.cidr, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %d, want %d", c.cidr, got, c.want)
		}
	}
	if _, err := cidrHostCount("10.0.0.0/8"); err == nil {
		t.Error("expected error for oversized cidr")
	}
}
