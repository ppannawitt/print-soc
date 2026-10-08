package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestHistoryPersistsAndRecoversInterruptedOperation(t *testing.T) {
	directory := t.TempDir()
	ctx := context.Background()
	first, err := OpenAt(directory)
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC)
	submitted := Job{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Username: "student", Host: "stu.comp.nus.edu.sg", FileName: "notes.pdf", PrinterID: "psc008", Queue: "psc008-sx", SubmittedAt: when}
	if err := first.AddPending(ctx, submitted); err != nil {
		t.Fatal(err)
	}
	if err := first.Finish(ctx, submitted.ID, "submitted", "psc008-sx-42", "accepted"); err != nil {
		t.Fatal(err)
	}
	interrupted := Job{ID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Username: "student", Host: "stu.comp.nus.edu.sg", FileName: "draft.pdf", PrinterID: "psc008", Queue: "psc008-sx", SubmittedAt: when.Add(time.Second)}
	if err := first.AddPending(ctx, interrupted); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := OpenAt(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	jobs, err := second.List(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(jobs))
	}
	if jobs[0].ID != interrupted.ID || jobs[0].State != "unknown" {
		t.Fatalf("interrupted operation was not recovered as unknown: %#v", jobs[0])
	}
	if jobs[1].ID != submitted.ID || jobs[1].State != "submitted" || jobs[1].SpoolerID != "psc008-sx-42" {
		t.Fatalf("successful submission did not persist: %#v", jobs[1])
	}
}

func TestUpgradePreservesOldHistoryAndStoresPrintSettings(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "history.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE jobs (id TEXT PRIMARY KEY,username TEXT NOT NULL,host TEXT NOT NULL,file_name TEXT NOT NULL,printer_id TEXT NOT NULL,queue TEXT NOT NULL,submitted_at TEXT NOT NULL,state TEXT NOT NULL,spooler_id TEXT NOT NULL DEFAULT '',message TEXT NOT NULL DEFAULT '')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO jobs VALUES ('old','student','stu','old.pdf','psc008','psc008','2026-10-01T00:00:00Z','submitted','','')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	history, err := OpenAt(directory)
	if err != nil {
		t.Fatal(err)
	}
	options := `{"Paper":"A4","Copies":2,"PagesPerSheet":4}`
	err = history.AddPending(context.Background(), Job{ID: "new", Username: "student", Host: "stu", FileName: "new.pdf", PrinterID: "psc008", Queue: "psc008", SubmittedAt: time.Now(), PrintSettings: options})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := history.List(context.Background(), 100)
	if err != nil || len(rows) != 2 {
		t.Fatalf("history migration failed: %v %v", rows, err)
	}
	if rows[0].PrintSettings != options || rows[1].ID != "old" || rows[1].PrintSettings != "" {
		t.Fatalf("settings/history corrupted: %#v", rows)
	}
	history.Close()
	again, err := OpenAt(directory)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
}
