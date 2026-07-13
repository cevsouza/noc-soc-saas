package domain

import (
	"sort"
	"testing"
)

func TestForSource(t *testing.T) {
	soc := []string{"wazuh", "sentinel", "crowdstrike", "paloalto", "fortinet"}
	for _, s := range soc {
		if ForSource(s) != SOC {
			t.Errorf("ForSource(%q) = %q, want soc", s, ForSource(s))
		}
	}
	noc := []string{"prometheus", "zabbix", "uptimekuma", "grafana", "snmp", "agent", "system", "cloudwatch", "unknown-thing"}
	for _, s := range noc {
		if ForSource(s) != NOC {
			t.Errorf("ForSource(%q) = %q, want noc", s, ForSource(s))
		}
	}
}

func TestSourcesForDomain(t *testing.T) {
	if _, ok := SourcesForDomain("bogus"); ok {
		t.Fatal("bogus domain must be invalid")
	}
	socSrc, ok := SourcesForDomain(SOC)
	if !ok {
		t.Fatal("soc must be valid")
	}
	sort.Strings(socSrc)
	// Every source returned for SOC must classify as SOC (round-trip consistency).
	for _, s := range socSrc {
		if ForSource(s) != SOC {
			t.Errorf("SourcesForDomain(soc) returned %q which is not soc", s)
		}
	}
	nocSrc, ok := SourcesForDomain(NOC)
	if !ok || len(nocSrc) == 0 {
		t.Fatal("noc must be valid and non-empty")
	}
	for _, s := range nocSrc {
		if ForSource(s) != NOC {
			t.Errorf("SourcesForDomain(noc) returned %q which is not noc", s)
		}
	}
}
