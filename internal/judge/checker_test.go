package judge

import "testing"

func TestCheckOutput(t *testing.T) {
	tests := []struct{name, mode, want, got string; eps float64; pass bool}{
		{"exact preserves spaces", "exact", "a \n", "a\n", 0, false},
		{"exact normalizes CRLF", "exact", "a\r\n", "a\n", 0, true},
		{"trim line endings", "trim", "a  \r\nb\n\n", "a\nb\n", 0, true},
		{"trim preserves leading spaces", "trim", " a\n", "a\n", 0, false},
		{"float relative error", "float", "1000000", "1000000.5", 1e-6, true},
		{"float absolute error", "float", "0", "0.0000005", 1e-6, true},
		{"float token count", "float", "1 2", "1", 1e-6, false},
		{"float text tokens", "float", "YES 1", "YES 1.00000001", 1e-6, true},
		{"reject NaN", "float", "1", "NaN", 1e-6, false},
		{"reject infinity", "float", "1", "+Inf", 1e-6, false},
		{"unknown checker", "oops", "1", "1", 0, false},
	}
	for _, tt := range tests {t.Run(tt.name,func(t *testing.T){if got:=CheckOutput(tt.mode,tt.want,tt.got,tt.eps);got!=tt.pass {t.Fatalf("CheckOutput = %v; want %v",got,tt.pass)}})}
}
