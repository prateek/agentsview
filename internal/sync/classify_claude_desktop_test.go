package sync

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wesm/agentsview/internal/parser"
)

func TestClassifyOnePath_ClaudeDesktop(t *testing.T) {
	root := t.TempDir()
	rawID := "local_11111111-2222-3333-4444-555555555555"
	metaPath := filepath.Join(
		root, "acct-a", "org-a", rawID+".json",
	)
	auditPath := filepath.Join(
		root, "acct-a", "org-a", rawID, "audit.jsonl",
	)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(auditPath), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(
			metaPath,
			[]byte(`{"cwd":"/sessions/hello-world","initialMessage":"hi"}`),
			0o644,
		),
	)
	require.NoError(
		t, os.WriteFile(auditPath, []byte("{}"), 0o644),
	)

	eng := &Engine{
		agentDirs: map[parser.AgentType][]string{
			parser.AgentClaudeCowork: {root},
		},
	}
	geminiMap := make(map[string]map[string]string)

	got, ok := eng.classifyOnePath(metaPath, geminiMap)
	assert.True(t, ok)
	assert.Equal(t, parser.AgentClaudeCowork, got.Agent)
	assert.Equal(t, auditPath, got.Path)
	assert.Equal(t, "acct-a_org-a", got.Project)

	got, ok = eng.classifyOnePath(auditPath, geminiMap)
	assert.True(t, ok)
	assert.Equal(t, parser.AgentClaudeCowork, got.Agent)
	assert.Equal(t, auditPath, got.Path)
	assert.Equal(t, "acct-a_org-a", got.Project)

	missingMetaAudit := filepath.Join(
		root,
		"acct-a",
		"org-a",
		"local_missingmeta",
		"audit.jsonl",
	)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(missingMetaAudit), 0o755),
	)
	require.NoError(
		t, os.WriteFile(missingMetaAudit, []byte("{}"), 0o644),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(
				root,
				"acct-a",
				"org-a",
				"local_missingmeta.json.tmp",
			),
			[]byte("{}"),
			0o644,
		),
	)
	_, ok = eng.classifyOnePath(missingMetaAudit, geminiMap)
	assert.False(t, ok)

	missingMeta := filepath.Join(
		root,
		"acct-a",
		"org-a",
		"local_22222222-3333-4444-5555-666666666666.json",
	)
	require.NoError(
		t, os.WriteFile(missingMeta, []byte("{}"), 0o644),
	)
	_, ok = eng.classifyOnePath(missingMeta, geminiMap)
	assert.False(t, ok)

	_, ok = eng.classifyOnePath(
		filepath.Join(root, "acct-a", "org-a", "notes.txt"),
		geminiMap,
	)
	assert.False(t, ok)

	desktopMetaPath := filepath.Join(
		root, "acct-a", "org-a", "local_desktop-code.json",
	)
	desktopAuditPath := filepath.Join(
		root, "acct-a", "org-a", "local_desktop-code", "audit.jsonl",
	)
	require.NoError(
		t,
		os.MkdirAll(filepath.Dir(desktopAuditPath), 0o755),
	)
	require.NoError(
		t,
		os.WriteFile(
			desktopMetaPath,
			[]byte(`{"cwd":"/Users/prateek/dotfiles","initialMessage":"old code prompt"}`),
			0o644,
		),
	)
	require.NoError(
		t,
		os.WriteFile(desktopAuditPath, []byte("{}"), 0o644),
	)
	_, ok = eng.classifyOnePath(desktopMetaPath, geminiMap)
	assert.False(t, ok)
	_, ok = eng.classifyOnePath(desktopAuditPath, geminiMap)
	assert.False(t, ok)
}
