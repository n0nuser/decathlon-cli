package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "nested", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenCreatesPrivateDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if fi, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Fatalf("database mode %v", fi.Mode().Perm())
	}
	if fi, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode %v", fi.Mode().Perm())
	}
}

func TestWatchLifecycle(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	w := Watch{Name: "tent", Kind: KindProduct, Target: json.RawMessage(`{"ref":"1/2"}`), Interval: Duration(30 * time.Minute)}
	if err := st.AddWatch(ctx, &w); err != nil {
		t.Fatal(err)
	}
	if w.ID == 0 || w.CreatedAt.IsZero() {
		t.Fatalf("insert did not fill the watch: %+v", w)
	}
	if err := st.AddWatch(ctx, &Watch{Name: "tent", Kind: KindSearch, Interval: Duration(time.Hour)}); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
	ws, err := st.Watches(ctx)
	if err != nil || len(ws) != 1 || ws[0].Name != "tent" || ws[0].Kind != KindProduct {
		t.Fatalf("%+v %v", ws, err)
	}
	got, err := st.Watch(ctx, "tent")
	if err != nil || got.ID != w.ID || got.Interval != Duration(30*time.Minute) {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := st.Watch(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing watch: %v", err)
	}
	if err := st.RemoveWatch(ctx, "tent"); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveWatch(ctx, "tent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second remove: %v", err)
	}
}

func TestCommitCheckAppliesEverythingTogether(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	w := Watch{Name: "tent", Kind: KindProduct, Interval: Duration(time.Hour), Target: json.RawMessage(`{}`)}
	if err := st.AddWatch(ctx, &w); err != nil {
		t.Fatal(err)
	}
	if seen, err := st.SeenFor(ctx, w.ID); err != nil || len(seen) != 0 {
		t.Fatalf("new watch has state: %+v %v", seen, err)
	}
	at := time.Now().UTC()
	events := []Event{
		{Type: ProductPriceChanged, Watch: "tent", At: at.Add(-2 * time.Hour), Product: json.RawMessage(`{"id":"1"}`)},
		{Type: ProductPriceChanged, Watch: "tent", At: at, Product: json.RawMessage(`{"id":"1"}`)},
	}
	upserts := []Seen{{Key: "m:1", Snapshot: json.RawMessage(`{"price":10}`)}, {Key: "s:1", Snapshot: json.RawMessage(`{"online":true}`)}}
	if err := st.CommitCheck(ctx, w, upserts, nil, events); err != nil {
		t.Fatal(err)
	}
	seen, err := st.SeenFor(ctx, w.ID)
	if err != nil || len(seen) != 2 || string(seen["m:1"].Snapshot) != `{"price":10}` {
		t.Fatalf("seen %+v %v", seen, err)
	}
	after, err := st.Watch(ctx, "tent")
	if err != nil || !after.Baselined || after.LastCheckAt.IsZero() {
		t.Fatalf("check not recorded: %+v %v", after, err)
	}

	// A second check replaces one key, drops another and records its event.
	upserts = []Seen{{Key: "m:1", Snapshot: json.RawMessage(`{"price":9}`)}}
	if err := st.CommitCheck(ctx, w, upserts, []string{"s:1"}, []Event{{Type: ProductPriceChanged, Watch: "tent", At: at.Add(time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	seen, _ = st.SeenFor(ctx, w.ID)
	if len(seen) != 1 || string(seen["m:1"].Snapshot) != `{"price":9}` {
		t.Fatalf("upsert/remove: %+v", seen)
	}
}

func TestEventsFiltersAndOrders(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	w := Watch{Name: "tent", Kind: KindSearch, Interval: Duration(time.Hour), Target: json.RawMessage(`{}`)}
	if err := st.AddWatch(ctx, &w); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	old := Event{Type: ProductNew, Watch: "tent", At: now.Add(-2 * time.Hour), Product: json.RawMessage(`{"id":"old"}`)}
	fresh := Event{Type: ProductNew, Watch: "tent", At: now, Product: json.RawMessage(`{"id":"fresh"}`)}
	other := Event{Type: ProductNew, Watch: "other", At: now, Product: json.RawMessage(`{"id":"other"}`)}
	if err := st.CommitCheck(ctx, w, nil, nil, []Event{old, fresh, other}); err != nil {
		t.Fatal(err)
	}
	all, err := st.Events(ctx, "", time.Time{}, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("all %+v %v", all, err)
	}
	if !all[0].At.Equal(now) || !all[2].At.Equal(now.Add(-2*time.Hour)) {
		t.Fatalf("order %v", []time.Time{all[0].At, all[1].At, all[2].At})
	}
	recent, err := st.Events(ctx, "", now.Add(-time.Hour), 0)
	if err != nil || len(recent) != 2 {
		t.Fatalf("since: %+v %v", recent, err)
	}
	for _, e := range recent {
		if e.At.Before(now.Add(-time.Hour)) {
			t.Fatalf("old event leaked in: %+v", e)
		}
	}
	one, err := st.Events(ctx, "tent", now.Add(-time.Hour), 1)
	if err != nil || len(one) != 1 || one[0].Watch != "tent" {
		t.Fatalf("limit/watch: %+v %v", one, err)
	}
	if e := one[0]; e.Type != ProductNew || string(e.Product) != `{"id":"fresh"}` || e.ID == 0 {
		t.Fatalf("event body: %+v", e)
	}
}

func TestRemoveWatchDropsItsEvents(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	w := Watch{Name: "tent", Kind: KindSearch, Interval: Duration(time.Hour), Target: json.RawMessage(`{}`)}
	if err := st.AddWatch(ctx, &w); err != nil {
		t.Fatal(err)
	}
	if err := st.CommitCheck(ctx, w, nil, nil, []Event{{Type: ProductNew, Watch: "tent", At: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	if err := st.RemoveWatch(ctx, "tent"); err != nil {
		t.Fatal(err)
	}
	if evs, err := st.Events(ctx, "tent", time.Time{}, 0); err != nil || len(evs) != 0 {
		t.Fatalf("events survived: %+v %v", evs, err)
	}
	// Removing the watch cascades its seen rows.
	var n int
	if err := st.db.QueryRow(`SELECT count(*) FROM seen`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("seen rows %d %v", n, err)
	}
}

func TestDurationJSON(t *testing.T) {
	raw, err := json.Marshal(Duration(90 * time.Minute))
	if err != nil || string(raw) != `"1h30m0s"` {
		t.Fatalf("%s %v", raw, err)
	}
	var d Duration
	if err := json.Unmarshal([]byte(`"30m"`), &d); err != nil || time.Duration(d) != 30*time.Minute {
		t.Fatalf("%s %v", d, err)
	}
	if err := json.Unmarshal([]byte(`1`), &d); err == nil {
		t.Fatal("a non-string duration was accepted")
	}
}
