package worker

import (
	"strings"
	"testing"
)

func TestFormatOnCallSuffix(t *testing.T) {
	cases := []struct {
		name   string
		people []onCallPerson
		want   string
	}{
		{"empty", nil, ""},
		{"one with email", []onCallPerson{{Name: "Alice", Email: "alice@x.com"}}, " · Plantão: Alice <alice@x.com>"},
		{"one no email", []onCallPerson{{Name: "Bob"}}, " · Plantão: Bob"},
		{"two", []onCallPerson{{Name: "Alice", Email: "a@x"}, {Name: "Bob", Email: "b@x"}}, " · Plantão: Alice <a@x>, Bob <b@x>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatOnCallSuffix(c.people); got != c.want {
				t.Fatalf("formatOnCallSuffix(%s) = %q, want %q", c.name, got, c.want)
			}
		})
	}

	// Cap: more than 3 names collapses the tail into a "+N" marker.
	many := []onCallPerson{
		{Name: "A", Email: "a"}, {Name: "B", Email: "b"}, {Name: "C", Email: "c"},
		{Name: "D", Email: "d"}, {Name: "E", Email: "e"},
	}
	got := formatOnCallSuffix(many)
	if !strings.Contains(got, "+2") {
		t.Fatalf("expected a +2 overflow marker, got %q", got)
	}
	if strings.Contains(got, "D <d>") || strings.Contains(got, "E <e>") {
		t.Fatalf("expected names beyond the cap to be omitted, got %q", got)
	}
}
