package main

import "testing"

func TestNormalizeRequestedNodeRole(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty defaults auto", in: "", want: "auto"},
		{name: "auto", in: "auto", want: "auto"},
		{name: "validator", in: "validator", want: "validator"},
		{name: "full node alias", in: "full-node", want: "full"},
		{name: "light node alias", in: "lightnode", want: "light"},
		{name: "unknown defaults auto", in: "unexpected", want: "auto"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeRequestedNodeRole(tt.in); got != tt.want {
				t.Fatalf("normalizeRequestedNodeRole(%q)=%q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestResolveAutoNodeRole(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		keyLoaded bool
		wantRole  string
		wantWhy   string
	}{
		{name: "auto with key becomes validator", requested: "auto", keyLoaded: true, wantRole: "validator", wantWhy: "validator_key_loaded"},
		{name: "auto without key becomes full", requested: "auto", keyLoaded: false, wantRole: "full", wantWhy: "validator_key_unavailable"},
		{name: "explicit validator preserved", requested: "validator", keyLoaded: false, wantRole: "validator", wantWhy: "explicit_validator"},
		{name: "explicit full preserved", requested: "full", keyLoaded: true, wantRole: "full", wantWhy: "explicit_full"},
		{name: "explicit light preserved", requested: "light", keyLoaded: true, wantRole: "light", wantWhy: "explicit_light"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRole, gotWhy := resolveAutoNodeRole(tt.requested, tt.keyLoaded)
			if gotRole != tt.wantRole || gotWhy != tt.wantWhy {
				t.Fatalf("resolveAutoNodeRole(%q,%t)=(%q,%q), want (%q,%q)",
					tt.requested,
					tt.keyLoaded,
					gotRole,
					gotWhy,
					tt.wantRole,
					tt.wantWhy,
				)
			}
		})
	}
}
