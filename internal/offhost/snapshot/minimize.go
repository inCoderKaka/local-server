package snapshot

import (
	"fmt"
	"sort"
	"strings"
)

// protectedTables are the tables a backup always keeps (req_001 D-139): minimizing
// ops_schema_migrations makes a restore fail at migration time, and minimizing hst_host or hst_key
// leaves a snapshot that cannot recover the host's keys.
var protectedTables = []string{"hst_host", "hst_key", "ops_schema_migrations"}

// ProtectedTables returns the tables a backup always keeps, sorted. A backup.minimize.<table>
// setting exists for each, but a write to it is refused and its stored value is never honored
// (req_001 D-139, refining D-64 and D-68).
func ProtectedTables() []string { return append([]string(nil), protectedTables...) }

// IsProtected reports whether table is always kept in a backup (req_001 D-139).
func IsProtected(table string) bool {
	for _, t := range protectedTables {
		if t == table {
			return true
		}
	}
	return false
}

// protectedError is the refusal of a write to the minimize setting of a protected table.
func protectedError(table string) error {
	return fmt.Errorf("table %s is protected and always included in a backup: leaving it out would make a restore fail or leave a snapshot that cannot recover this host's keys (req_001 D-139)", table)
}

// Violation is one table set to minimize while tables that reference it, directly or through
// other tables, are kept (req_001 D-69).
type Violation struct {
	// Table is the minimized table.
	Table string
	// Kept are the tables that reach Table through foreign keys and are not set to minimize.
	Kept []string
}

// MinimizeError is a minimize configuration that breaks req_001 D-69. It names tables only.
type MinimizeError struct {
	Violations []Violation
}

func (e *MinimizeError) Error() string {
	parts := make([]string, len(e.Violations))
	for i, v := range e.Violations {
		parts[i] = fmt.Sprintf("%s cannot be minimized while %s that reference it are kept", v.Table, strings.Join(v.Kept, ", "))
	}
	return "backup minimize rule (a table can be minimized only when every table that references it is also minimized): " + strings.Join(parts, "; ")
}

// Tables returns every table the error names, sorted and without repeats.
func (e *MinimizeError) Tables() []string {
	set := map[string]bool{}
	for _, v := range e.Violations {
		set[v.Table] = true
		for _, k := range v.Kept {
			set[k] = true
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// CheckMinimize applies req_001 D-69. referrers maps a table to the tables whose foreign keys
// point at it directly (storage.DB.ForeignKeyReferrers); minimize says, per table, whether it
// is set to minimize (a table missing from it is kept). It returns nil when every table set to
// minimize has all of its direct and indirect referrers minimized too.
func CheckMinimize(referrers map[string][]string, minimize map[string]bool) *MinimizeError {
	var tables []string
	for t, m := range minimize {
		if m {
			tables = append(tables, t)
		}
	}
	sort.Strings(tables)
	var out MinimizeError
	for _, t := range tables {
		var kept []string
		for _, r := range reach(referrers, t) {
			if !minimize[r] {
				kept = append(kept, r)
			}
		}
		if len(kept) > 0 {
			sort.Strings(kept)
			out.Violations = append(out.Violations, Violation{Table: t, Kept: kept})
		}
	}
	if len(out.Violations) == 0 {
		return nil
	}
	return &out
}

// reach returns every table that references t directly or through other tables, t itself not
// included.
func reach(referrers map[string][]string, t string) []string {
	seen := map[string]bool{t: true}
	queue := []string{t}
	var out []string
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, r := range referrers[cur] {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
				queue = append(queue, r)
			}
		}
	}
	return out
}

// involving keeps only the violations that a write of table's setting is responsible for: table
// itself being minimized with referrers kept, or table being kept while a table it references is
// minimized.
func (e *MinimizeError) involving(table string) *MinimizeError {
	if e == nil {
		return nil
	}
	var out MinimizeError
	for _, v := range e.Violations {
		if v.Table == table {
			out.Violations = append(out.Violations, v)
			continue
		}
		for _, k := range v.Kept {
			if k == table {
				out.Violations = append(out.Violations, Violation{Table: v.Table, Kept: []string{table}})
				break
			}
		}
	}
	if len(out.Violations) == 0 {
		return nil
	}
	return &out
}
