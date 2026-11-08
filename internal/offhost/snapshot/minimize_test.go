package snapshot

import (
	"strings"
	"testing"
)

// payload and result reference job; job is referenced by nothing else; sess stands alone;
// grandchild references child which references root.
var testRefs = map[string][]string{
	"job":   {"payload", "result"},
	"root":  {"child"},
	"child": {"grandchild"},
	"self":  {"self"},
}

func TestCheckMinimizeKeepsTheRuleOfD69(t *testing.T) {
	cases := []struct {
		name     string
		minimize map[string]bool
		wantTabs string // "" means no violation
	}{
		{"nothing minimized", map[string]bool{}, ""},
		{"leaf alone", map[string]bool{"payload": true}, ""},
		{"parent with all referrers", map[string]bool{"job": true, "payload": true, "result": true}, ""},
		{"parent with one referrer kept", map[string]bool{"job": true, "payload": true}, "job,result"},
		{"indirect referrer kept", map[string]bool{"root": true, "child": true}, "child,grandchild,root"},
		{"whole chain", map[string]bool{"root": true, "child": true, "grandchild": true}, ""},
		{"self reference alone", map[string]bool{"self": true}, ""},
	}
	for _, c := range cases {
		err := CheckMinimize(testRefs, c.minimize)
		if c.wantTabs == "" {
			if err != nil {
				t.Errorf("%s: unexpected violation: %v", c.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: no violation", c.name)
			continue
		}
		got := strings.Join(err.Tables(), ",")
		if got != c.wantTabs {
			t.Errorf("%s: tables %q, want %q", c.name, got, c.wantTabs)
		}
		for _, tab := range strings.Split(c.wantTabs, ",") {
			if !strings.Contains(err.Error(), tab) {
				t.Errorf("%s: message %q does not name %s", c.name, err.Error(), tab)
			}
		}
	}
}

func TestInvolvingFiltersToTheWrittenTable(t *testing.T) {
	// payload kept while job is minimized: writing payload=false must be refused, naming job.
	merr := CheckMinimize(testRefs, map[string]bool{"job": true, "result": true}).involving("payload")
	if merr == nil || strings.Join(merr.Tables(), ",") != "job,payload" {
		t.Fatalf("involving payload: %v", merr)
	}
	// An unrelated table is not blamed.
	if CheckMinimize(testRefs, map[string]bool{"job": true, "result": true}).involving("root") != nil {
		t.Fatal("an unrelated table was blamed")
	}
}
