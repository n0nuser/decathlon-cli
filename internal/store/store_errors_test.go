package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenFailsWhenTheParentIsAFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(file, "state.db")); err == nil {
		t.Fatal("Open under a regular file succeeded")
	}
}

func TestClosedStoreReportsErrors(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	w := Watch{Name: "tent", Kind: KindSearch, Interval: Duration(time.Hour), Target: json.RawMessage(`{}`)}
	if err := st.AddWatch(ctx, &w); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if _, err := st.Watches(ctx); err == nil {
		t.Error("Watches on a closed store")
	}
	if _, err := st.Watch(ctx, "tent"); err == nil {
		t.Error("Watch on a closed store")
	}
	if _, err := st.SeenFor(ctx, w.ID); err == nil {
		t.Error("SeenFor on a closed store")
	}
	if err := st.CommitCheck(ctx, w, nil, nil, nil); err == nil {
		t.Error("CommitCheck on a closed store")
	}
	if _, err := st.Events(ctx, "", time.Time{}, 0); err == nil {
		t.Error("Events on a closed store")
	}
	if err := st.RemoveWatch(ctx, "tent"); err == nil {
		t.Error("RemoveWatch on a closed store")
	}
}
