package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wesm/agentsview/internal/db"
)

func strPtrEvents(s string) *string { return &s }

func int64PtrEvents(v int64) *int64 { return &v }

func TestStatMtime_NonexistentFile(t *testing.T) {
	t.Parallel()
	got := statMtime(
		filepath.Join(t.TempDir(), "no-such-file"),
	)
	if got != 0 {
		t.Errorf("statMtime(nonexistent) = %d, want 0", got)
	}
}

func TestStatMtime_ExistingFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(
		path, []byte("data"), 0o644,
	); err != nil {
		t.Fatal(err)
	}
	got := statMtime(path)
	if got == 0 {
		t.Error("statMtime(existing) = 0, want nonzero")
	}
}

func TestCheckDBForChanges_FileDisappears(t *testing.T) {
	t.Parallel()
	srv := testServer(t, 5*time.Second)

	path := filepath.Join(t.TempDir(), "gone.jsonl")
	var lastMtime int64 = 12345
	var mchanged time.Time
	var lastCount int
	var lastDBMtime int64

	changed := srv.checkDBForChanges(
		"test-session",
		&lastCount,
		&lastDBMtime,
		&path,
		&lastMtime,
		&mchanged,
	)
	if changed {
		t.Error("expected no change signal")
	}
	if path != "" {
		t.Errorf("sourcePath = %q, want empty", path)
	}
	if lastMtime != 0 {
		t.Errorf("lastMtime = %d, want 0", lastMtime)
	}
}

func TestCheckDBForChanges_TracksFileMtime(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "events.db")
	d, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	sessionID := "claude-cowork:local_123"
	if err := d.UpsertSession(db.Session{
		ID:               sessionID,
		Project:          "proj",
		Machine:          "local",
		Agent:            "claude-cowork",
		MessageCount:     1,
		UserMessageCount: 1,
		FilePath:         strPtrEvents(filepath.Join(t.TempDir(), "audit.jsonl")),
		FileSize:         int64PtrEvents(10),
		FileMtime:        int64PtrEvents(123),
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	srv := &Server{db: d}
	lastCount, lastDBVersion, ok := d.GetSessionVersion(sessionID)
	if !ok {
		t.Fatal("expected initial session version")
	}

	nextMtime := int64(456)
	if err := d.UpsertSession(db.Session{
		ID:               sessionID,
		Project:          "proj",
		Machine:          "local",
		Agent:            "claude-cowork",
		MessageCount:     1,
		UserMessageCount: 1,
		FilePath:         strPtrEvents(filepath.Join(t.TempDir(), "audit.jsonl")),
		FileSize:         int64PtrEvents(10),
		FileMtime:        &nextMtime,
	}); err != nil {
		t.Fatalf("UpsertSession update: %v", err)
	}

	sourcePath := ""
	var lastFileMtime int64
	var fileMtimeChangedAt time.Time
	changed := srv.checkDBForChanges(
		sessionID,
		&lastCount,
		&lastDBVersion,
		&sourcePath,
		&lastFileMtime,
		&fileMtimeChangedAt,
	)
	if !changed {
		t.Fatal("expected DB change after file_mtime bump")
	}
}
