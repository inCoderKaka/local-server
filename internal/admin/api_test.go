package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/hostmetrics"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

func TestHostInfoAndPauseResume(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	pid := e.adminPrincipal()

	r := e.call(token, "GET", "/admin/v1/host", "")
	status(t, r, http.StatusOK)
	b := r.json()
	if b["host_kid"] != hostID || b["sharing_state"] != "sharing" || b["software_version"] != "1.2.3-test" || b["engine_state"] != "available" ||
		b["paused_at"] != nil || b["sharing_resumes_at"] != nil || b["engine_version"] != nil {
		t.Fatalf("host = %s", r.Body.String())
	}

	e.clk.advance(time.Minute)
	r = e.call(token, "POST", "/admin/v1/host/pause", "")
	status(t, r, http.StatusOK)
	b = r.json()
	if b["sharing_state"] != "paused" || b["paused_at"] != e.clk.now().UTC().Format(time.RFC3339Nano) && b["paused_at"] != e.clk.now().UTC().Format("2006-01-02T15:04:05Z") {
		t.Fatalf("paused host = %s", r.Body.String())
	}
	rows := e.auditRows("host.paused")
	if len(rows) != 1 || rows[0].actor != pid {
		t.Fatalf("host.paused rows = %+v; want one with the admin as actor", rows)
	}
	// Pausing a paused host changes and audits nothing.
	status(t, e.call(token, "POST", "/admin/v1/host/pause", ""), http.StatusOK)
	if n := len(e.auditRows("host.paused")); n != 1 {
		t.Errorf("host.paused rows after a repeat = %d", n)
	}
	if st, _ := e.share.Effective(context.Background()); st != host.Paused {
		t.Errorf("effective state = %s", st)
	}

	r = e.call(token, "POST", "/admin/v1/host/resume", "")
	status(t, r, http.StatusOK)
	b = r.json()
	if b["sharing_state"] != "sharing" || b["paused_at"] != nil {
		t.Fatalf("resumed host = %s", r.Body.String())
	}
	rows = e.auditRows("host.resumed")
	if len(rows) != 1 || rows[0].actor != pid {
		t.Fatalf("host.resumed rows = %+v", rows)
	}
}

func TestHostInfoQuietHoursEngineAndVersion(t *testing.T) {
	ve := versionedEngine{Engine: fake.New(), version: "0.12.9"}
	e := newEnv(t, func(o *envOpts) { o.engine = ve })
	token := e.setup()
	e.set(host.KeyQuietEnabled, "true")
	e.set(host.KeyQuietTimeZone, "UTC")
	e.set(host.KeyQuietStart, "11:00")
	e.set(host.KeyQuietEnd, "14:30")
	r := e.call(token, "GET", "/admin/v1/host", "")
	status(t, r, http.StatusOK)
	b := r.json()
	if b["sharing_state"] != "quiet" || b["sharing_resumes_at"] != "2026-11-04T14:30:00Z" || b["engine_version"] != "0.12.9" || b["engine_state"] != "available" {
		t.Fatalf("host = %s", r.Body.String())
	}
}

func TestHostInfoEngineDown(t *testing.T) {
	e := newEnv(t, func(o *envOpts) { o.engine = downEngine{} })
	token := e.setup()
	r := e.call(token, "GET", "/admin/v1/host", "")
	status(t, r, http.StatusOK)
	b := r.json()
	if b["engine_state"] != "unavailable" || b["engine_version"] != nil {
		t.Fatalf("host = %s", r.Body.String())
	}
}

func fullReading(at time.Time) hostmetrics.Reading {
	m := func(v int64, k hostmetrics.Measure) hostmetrics.Bytes { return hostmetrics.Bytes{Value: v, Measure: k} }
	p := func(v float64, k hostmetrics.Measure) hostmetrics.Percent {
		return hostmetrics.Percent{Value: v, Measure: k}
	}
	return hostmetrics.Reading{At: at,
		Host: hostmetrics.HostTotals{MemoryTotal: m(16<<30, hostmetrics.MeasureHostTotal), MemoryUsed: m(12<<30, hostmetrics.MeasureHostTotal),
			CPUCores: 8, CPU: p(12.5, hostmetrics.MeasureHostTotal)},
		Slice: hostmetrics.Slice{Memory: m(8<<30, hostmetrics.MeasureDerived), CPU: p(6.25, hostmetrics.MeasureDerived),
			LocalServerCPU: p(1, hostmetrics.MeasureProcessRSS), OllamaCPU: p(5.25, hostmetrics.MeasureProcessRSS)},
		Other:        hostmetrics.Other{Memory: m(4<<30, hostmetrics.MeasureDerived), CPU: hostmetrics.Percent{Measure: hostmetrics.MeasureUnavailable}},
		Unattributed: 3,
	}
}

func resourceOf(t *testing.T, b map[string]any, part, field string) (value any, method string) {
	t.Helper()
	m := b[part].(map[string]any)[field].(map[string]any)
	return m["value"], m["method"].(string)
}

func TestHostMetricsCurrent(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	rd := fullReading(t0)
	e.sampler.read = rd

	// No sample yet: one reading is taken.
	r := e.call(token, "GET", "/admin/v1/host-metrics", "")
	status(t, r, http.StatusOK)
	if e.sampler.reads != 1 {
		t.Fatalf("reads = %d, want 1", e.sampler.reads)
	}
	cur := fullReading(t0.Add(time.Minute))
	e.sampler.current = &cur
	r = e.call(token, "GET", "/admin/v1/host-metrics", "")
	status(t, r, http.StatusOK)
	if e.sampler.reads != 1 {
		t.Errorf("the latest sample was not used (reads = %d)", e.sampler.reads)
	}
	b := r.json()
	if b["sampled_at"] != "2026-11-04T12:01:00Z" {
		t.Errorf("sampled_at = %v", b["sampled_at"])
	}
	for _, c := range []struct {
		part, field string
		value       any
		method      string
	}{
		{"total", "memory_bytes", float64(12 << 30), "host_total"},
		{"total", "cpu_percent", 12.5, "host_total"},
		{"share_budget_slice", "memory_bytes", float64(8 << 30), "derived"},
		{"share_budget_slice", "cpu_percent", 6.25, "derived"},
		{"other_apps", "memory_bytes", float64(4 << 30), "derived"},
		{"other_apps", "cpu_percent", nil, "unavailable"}, // not read: null, never 0
		{"unattributed", "memory_bytes", nil, "unavailable: the OS would not report 3 processes"},
		{"unattributed", "cpu_percent", nil, "unavailable: the OS would not report 3 processes"},
	} {
		v, m := resourceOf(t, b, c.part, c.field)
		if v != c.value || m != c.method {
			t.Errorf("%s.%s = %v (%s), want %v (%s)", c.part, c.field, v, m, c.value, c.method)
		}
	}
}

func minuteRow(start time.Time, memUsed int64) hostmetrics.Minute {
	return hostmetrics.Minute{Start: start, SampledAt: start.Add(10 * time.Second), MemoryTotal: 16 << 30, MemoryUsed: memUsed, CPUCores: 8, CPUPercent: 10,
		SliceMemory: 1 << 30, SliceCPU: 2, OtherMemory: memUsed - 1<<30, OtherCPU: 8, Unattributed: 0,
		Measures: map[string]hostmetrics.Measure{"host_memory": hostmetrics.MeasureHostTotal, "host_cpu": hostmetrics.MeasureHostTotal,
			"slice": hostmetrics.MeasureDerived, "local_server_cpu": hostmetrics.MeasureProcessRSS, "ollama_cpu": hostmetrics.MeasureUnavailable,
			"other": hostmetrics.MeasureDerived, "other_cpu": hostmetrics.MeasureDerived}}
}

func TestHostMetricsHistory(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	for i := 0; i < 180; i++ {
		e.sampler.minutes = append(e.sampler.minutes, minuteRow(t0.Add(time.Duration(i-180)*time.Minute), int64(i+1)<<30))
	}
	e.sampler.minutes[179].Measures["host_cpu"] = hostmetrics.MeasureUnavailable

	hist := func(params map[string]string) (resp, []any) {
		r := e.call(token, "GET", "/admin/v1/host-metrics/history"+q(params), "")
		if r.Code != http.StatusOK {
			return r, nil
		}
		d, _ := r.json()["data"].([]any)
		return r, d
	}
	// Default range: the last hour (setting), oldest first.
	r, d := hist(nil)
	status(t, r, http.StatusOK)
	if len(d) != 60 {
		t.Fatalf("default range returned %d points, want 60", len(d))
	}
	first := d[0].(map[string]any)["sampled_at"].(string)
	last := d[len(d)-1].(map[string]any)["sampled_at"].(string)
	if first >= last {
		t.Errorf("not oldest first: %s .. %s", first, last)
	}
	// The last stored minute has host_cpu unavailable: null, with the measure as method.
	if v, m := resourceOf(t, d[len(d)-1].(map[string]any), "total", "cpu_percent"); v != nil || m != "unavailable" {
		t.Errorf("unreadable cpu = %v (%s)", v, m)
	}
	if v, m := resourceOf(t, d[0].(map[string]any), "total", "cpu_percent"); v != 10.0 || m != "host_total" {
		t.Errorf("cpu = %v (%s)", v, m)
	}
	// Slice CPU: derived while one of its parts was read.
	if _, m := resourceOf(t, d[0].(map[string]any), "share_budget_slice", "cpu_percent"); m != "derived" {
		t.Errorf("slice cpu method = %s", m)
	}
	// An explicit range.
	_, d = hist(map[string]string{"from": "2026-11-04T10:00:00Z", "to": "2026-11-04T10:10:00Z"})
	if len(d) != 10 {
		t.Errorf("explicit range returned %d points, want 10", len(d))
	}
	// A bad range.
	r, _ = hist(map[string]string{"from": "2026-11-04T10:10:00Z", "to": "2026-11-04T10:00:00Z"})
	status(t, r, http.StatusBadRequest)
	r.errBody(t)
	r, _ = hist(map[string]string{"from": "yesterday"})
	status(t, r, http.StatusBadRequest)
	r.errBody(t)

	// A huge range returns only the newest points, and reads no more than that.
	e.set(admin.KeyMetricsHistoryPoints, "25")
	e.sampler.ranges = nil
	r, d = hist(map[string]string{"from": "2000-01-01T00:00:00Z"})
	status(t, r, http.StatusOK)
	if len(d) != 25 {
		t.Fatalf("capped history has %d points, want 25", len(d))
	}
	if got := d[len(d)-1].(map[string]any)["sampled_at"].(string); got != e.sampler.minutes[179].SampledAt.Format("2006-01-02T15:04:05Z") {
		t.Errorf("newest point = %s", got)
	}
	if rg := e.sampler.ranges[0]; rg[1].Sub(rg[0]) > 25*time.Minute {
		t.Errorf("the sampler was asked for %v, more than the 25 points returned", rg[1].Sub(rg[0]))
	}
	e.set(admin.KeyMetricsHistoryRange, "10m")
	e.set(admin.KeyMetricsHistoryPoints, "1440")
	if _, d = hist(nil); len(d) != 10 {
		t.Errorf("default range setting: %d points, want 10", len(d))
	}
}

func settingsOf(t *testing.T, r resp) map[string]map[string]any {
	t.Helper()
	status(t, r, http.StatusOK)
	out := map[string]map[string]any{}
	var order []string
	for _, s := range r.json()["data"].([]any) {
		m := s.(map[string]any)
		out[m["key"].(string)] = m
		order = append(order, m["key"].(string))
	}
	if !sort.StringsAreSorted(order) {
		t.Errorf("settings are not sorted by key: %v", order)
	}
	return out
}

func TestSettingsListsOnlyHostOwnedTyped(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	// Settings of other owners exist on the host and must not be listed.
	if err := e.st.Register(settings.Def{Key: "x.consumer_pref", Kind: settings.Int, Default: "1", Owner: settings.Consumer},
		settings.Def{Key: "x.platform_pref", Kind: settings.Bool, Default: "false", Owner: settings.PlatformAdmin},
		settings.Def{Key: "x.scoped", Kind: settings.Int, Default: "1", Owner: settings.Host, Scoped: true}); err != nil {
		t.Fatal(err)
	}
	e.set(admin.KeyMaxBodyBytes, "2048")
	all := settingsOf(t, e.call(token, "GET", "/admin/v1/settings", ""))
	for _, k := range []string{"x.consumer_pref", "x.platform_pref", "x.scoped"} {
		if _, ok := all[k]; ok {
			t.Errorf("%s is listed", k)
		}
	}
	for key, c := range map[string]struct{ value, def any }{
		admin.KeyMaxBodyBytes:         {float64(2048), float64(1048576)}, // int -> number
		host.KeyQuietEnabled:          {false, false},                    // bool -> bool
		admin.KeySessionTTL:           {"12h", "12h"},                    // duration -> string
		admin.KeyTailnetName:          {"", ""},                          // string -> string
		credential.KeyMemoryKiB:       {float64(19456), float64(19456)},
		credential.KeyLockoutFailures: {float64(5), float64(5)},
	} {
		s, ok := all[key]
		if !ok {
			t.Errorf("%s is not listed", key)
			continue
		}
		if s["value"] != c.value || s["default_value"] != c.def || s["owner"] != "host" || s["description"] == "" || s["description"] == nil {
			t.Errorf("%s = %v", key, s)
		}
	}
}

// A settings write leaves an audit row with the old and new value and the admin as the actor,
// committed with the setting (req_001 D-07, D-54).
func TestSettingsWriteIsAudited(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	pid := e.adminPrincipal()
	r := e.call(token, "PATCH", "/admin/v1/settings", `{"values":{"admin.session_ttl":"2h","admin.max_body_bytes":4096,"host.quiet_hours.enabled":true,"admin.engine_probe_timeout":"2s"}}`)
	all := settingsOf(t, r)
	if all[admin.KeySessionTTL]["value"] != "2h" || all[admin.KeyMaxBodyBytes]["value"] != float64(4096) || all[host.KeyQuietEnabled]["value"] != true {
		t.Fatalf("response = %s", r.Body.String())
	}
	if d, _ := e.st.Duration(context.Background(), admin.KeySessionTTL, ""); d != 2*time.Hour {
		t.Fatalf("the setting is %v", d)
	}
	rows := e.auditRows("setting.changed")
	if len(rows) != 3 { // engine_probe_timeout 2s is already the default: not a change
		t.Fatalf("setting.changed rows = %d, want 3: %+v", len(rows), rows)
	}
	got := map[string][2]any{}
	for _, row := range rows {
		if row.actor != pid || row.target != "" {
			t.Errorf("row = %+v; want the admin as actor and no target", row)
		}
		got[row.detail["key"].(string)] = [2]any{row.detail["old_value"], row.detail["new_value"]}
	}
	want := map[string][2]any{
		admin.KeySessionTTL:   {"12h", "2h"},
		admin.KeyMaxBodyBytes: {"1048576", "4096"},
		host.KeyQuietEnabled:  {"false", "true"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("audited changes = %v, want %v", got, want)
	}

	// null returns a setting to its default, and that is audited too.
	status(t, e.call(token, "PATCH", "/admin/v1/settings", `{"values":{"admin.session_ttl":null}}`), http.StatusOK)
	if d, _ := e.st.Duration(context.Background(), admin.KeySessionTTL, ""); d != 12*time.Hour {
		t.Errorf("after null the setting is %v", d)
	}
	rows = e.auditRows("setting.changed")
	if last := rows[len(rows)-1]; last.detail["key"] != admin.KeySessionTTL || last.detail["old_value"] != "2h" || last.detail["new_value"] != "12h" {
		t.Errorf("reset row = %+v", last)
	}
	// Setting the value it already has writes nothing.
	n := len(rows)
	status(t, e.call(token, "PATCH", "/admin/v1/settings", `{"values":{"admin.session_ttl":"12h"}}`), http.StatusOK)
	if len(e.auditRows("setting.changed")) != n {
		t.Error("an unchanged value was audited")
	}
}

// The audit trail keeps no password: a URL value loses the password in its userinfo.
func TestSettingsAuditRedactsURLPasswords(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	if err := e.st.Register(settings.Def{Key: "x.upstream_url", Kind: settings.String, Default: "http://127.0.0.1:11434", Owner: settings.Host}); err != nil {
		t.Fatal(err)
	}
	status(t, e.call(token, "PATCH", "/admin/v1/settings", `{"values":{"x.upstream_url":"http://user:hunter2@127.0.0.1:11434"}}`), http.StatusOK)
	rows := e.auditRows("setting.changed")
	if len(rows) != 1 || rows[0].detail["new_value"] != "http://user:xxxxx@127.0.0.1:11434" || rows[0].detail["old_value"] != "http://127.0.0.1:11434" {
		t.Fatalf("rows = %+v", rows)
	}
	var detail string
	if err := e.db.Reader().QueryRow(context.Background(), `SELECT detail FROM aud_event WHERE action = 'setting.changed'`).Scan(&detail); err != nil || strings.Contains(detail, "hunter2") {
		t.Errorf("detail = %s, %v", detail, err)
	}
}

func TestSettingsRefusalsApplyNothing(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	if err := e.st.Register(settings.Def{Key: "x.consumer_pref", Kind: settings.Int, Default: "1", Owner: settings.Consumer}); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		body   string
		status int
		param  string
	}{
		"not host-owned":    {`{"values":{"admin.session_ttl":"3h","x.consumer_pref":2}}`, http.StatusForbidden, ""},
		"unknown key":       {`{"values":{"admin.session_ttl":"3h","nope.setting":2}}`, http.StatusBadRequest, "nope.setting"},
		"unknown odd key":   {`{"values":{"admin.session_ttl":"3h","<script>":2}}`, http.StatusBadRequest, ""},
		"string for int":    {`{"values":{"admin.session_ttl":"3h","admin.max_body_bytes":"4096"}}`, http.StatusBadRequest, admin.KeyMaxBodyBytes},
		"float for int":     {`{"values":{"admin.session_ttl":"3h","admin.max_body_bytes":1.5}}`, http.StatusBadRequest, admin.KeyMaxBodyBytes},
		"number for string": {`{"values":{"admin.session_ttl":"3h","admin.tailnet_name":5}}`, http.StatusBadRequest, admin.KeyTailnetName},
		"bool for duration": {`{"values":{"admin.session_ttl":true}}`, http.StatusBadRequest, admin.KeySessionTTL},
		"object value":      {`{"values":{"admin.session_ttl":{"a":1}}}`, http.StatusBadRequest, admin.KeySessionTTL},
		"bad duration":      {`{"values":{"admin.session_ttl":"3 hours"}}`, http.StatusBadRequest, admin.KeySessionTTL},
		"below OWASP":       {`{"values":{"admin.session_ttl":"3h","auth.password.memory_kib":1024}}`, http.StatusBadRequest, credential.KeyMemoryKiB},
		"validator refuses": {`{"values":{"admin.session_ttl":"3h","admin.max_body_bytes":0}}`, http.StatusBadRequest, admin.KeyMaxBodyBytes},
		"bad tailnet name":  {`{"values":{"admin.session_ttl":"3h","admin.tailnet_name":"http://x.ts.net"}}`, http.StatusBadRequest, admin.KeyTailnetName},
		"empty values":      {`{"values":{}}`, http.StatusBadRequest, "values"},
	} {
		r := e.call(token, "PATCH", "/admin/v1/settings", c.body)
		if r.Code != c.status {
			t.Errorf("%s: status %d, want %d (%s)", name, r.Code, c.status, r.Body.String())
			continue
		}
		inner := r.errBody(t)
		if c.param != "" && inner["param"] != c.param {
			t.Errorf("%s: param = %v, want %s", name, inner["param"], c.param)
		}
		for _, leak := range []string{"3 hours", "<script>", "http://x.ts.net", "1024"} {
			if strings.Contains(r.Body.String(), leak) {
				t.Errorf("%s: the answer repeats the value %q: %s", name, leak, r.Body.String())
			}
		}
		if d, _ := e.st.Duration(context.Background(), admin.KeySessionTTL, ""); d != 12*time.Hour {
			t.Errorf("%s: a good key in the same request was applied (%v)", name, d)
		}
		if n := e.count(`SELECT count(*) FROM aud_event WHERE action = 'setting.changed'`); n != 0 {
			t.Errorf("%s: %d audit rows after a refused request", name, n)
		}
	}
}

func TestSettingsAuditFailureRollsTheSettingBack(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	// An audit row whose actor does not exist fails; the setting must not stay. The actor is the
	// session's principal, so remove it from under the session by dropping the FK target is not
	// possible; instead make the audit insert fail with a trigger.
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER test_no_setting_audit BEFORE INSERT ON aud_event WHEN NEW.action = 'setting.changed' BEGIN SELECT RAISE(ABORT, 'audit down'); END`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r := e.call(token, "PATCH", "/admin/v1/settings", `{"values":{"admin.session_ttl":"3h"}}`)
	status(t, r, http.StatusInternalServerError)
	r.errBody(t)
	if d, _ := e.st.Duration(context.Background(), admin.KeySessionTTL, ""); d != 12*time.Hour {
		t.Errorf("the setting stayed at %v although its audit row failed", d)
	}
}

func TestAuditListShapeFilterAndPaging(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	pid := e.adminPrincipal()
	e.clk.advance(time.Minute)
	status(t, e.call(token, "POST", "/admin/v1/host/pause", ""), http.StatusOK)
	e.clk.advance(time.Minute)
	status(t, e.call(token, "POST", "/admin/v1/host/resume", ""), http.StatusOK)
	e.clk.advance(time.Minute)
	status(t, e.call(token, "PATCH", "/admin/v1/settings", `{"values":{"admin.session_ttl":"3h"}}`), http.StatusOK)

	r := e.call(token, "GET", "/admin/v1/audit-events", "")
	status(t, r, http.StatusOK)
	body := r.json()
	data := body["data"].([]any)
	if len(data) != 4 || body["next_cursor"] != nil { // admin.created, host.paused, host.resumed, setting.changed
		t.Fatalf("audit list = %s", r.Body.String())
	}
	newest := data[0].(map[string]any)
	if newest["action"] != "setting.changed" || newest["occurred_at"] != "2026-11-04T12:03:00Z" || newest["id"] == "" {
		t.Errorf("newest = %v", newest)
	}
	actor := newest["actor"].(map[string]any)
	if actor["principal_id"] != pid || actor["kind"] != "host_admin" || actor["display_name"] != testUser {
		t.Errorf("actor = %v", actor)
	}
	if _, err := uuid.Parse(newest["id"].(string)); err != nil {
		t.Errorf("id: %v", err)
	}
	details := newest["details"].(map[string]any)
	if details["key"] != "admin.session_ttl" || details["new_value"] != "3h" {
		t.Errorf("details = %v", details)
	}
	if newest["target_id"] != nil || newest["target_kind"] != nil {
		t.Errorf("target = %v %v", newest["target_kind"], newest["target_id"])
	}
	created := data[3].(map[string]any)
	if created["action"] != "admin.created" || created["target_id"] != pid || created["target_kind"] != "host_admin" {
		t.Errorf("admin.created = %v", created)
	}

	// Filters.
	r = e.call(token, "GET", "/admin/v1/audit-events"+q(map[string]string{"action": "host.paused"}), "")
	status(t, r, http.StatusOK)
	if d := r.json()["data"].([]any); len(d) != 1 || d[0].(map[string]any)["action"] != "host.paused" {
		t.Errorf("action filter = %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/audit-events"+q(map[string]string{"from": "2026-11-04T12:01:00Z", "to": "2026-11-04T12:03:00Z"}), "")
	status(t, r, http.StatusOK)
	if d := r.json()["data"].([]any); len(d) != 2 {
		t.Errorf("time filter returned %d events, want 2", len(d))
	}
	// Paging.
	r = e.call(token, "GET", "/admin/v1/audit-events"+q(map[string]string{"limit": "3"}), "")
	status(t, r, http.StatusOK)
	cursor, _ := r.json()["next_cursor"].(string)
	if len(r.json()["data"].([]any)) != 3 || cursor == "" {
		t.Fatalf("page 1 = %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/audit-events"+q(map[string]string{"limit": "3", "cursor": cursor}), "")
	status(t, r, http.StatusOK)
	if d := r.json()["data"].([]any); len(d) != 1 || r.json()["next_cursor"] != nil {
		t.Errorf("page 2 = %s", r.Body.String())
	}
	// Invalid cursor and limit.
	r = e.call(token, "GET", "/admin/v1/audit-events"+q(map[string]string{"cursor": "garbage"}), "")
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "cursor" {
		t.Errorf("cursor error = %s", r.Body.String())
	}
	status(t, e.call(token, "GET", "/admin/v1/audit-events"+q(map[string]string{"limit": "0"}), ""), http.StatusBadRequest)
	status(t, e.call(token, "GET", "/admin/v1/audit-events"+q(map[string]string{"from": "nope"}), ""), http.StatusBadRequest)
}

// An actor-less (system) event is a null actor; an actor that is a consumer has its kind.
func TestAuditListSystemEvent(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO aud_event (id, action, detail, created_at) VALUES (?, 'key.expired', '{"n":1}', ?)`, uuid.NewString(), e.clk.now().UnixMilli()+1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r := e.call(token, "GET", "/admin/v1/audit-events"+q(map[string]string{"action": "key.expired"}), "")
	status(t, r, http.StatusOK)
	ev := r.json()["data"].([]any)[0].(map[string]any)
	if ev["actor"] != nil || ev["target_id"] != nil || ev["details"].(map[string]any)["n"] != float64(1) {
		t.Errorf("event = %v", ev)
	}
}

// seedRollups writes rollup rows of two consumers the way the rollup worker would.
func seedRollups(t *testing.T, e *env) (consA, consB string) {
	t.Helper()
	consA, consB = uuid.NewString(), uuid.NewString()
	keyA, keyB := uuid.NewString(), uuid.NewString()
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		for _, g := range []string{"minute", "hour", "day"} {
			for i, row := range []struct{ cons, key, model string }{{consA, keyA, "model-a"}, {consB, keyB, "model-b"}, {consB, keyB, "model-c"}} {
				if _, err := tx.Exec(`INSERT INTO use_rollup (granularity, bucket_start, consumer_id, access_key_id, model, requests, prompt_tokens, completion_tokens, total_tokens)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, g, int64(60000*(i+1)), row.cons, row.key, row.model, i+1, 10*(i+1), 5*(i+1), 15*(i+1)); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return consA, consB
}

// /admin/v1/usage answers with the same rollups usage.QueryRollups returns for the same query,
// across every consumer unless one is asked for (req_001 D-27, D-104).
func TestUsageIsTheQueryFunction(t *testing.T) {
	e := newEnv(t)
	token := e.setup()
	consA, consB := seedRollups(t, e)
	for _, c := range []struct {
		name  string
		query map[string]string
		q     usage.RollupQuery
		want  int
	}{
		{"day, all consumers", map[string]string{"granularity": "day"}, usage.RollupQuery{Granularity: usage.GranularityDay}, 3},
		{"hour, all consumers", map[string]string{"granularity": "hour"}, usage.RollupQuery{Granularity: usage.GranularityHour}, 3},
		{"minute, one consumer", map[string]string{"granularity": "minute", "consumer_id": consB}, usage.RollupQuery{Granularity: usage.GranularityMinute, ConsumerID: consB}, 2},
		{"one model", map[string]string{"granularity": "day", "model": "model-a"}, usage.RollupQuery{Granularity: usage.GranularityDay, Model: "model-a"}, 1},
		{"consumer A", map[string]string{"granularity": "day", "consumer_id": consA}, usage.RollupQuery{Granularity: usage.GranularityDay, ConsumerID: consA}, 1},
	} {
		want, err := usage.QueryRollups(context.Background(), e.db.Reader(), e.st, c.q)
		if err != nil || len(want.Rollups) != c.want {
			t.Fatalf("%s: QueryRollups = %d rows, %v; want %d", c.name, len(want.Rollups), err, c.want)
		}
		r := e.call(token, "GET", "/admin/v1/usage"+q(c.query), "")
		status(t, r, http.StatusOK)
		var got struct {
			Data []struct {
				BucketStart      time.Time `json:"bucket_start"`
				Granularity      string    `json:"granularity"`
				ConsumerID       string    `json:"consumer_id"`
				AccessKeyID      string    `json:"access_key_id"`
				Model            string    `json:"model"`
				Requests         int64     `json:"requests"`
				PromptTokens     int64     `json:"prompt_tokens"`
				CompletionTokens int64     `json:"completion_tokens"`
				TotalTokens      int64     `json:"total_tokens"`
			}
			NextCursor *string `json:"next_cursor"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Data) != len(want.Rollups) {
			t.Fatalf("%s: %d rows, want %d", c.name, len(got.Data), len(want.Rollups))
		}
		for i, w := range want.Rollups {
			g := got.Data[i]
			if !g.BucketStart.Equal(w.BucketStart) || g.Granularity != string(w.Granularity) || g.ConsumerID != w.ConsumerID || g.AccessKeyID != w.AccessKeyID ||
				g.Model != w.Model || g.Requests != w.Requests || g.PromptTokens != w.PromptTokens || g.CompletionTokens != w.CompletionTokens || g.TotalTokens != w.TotalTokens {
				t.Errorf("%s: row %d = %+v, want %+v", c.name, i, g, w)
			}
		}
		if (want.NextCursor == "") != (got.NextCursor == nil) {
			t.Errorf("%s: next_cursor %v, want %q", c.name, got.NextCursor, want.NextCursor)
		}
	}
	// Paging and the error mappings are the query function's.
	r := e.call(token, "GET", "/admin/v1/usage"+q(map[string]string{"granularity": "day", "limit": "2"}), "")
	status(t, r, http.StatusOK)
	cursor, _ := r.json()["next_cursor"].(string)
	if len(r.json()["data"].([]any)) != 2 || cursor == "" {
		t.Fatalf("page 1 = %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/usage"+q(map[string]string{"granularity": "day", "limit": "2", "cursor": cursor}), "")
	status(t, r, http.StatusOK)
	if len(r.json()["data"].([]any)) != 1 {
		t.Errorf("page 2 = %s", r.Body.String())
	}
	r = e.call(token, "GET", "/admin/v1/usage"+q(map[string]string{"granularity": "day", "cursor": "garbage"}), "")
	status(t, r, http.StatusBadRequest)
	if r.errBody(t)["param"] != "cursor" {
		t.Errorf("cursor error = %s", r.Body.String())
	}
	status(t, e.call(token, "GET", "/admin/v1/usage"+q(map[string]string{"granularity": "year"}), ""), http.StatusBadRequest)
	status(t, e.call(token, "GET", "/admin/v1/usage", ""), http.StatusBadRequest) // granularity is required
	status(t, e.call(token, "GET", "/admin/v1/usage"+q(map[string]string{"granularity": "day", "consumer_id": "not-a-uuid"}), ""), http.StatusBadRequest)
}
