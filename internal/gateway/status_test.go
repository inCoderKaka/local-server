package gateway_test

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/limits"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/storage"
)

var statusPaths = []string{"/v1/models", "/v1/health", "/v1/capabilities"}

func modelIDs(t *testing.T, r resp) []string {
	t.Helper()
	statusIs(t, r, http.StatusOK)
	var out []string
	data, _ := r.json()["data"].([]any)
	for _, d := range data {
		out = append(out, d.(map[string]any)["id"].(string))
	}
	return out
}

func catalogueID(t *testing.T, e *env, name string) string {
	t.Helper()
	ms, err := e.gr.Models.List(context.Background(), e.db.Reader(), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.Name == name {
			return m.ID
		}
	}
	t.Fatalf("no model %s", name)
	return ""
}

// Every status endpoint needs a credential exactly like the inference routes (req_001 D-24).
func TestStatusEndpointsRequireACredential(t *testing.T) {
	e := newEnv(t)
	for _, p := range statusPaths {
		for _, hdr := range []string{"", "Bearer ls_nope_nopenopenopenope"} {
			r := e.doAuth(hdr, http.MethodGet, p, "")
			statusIs(t, r, http.StatusUnauthorized)
			if eb := r.errBody(); eb["code"] != "invalid_api_key" {
				t.Fatalf("%s: error = %v", p, eb)
			}
		}
		statusIs(t, e.do(http.MethodGet, p, ""), http.StatusOK)
	}
}

func TestModelsAreFilteredByGrantAndKey(t *testing.T) {
	// The grant allows the chat model only; the catalogue also has the embedding model.
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models[:1]}
		}
	})
	r := e.do(http.MethodGet, "/v1/models", "")
	if got := modelIDs(t, r); !reflect.DeepEqual(got, []string{chatModel}) {
		t.Fatalf("models = %v, want only %s", got, chatModel)
	}
	m := r.json()["data"].([]any)[0].(map[string]any)
	if r.json()["object"] != "list" || m["object"] != "model" || m["owned_by"] == "" || m["created"].(float64) <= 0 {
		t.Fatalf("not an OpenAI model list: %s", r.Body.String())
	}
}

func TestModelsKeyNarrowing(t *testing.T) {
	e := newEnv(t)
	if got := modelIDs(t, e.do(http.MethodGet, "/v1/models", "")); !reflect.DeepEqual(got, []string{chatModel, embedModel}) {
		t.Fatalf("wide key models = %v", got)
	}
	narrow, _ := e.issue(auth.Narrowing{ModelIDs: []string{catalogueID(t, e, embedModel)}})
	if got := modelIDs(t, e.doAuth(narrow, http.MethodGet, "/v1/models", "")); !reflect.DeepEqual(got, []string{embedModel}) {
		t.Fatalf("narrowed key models = %v", got)
	}
}

func TestHealthReportsSharingEngineAndLoadedModels(t *testing.T) {
	e := newEnv(t)
	r := e.do(http.MethodGet, "/v1/health", "")
	statusIs(t, r, http.StatusOK)
	h := r.json()
	if h["sharing_state"] != "sharing" || h["engine_state"] != "available" || h["sharing_resumes_at"] != nil {
		t.Fatalf("health = %s", r.Body.String())
	}
	if l, _ := h["loaded_models"].([]any); l == nil || len(l) != 0 {
		t.Fatalf("loaded_models = %v, want an empty list", h["loaded_models"])
	}
	if _, err := time.Parse(time.RFC3339Nano, h["checked_at"].(string)); err != nil {
		t.Fatalf("checked_at: %v", err)
	}
	if err := e.eng.Load(context.Background(), chatModel, time.Minute); err != nil {
		t.Fatal(err)
	}
	h = e.do(http.MethodGet, "/v1/health", "").json()
	if got := h["loaded_models"].([]any); len(got) != 1 || got[0] != chatModel {
		t.Fatalf("loaded_models = %v", got)
	}
	// A loaded model the key may not use is not shown.
	narrow, _ := e.issue(auth.Narrowing{ModelIDs: []string{catalogueID(t, e, embedModel)}})
	h = e.doAuth(narrow, http.MethodGet, "/v1/health", "").json()
	if got := h["loaded_models"].([]any); len(got) != 0 {
		t.Fatalf("narrowed loaded_models = %v", got)
	}
}

func TestHealthEngineUnavailable(t *testing.T) {
	e := newEnv(t)
	e.eng.InjectError(fake.OpLoaded, engineDown(), 1)
	h := e.do(http.MethodGet, "/v1/health", "").json()
	if h["engine_state"] != "unavailable" || h["sharing_state"] != "sharing" {
		t.Fatalf("health = %v", h)
	}
}

func TestHealthPausedAndQuiet(t *testing.T) {
	e := newEnv(t)
	if _, err := e.share.Pause(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	h := e.do(http.MethodGet, "/v1/health", "").json()
	if h["sharing_state"] != "paused" || h["sharing_resumes_at"] != nil {
		t.Fatalf("paused health = %v", h)
	}
	if _, err := e.share.Resume(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	e.set(host.KeyQuietEnabled, "true")
	e.set(host.KeyQuietTimeZone, "UTC")
	e.set(host.KeyQuietStart, now.Add(-time.Hour).Format("15:04"))
	e.set(host.KeyQuietEnd, now.Add(time.Hour).Format("15:04"))
	h = e.do(http.MethodGet, "/v1/health", "").json()
	if h["sharing_state"] != "quiet" {
		t.Fatalf("quiet health = %v", h)
	}
	end, err := time.Parse(time.RFC3339Nano, h["sharing_resumes_at"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if d := end.Sub(now.Add(time.Hour)); d < -2*time.Minute || d > 2*time.Minute {
		t.Fatalf("sharing_resumes_at = %v, want about %v", end, now.Add(time.Hour))
	}
}

func TestHealthQueueDepthByClass(t *testing.T) {
	e := newEnv(t)
	e.set(scheduler.KeyTotalConcurrency, "1")
	e.set(scheduler.KeyOllamaNumParallel, "1")
	e.set(scheduler.KeyQueueLength, "8")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	held, err := e.sched.Admit(ctx, scheduler.Request{GrantID: "other", Class: scheduler.ClassInteractive, Model: chatModel, EstimatedCtxTokens: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	for _, c := range []scheduler.Class{scheduler.ClassInteractive, scheduler.ClassInteractive, scheduler.ClassBackground} {
		go func(c scheduler.Class) {
			if tk, err := e.sched.Admit(ctx, scheduler.Request{GrantID: "g" + strconv.Itoa(int(time.Now().UnixNano())), Class: c, Model: chatModel, EstimatedCtxTokens: 8}); err == nil {
				tk.Release()
			}
		}(c)
	}
	deadline := time.Now().Add(5 * time.Second)
	for e.sched.Stats().Queued != 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	q := e.do(http.MethodGet, "/v1/health", "").json()["queue_depth"].(map[string]any)
	if q["interactive"] != float64(2) || q["background"] != float64(1) {
		t.Fatalf("queue_depth = %v, want 2 interactive and 1 background", q)
	}
}

func TestHealthBackgroundDepthCountsJobsStillQueued(t *testing.T) {
	e := newEnv(t)
	depth := func() map[string]any {
		return e.do(http.MethodGet, "/v1/health", "").json()["queue_depth"].(map[string]any)
	}
	if q := depth(); q["background"] != float64(0) {
		t.Fatalf("queue_depth = %v, want no background", q)
	}
	statusIs(t, e.postJob("idem-1", jobBody), http.StatusAccepted) // no worker runs: it stays in sch_job
	q := depth()
	if q["background"] != float64(1) || q["interactive"] != float64(0) {
		t.Fatalf("queue_depth = %v, want 1 background and 0 interactive", q)
	}
}

func TestCapabilitiesAfterGrantAndKeyNarrowing(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			models[0].ContextCap, models[0].MaxOutputTokens = i64(4000), i64(500)
			props := `["priority","tag"]`
			return grantsNewVersion{Models: models, MaxConcurrency: i64(4), Quotas: dayQuotas(grants.MetricTokens, 1000), AllowedProperties: &props, OnContextOverflow: "truncate"}
		}
	})
	e.set(gateway.KeyDefaultContextTokens, "2048")
	e.set(gateway.KeyMaxTokensCeiling, "300")
	narrow, keyID := e.issue(auth.Narrowing{ContextCap: i64(1000), MaxConcurrency: i64(2), QuotaTokensPerDay: i64(100), QuotaRequestsPerDay: i64(10)})
	r := e.doAuth(narrow, http.MethodGet, "/v1/capabilities", "")
	statusIs(t, r, http.StatusOK)
	c := r.json()
	if c["consumer_id"] != e.cons.ID || c["access_key_id"] != keyID || c["grant_version_id"] == "" {
		t.Fatalf("ids = %s", r.Body.String())
	}
	models := c["models"].([]any)
	got := map[string][2]float64{}
	for _, m := range models {
		m := m.(map[string]any)
		got[m["model"].(string)] = [2]float64{m["context_limit"].(float64), m["max_output_tokens"].(float64)}
	}
	// The key lowers the context cap to 1000; the output cap stays the grant's 500. The second
	// model has no cap of its own, so it shows the key's context cap and the host's ceiling.
	if got[chatModel] != [2]float64{1000, 500} || got[embedModel] != [2]float64{1000, 300} {
		t.Fatalf("model limits = %v", got)
	}
	if c["concurrency"] != float64(2) || c["on_context_overflow"] != "truncate" {
		t.Fatalf("concurrency/overflow = %v %v", c["concurrency"], c["on_context_overflow"])
	}
	if !reflect.DeepEqual(c["allowed_request_properties"], []any{"priority", "tag"}) {
		t.Fatalf("allowed_request_properties = %v", c["allowed_request_properties"])
	}
	if rl := c["rate_limit"]; rl != nil { // the test env turns rate limiting off
		t.Fatalf("rate limit is off but reported: %v", rl)
	}
	if c["expires_at"] != nil {
		t.Fatalf("expires_at = %v", c["expires_at"])
	}
}

func TestCapabilitiesUnrestrictedAndDefaults(t *testing.T) {
	e := newEnv(t)
	e.set(limits.KeyRateEnabled, "true")
	e.set(limits.KeyRateCapacity, "30")
	e.set(limits.KeyRateWindow, "2m")
	e.set(gateway.KeyDefaultContextTokens, "2048")
	e.set(gateway.KeyMaxTokensCeiling, "300")
	c := e.do(http.MethodGet, "/v1/capabilities", "").json()
	if v, ok := c["allowed_request_properties"]; !ok || v != nil {
		t.Fatalf("an unrestricted grant must report null properties, got %v", v)
	}
	if c["concurrency"] != float64(1) || c["on_context_overflow"] != "refuse" {
		t.Fatalf("defaults = %v %v", c["concurrency"], c["on_context_overflow"])
	}
	m := c["models"].([]any)[0].(map[string]any)
	if m["context_limit"] != float64(2048) || m["max_output_tokens"] != float64(300) {
		t.Fatalf("model defaults = %v", m)
	}
	if rl, _ := c["rate_limit"].(map[string]any); rl["requests"] != float64(30) || rl["window_seconds"] != float64(120) {
		t.Fatalf("rate_limit = %v", c["rate_limit"])
	}
	if q := c["quotas"].([]any); len(q) != 0 {
		t.Fatalf("quotas = %v, want none", q)
	}
	// Reading capabilities does not consume the caller's quota.
	if n := e.useRecords(); n != 0 {
		t.Fatalf("capabilities wrote %d usage records", n)
	}
}

func TestCapabilitiesRateWindowRoundsUpToWholeSeconds(t *testing.T) {
	for _, c := range []struct {
		window string
		want   float64
	}{{"1500ms", 2}, {"500ms", 1}} {
		e := newEnv(t)
		e.set(limits.KeyRateEnabled, "true")
		e.set(limits.KeyRateCapacity, "30")
		e.set(limits.KeyRateWindow, c.window)
		cp := e.do(http.MethodGet, "/v1/capabilities", "").json()
		if rl, _ := cp["rate_limit"].(map[string]any); rl["window_seconds"] != c.want {
			t.Fatalf("window %s: rate_limit = %v, want window_seconds %v", c.window, cp["rate_limit"], c.want)
		}
	}
}

// The text "null" is not an array: policy refuses it as an invalid grant (I-113), so capabilities
// must not report it as "unrestricted".
func TestCapabilitiesRefusesNullAllowedPropertiesText(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			props := `null`
			return grantsNewVersion{Models: models, AllowedProperties: &props}
		}
	})
	r := e.do(http.MethodGet, "/v1/capabilities", "")
	if r.Code < 500 {
		t.Fatalf("status = %d, want 5xx: %s", r.Code, r.Body.String())
	}
}

func TestCapabilitiesRemainingQuota(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, Quotas: append(dayQuotas(grants.MetricTokens, 1000), dayQuotas(grants.MetricRequests, 50)...)}
		}
	})
	narrow, _ := e.issue(auth.Narrowing{QuotaTokensPerDay: i64(100)})
	// Nothing used yet.
	quotas := func() map[string]map[string]any {
		r := e.doAuth(narrow, http.MethodGet, "/v1/capabilities", "")
		statusIs(t, r, http.StatusOK)
		out := map[string]map[string]any{}
		for _, q := range r.json()["quotas"].([]any) {
			q := q.(map[string]any)
			out[q["scope"].(string)] = q
		}
		return out
	}
	q := quotas()
	if q["grant"]["tokens"] != float64(1000) || q["grant"]["remaining_tokens"] != float64(1000) || q["grant"]["requests"] != float64(50) ||
		q["access_key"]["tokens"] != float64(100) || q["access_key"]["remaining_tokens"] != float64(100) {
		t.Fatalf("fresh quotas = %v", q)
	}
	if _, ok := q["access_key"]["requests"]; ok {
		t.Fatalf("the key sets no request quota, got %v", q["access_key"])
	}
	// One call on the narrowed key uses its tokens (the fake engine counts them).
	statusIs(t, e.doAuth(narrow, http.MethodPost, "/v1/chat/completions", okChat), http.StatusOK)
	q = quotas()
	used := q["access_key"]["used_tokens"].(float64)
	if used <= 0 || q["access_key"]["remaining_tokens"] != 100-used || q["grant"]["remaining_tokens"] != 1000-used {
		t.Fatalf("quotas after one call = %v", q)
	}
	if q["grant"]["used_requests"] != float64(1) || q["grant"]["remaining_requests"] != float64(49) {
		t.Fatalf("grant requests = %v", q["grant"])
	}
	if _, err := time.Parse(time.RFC3339Nano, q["grant"]["resets_at"].(string)); err != nil || q["grant"]["window_seconds"] != float64(86400) {
		t.Fatalf("window = %v", q["grant"])
	}
}

// Every window is its own quota in the status, with that window's tokens and requests together
// (req_001 D-107).
func TestCapabilitiesReportsEveryWindow(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, Quotas: []grants.QuotaEntry{
				{Metric: grants.MetricTokens, Limit: 1000, Window: 24 * time.Hour},
				{Metric: grants.MetricTokens, Limit: 200, Window: time.Hour},
				{Metric: grants.MetricRequests, Limit: 10, Window: time.Hour}}}
		}
	})
	k, _ := e.issue(auth.Narrowing{Quotas: []grants.QuotaEntry{{Metric: grants.MetricRequests, Limit: 5, Window: 2 * time.Hour}}})
	statusIs(t, e.doAuth(k, http.MethodPost, "/v1/chat/completions", okChat), http.StatusOK)
	r := e.doAuth(k, http.MethodGet, "/v1/capabilities", "")
	statusIs(t, r, http.StatusOK)
	byWindow := map[string]map[string]any{}
	for _, q := range r.json()["quotas"].([]any) {
		q := q.(map[string]any)
		byWindow[fmt.Sprintf("%s/%v", q["scope"], q["window_seconds"])] = q
	}
	if len(byWindow) != 3 {
		t.Fatalf("quotas = %v, want grant/86400, grant/3600 and access_key/7200", r.json()["quotas"])
	}
	day, hour, key := byWindow["grant/86400"], byWindow["grant/3600"], byWindow["access_key/7200"]
	if day["tokens"] != float64(1000) || day["requests"] != nil {
		t.Errorf("day window = %v", day)
	}
	if hour["tokens"] != float64(200) || hour["requests"] != float64(10) || hour["used_requests"] != float64(1) || hour["remaining_requests"] != float64(9) {
		t.Errorf("hour window = %v", hour)
	}
	if key["requests"] != float64(5) || key["used_requests"] != float64(1) || key["tokens"] != nil {
		t.Errorf("key window = %v", key)
	}
}

// A used-up quota stops inference but not the status calls, so a client can see why.
func TestCapabilitiesAnswersWhenQuotaIsUsedUp(t *testing.T) {
	e := newEnv(t)
	k, _ := e.issue(auth.Narrowing{QuotaRequestsPerDay: i64(0)})
	statusIs(t, e.doAuth(k, http.MethodPost, "/v1/chat/completions", okChat), http.StatusTooManyRequests)
	r := e.doAuth(k, http.MethodGet, "/v1/capabilities", "")
	statusIs(t, r, http.StatusOK)
	q := r.json()["quotas"].([]any)[0].(map[string]any)
	if q["remaining_requests"] != float64(0) {
		t.Fatalf("quota = %v", q)
	}
}

func TestCapabilitiesExpiryIsTheEarlierOfKeyAndGrant(t *testing.T) {
	grantEnd := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			return grantsNewVersion{Models: models, ExpiresAt: &grantEnd}
		}
	})
	got := func(key string) time.Time {
		c := e.doAuth(key, http.MethodGet, "/v1/capabilities", "").json()
		ts, err := time.Parse(time.RFC3339Nano, c["expires_at"].(string))
		if err != nil {
			t.Fatal(err)
		}
		return ts
	}
	if ts := got(e.key); !ts.Equal(grantEnd) {
		t.Fatalf("expires_at = %v, want the grant's %v", ts, grantEnd)
	}
	keyEnd := time.Now().Add(time.Hour).Truncate(time.Second)
	var hdr string
	err := e.bus.Write(context.Background(), e.db, func(tx *storage.Tx) error {
		is, err := e.au.Issue(tx, auth.IssueParams{ConsumerID: e.cons.ID, ExpiresAt: &keyEnd})
		hdr = "Bearer " + is.Key.Reveal()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if ts := got(hdr); !ts.Equal(keyEnd) {
		t.Fatalf("expires_at = %v, want the key's %v", ts, keyEnd)
	}
}

// The benchmark fields are reserved (req_001 D-45): present and null, never computed.
func TestCapabilitiesBenchmarkFieldsAreReserved(t *testing.T) {
	e := newEnv(t)
	c := e.do(http.MethodGet, "/v1/capabilities", "").json()
	v, ok := c["benchmark"]
	if !ok || v != nil {
		t.Fatalf("benchmark = %v (present %v), want null", v, ok)
	}
}
