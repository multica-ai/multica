package main

import "testing"

func TestParseRuntimeConcurrencyArg(t *testing.T) {
	tests := []struct {
		arg     string
		want    any
		wantErr bool
	}{
		{arg: "none", want: nil},
		{arg: "NONE", want: nil},
		{arg: "1", want: 1},
		{arg: " 50 ", want: 50},
		{arg: "0", wantErr: true},
		{arg: "51", wantErr: true},
		{arg: "two", wantErr: true},
		{arg: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseRuntimeConcurrencyArg(tt.arg)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parseRuntimeConcurrencyArg(%q) = %v, want error", tt.arg, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseRuntimeConcurrencyArg(%q) error: %v", tt.arg, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseRuntimeConcurrencyArg(%q) = %v, want %v", tt.arg, got, tt.want)
		}
	}
}
