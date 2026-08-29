package main

import "testing"

// A ref that names its own owner/repo must survive parsing whichever spelling it
// uses. The shorthand form used to miss refRe and fall through to the bare-number
// fallback, which dropped the owner/repo and refiled the commit under the same
// number in the repo it was scraped from — a ref that can resolve to a real but
// unrelated issue, so the mistake looked like a correct answer.
func TestParseIssueKeepsTheOwnerRepoInBothSpellings(t *testing.T) {
	const scraped = "bigledger/blg-applet-wavelet-doc-item-maintenance-applet"

	cases := []struct {
		name     string
		message  string
		fallback string
		want     string
	}{
		{
			name:     "shorthand ref",
			message:  "Refs BigLedger-Support/tuhu-finance#48:\n- Reverted custom-field relabeling",
			fallback: scraped,
			want:     "BigLedger-Support/tuhu-finance#48",
		},
		{
			name:     "url path ref",
			message:  "Refs bigledger/blg-sd-senwave-senheng/issues/483:\n- Cap fix",
			fallback: scraped,
			want:     "bigledger/blg-sd-senwave-senheng#483",
		},
		{
			name:     "singular ref keyword",
			message:  "Ref BigLedger-Support/tuhu-finance#48",
			fallback: scraped,
			want:     "BigLedger-Support/tuhu-finance#48",
		},
		{
			// A bare number carries no repo of its own, so the scraped repo is
			// the only sensible reading.
			name:     "bare number falls back to the scraped repo",
			message:  "Fix the null cap #99",
			fallback: scraped,
			want:     scraped + "#99",
		},
		{
			name:     "no ref at all",
			message:  "Tidy up the readme",
			fallback: scraped,
			want:     "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseIssue(tc.message, tc.fallback); got != tc.want {
				t.Fatalf("parseIssue(%q, %q) = %q, want %q", tc.message, tc.fallback, got, tc.want)
			}
		})
	}
}
