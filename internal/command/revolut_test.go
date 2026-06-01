package command

import "testing"

// TestExtractCodeFromInput exercises the paste-back path's parser,
// which has to accept three shapes the user might realistically
// paste:
//
//  1. The full redirect URL with ?code=… (most common — they hit
//     Cmd+L, Cmd+C in their browser).
//  2. The bare code value (the user trimmed it themselves).
//  3. Quoted variants of either (some clipboard tools / shells
//     add quotes during copy/paste).
//
// The fragment case (#code= rather than ?code=) covers Revolut's
// response_mode=fragment — uncommon for fsvc but cheap to support.
func TestExtractCodeFromInput(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "full URL with ?code",
			input: "https://example.com/callback?code=oa_prod_abc123&state=xyz",
			want:  "oa_prod_abc123",
		},
		{
			name:  "full URL with fragment #code",
			input: "https://example.com/callback#code=oa_prod_frag42&state=xyz",
			want:  "oa_prod_frag42",
		},
		{
			name:  "URL with code only",
			input: "https://example.com/callback?code=oa_prod_only_code",
			want:  "oa_prod_only_code",
		},
		{
			name:  "bare code",
			input: "oa_prod_bare_code",
			want:  "oa_prod_bare_code",
		},
		{
			name:  "double-quoted bare code",
			input: `"oa_prod_quoted"`,
			want:  "oa_prod_quoted",
		},
		{
			name:  "single-quoted bare code",
			input: `'oa_sand_singled'`,
			want:  "oa_sand_singled",
		},
		{
			name:    "URL without code param",
			input:   "https://example.com/callback?state=xyz",
			wantErr: true,
		},
		{
			name:    "URL with explicit empty code",
			input:   "https://example.com/callback?code=",
			wantErr: true,
		},
		{
			name:  "http URL (sandbox style)",
			input: "http://localhost:8080/callback?code=oa_sand_loopback",
			want:  "oa_sand_loopback",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractCodeFromInput(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("extractCodeFromInput(%q) = %q, want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractCodeFromInput(%q) unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("extractCodeFromInput(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
