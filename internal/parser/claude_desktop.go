package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type claudeDesktopMetadata struct {
	SessionID           string                      `json:"sessionId"`
	CliSessionID        string                      `json:"cliSessionId"`
	Cwd                 string                      `json:"cwd"`
	CreatedAt           int64                       `json:"createdAt"`
	LastActivityAt      int64                       `json:"lastActivityAt"`
	Model               string                      `json:"model"`
	Title               string                      `json:"title"`
	InitialMessage      string                      `json:"initialMessage"`
	UserSelectedFolders []string                    `json:"userSelectedFolders"`
	FsDetectedFiles     []claudeDesktopDetectedFile `json:"fsDetectedFiles"`
}

func (m claudeDesktopMetadata) isCoworkLike() bool {
	if isClaudeDesktopVirtualCwd(m.Cwd) {
		return true
	}
	if len(m.UserSelectedFolders) > 0 ||
		len(m.FsDetectedFiles) > 0 {
		return true
	}
	return false
}

func isClaudeDesktopVirtualCwd(cwd string) bool {
	return strings.HasPrefix(
		filepath.ToSlash(strings.TrimSpace(cwd)),
		"/sessions/",
	)
}

type claudeDesktopDetectedFile struct {
	HostPath string `json:"hostPath"`
	FileName string `json:"fileName"`
}

type claudeDesktopEntry struct {
	entryType string
	line      string
	timestamp time.Time
	uuid      string
}

func newClaudeDesktopEntry(
	entryType string,
	line string,
	timestamp time.Time,
) claudeDesktopEntry {
	return claudeDesktopEntry{
		entryType: entryType,
		line:      line,
		timestamp: timestamp,
		uuid:      gjson.Get(line, "uuid").Str,
	}
}

func parseClaudeDesktopEntry(
	line string, timestamp time.Time,
) (claudeDesktopEntry, bool) {
	entryType := gjson.Get(line, "type").Str
	if entryType != "user" && entryType != "assistant" {
		return claudeDesktopEntry{}, false
	}
	if shouldSkipClaudeDesktopReplay(line) {
		return claudeDesktopEntry{}, false
	}
	return newClaudeDesktopEntry(entryType, line, timestamp), true
}

type ClaudeDesktopMetadataState struct {
	SessionID     string
	MetadataPath  string
	MetadataMtime int64
}

func ParseClaudeDesktopSession(
	path, project, machine string, agent AgentType,
) (*ParsedSession, []ParsedMessage, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("stat %s: %w", path, err)
	}

	meta, err := readClaudeDesktopMetadata(path)
	if err != nil {
		return nil, nil, err
	}
	rawID := claudeDesktopRawID(path)
	if meta.SessionID != "" {
		rawID = meta.SessionID
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var (
		entries        []claudeDesktopEntry
		sourceVersion  string
		malformedLines int
		lastLine       string
		globalStart    time.Time
		globalEnd      time.Time
	)

	lr := newLineReader(f, maxLineSize)
	for {
		line, ok := lr.next()
		if !ok {
			break
		}
		lastLine = line
		if !gjson.Valid(line) {
			malformedLines++
			continue
		}

		ts := extractClaudeDesktopTimestamp(line)
		globalStart = earlierTime(globalStart, ts)
		globalEnd = laterTime(globalEnd, ts)

		if sourceVersion == "" {
			if v := gjson.Get(line, "claude_code_version").Str; v != "" {
				sourceVersion = v
			}
		}

		entry, ok := parseClaudeDesktopEntry(line, ts)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}
	if err := lr.Err(); err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", path, err)
	}

	isTruncated := lastLine != "" &&
		strings.TrimSpace(lastLine) != "" &&
		!gjson.Valid(lastLine) &&
		!fileEndsWithNewline(f, info.Size())

	messages, startedAt, endedAt := extractClaudeDesktopMessages(
		entries, 0,
	)
	if startedAt.IsZero() {
		startedAt = globalStart
	}
	if endedAt.IsZero() {
		endedAt = globalEnd
	}

	if metaCreated := meta.createdAtTime(); !metaCreated.IsZero() {
		startedAt = earlierTime(startedAt, metaCreated)
	}
	if metaLast := meta.lastActivityAtTime(); !metaLast.IsZero() {
		endedAt = laterTime(endedAt, metaLast)
	}

	if p := meta.preferredProject(); p != "" {
		project = p
	} else {
		project = GetProjectName(project)
	}

	firstMessage := meta.InitialMessage
	for _, msg := range messages {
		if msg.Role == RoleUser && strings.TrimSpace(msg.Content) != "" {
			firstMessage = msg.Content
			break
		}
	}

	sess := &ParsedSession{
		ID:                  string(agent) + ":" + rawID,
		Project:             project,
		Machine:             machine,
		Agent:               agent,
		Cwd:                 meta.preferredCwd(),
		SourceSessionID:     meta.sourceSessionID(rawID),
		SourceVersion:       sourceVersion,
		MalformedLines:      malformedLines,
		IsTruncated:         isTruncated,
		FirstMessage:        firstMessage,
		DisplayName:         meta.Title,
		StartedAt:           startedAt,
		EndedAt:             endedAt,
		MessageCount:        len(messages),
		UserMessageCount:    claudeDesktopUserMessageCount(messages),
		SourceMetadataMtime: meta.metadataMtime(path),
		File: FileInfo{
			Path:  path,
			Size:  info.Size(),
			Mtime: info.ModTime().UnixNano(),
		},
	}
	if sourceMtime, err := ClaudeDesktopSourceMtimeFromInfo(path, info); err == nil {
		sess.File.Mtime = sourceMtime
	}
	accumulateMessageTokenUsage(sess, messages)
	return sess, messages, nil
}

func ParseClaudeDesktopSessionFrom(
	path string, offset int64, startOrdinal int,
) ([]ParsedMessage, time.Time, int64, error) {
	var (
		entries  []claudeDesktopEntry
		latestTS time.Time
		consumed int64
	)

	var err error
	consumed, err = readJSONLFrom(path, offset, func(line string) {
		ts := extractClaudeDesktopTimestamp(line)
		latestTS = laterTime(latestTS, ts)

		entry, ok := parseClaudeDesktopEntry(line, ts)
		if !ok {
			return
		}
		entries = append(entries, entry)
	})
	if err != nil {
		return nil, time.Time{}, consumed, fmt.Errorf(
			"reading %s from %d: %w", path, offset, err,
		)
	}

	messages, _, endedAt := extractClaudeDesktopMessages(
		entries, startOrdinal,
	)
	if endedAt.IsZero() {
		endedAt = latestTS
	}
	return messages, endedAt, consumed, nil
}

func extractClaudeDesktopMessages(
	entries []claudeDesktopEntry, startOrdinal int,
) ([]ParsedMessage, time.Time, time.Time) {
	var (
		messages  []ParsedMessage
		startedAt time.Time
		endedAt   time.Time
		ordinal   = startOrdinal
	)

	for _, entry := range entries {
		startedAt = earlierTime(startedAt, entry.timestamp)
		endedAt = laterTime(endedAt, entry.timestamp)

		content := gjson.Get(entry.line, "message.content")
		text, hasThinking, hasToolUse, tcs, trs := ExtractTextContent(
			content,
		)

		if entry.entryType == "user" {
			if cmdText, ok := extractCommandText(text); ok {
				text = cmdText
			} else if isCommandEnvelope(text) {
				continue
			}
		}

		if strings.TrimSpace(text) == "" && len(trs) == 0 {
			continue
		}
		if entry.entryType == "user" &&
			isClaudeSystemMessage(text) {
			continue
		}

		msg := ParsedMessage{
			Ordinal:            ordinal,
			Role:               RoleType(entry.entryType),
			Content:            text,
			Timestamp:          entry.timestamp,
			HasThinking:        hasThinking,
			HasToolUse:         hasToolUse,
			ContentLength:      len(text),
			ToolCalls:          tcs,
			ToolResults:        trs,
			SourceType:         entry.entryType,
			SourceUUID:         entry.uuid,
			tokenPresenceKnown: entry.entryType == "assistant",
		}
		if entry.entryType == "assistant" {
			extractClaudeTokenFields(&msg, entry.line)
		}

		messages = append(messages, msg)
		ordinal++
	}

	return messages, startedAt, endedAt
}

func claudeDesktopUserMessageCount(messages []ParsedMessage) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == RoleUser {
			count++
		}
	}
	return count
}

func extractClaudeDesktopTimestamp(line string) time.Time {
	for _, path := range []string{
		"_audit_timestamp",
		"timestamp",
		"snapshot.timestamp",
	} {
		tsStr := gjson.Get(line, path).Str
		ts := parseTimestamp(tsStr)
		if !ts.IsZero() {
			return ts
		}
	}
	return time.Time{}
}

func readClaudeDesktopMetadata(
	auditPath string,
) (claudeDesktopMetadata, error) {
	metaPath := claudeDesktopMetadataPath(auditPath)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return claudeDesktopMetadata{}, fmt.Errorf(
			"read %s: %w", metaPath, err,
		)
	}

	var meta claudeDesktopMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return claudeDesktopMetadata{}, fmt.Errorf(
			"parse %s: %w", metaPath, err,
		)
	}
	return meta, nil
}

func ClaudeDesktopMetadataPath(auditPath string) string {
	return claudeDesktopMetadataPath(auditPath)
}

func ClaudeDesktopSourceMtime(auditPath string) (int64, error) {
	info, err := os.Stat(auditPath)
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", auditPath, err)
	}
	return ClaudeDesktopSourceMtimeFromInfo(auditPath, info)
}

func ClaudeDesktopSourceMtimeFromInfo(
	auditPath string, auditInfo os.FileInfo,
) (int64, error) {
	metaInfo, err := os.Stat(claudeDesktopMetadataPath(auditPath))
	if err != nil {
		return 0, fmt.Errorf(
			"stat %s: %w", claudeDesktopMetadataPath(auditPath), err,
		)
	}
	auditMtime := auditInfo.ModTime().UnixNano()
	metaMtime := metaInfo.ModTime().UnixNano()
	if metaMtime > auditMtime {
		return metaMtime, nil
	}
	return auditMtime, nil
}

func claudeDesktopMetadataPath(auditPath string) string {
	sessionDir := filepath.Dir(auditPath)
	return filepath.Join(
		filepath.Dir(sessionDir),
		filepath.Base(sessionDir)+".json",
	)
}

func ReadClaudeDesktopMetadataState(
	auditPath string,
) (ClaudeDesktopMetadataState, error) {
	metaPath := claudeDesktopMetadataPath(auditPath)
	info, err := os.Stat(metaPath)
	if err != nil {
		return ClaudeDesktopMetadataState{}, fmt.Errorf(
			"stat %s: %w", metaPath, err,
		)
	}
	meta, err := readClaudeDesktopMetadata(auditPath)
	if err != nil {
		return ClaudeDesktopMetadataState{}, err
	}
	rawID := claudeDesktopRawID(auditPath)
	if meta.SessionID != "" {
		rawID = meta.SessionID
	}
	return ClaudeDesktopMetadataState{
		SessionID:     rawID,
		MetadataPath:  metaPath,
		MetadataMtime: info.ModTime().UnixNano(),
	}, nil
}

func IsClaudeCoworkAuditPath(auditPath string) (bool, error) {
	meta, err := readClaudeDesktopMetadata(auditPath)
	if err != nil {
		return false, err
	}
	return meta.isCoworkLike(), nil
}

func claudeDesktopRawID(auditPath string) string {
	return filepath.Base(filepath.Dir(auditPath))
}

func (m claudeDesktopMetadata) preferredProject() string {
	if hostPath := m.preferredHostPath(); hostPath != "" {
		return ExtractProjectFromCwd(hostPath)
	}
	return ""
}

func (m claudeDesktopMetadata) preferredCwd() string {
	if hostPath := m.preferredHostPath(); hostPath != "" {
		return hostPath
	}
	return m.Cwd
}

func (m claudeDesktopMetadata) preferredHostPath() string {
	for _, dir := range m.UserSelectedFolders {
		if strings.TrimSpace(dir) != "" {
			return dir
		}
	}
	for _, file := range m.FsDetectedFiles {
		hostPath := strings.TrimSpace(file.HostPath)
		if hostPath == "" {
			continue
		}
		if file.FileName != "" &&
			filepath.Base(hostPath) == file.FileName {
			return filepath.Dir(hostPath)
		}
		if info, err := os.Stat(hostPath); err == nil && !info.IsDir() {
			return filepath.Dir(hostPath)
		}
		return hostPath
	}
	return ""
}

func (m claudeDesktopMetadata) sourceSessionID(
	rawID string,
) string {
	switch {
	case m.CliSessionID != "":
		return m.CliSessionID
	case m.SessionID != "":
		return m.SessionID
	default:
		return rawID
	}
}

func (m claudeDesktopMetadata) metadataMtime(
	auditPath string,
) int64 {
	info, err := os.Stat(claudeDesktopMetadataPath(auditPath))
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

func (m claudeDesktopMetadata) createdAtTime() time.Time {
	return claudeDesktopUnixMilliTime(m.CreatedAt)
}

func (m claudeDesktopMetadata) lastActivityAtTime() time.Time {
	return claudeDesktopUnixMilliTime(m.LastActivityAt)
}

func claudeDesktopUnixMilliTime(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func shouldSkipClaudeDesktopReplay(line string) bool {
	return gjson.Get(line, "isReplay").Bool()
}
