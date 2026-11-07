package storage

import (
	"context"
	"fmt"
	"strings"
)

// SchemaDoc renders the schema reference (Markdown) from the live schema and
// ops_data_catalog. The output is deterministic: no timestamps, sorted by table and
// column position. docs/schema-reference.md is kept equal to it, for every registered module, by TestSchemaDoc in cmd/local-server.
func (db *DB) SchemaDoc(ctx context.Context) (string, error) {
	var b strings.Builder
	b.WriteString("# Schema reference\n\n")
	b.WriteString("Generated from the migrations by `go generate ./cmd/local-server` (storage module, D-44). Do not edit by hand.\n")
	b.WriteString("Classification: public, internal, personal, sensitive or content. Billable: whether the data may be billed for later.\n")
	trows, err := db.read.QueryContext(ctx, `SELECT table_name, module, classification, billable, comment FROM ops_data_catalog WHERE column_name='' ORDER BY table_name`)
	if err != nil {
		return "", err
	}
	type tbl struct {
		name, module, class, comment string
		billable                     bool
	}
	var tables []tbl
	for trows.Next() {
		var t tbl
		var bi int
		if err := trows.Scan(&t.name, &t.module, &t.class, &bi, &t.comment); err != nil {
			trows.Close()
			return "", err
		}
		t.billable = bi == 1
		tables = append(tables, t)
	}
	if err := trows.Err(); err != nil {
		trows.Close()
		return "", err
	}
	trows.Close()
	for _, t := range tables {
		fmt.Fprintf(&b, "\n## %s\n\nModule: %s. Classification: %s. Billable: %t.\n\n%s\n\n", t.name, t.module, t.class, t.billable, t.comment)
		b.WriteString("| Column | Type | Null | Key | Classification | Billable | Description |\n|---|---|---|---|---|---|---|\n")
		crows, err := db.read.QueryContext(ctx, `SELECT i.name, i.type, i."notnull", i.pk, c.classification, c.billable, c.comment
			FROM pragma_table_info(?) i JOIN ops_data_catalog c ON c.table_name=? AND c.column_name=i.name ORDER BY i.cid`, t.name, t.name)
		if err != nil {
			return "", err
		}
		for crows.Next() {
			var name, typ, class, comment string
			var notnull, pk, bill int
			if err := crows.Scan(&name, &typ, &notnull, &pk, &class, &bill, &comment); err != nil {
				crows.Close()
				return "", err
			}
			null, key := "yes", ""
			if notnull == 1 || pk > 0 {
				null = "no"
			}
			if pk > 0 {
				key = "PK"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %t | %s |\n", name, typ, null, key, class, bill == 1, strings.ReplaceAll(comment, "|", "\\|"))
		}
		if err := crows.Err(); err != nil {
			crows.Close()
			return "", err
		}
		crows.Close()
	}
	return b.String(), nil
}
