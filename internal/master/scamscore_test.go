package master

import "testing"

func sp(s string) *string { return &s }

func TestExtractScamScore(t *testing.T) {
	tests := []struct {
		name  string
		score map[string]*string
		want  int
	}{
		{
			name:  "empty → 0",
			score: map[string]*string{},
			want:  0,
		},
		{
			// Regression: previously this stored 0 because only SCAMALYTICS
			// was read; now ipapi abuser_score (0.0-1.0) is used as fallback.
			name:  "ipapi abuser_score fallback (0.0868 (High)) → 9",
			score: map[string]*string{"ipapi": sp("0.0868 (High)")},
			want:  9,
		},
		{
			name:  "scamalytics 0-100 preferred over ipapi",
			score: map[string]*string{"SCAMALYTICS": sp("37"), "ipapi": sp("0.9 (High)")},
			want:  37,
		},
		{
			name:  "IPQS with percent",
			score: map[string]*string{"IPQS": sp("75%")},
			want:  75,
		},
		{
			name:  "ipapi high fraction",
			score: map[string]*string{"ipapi": sp("0.95 (Very High)")},
			want:  95,
		},
		{
			name:  "clamp above 100",
			score: map[string]*string{"SCAMALYTICS": sp("150")},
			want:  100,
		},
		{
			name:  "nil values ignored",
			score: map[string]*string{"SCAMALYTICS": nil, "ipapi": sp("0.5")},
			want:  50,
		},
		{
			name:  "non-numeric ignored",
			score: map[string]*string{"ipapi": sp("N/A")},
			want:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractScamScore(tt.score); got != tt.want {
				t.Fatalf("extractScamScore = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseLeadingFloat(t *testing.T) {
	cases := []struct {
		in string
		f  float64
		ok bool
	}{
		{"0.0868 (High)", 0.0868, true},
		{"37", 37, true},
		{"75%", 75, true},
		{"N/A", 0, false},
		{"", 0, false},
		{"-5 low", -5, true},
	}
	for _, c := range cases {
		f, ok := parseLeadingFloat(c.in)
		if ok != c.ok || (ok && f != c.f) {
			t.Errorf("parseLeadingFloat(%q) = %v,%v want %v,%v", c.in, f, ok, c.f, c.ok)
		}
	}
}
