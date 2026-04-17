package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClaudeDesktopSession(t *testing.T) {
	t.Parallel()

	path := writeClaudeDesktopTestSession(
		t,
		"local_11111111-2222-3333-4444-555555555555",
		map[string]any{
			"sessionId":      "local_11111111-2222-3333-4444-555555555555",
			"cliSessionId":   "cli-123",
			"cwd":            "/Users/alice/.claude/projects/-Users-alice-code-my-app",
			"createdAt":      int64(1704103200000),
			"lastActivityAt": int64(1704103265000),
			"title":          "Desktop test session",
			"initialMessage": "metadata hello",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
			"fsDetectedFiles": []map[string]any{
				{"hostPath": "/Users/alice/code/my-app/main.go"},
			},
		},
		[]map[string]any{
			{
				"type":                "system",
				"subtype":             "init",
				"uuid":                "sys-1",
				"session_id":          "cli-123",
				"claude_code_version": "2.1.63",
				"_audit_timestamp":    tsEarly,
				"message":             map[string]any{},
			},
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
				"requestId":        "req-1",
				"_audit_timestamp": tsEarlyS1,
				"message": map[string]any{
					"id":    "msg-1",
					"model": "claude-sonnet-4",
					"usage": map[string]any{
						"input_tokens":                123,
						"cache_creation_input_tokens": 4,
						"output_tokens":               45,
					},
					"content": []map[string]any{
						{"type": "text", "text": "I checked the file."},
						{
							"type":  "tool_use",
							"id":    "toolu_1",
							"name":  "Read",
							"input": map[string]any{"file_path": "main.go"},
						},
					},
				},
			},
			{
				"type":             "user",
				"uuid":             "u-2",
				"_audit_timestamp": tsEarlyS5,
				"message": map[string]any{
					"content": []map[string]any{
						{
							"type":        "tool_result",
							"tool_use_id": "toolu_1",
							"content":     "package main",
						},
					},
				},
			},
			{
				"type":             "assistant",
				"uuid":             "a-2",
				"_audit_timestamp": tsLate,
				"message": map[string]any{
					"content": []map[string]any{
						{
							"type": "text",
							"text": "The file uses package main.",
						},
					},
				},
			},
		},
	)

	sess, msgs, err := ParseClaudeDesktopSession(
		path, "fallback_project", "local", AgentClaudeCowork,
	)
	require.NoError(t, err)
	require.NotNil(t, sess)
	require.Len(t, msgs, 4)

	assertSessionMeta(
		t, sess,
		"claude-cowork:local_11111111-2222-3333-4444-555555555555",
		"my_app",
		AgentClaudeCowork,
	)
	assert.Equal(t, "Desktop test session", sess.DisplayName)
	assert.Equal(t, "Investigate main.go", sess.FirstMessage)
	assert.Equal(t, "/Users/alice/code/my-app", sess.Cwd)
	assert.Equal(t, "cli-123", sess.SourceSessionID)
	assert.Equal(t, "2.1.63", sess.SourceVersion)
	assert.Equal(t, 4, sess.MessageCount)
	assert.Equal(t, 2, sess.UserMessageCount)
	assert.Equal(t, 45, sess.TotalOutputTokens)
	assert.Equal(t, 127, sess.PeakContextTokens)
	assert.Equal(t, path, sess.File.Path)
	assertTimestamp(t, sess.StartedAt, parseTimestamp(tsEarly))
	assertTimestamp(t, sess.EndedAt, parseTimestamp(tsLateS5))

	assertMessage(t, msgs[0], RoleUser, "Investigate main.go")
	assert.Equal(t, 0, msgs[0].Ordinal)

	assertMessage(t, msgs[1], RoleAssistant, "I checked the file.")
	assert.Equal(t, 1, msgs[1].Ordinal)
	require.Len(t, msgs[1].ToolCalls, 1)
	assert.Equal(t, "toolu_1", msgs[1].ToolCalls[0].ToolUseID)
	assert.Equal(t, "Read", msgs[1].ToolCalls[0].ToolName)
	assert.Equal(t, "Read", msgs[1].ToolCalls[0].Category)
	assert.Equal(
		t,
		`{"file_path":"main.go"}`,
		msgs[1].ToolCalls[0].InputJSON,
	)
	assert.Equal(t, "claude-sonnet-4", msgs[1].Model)
	assert.Equal(t, 127, msgs[1].ContextTokens)
	assert.Equal(t, 45, msgs[1].OutputTokens)
	assert.True(t, msgs[1].HasContextTokens)
	assert.True(t, msgs[1].HasOutputTokens)
	assert.Equal(t, "msg-1", msgs[1].ClaudeMessageID)
	assert.Equal(t, "req-1", msgs[1].ClaudeRequestID)

	assertMessage(t, msgs[2], RoleUser, "")
	assert.Equal(t, 2, msgs[2].Ordinal)
	require.Len(t, msgs[2].ToolResults, 1)
	assert.Equal(t, "toolu_1", msgs[2].ToolResults[0].ToolUseID)
	assert.Equal(
		t, "package main",
		DecodeContent(msgs[2].ToolResults[0].ContentRaw),
	)

	assertMessage(
		t, msgs[3], RoleAssistant, "The file uses package main.",
	)
	assert.Equal(t, 3, msgs[3].Ordinal)
}

func TestParseClaudeDesktopSessionFrom_Incremental(t *testing.T) {
	t.Parallel()

	path := writeClaudeDesktopTestSession(
		t,
		"local_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		map[string]any{
			"sessionId": "local_aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
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

	info, err := os.Stat(path)
	require.NoError(t, err)
	offset := info.Size()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(
		mustMarshalTestJSON(map[string]any{
			"type":             "system",
			"_audit_timestamp": tsEarlyS1,
			"message":          map[string]any{},
		}) + "\n",
	)
	require.NoError(t, err)
	_, err = f.WriteString(
		mustMarshalTestJSON(map[string]any{
			"type":             "assistant",
			"uuid":             "a-1",
			"_audit_timestamp": tsLate,
			"message": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "hi there"},
				},
			},
		}) + "\n",
	)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	newMsgs, endedAt, _, err := ParseClaudeDesktopSessionFrom(
		path, offset, 1,
	)
	require.NoError(t, err)
	require.Len(t, newMsgs, 1)
	assert.Equal(t, 1, newMsgs[0].Ordinal)
	assert.Equal(t, RoleAssistant, newMsgs[0].Role)
	assert.Contains(t, newMsgs[0].Content, "hi there")
	assertTimestamp(t, endedAt, parseTimestamp(tsLate))
}

func TestParseClaudeCoworkSession_IgnoresReplayRows(t *testing.T) {
	t.Parallel()

	path := writeClaudeDesktopTestSession(
		t,
		"local_deadbeef-dead-beef-dead-beefdeadbeef",
		map[string]any{
			"sessionId": "local_deadbeef-dead-beef-dead-beefdeadbeef",
			"cwd":       "/sessions/quirky-pensive-allen",
			"userSelectedFolders": []string{
				"/Users/alice/code/my-app",
			},
		},
		[]map[string]any{
			{
				"type":             "user",
				"uuid":             "u-1",
				"session_id":       "deadbeef-dead-beef-dead-beefdeadbeef",
				"_audit_timestamp": tsEarly,
				"message": map[string]any{
					"content": "say hey!",
				},
			},
			{
				"type":             "system",
				"subtype":          "init",
				"uuid":             "sys-1",
				"session_id":       "runtime-123",
				"_audit_timestamp": tsEarlyS1,
			},
			{
				"type":             "user",
				"uuid":             "u-1",
				"session_id":       "runtime-123",
				"isReplay":         true,
				"_audit_timestamp": tsEarlyS1,
				"message": map[string]any{
					"content": "say hey!",
				},
			},
			{
				"type":             "assistant",
				"uuid":             "a-1",
				"session_id":       "runtime-123",
				"_audit_timestamp": tsEarlyS5,
				"message": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": "hey!"},
					},
				},
			},
		},
	)

	sess, msgs, err := ParseClaudeDesktopSession(
		path, "", "local", AgentClaudeCowork,
	)
	require.NoError(t, err)
	require.NotNil(t, sess)
	require.Len(t, msgs, 2)

	assert.Equal(t, AgentClaudeCowork, sess.Agent)
	assert.Equal(t, 2, sess.MessageCount)
	assert.Equal(t, 1, sess.UserMessageCount)
	assert.Equal(t, "/Users/alice/code/my-app", sess.Cwd)
	assert.Equal(t, "say hey!", msgs[0].Content)
	assert.Equal(t, RoleUser, msgs[0].Role)
	assert.Equal(t, "hey!", msgs[1].Content)
	assert.Equal(t, RoleAssistant, msgs[1].Role)
}

func TestClaudeDesktopPreferredHostPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		meta claudeDesktopMetadata
		want string
	}{
		{
			name: "detected file uses parent directory",
			meta: claudeDesktopMetadata{
				FsDetectedFiles: []claudeDesktopDetectedFile{
					{
						HostPath: "/Users/alice/code/my-app/main.go",
						FileName: "main.go",
					},
				},
			},
			want: "/Users/alice/code/my-app",
		},
		{
			name: "dotted directory stays intact without filename hint",
			meta: claudeDesktopMetadata{
				FsDetectedFiles: []claudeDesktopDetectedFile{
					{
						HostPath: "/Users/alice/code/foo.bar",
					},
				},
			},
			want: "/Users/alice/code/foo.bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.meta.preferredHostPath())
		})
	}
}

func writeClaudeDesktopTestSession(
	t *testing.T,
	rawID string,
	metadata map[string]any,
	lines []map[string]any,
) string {
	t.Helper()

	dir := t.TempDir()
	sessionDir := filepath.Join(dir, rawID)
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))

	metaPath := filepath.Join(dir, rawID+".json")
	require.NoError(
		t,
		os.WriteFile(
			metaPath,
			[]byte(mustMarshalTestJSON(metadata)),
			0o644,
		),
	)

	var auditLines []string
	for _, line := range lines {
		auditLines = append(auditLines, mustMarshalTestJSON(line))
	}

	auditPath := filepath.Join(sessionDir, "audit.jsonl")
	require.NoError(
		t,
		os.WriteFile(
			auditPath,
			[]byte(joinJSONLines(auditLines)),
			0o644,
		),
	)
	return auditPath
}

func mustUnixMilli(ts string) int64 {
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return parsed.UnixMilli()
}

func mustMarshalTestJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func joinJSONLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	out := lines[0]
	for _, line := range lines[1:] {
		out += "\n" + line
	}
	return out + "\n"
}
