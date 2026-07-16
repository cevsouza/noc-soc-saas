package api

import "testing"

func TestValidConsole(t *testing.T) {
	for _, c := range []string{"all", "noc", "soc"} {
		if !validConsole(c) {
			t.Errorf("validConsole(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"", "NOC", "unified", "both", "sec", " noc "} {
		// " noc " is trimmed to "noc" and IS valid; assert only the truly-invalid ones.
		if c == " noc " {
			if !validConsole(c) {
				t.Errorf("validConsole(%q) should trim and be valid", c)
			}
			continue
		}
		if validConsole(c) {
			t.Errorf("validConsole(%q) = true, want false", c)
		}
	}
}
