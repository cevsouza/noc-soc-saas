package api

import "testing"

func TestValidateAssetInput(t *testing.T) {
	cases := []struct {
		name    string
		asset   Asset
		wantErr bool
	}{
		{"ok minimal", Asset{Identifier: "192.168.1.1", Name: "fw"}, false},
		{"ok full", Asset{Identifier: "svc-billing", Name: "Billing", BusinessCriticality: "critical"}, false},
		{"missing identifier", Asset{Name: "x"}, true},
		{"missing name", Asset{Identifier: "10.0.0.1"}, true},
		{"bad criticality", Asset{Identifier: "10.0.0.1", Name: "x", BusinessCriticality: "urgent"}, true},
		{"blank criticality ok", Asset{Identifier: "10.0.0.1", Name: "x", BusinessCriticality: ""}, false},
	}
	for _, tc := range cases {
		err := validateAssetInput(tc.asset)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: got err=%v, wantErr=%v", tc.name, err, tc.wantErr)
		}
	}
}

func TestNormalizeTags(t *testing.T) {
	got := normalizeTags([]string{" prod ", "prod", "", "DB", "db", "core"})
	// trims, drops empties, de-dupes case-insensitively (first spelling wins)
	want := []string{"prod", "DB", "core"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tag[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestMergeAssets(t *testing.T) {
	devices := []discoveredRow{
		{IP: "192.168.1.1", SysName: "edge-fw", Vendor: "Fortinet", DeviceType: "firewall"},
		{IP: "192.168.1.2", SysName: "core-sw", Vendor: "Cisco", DeviceType: "switch"},
	}
	assets := []Asset{
		// Overlay on a discovered device: sets criticality + owner, keeps discovered name/vendor.
		{Identifier: "192.168.1.1", BusinessCriticality: "critical", Owner: "netops", Tags: []string{"perimeter"}},
		// Manual-only asset (no discovery).
		{Identifier: "svc-billing", Name: "Billing API", AssetType: "service", BusinessCriticality: "high"},
	}

	views := mergeAssets(assets, devices)
	if len(views) != 3 {
		t.Fatalf("got %d views, want 3: %+v", len(views), views)
	}

	byID := map[string]AssetView{}
	for _, v := range views {
		byID[v.Identifier] = v
	}

	fw := byID["192.168.1.1"]
	if !fw.Managed || !fw.Discovered || fw.BusinessCriticality != "critical" || fw.Owner != "netops" {
		t.Errorf("firewall overlay wrong: %+v", fw)
	}
	if fw.Name != "edge-fw" || fw.Vendor != "Fortinet" {
		t.Errorf("firewall should keep discovered name/vendor: %+v", fw)
	}

	sw := byID["192.168.1.2"]
	if sw.Managed || !sw.Discovered || sw.BusinessCriticality != "medium" {
		t.Errorf("switch should be unmanaged+discovered+medium: %+v", sw)
	}

	svc := byID["svc-billing"]
	if !svc.Managed || svc.Discovered || svc.Name != "Billing API" || svc.BusinessCriticality != "high" {
		t.Errorf("manual service asset wrong: %+v", svc)
	}

	// Most-critical first.
	if views[0].Identifier != "192.168.1.1" {
		t.Errorf("expected critical firewall first, got %s", views[0].Identifier)
	}
}
