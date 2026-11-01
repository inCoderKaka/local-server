package gateway

import "testing"

func TestWithTagAndSameModel(t *testing.T) {
	for _, tc := range []struct {
		name, in, tagged string
	}{
		{"untagged", "llama3.2", "llama3.2:latest"},
		{"tagged", "llama3.2:3b", "llama3.2:3b"},
		{"registry port without a tag", "host:5000/ns/model", "host:5000/ns/model:latest"},
		{"registry port with a tag", "host:5000/ns/model:v2", "host:5000/ns/model:v2"},
		{"hf.co with a quantisation tag", "hf.co/u/m:Q4", "hf.co/u/m:Q4"},
		{"hf.co without a tag", "hf.co/u/m", "hf.co/u/m:latest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := withTag(tc.in); got != tc.tagged {
				t.Fatalf("withTag(%q) = %q, want %q", tc.in, got, tc.tagged)
			}
		})
	}
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"llama3.2", "llama3.2:latest", true},
		{"llama3.2:latest", "llama3.2", true},
		{"llama3.2", "llama3.2:3b", false},
		{"host:5000/ns/model", "host:5000/ns/model:latest", true},
		{"host:5000/ns/model", "host", false},
		{"hf.co/u/m:Q4", "hf.co/u/m", false},
		{"hf.co/u/m:Q4", "hf.co/u/m:Q4", true},
	} {
		if got := sameModel(tc.a, tc.b); got != tc.want {
			t.Errorf("sameModel(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
