package gateway_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"testing"

	"golang.org/x/mod/sumdb/tlog"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/ledger/checkpoint"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func TestKeysEndpointServesTheJWKSetAndVerifiesReceipts(t *testing.T) {
	e := newEnv(t)
	r := e.do(http.MethodGet, "/v1/keys", "")
	statusIs(t, r, http.StatusOK)
	keys, _ := r.json()["keys"].([]any)
	roles := map[string]bool{}
	for _, k := range keys {
		roles[k.(map[string]any)["ls_role"].(string)] = true
	}
	if !roles["identity"] || !roles["receipt"] || !roles["checkpoint"] {
		t.Fatalf("roles = %v, want identity, receipt and checkpoint", roles)
	}
	// What the endpoint serves is what Service.KeySet builds.
	want, err := e.cps.KeySet(context.Background())
	if err != nil || len(keys) != len(want.Keys) {
		t.Fatalf("keys = %d, KeySet has %d (%v)", len(keys), len(want.Keys), err)
	}
	// No key: the same envelope as everywhere else.
	statusIs(t, e.doAuth("", http.MethodGet, "/v1/keys", ""), http.StatusUnauthorized)
}

func TestCheckpointAndConsistencyEndpoints(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r := e.do(http.MethodGet, "/v1/ledger/checkpoint", "")
	statusIs(t, r, http.StatusNotFound) // nothing signed yet
	if r.errBody()["type"] == nil {
		t.Fatalf("not an envelope: %s", r.Body.String())
	}

	for i := 0; i < 2; i++ {
		statusIs(t, e.chat(okChat), http.StatusOK)
	}
	first, made, err := e.cps.Build(ctx)
	if err != nil || !made {
		t.Fatalf("build: %v %v", made, err)
	}
	for i := 0; i < 3; i++ {
		statusIs(t, e.chat(okChat), http.StatusOK)
	}
	latest, made, err := e.cps.Build(ctx)
	if err != nil || !made {
		t.Fatalf("build: %v %v", made, err)
	}

	r = e.do(http.MethodGet, "/v1/ledger/checkpoint", "")
	statusIs(t, r, http.StatusOK)
	body := r.json()
	if body["checkpoint"] != latest.Note || int64(body["tree_size"].(float64)) != latest.TreeSize {
		t.Fatalf("checkpoint body = %v", body)
	}
	if body["from_tree_size"] != nil || body["consistency_proof"] != nil {
		t.Fatalf("no proof was asked for: %v", body)
	}
	root, err := base64.StdEncoding.DecodeString(body["root_hash"].(string))
	if err != nil || string(root) != string(latest.RootHash[:]) {
		t.Fatalf("root_hash = %v (%v)", body["root_hash"], err)
	}

	r = e.do(http.MethodGet, "/v1/ledger/checkpoint?from_tree_size="+strconv.FormatInt(first.TreeSize, 10), "")
	statusIs(t, r, http.StatusOK)
	body = r.json()
	proof, _ := body["consistency_proof"].([]any)
	if int64(body["from_tree_size"].(float64)) != first.TreeSize || len(proof) == 0 {
		t.Fatalf("consistency body = %v", body)
	}
	var hashes tlog.TreeProof
	for _, p := range proof {
		b, err := base64.StdEncoding.DecodeString(p.(string))
		if err != nil || len(b) != len(tlog.Hash{}) {
			t.Fatalf("proof element %v: %v", p, err)
		}
		var h tlog.Hash
		copy(h[:], b)
		hashes = append(hashes, h)
	}
	if err := tlog.CheckTree(hashes, latest.TreeSize, latest.RootHash, first.TreeSize, first.RootHash); err != nil {
		t.Fatalf("the served consistency proof does not verify: %v", err)
	}

	// from the latest size itself: an empty proof; beyond it, or below 1: 400.
	r = e.do(http.MethodGet, "/v1/ledger/checkpoint?from_tree_size="+strconv.FormatInt(latest.TreeSize, 10), "")
	statusIs(t, r, http.StatusOK)
	for _, from := range []int64{latest.TreeSize + 1, 0, -3} {
		r = e.do(http.MethodGet, "/v1/ledger/checkpoint?from_tree_size="+strconv.FormatInt(from, 10), "")
		statusIs(t, r, http.StatusBadRequest)
		if r.errBody()["param"] != "from_tree_size" {
			t.Fatalf("from %d: %s", from, r.Body.String())
		}
	}
}

func TestUsageProofEndpoint(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r := e.chat(okChat)
	statusIs(t, r, http.StatusOK)
	rc := e.verify(r.Header().Get("LS-Receipt"))

	// Before a checkpoint covers the leaf: 409 with Retry-After.
	pr := e.do(http.MethodGet, "/v1/usage/"+rc.UsageID+"/proof", "")
	statusIs(t, pr, http.StatusConflict)
	if secs, err := strconv.Atoi(pr.Header().Get("Retry-After")); err != nil || secs < 1 {
		t.Fatalf("Retry-After = %q", pr.Header().Get("Retry-After"))
	}

	cp, _, err := e.cps.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pr = e.do(http.MethodGet, "/v1/usage/"+rc.UsageID+"/proof", "")
	statusIs(t, pr, http.StatusOK)
	body := pr.json()
	if body["usage_id"] != rc.UsageID || int64(body["leaf_index"].(float64)) != *rc.LeafIndex || body["checkpoint"] != cp.Note {
		t.Fatalf("proof body = %v", body)
	}
	var hashes tlog.RecordProof
	for _, p := range body["inclusion_proof"].([]any) {
		b, _ := base64.StdEncoding.DecodeString(p.(string))
		var h tlog.Hash
		copy(h[:], b)
		hashes = append(hashes, h)
	}
	leaf, _ := base64.StdEncoding.DecodeString(body["leaf_hash"].(string))
	var lh tlog.Hash
	copy(lh[:], leaf)
	if err := tlog.CheckRecord(hashes, cp.TreeSize, cp.RootHash, *rc.LeafIndex, lh); err != nil {
		t.Fatalf("the served inclusion proof does not verify: %v", err)
	}
	if want := checkpoint.EncodeHashes(hashes); len(want) != len(body["inclusion_proof"].([]any)) {
		t.Fatal("proof length")
	}

	// An unknown id: 404. So is another consumer's record.
	statusIs(t, e.do(http.MethodGet, "/v1/usage/0190c0de-0000-7000-8000-000000000000/proof", ""), http.StatusNotFound)
	var otherKey string
	err = e.db.Write(ctx, func(tx *storage.Tx) error {
		p, err := e.ir.Principals.Create(tx, "consumer", nil, nil)
		if err != nil {
			return err
		}
		c, err := e.ir.Consumers.Create(tx, p.ID, "application", nil, nil)
		if err != nil {
			return err
		}
		g, err := e.gr.Grants.Create(tx, c.ID)
		if err != nil {
			return err
		}
		if _, err := e.gr.Versions.Create(tx, g.ID, grantsNewVersion{}); err != nil {
			return err
		}
		is, err := e.au.Issue(tx, auth.IssueParams{ConsumerID: c.ID})
		otherKey = "Bearer " + is.Key.Reveal()
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	statusIs(t, e.doAuth(otherKey, http.MethodGet, "/v1/usage/"+rc.UsageID+"/proof", ""), http.StatusNotFound)
}
