package server

import "testing"

func TestEgressChangeNote(t *testing.T) {
	cases := []struct {
		name      string
		prev      string
		connect   string
		relay     string
		declared  string
		declFlag  bool
		wantEmpty bool
	}{
		{"nat stable", "1.1.1.1", "1.1.1.1", "9.9.9.9", "", false, true},
		{"connect matches relay", "1.1.1.1", "9.9.9.9", "9.9.9.9", "", false, true},
		{"ip changed", "1.1.1.1", "2.2.2.2", "9.9.9.9", "", false, false},
		{"declared flag", "1.1.1.1", "2.2.2.2", "9.9.9.9", "", true, true},
		{"declared host", "1.1.1.1", "2.2.2.2", "9.9.9.9", "9.9.9.9", false, true},
		{"domain relay", "1.1.1.1", "2.2.2.2", "exit.example", "", false, true},
		{"no previous", "", "2.2.2.2", "9.9.9.9", "", false, true},
		{"v6 connect", "1.1.1.1", "2001:db8::1", "9.9.9.9", "", false, true},
	}
	for _, tc := range cases {
		got := egressChangeNote(tc.prev, tc.connect, tc.relay, tc.declared, tc.declFlag)
		if tc.wantEmpty && got != "" {
			t.Errorf("%s: got %q, want empty", tc.name, got)
		}
		if !tc.wantEmpty {
			want := "出口已变，对外地址未改（这次 2.2.2.2，仍是 9.9.9.9）"
			if got != want {
				t.Errorf("%s: got %q, want %q", tc.name, got, want)
			}
		}
	}
}
