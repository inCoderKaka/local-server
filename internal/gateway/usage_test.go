package gateway_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// usageEnv makes some metered chats with the env's two access keys, rolls them up, and adds
// rollup rows of another consumer directly, so isolation is tested against real rows.
func usageEnv(t *testing.T) (e *env, key2 string, other string) {
	t.Helper()
	e = newEnv(t)
	hdr2, key2 := e.issue(auth.Narrowing{})
	for i := 0; i < 3; i++ {
		statusIs(t, e.chat(okChat), http.StatusOK)
	}
	statusIs(t, e.do(http.MethodPost, "/v1/embeddings", `{"model":"`+embedModel+`","input":"x"}`), http.StatusOK)
	statusIs(t, e.doAuth(hdr2, http.MethodPost, "/v1/chat/completions", okChat), http.StatusOK)

	r, err := usage.NewRollups(e.db, e.st, e.tel)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.RunOnce(context.Background()); err != nil || n != 5 {
		t.Fatalf("RunOnce = %d, %v; want 5 records", n, err)
	}
	other = uuid.NewString()
	err = e.db.Write(context.Background(), func(tx *storage.Tx) error {
		for _, g := range []string{"minute", "hour", "day"} {
			if _, err := tx.Exec(`INSERT INTO use_rollup (granularity, bucket_start, consumer_id, access_key_id, model, requests, prompt_tokens, completion_tokens, total_tokens)
VALUES (?, 1000, ?, ?, 'secret-model', 99, 99, 99, 198)`, g, other, uuid.NewString()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, key2, other
}

func (e *env) usage(query string) resp {
	return e.do(http.MethodGet, "/v1/usage?"+query, "")
}

func rows(t *testing.T, r resp) []map[string]any {
	t.Helper()
	statusIs(t, r, http.StatusOK)
	var out []map[string]any
	data, _ := r.json()["data"].([]any)
	for _, d := range data {
		out = append(out, d.(map[string]any))
	}
	return out
}

func TestUsageShowsOnlyTheCallersConsumer(t *testing.T) {
	e, _, other := usageEnv(t)
	for _, g := range []string{"minute", "hour", "day"} {
		r := e.usage("granularity=" + g)
		got := rows(t, r)
		if len(got) == 0 {
			t.Fatalf("%s: no rows for the caller", g)
		}
		var requests float64
		for _, row := range got {
			if row["consumer_id"] != e.cons.ID || row["granularity"] != g {
				t.Fatalf("%s: row %v is not the caller's", g, row)
			}
			if row["model"] == "secret-model" || row["consumer_id"] == other {
				t.Fatalf("%s: another consumer's row leaked: %v", g, row)
			}
			requests += row["requests"].(float64)
		}
		if requests != 5 {
			t.Fatalf("%s: %v requests, want 5 (3 chats and 1 embedding on key 1, 1 chat on key 2)", g, requests)
		}
		if r.json()["next_cursor"] != nil {
			t.Fatalf("%s: unexpected cursor", g)
		}
	}
	// Naming another consumer's key narrows to nothing, not to its rows.
	if got := rows(t, e.usage("granularity=day&access_key_id="+uuid.NewString())); len(got) != 0 {
		t.Fatalf("an unknown key returned %v", got)
	}
}

func TestUsageFiltersByKeyAndModel(t *testing.T) {
	e, key2, _ := usageEnv(t)
	byKey1 := rows(t, e.usage("granularity=day&access_key_id="+e.keyID))
	if len(byKey1) != 2 { // chat model and embedding model
		t.Fatalf("key 1 rows = %v, want 2", byKey1)
	}
	byKey2 := rows(t, e.usage("granularity=day&access_key_id="+key2))
	if len(byKey2) != 1 || byKey2[0]["access_key_id"] != key2 || byKey2[0]["requests"].(float64) != 1 {
		t.Fatalf("key 2 rows = %v, want one row with one request", byKey2)
	}
	byModel := rows(t, e.usage("granularity=day&model="+chatModel))
	if len(byModel) != 2 { // one per key
		t.Fatalf("chat model rows = %v, want 2", byModel)
	}
	for _, row := range byModel {
		if row["model"] != chatModel {
			t.Fatalf("model filter returned %v", row)
		}
	}
	if got := rows(t, e.usage("granularity=day&model=secret-model")); len(got) != 0 {
		t.Fatalf("the other consumer's model returned %v", got)
	}
	both := rows(t, e.usage("granularity=day&model="+chatModel+"&access_key_id="+e.keyID))
	if len(both) != 1 || both[0]["requests"].(float64) != 3 {
		t.Fatalf("key 1 and chat model = %v, want one row with 3 requests", both)
	}
	// from is inclusive and to exclusive.
	if got := rows(t, e.usage("granularity=day&to=1970-01-02T00:00:00Z")); len(got) != 0 {
		t.Fatalf("a range before the records returned %v", got)
	}
	if got := rows(t, e.usage("granularity=day&from=2100-01-01T00:00:00Z")); len(got) != 0 {
		t.Fatalf("a range after the records returned %v", got)
	}
	if got := rows(t, e.usage("granularity=day&from=1970-01-02T00:00:00Z&to=2100-01-01T00:00:00Z")); len(got) != 3 {
		t.Fatalf("a wide range returned %d rows, want 3", len(got))
	}
}

func TestUsagePaginates(t *testing.T) {
	e, _, _ := usageEnv(t)
	whole := rows(t, e.usage("granularity=day"))
	if len(whole) != 3 {
		t.Fatalf("%d day rows, want 3", len(whole))
	}
	var got []map[string]any
	cursor := ""
	for pages := 0; pages < 5; pages++ {
		q := "granularity=day&limit=2"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		r := e.usage(q)
		page := rows(t, r)
		if len(page) > 2 {
			t.Fatalf("page of %d rows with limit 2", len(page))
		}
		got = append(got, page...)
		next, _ := r.json()["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	if len(got) != 3 {
		t.Fatalf("paged rows = %d, want 3", len(got))
	}
	for i := range got {
		if got[i]["access_key_id"] != whole[i]["access_key_id"] || got[i]["model"] != whole[i]["model"] {
			t.Fatalf("row %d differs between paged and whole reads: %v vs %v", i, got[i], whole[i])
		}
	}
	// The host's page-size setting is the default and the cap.
	e.set(usage.KeyPageSize, "1")
	for _, q := range []string{"granularity=day", "granularity=day&limit=50"} {
		r := e.usage(q)
		if n := len(rows(t, r)); n != 1 || r.json()["next_cursor"] == nil {
			t.Fatalf("%s with page_size 1: %d rows, cursor %v", q, n, r.json()["next_cursor"])
		}
	}
}

func TestUsageRefusesBadInput(t *testing.T) {
	e, _, _ := usageEnv(t)
	for name, q := range map[string]string{
		"unknown granularity": "granularity=week",
		"no granularity":      "",
		"bad cursor":          "granularity=day&cursor=not-a-cursor",
		"cursor not json":     "granularity=day&cursor=" + url.QueryEscape("bm90LWpzb24"),
		"limit zero":          "granularity=day&limit=0",
		"bad time":            "granularity=day&from=yesterday",
		"bad key id":          "granularity=day&access_key_id=nope",
	} {
		r := e.usage(q)
		statusIs(t, r, http.StatusBadRequest)
		if r.errBody()["type"] != "invalid_request_error" {
			t.Errorf("%s: not an error envelope: %s", name, r.Body.String())
		}
	}
	r := e.usage("granularity=day&cursor=not-a-cursor")
	if r.errBody()["param"] != "cursor" || strings.Contains(r.Body.String(), "not-a-cursor") {
		t.Errorf("bad cursor answer = %s, want param cursor and no echo", r.Body.String())
	}
	if r.errBody()["param"] != "cursor" {
		t.Errorf("param = %v", r.errBody()["param"])
	}
	// Reversed range: an empty page, not an error.
	if got := rows(t, e.usage("granularity=day&from=2100-01-01T00:00:00Z&to=2000-01-01T00:00:00Z")); len(got) != 0 {
		t.Fatalf("a reversed range returned %v", got)
	}
}

func TestUsageNeedsAKey(t *testing.T) {
	e, _, _ := usageEnv(t)
	statusIs(t, e.doAuth("", http.MethodGet, "/v1/usage?granularity=day", ""), http.StatusUnauthorized)
	statusIs(t, e.doAuth("Bearer ls_nope", http.MethodGet, "/v1/usage?granularity=day", ""), http.StatusUnauthorized)
}
