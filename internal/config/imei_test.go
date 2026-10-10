package config

import "testing"

func TestNormalizeIMEI(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain 15-digit", "000000000004444", "00000000000444"},
		{"trailing space", "000000000004444 ", "00000000000444"},
		{"leading space + newline", " 000000000004444\n", "00000000000444"},
		{"imeisv 16-digit", "0000000000044401", "00000000000444"},
		{"embedded non-digits", "00-0000 0000.04444", "00000000000444"},
		{"too short", "12345", ""},
		{"empty", "", ""},
		{"exactly 14", "00000000000444", "00000000000444"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeIMEI(tc.in); got != tc.want {
				t.Fatalf("NormalizeIMEI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIMEIMatches(t *testing.T) {
	if !IMEIMatches("000000000004444", " 000000000004444") {
		t.Fatal("whitespace-differing same IMEI should match")
	}
	if !IMEIMatches("000000000004444", "0000000000044401") {
		t.Fatal("IMEI(15) and IMEISV(16) of same modem should match")
	}
	if IMEIMatches("000000000004444", "000000000005555") {
		t.Fatal("different modems must not match")
	}
	if IMEIMatches("", "") {
		t.Fatal("empty must never match")
	}
	if IMEIMatches("12345", "12345") {
		t.Fatal("invalid (<14 digits) must never match")
	}
}
