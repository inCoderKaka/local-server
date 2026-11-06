package grants_test

import (
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/admin/consumers"
	admingrants "github.com/inCoderKaka/local-server/internal/admin/grants"
	"github.com/inCoderKaka/local-server/internal/engine/fake"
)

func modelByName(t *testing.T, ms []admingrants.HostModel, name string) admingrants.HostModel {
	t.Helper()
	for _, m := range ms {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("no model %q in %+v", name, ms)
	return admingrants.HostModel{}
}

// The engine's models are stored on first sight, closed unless the host chose otherwise, and shown
// with their loaded state.
func TestListModelsSyncsTheEngineAndShowsLoadedState(t *testing.T) {
	e := newEnv(t)
	ms, err := e.svc.ListModels(ctx)
	if err != nil || len(ms) != 2 {
		t.Fatalf("models = %+v, %v", ms, err)
	}
	m1 := modelByName(t, ms, "m1")
	if m1.Allowed || m1.Preload || m1.Loaded || m1.KeepAliveSeconds != nil || m1.Engine != "ollama" || m1.ID == "" {
		t.Fatalf("m1 = %+v", m1)
	}
	if err := e.eng.Load(ctx, "m1", 0); err != nil {
		t.Fatal(err)
	}
	ms, err = e.svc.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := modelByName(t, ms, "m1"); !got.Loaded || got.ID != m1.ID || got.SizeBytes == nil || *got.SizeBytes != 1<<30 {
		t.Fatalf("loaded m1 = %+v", got)
	}
	if n := e.count(`SELECT count(*) FROM grt_model`); n != 2 {
		t.Fatalf("%d model rows, want 2 (no duplicates)", n)
	}

	// The host chooses whether a model it sees later is open at once.
	e.openModels()
	e.eng = fake.New(fake.WithModels(fake.ModelSpec{Name: "m1"}, fake.ModelSpec{Name: "m3"}))
	e2 := rebuild(t, e)
	ms, err = e2.svc.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || !modelByName(t, ms, "m3").Allowed || modelByName(t, ms, "m1").Allowed {
		t.Fatalf("models = %+v (m2 is gone from the engine, m3 opens at once, m1 stays as it was)", ms)
	}
}

// When the engine cannot be asked, the stored models are shown, none loaded.
func TestListModelsWithTheEngineDown(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.ListModels(ctx); err != nil {
		t.Fatal(err)
	}
	e2 := rebuildWith(t, e, downEngine{})
	ms, err := e2.svc.ListModels(ctx)
	if err != nil || len(ms) != 2 || ms[0].Loaded || ms[0].SizeBytes != nil {
		t.Fatalf("models = %+v, %v", ms, err)
	}
}

func TestUpdateModel(t *testing.T) {
	e := newEnv(t)
	ms, _ := e.svc.ListModels(ctx)
	m1, m2 := modelByName(t, ms, "m1"), modelByName(t, ms, "m2")
	yes, no := true, false

	// Allowed and keep-alive are stored on the model.
	got, err := e.svc.UpdateModel(ctx, e.admin, m1.ID, admingrants.ModelChange{Allowed: &yes, KeepAliveSeconds: consumers.Value(int64(300))})
	if err != nil || !got.Allowed || got.KeepAliveSeconds == nil || *got.KeepAliveSeconds != 300 || got.Preload {
		t.Fatalf("model = %+v, %v", got, err)
	}
	// Fields left out stay; null clears the keep-alive.
	got, err = e.svc.UpdateModel(ctx, e.admin, m1.ID, admingrants.ModelChange{KeepAliveSeconds: consumers.Null[int64]()})
	if err != nil || !got.Allowed || got.KeepAliveSeconds != nil {
		t.Fatalf("model = %+v, %v", got, err)
	}
	// Preload rewrites the scheduler's setting, keeping what else it lists.
	e.set("scheduler.preload_models", "other, m2")
	if got, err = e.svc.UpdateModel(ctx, e.admin, m1.ID, admingrants.ModelChange{Preload: &yes}); err != nil || !got.Preload {
		t.Fatalf("model = %+v, %v", got, err)
	}
	if v, _ := e.st.String(ctx, "scheduler.preload_models", ""); v != "other,m2,m1" {
		t.Fatalf("preload_models = %q", v)
	}
	if got, err = e.svc.UpdateModel(ctx, e.admin, m2.ID, admingrants.ModelChange{Preload: &no}); err != nil || got.Preload {
		t.Fatalf("model = %+v, %v", got, err)
	}
	if v, _ := e.st.String(ctx, "scheduler.preload_models", ""); v != "other,m1" {
		t.Fatalf("preload_models = %q", v)
	}
	ms, _ = e.svc.ListModels(ctx)
	if !modelByName(t, ms, "m1").Preload || modelByName(t, ms, "m2").Preload {
		t.Fatalf("listed preload flags = %+v", ms)
	}

	// Each change is audited with the names of the fields that changed, and nothing else.
	txt := e.auditText()
	if n := strings.Count(txt, "model.updated"); n != 4 {
		t.Fatalf("%d model.updated rows, want 4:\n%s", n, txt)
	}
	if !strings.Contains(txt, `"changed":["preload"]`) || !strings.Contains(txt, `"changed":["allowed","keep_alive_seconds"]`) {
		t.Fatalf("audit = %s", txt)
	}
	// A change that changes nothing is not audited.
	if _, err := e.svc.UpdateModel(ctx, e.admin, m1.ID, admingrants.ModelChange{Allowed: &yes, Preload: &yes}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(e.auditText(), "model.updated"); n != 4 {
		t.Fatalf("%d model.updated rows after a no-op, want 4", n)
	}
	// Taking the last model off the list returns the setting to its default.
	e.set("scheduler.preload_models", "m1")
	if _, err := e.svc.UpdateModel(ctx, e.admin, m1.ID, admingrants.ModelChange{Preload: &no}); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.st.String(ctx, "scheduler.preload_models", ""); v != "" {
		t.Fatalf("preload_models = %q", v)
	}
	_, err = e.svc.UpdateModel(ctx, e.admin, m1.ID, admingrants.ModelChange{KeepAliveSeconds: consumers.Value(int64(-1))})
	wantKind(t, err, admingrants.KindInvalid, "keep_alive_seconds")
	_, err = e.svc.UpdateModel(ctx, e.admin, "0192e5a0-000c-7000-8000-00000000000c", admingrants.ModelChange{Allowed: &yes})
	wantKind(t, err, admingrants.KindNotFound, "")
}
