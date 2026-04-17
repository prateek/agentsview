package sync_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wesm/agentsview/internal/db"
	"github.com/wesm/agentsview/internal/dbtest"
	"github.com/wesm/agentsview/internal/parser"
	"github.com/wesm/agentsview/internal/sync"
)

func TestSyncClaudeCoworkIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_11111111-2222-3333-4444-555555555555"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId":      rawID,
			"cliSessionId":   "cli-123",
			"createdAt":      int64(1704103200000),
			"lastActivityAt": int64(1704103265000),
			"title":          "Cowork test session",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "user",
				"uuid":             "u-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "Investigate main.go",
				},
			},
			{
				"type":             "assistant",
				"uuid":             "a-1",
				"_audit_timestamp": "2024-01-01T10:01:00Z",
				"message": map[string]any{
					"usage": map[string]any{
						"input_tokens":  120,
						"output_tokens": 34,
					},
					"content": []map[string]any{
						{
							"type": "text",
							"text": "I checked the file.",
						},
					},
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{
			TotalSessions: 1,
			Synced:        1,
			Skipped:       0,
		},
	)

	sessionID := "claude-cowork:" + rawID
	assertSessionProject(t, database, sessionID, "my_app")
	assertSessionMessageCount(t, database, sessionID, 2)
	assertMessageRoles(t, database, sessionID, "user", "assistant")
	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.Equal(t, "claude-cowork", sess.Agent)
			require.Equal(t, "/Users/alice/code/my-app", sess.Cwd)
			require.Equal(t, "cli-123", sess.SourceSessionID)
			require.Equal(t, 34, sess.TotalOutputTokens)
			require.Equal(t, 120, sess.PeakContextTokens)
		},
	)

	got := engine.FindSourceFile(sessionID)
	want := filepath.Join(
		root, "acct-a", "org-a", rawID, "audit.jsonl",
	)
	require.Equal(t, want, got)

	require.NoError(t, engine.SyncSingleSession(sessionID))
	assertSessionMessageCount(t, database, sessionID, 2)
}

func TestSyncClaudeCoworkMetadataOnlySemanticChangeResyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "user",
				"uuid":             "u-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	sessionID := "claude-cowork:" + rawID
	before, err := database.GetSessionFull(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, before)
	require.NotNil(t, before.EndedAt)
	require.Equal(t, "2024-01-01T10:00:05Z", *before.EndedAt)

	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync("2024-01-01T10:01:00Z"),
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	after, err := database.GetSessionFull(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, after)
	require.NotNil(t, after.EndedAt)
	require.Equal(t, "2024-01-01T10:01:00Z", *after.EndedAt)
}

func TestSyncClaudeCoworkMetadataInitialMessageResyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_initial-aaaa-bbbb-cccc-dddddddddddd"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
			"initialMessage": "Old initial message",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "assistant",
				"uuid":             "a-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	sessionID := "claude-cowork:" + rawID
	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.NotNil(t, sess.FirstMessage)
			require.Equal(t, "Old initial message", *sess.FirstMessage)
		},
	)

	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
			"initialMessage": "New initial message",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.NotNil(t, sess.FirstMessage)
			require.Equal(t, "New initial message", *sess.FirstMessage)
		},
	)
}

func TestSyncClaudeCoworkMetadataVirtualCwdChangeResyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_cwd-aaaa-bbbb-cccc-dddddddddddd"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
			"cwd":            "/sessions/first-worktree",
			"title":          "Cowork cwd test",
		},
		[]map[string]any{
			{
				"type":             "assistant",
				"uuid":             "a-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	sessionID := "claude-cowork:" + rawID
	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.Equal(t, "/sessions/first-worktree", sess.Cwd)
		},
	)

	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
			"cwd":            "/sessions/second-worktree",
			"title":          "Cowork cwd test",
		},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.Equal(t, "/sessions/second-worktree", sess.Cwd)
		},
	)
}

func TestSyncClaudeCoworkMetadataTimestampCorrectionResyncs(
	t *testing.T,
) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_backward-cccc-dddd-eeee-ffffffffffff"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync("2024-01-01T10:01:00Z"),
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "user",
				"uuid":             "u-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	sessionID := "claude-cowork:" + rawID
	before, err := database.GetSessionFull(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, before)
	require.NotNil(t, before.EndedAt)
	require.Equal(t, "2024-01-01T10:01:00Z", *before.EndedAt)

	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	after, err := database.GetSessionFull(context.Background(), sessionID)
	require.NoError(t, err)
	require.NotNil(t, after)
	require.NotNil(t, after.EndedAt)
	require.Equal(t, "2024-01-01T10:00:05Z", *after.EndedAt)
}

func TestSyncClaudeCoworkMetadataOnlyQuickSyncResyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_quicksync-aaaa-bbbb-cccc-dddddddddddd"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
			"initialMessage": "Before quick sync",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "assistant",
				"uuid":             "a-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	auditPath := filepath.Join(root, "acct-a", "org-a", rawID, "audit.jsonl")
	metaPath := filepath.Join(root, "acct-a", "org-a", rawID+".json")
	oldTime := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(auditPath, oldTime, oldTime))
	require.NoError(t, os.Chtimes(metaPath, oldTime, oldTime))

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	sessionID := "claude-cowork:" + rawID
	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.NotNil(t, sess.FirstMessage)
			require.Equal(t, "Before quick sync", *sess.FirstMessage)
		},
	)

	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
			"initialMessage": "After quick sync",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
	)
	cutoff := time.Now().Add(-1 * time.Hour)
	stats := engine.SyncAllSince(
		context.Background(), cutoff, nil,
	)
	require.Equal(t, 1, stats.TotalSessions)
	require.Equal(t, 1, stats.Synced)
	require.Equal(t, 0, stats.Skipped)

	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.NotNil(t, sess.FirstMessage)
			require.Equal(t, "After quick sync", *sess.FirstMessage)
		},
	)
}

func TestSyncClaudeCoworkMetadataTitleResyncs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_title000-1111-2222-3333-444444444444"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId": rawID,
			"title":     "Before title change",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "assistant",
				"uuid":             "a-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	sessionID := "claude-cowork:" + rawID
	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.NotNil(t, sess.DisplayName)
			require.Equal(t, "Before title change", *sess.DisplayName)
		},
	)

	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId": rawID,
			"title":     "After title change",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.NotNil(t, sess.DisplayName)
			require.Equal(t, "After title change", *sess.DisplayName)
		},
	)
}

func TestSyncClaudeCoworkMetadataChangeWithAuditAppendForcesFullParse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_titleappend-1111-2222-3333-444444444444"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId": rawID,
			"title":     "Before title change",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "assistant",
				"uuid":             "a-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId": rawID,
			"title":     "After title change",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
	)
	auditPath := filepath.Join(
		root,
		"acct-a",
		"org-a",
		rawID,
		"audit.jsonl",
	)
	f, err := os.OpenFile(auditPath, os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString(
		mustMarshalSyncJSON(map[string]any{
			"type":             "assistant",
			"uuid":             "a-2",
			"_audit_timestamp": tsEarlyS5,
			"message": map[string]any{
				"content": "updated",
			},
		}) + "\n",
	)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	assertSessionState(
		t, database, "claude-cowork:"+rawID,
		func(sess *db.Session) {
			require.NotNil(t, sess.DisplayName)
			require.Equal(t, "After title change", *sess.DisplayName)
			require.Equal(t, 2, sess.MessageCount)
		},
	)
}

func TestSyncClaudeCoworkMetadataRewriteReparsesOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_bbbbbbbb-cccc-dddd-eeee-ffffffffffff"
	metadata := map[string]any{
		"sessionId":      rawID,
		"createdAt":      mustUnixMilliSync(tsEarly),
		"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
		"userSelectedFolders": []string{
			"/Users/alice/code/my-app",
		},
	}
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		metadata,
		[]map[string]any{
			{
				"type":             "user",
				"uuid":             "u-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId":      rawID,
			"createdAt":      mustUnixMilliSync(tsEarly),
			"lastActivityAt": mustUnixMilliSync(tsEarlyS5),
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
			"remoteMcpServersConfig": []map[string]any{
				{"uuid": "srv-1", "name": "Gmail"},
			},
		},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 0, Skipped: 1},
	)
}

func TestSyncClaudeCoworkUsesMetadataSessionIDForSkips(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_dir-1111-2222-3333-444444444444"
	metaID := "local_meta-1111-2222-3333-444444444444"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId": metaID,
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "assistant",
				"uuid":             "a-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)
	assertSessionState(
		t, database, "claude-cowork:"+metaID,
		func(sess *db.Session) {
			require.Equal(t, "claude-cowork:"+metaID, sess.ID)
		},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 0, Skipped: 1},
	)
}

func TestSyncClaudeCoworkRetriesAfterMetadataRepair(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_repair000-1111-2222-3333-444444444444"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId": rawID,
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "user",
				"uuid":             "u-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	metaPath := filepath.Join(root, "acct-a", "org-a", rawID+".json")
	require.NoError(
		t,
		os.WriteFile(metaPath, []byte("{"), 0o644),
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 0, Failed: 1},
	)

	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId": rawID,
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	assertSessionProject(
		t, database,
		"claude-cowork:"+rawID,
		"my_app",
	)
}

func TestSyncClaudeCoworkPreservesHostPathOnFullResync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := t.TempDir()
	rawID := "local_cccccccc-dddd-eeee-ffff-000000000000"
	writeClaudeDesktopSyncSession(
		t,
		root,
		"acct-a",
		"org-a",
		rawID,
		map[string]any{
			"sessionId": rawID,
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "user",
				"uuid":             "u-1",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "hello",
				},
			},
		},
	)

	database := dbtest.OpenTestDB(t)
	engine := sync.NewEngine(database, sync.EngineConfig{
		AgentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
		Machine: "local",
	})

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 1, Skipped: 0},
	)

	sessionID := "claude-cowork:" + rawID
	writeClaudeDesktopMetadata(
		t, root, "acct-a", "org-a", rawID,
		map[string]any{
			"sessionId": rawID,
			"cwd":       "/sessions/quirky-pensive-allen",
		},
	)

	err := database.Update(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			"UPDATE sessions SET file_mtime = NULL WHERE id = ?",
			sessionID,
		)
		return err
	})
	require.NoError(t, err)

	require.NoError(t, engine.SyncSingleSession(sessionID))

	assertSessionProject(t, database, sessionID, "my_app")
	assertSessionState(
		t, database, sessionID,
		func(sess *db.Session) {
			require.Equal(t, "/Users/alice/code/my-app", sess.Cwd)
		},
	)

	runSyncAndAssert(
		t, engine,
		sync.SyncStats{TotalSessions: 1, Synced: 0, Skipped: 1},
	)
}

func writeClaudeDesktopSyncSession(
	t *testing.T,
	root, accountID, orgID, rawID string,
	metadata map[string]any,
	lines []map[string]any,
) {
	t.Helper()

	metaDir := filepath.Join(root, accountID, orgID)
	sessionDir := filepath.Join(metaDir, rawID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))

	metaPath := filepath.Join(metaDir, rawID+".json")
	require.NoError(
		t,
		os.WriteFile(
			metaPath,
			[]byte(mustMarshalSyncJSON(metadata)),
			0o644,
		),
	)

	var audit string
	for i, line := range lines {
		if i > 0 {
			audit += "\n"
		}
		audit += mustMarshalSyncJSON(line)
	}
	audit += "\n"

	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(sessionDir, "audit.jsonl"),
			[]byte(audit),
			0o644,
		),
	)
}

func mustMarshalSyncJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func writeClaudeDesktopMetadata(
	t *testing.T,
	root, accountID, orgID, rawID string,
	metadata map[string]any,
) {
	t.Helper()

	metaPath := filepath.Join(
		root, accountID, orgID, rawID+".json",
	)
	require.NoError(
		t,
		os.WriteFile(metaPath, []byte(mustMarshalSyncJSON(metadata)), 0o644),
	)
}

func mustUnixMilliSync(ts string) int64 {
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return parsed.UnixMilli()
}
