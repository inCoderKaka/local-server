package gateway_test

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine/fake"
	"github.com/inCoderKaka/local-server/internal/gateway"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func (e *env) addRule(name string, global bool, position int64, body string) grants.PolicyRule {
	e.t.Helper()
	var rule grants.PolicyRule
	err := e.db.Write(context.Background(), func(tx *storage.Tx) (err error) {
		var gid *string
		if !global {
			gid = &e.grant.ID
		}
		rule, err = e.gr.PolicyRules.Create(tx, gid, name, position, 1, body)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return rule
}

// D-79: the status, the code, and what the body names.
func TestPolicyRefusalsFollowD79(t *testing.T) {
	e := newEnv(t, func(o *envOpts) {
		o.version = func(models []grantsModel) grantsNewVersion {
			allowed := `["background","purpose"]`
			return grantsNewVersion{Models: models, AllowedProperties: &allowed}
		}
	})
	deny := e.addRule("no-secret-model", false, 0, `{"format_version":1,"when":{"properties":{"purpose":"secret"}},"deny":true}`)
	soon := time.Now().UTC().Add(2 * time.Hour)
	later := soon.Add(time.Hour)
	window := e.addRule("night-only", false, 1,
		fmt.Sprintf(`{"format_version":1,"when":{"properties":{"purpose":"batch"}},"time_zone":"UTC","windows":[{"start":"%s","end":"%s"}]}`,
			soon.Format("15:04"), later.Format("15:04")))

	t.Run("model not granted is 403", func(t *testing.T) {
		r := e.chat(`{"model":"nope","messages":[{"role":"user","content":"hi"}]}`)
		statusIs(t, r, http.StatusForbidden)
		if eb := r.errBody(); eb["code"] != "model_not_granted" || eb["type"] != "invalid_request_error" || eb["param"] != "model" {
			t.Fatalf("error = %v", eb)
		}
	})
	t.Run("a deny rule is 403 policy_denied and names the rule", func(t *testing.T) {
		r := e.chat(okChat, "LS-Request-Properties", "purpose=secret")
		statusIs(t, r, http.StatusForbidden)
		eb := r.errBody()
		if eb["code"] != "policy_denied" || eb["rule_id"] != deny.ID || eb["rule_name"] != "no-secret-model" {
			t.Fatalf("error = %v", eb)
		}
		if r.Header().Get("Retry-After") != "" {
			t.Error("policy_denied has no Retry-After")
		}
		if strings.Contains(r.Body.String(), "secret\"") || strings.Contains(r.Body.String(), "purpose=secret") {
			t.Errorf("the body quotes a property value: %s", r.Body.String())
		}
	})
	t.Run("a property the grant does not allow is 403 policy_denied and names the property", func(t *testing.T) {
		r := e.chat(okChat, "LS-Request-Properties", "mood=happy")
		statusIs(t, r, http.StatusForbidden)
		eb := r.errBody()
		props, _ := eb["properties"].([]any)
		if eb["code"] != "policy_denied" || len(props) != 1 || props[0] != "mood" {
			t.Fatalf("error = %v", eb)
		}
		if strings.Contains(r.Body.String(), "happy") {
			t.Errorf("the body quotes a property value: %s", r.Body.String())
		}
	})
	t.Run("every property the grant does not allow is named, without values", func(t *testing.T) {
		r := e.chat(okChat, "LS-Request-Properties", "mood=happy, zeta=1")
		statusIs(t, r, http.StatusForbidden)
		eb := r.errBody()
		props, _ := eb["properties"].([]any)
		if eb["code"] != "policy_denied" || len(props) != 2 || props[0] != "mood" || props[1] != "zeta" {
			t.Fatalf("error = %v", eb)
		}
		if strings.Contains(r.Body.String(), "happy") {
			t.Errorf("the body quotes a property value: %s", r.Body.String())
		}
	})
	t.Run("outside a time window is 429 with Retry-After", func(t *testing.T) {
		r := e.chat(okChat, "LS-Request-Properties", "purpose=batch")
		statusIs(t, r, http.StatusTooManyRequests)
		eb := r.errBody()
		if eb["code"] != "outside_time_window" || eb["rule_id"] != window.ID || eb["rule_name"] != "night-only" {
			t.Fatalf("error = %v", eb)
		}
		secs, err := strconv.Atoi(r.Header().Get("Retry-After"))
		if err != nil || secs < 2*3600-120 || secs > 2*3600+120 {
			t.Fatalf("Retry-After = %q, want about two hours", r.Header().Get("Retry-After"))
		}
	})
	t.Run("a malformed properties header is 400 invalid_request_properties", func(t *testing.T) {
		r := e.chat(okChat, "LS-Request-Properties", "a=(b")
		statusIs(t, r, http.StatusBadRequest)
		if eb := r.errBody(); eb["code"] != "invalid_request_properties" || eb["param"] != "LS-Request-Properties" {
			t.Fatalf("error = %v", eb)
		}
		if strings.Contains(r.Body.String(), "(b") {
			t.Errorf("the body quotes the header: %s", r.Body.String())
		}
	})
	t.Run("allowed properties pass; an embeddings call is checked the same way", func(t *testing.T) {
		statusIs(t, e.chat(okChat, "LS-Request-Properties", "background, purpose=summarize"), http.StatusOK)
		r := e.embed(`{"model":"`+embedModel+`","input":"hi"}`, "LS-Request-Properties", "mood")
		statusIs(t, r, http.StatusForbidden)
	})
}

func TestPolicyBackgroundPriorityAndCapsReachTheEngine(t *testing.T) {
	e := newEnv(t)
	e.addRule("bg", true, 0, `{"format_version":1,"when":{"properties":{"background":true}},"priority":"background","caps":{"context_cap":64,"max_output_tokens":3,"max_concurrency":2}}`)
	statusIs(t, e.chat(okChat, "LS-Request-Properties", "background"), http.StatusOK)
	got := e.eng.chats[0]
	if got.NumCtx != 64 || got.NumPredict != 3 {
		t.Fatalf("engine saw num_ctx %d, num_predict %d; want the rule's caps 64 and 3", got.NumCtx, got.NumPredict)
	}
	req := e.timed.lastRequest()
	if req.Class != scheduler.ClassBackground || req.GrantConcurrency != 2 || req.Job {
		t.Fatalf("scheduler saw class %v, grant concurrency %d, job %v; want background, 2, false", req.Class, req.GrantConcurrency, req.Job)
	}
}

// D-73, PLT-B-020: a rule that does not parse fails closed, is counted, logged and announced.
func TestBrokenPolicyRuleFailsClosedAndIsReported(t *testing.T) {
	e := newEnv(t)
	broken := e.addRule("broken", true, 0, `{"format_version":1,"deny":"yes"}`)
	for i := 0; i < 3; i++ {
		r := e.chat(okChat)
		statusIs(t, r, http.StatusInternalServerError)
		eb := r.errBody()
		if eb["code"] != nil || eb["type"] != "server_error" {
			t.Fatalf("error = %v", eb)
		}
		if strings.Contains(r.Body.String(), broken.ID) || strings.Contains(r.Body.String(), "broken") {
			t.Fatalf("the consumer must not learn the rule: %s", r.Body.String())
		}
	}
	if n := e.gw.RuleFailures(broken.ID); n != 3 {
		t.Fatalf("RuleFailures = %d, want 3", n)
	}
	if got := e.tel.count("gateway_policy_rule_failures_total", "invalid_policy_rule"); got != 3 {
		t.Fatalf("counter = %v, want 3", got)
	}
	evs := e.events("policy.rule_invalid")
	if len(evs) != 1 {
		t.Fatalf("policy.rule_invalid events = %d, want 1 (rate limited)", len(evs))
	}
	if p := string(evs[0].Payload); !strings.Contains(p, broken.ID) || strings.Contains(p, "deny") || strings.Contains(p, "broken") {
		t.Fatalf("payload = %s, want the rule id and no body or name", p)
	}
	if !strings.Contains(e.tel.logText(), broken.ID) {
		t.Error("no log line names the broken rule")
	}
	if n := e.useRecords(); n != 0 || e.eng.Calls(fake.OpChat) != 0 {
		t.Fatalf("a refused request reached the engine or the ledger")
	}

	// After the interval another failure raises another event; a fixed rule is forgotten.
	e.set(gateway.KeyRuleInvalidInterval, "1ms")
	time.Sleep(5 * time.Millisecond)
	statusIs(t, e.chat(okChat), http.StatusInternalServerError)
	if n := len(e.events("policy.rule_invalid")); n != 2 {
		t.Fatalf("events after the interval = %d, want 2", n)
	}
	e.gw.ClearRuleFailures(broken.ID)
	if n := e.gw.RuleFailures(broken.ID); n != 0 {
		t.Fatalf("RuleFailures after clear = %d", n)
	}
}

func TestUnknownRuleFormatVersionIsReportedToo(t *testing.T) {
	e := newEnv(t)
	err := e.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := e.gr.PolicyRules.Create(tx, nil, "future", 0, 99, `{"format_version":99}`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	statusIs(t, e.chat(okChat), http.StatusInternalServerError)
	if got := e.tel.count("gateway_policy_rule_failures_total", "unknown_rule_format_version"); got != 1 {
		t.Fatalf("counter = %v, want 1", got)
	}
	if n := len(e.events("policy.rule_invalid")); n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}
}

type panicPolicy struct{}

func (panicPolicy) Evaluate(context.Context, policy.Input) (policy.Decision, error) {
	panic("boom SECRETMARKER")
}

func TestAPanicAnswersInTheEnvelopeAndLogsNoValue(t *testing.T) {
	e := newEnv(t, func(o *envOpts) { o.policy = panicPolicy{} })
	r := e.chat(okChat)
	statusIs(t, r, http.StatusInternalServerError)
	if r.errBody()["type"] != "server_error" {
		t.Fatalf("body = %s", r.Body.String())
	}
	if strings.Contains(r.Body.String(), "SECRETMARKER") || strings.Contains(e.tel.logText(), "SECRETMARKER") {
		t.Fatal("a panic value leaked")
	}
}
