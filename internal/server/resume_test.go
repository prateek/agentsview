package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wesm/agentsview/internal/db"
)

func canonicalTestDir(path string) string {
	if path == "" {
		return ""
	}
	if normalized := normalizeCursorDir(path); normalized != "" {
		return normalized
	}
	return filepath.Clean(path)
}

func assertSameDir(t *testing.T, label, got, want string) {
	t.Helper()
	got = canonicalTestDir(got)
	want = canonicalTestDir(want)
	if got != want {
		t.Errorf("%s = %q, want %q", label, got, want)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple uuid", "abc-123-def", "abc-123-def"},
		{"alphanumeric", "session42", "session42"},
		{"with colon", "a:b", "'a:b'"},
		{"with spaces", "has space", "'has space'"},
		{"with single quote", "it's", `'it'"'"'s'`},
		{"command injection attempt", "$(whoami)", "'$(whoami)'"},
		{"backtick injection", "`rm -rf /`", "'`rm -rf /`'"},
		{"semicolon", "id;rm -rf /", "'id;rm -rf /'"},
		{"pipe", "id|cat", "'id|cat'"},
		{"empty passthrough", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.in)
			if got != tt.want {
				t.Errorf(
					"shellQuote(%q) = %q, want %q",
					tt.in, got, tt.want,
				)
			}
		})
	}
}

func TestDetectTerminalLinux_NoTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux-only terminal detection")
	}
	// Empty PATH and no $TERMINAL — no terminal should be found.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TERMINAL", "")
	_, _, _, err := detectTerminalLinux("echo test")
	if err == nil {
		t.Error("expected error with empty PATH, got nil")
	}
}

func TestDetectTerminalLinux_EnvTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux-only terminal detection")
	}
	// Create a fake terminal binary on PATH.
	binDir := t.TempDir()
	fakeBin := filepath.Join(binDir, "myterm")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("TERMINAL", "myterm")

	bin, args, name, err := detectTerminalLinux("echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bin != fakeBin {
		t.Errorf("bin = %q, want %q", bin, fakeBin)
	}
	if name != "myterm" {
		t.Errorf("name = %q, want %q", name, "myterm")
	}
	if len(args) == 0 {
		t.Error("expected non-empty args")
	}
}

func TestDetectTerminalLinux_EnvTerminalWithArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux-only terminal detection")
	}
	binDir := t.TempDir()
	fakeBin := filepath.Join(binDir, "kitty")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("TERMINAL", "kitty --single-instance")

	bin, args, name, err := detectTerminalLinux("echo hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bin != fakeBin {
		t.Errorf("bin = %q, want %q", bin, fakeBin)
	}
	if name != "kitty" {
		t.Errorf("name = %q, want %q", name, "kitty")
	}
	// Should have --single-instance prepended before template args.
	if len(args) < 2 || args[0] != "--single-instance" {
		t.Errorf("args = %v, want --single-instance as first arg", args)
	}
}

func TestLaunchClaudeDesktopCode(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		cwd       string
		wantArg   string
	}{
		{
			name:      "simple session id",
			sessionID: "abc-123",
			cwd:       "",
			wantArg:   "claude://resume?session=abc-123",
		},
		{
			name:      "session id with cwd",
			sessionID: "abc-123",
			cwd:       "/Users/test/project",
			wantArg:   "claude://resume?session=abc-123&cwd=%2FUsers%2Ftest%2Fproject",
		},
		{
			name:      "cwd with spaces",
			sessionID: "sess-1",
			cwd:       "/Users/test/my project",
			wantArg:   "claude://resume?session=sess-1&cwd=%2FUsers%2Ftest%2Fmy+project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := launchClaudeDesktopCode(
				tt.sessionID, tt.cwd,
			)
			if runtime.GOOS != "darwin" {
				if cmd != nil {
					t.Fatalf("launchClaudeDesktopCode() = %v, want nil on %s", cmd, runtime.GOOS)
				}
				return
			}
			if cmd.Path == "" {
				t.Fatal("expected non-empty command path")
			}
			args := cmd.Args
			if len(args) == 0 {
				t.Fatalf("args = %v, want non-empty args", args)
			}
			if got := args[len(args)-1]; got != tt.wantArg {
				t.Errorf("url = %q, want %q", got, tt.wantArg)
			}
		})
	}
}

func TestLaunchClaudeDesktopLocalSession(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		wantArg   string
	}{
		{
			name:      "local session id",
			sessionID: "local_8d81b324-060a-460c-bd68-ab042f55fc12",
			wantArg:   "claude://claude.ai/local_sessions/local_8d81b324-060a-460c-bd68-ab042f55fc12",
		},
		{
			name:      "preserves session id verbatim",
			sessionID: "local_deadbeef-dead-beef-dead-beefdeadbeef",
			wantArg:   "claude://claude.ai/local_sessions/local_deadbeef-dead-beef-dead-beefdeadbeef",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := launchClaudeDesktopLocalSession(tt.sessionID)
			if runtime.GOOS != "darwin" {
				if cmd != nil {
					t.Fatalf("launchClaudeDesktopLocalSession() = %v, want nil on %s", cmd, runtime.GOOS)
				}
				return
			}
			if cmd == nil || cmd.Path == "" {
				t.Fatal("expected non-empty command path")
			}
			args := cmd.Args
			if len(args) == 0 {
				t.Fatalf("args = %v, want non-empty args", args)
			}
			if got := args[len(args)-1]; got != tt.wantArg {
				t.Errorf("url = %q, want %q", got, tt.wantArg)
			}
		})
	}
}

func TestDesktopURLCommand(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		url      string
		wantPath string
		wantArgs []string
	}{
		{
			name:     "darwin",
			goos:     "darwin",
			url:      "claude://resume?session=abc-123",
			wantPath: "open",
			wantArgs: []string{"open", "claude://resume?session=abc-123"},
		},
		{
			name: "unsupported OS",
			goos: "linux",
			url:  "claude://resume?session=abc-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := desktopURLCommand(tt.goos, tt.url)
			if tt.wantPath == "" {
				if cmd != nil {
					t.Fatalf("desktopURLCommand() = %v, want nil", cmd)
				}
				return
			}
			if cmd == nil {
				t.Fatal("expected command, got nil")
			}
			if got := filepath.Base(cmd.Path); got != tt.wantPath {
				t.Fatalf("path = %q, want %q", got, tt.wantPath)
			}
			if !reflect.DeepEqual(cmd.Args, tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", cmd.Args, tt.wantArgs)
			}
		})
	}
}

func TestLaunchClaudeDesktopAndRespondFallsBackOnLaunchError(t *testing.T) {
	t.Cleanup(func() {
		runDesktopURLProcess = func(proc *exec.Cmd) error {
			return proc.Run()
		}
	})

	runDesktopURLProcess = func(proc *exec.Cmd) error {
		return exec.ErrNotFound
	}

	w := httptest.NewRecorder()
	launchClaudeDesktopAndRespond(
		w,
		"claude-cowork",
		"local_123",
		"",
		"Claude Desktop",
		"",
	)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp resumeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Launched {
		t.Fatalf("launched = true, want false")
	}
	if resp.Error != "desktop_launch_failed" {
		t.Fatalf("error = %q, want %q", resp.Error, "desktop_launch_failed")
	}
}

func TestReadSessionCwd_LargeLine(t *testing.T) {
	// Verify that readSessionCwd handles lines larger than the
	// old 2MB scanner limit without losing the cwd field.
	dir := t.TempDir()
	cwdDir := filepath.Join(dir, "project")
	if err := os.Mkdir(cwdDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cwdJSON, _ := json.Marshal(cwdDir)
	// Build a 3MB padding string to exceed the old scanner limit.
	padding := strings.Repeat("x", 3*1024*1024)
	line := `{"cwd":` + string(cwdJSON) +
		`,"big":"` + padding + `"}` + "\n"

	sessionFile := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readSessionCwd(sessionFile)
	if got != cwdDir {
		t.Errorf("readSessionCwd() = %q, want %q", got, cwdDir)
	}
}

func TestSupportsDesktopURLLaunch(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		lookup  func(string) (string, error)
		want    bool
		wantBin string
	}{
		{
			name: "darwin uses open",
			goos: "darwin",
			lookup: func(bin string) (string, error) {
				if bin == "open" {
					return "/usr/bin/open", nil
				}
				return "", exec.ErrNotFound
			},
			want:    true,
			wantBin: "open",
		},
		{
			name: "linux returns false",
			goos: "linux",
			lookup: func(bin string) (string, error) {
				return "", nil
			},
			want: false,
		},
		{
			name: "unsupported OS returns false",
			goos: "plan9",
			lookup: func(bin string) (string, error) {
				return "", nil
			},
			want: false,
		},
		{
			name: "missing launcher returns false",
			goos: "linux",
			lookup: func(bin string) (string, error) {
				return "", exec.ErrNotFound
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, bin := supportsDesktopURLLaunch(tt.goos, tt.lookup)
			if got != tt.want {
				t.Fatalf("supportsDesktopURLLaunch() = %v, want %v", got, tt.want)
			}
			if bin != tt.wantBin {
				t.Fatalf("supportsDesktopURLLaunch() bin = %q, want %q", bin, tt.wantBin)
			}
		})
	}
}

func TestDetectClaudeDesktopOpener(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		statErr error
		lookup  func(string) (string, error)
		want    bool
		wantBin string
	}{
		{
			name:    "darwin app bundle present",
			goos:    "darwin",
			statErr: nil,
			lookup: func(bin string) (string, error) {
				if bin == "open" {
					return "/usr/bin/open", nil
				}
				return "", exec.ErrNotFound
			},
			want:    true,
			wantBin: "/Applications/Claude.app",
		},
		{
			name:    "darwin missing app bundle",
			goos:    "darwin",
			statErr: os.ErrNotExist,
			lookup: func(bin string) (string, error) {
				return "/usr/bin/open", nil
			},
			want: false,
		},
		{
			name: "linux does not advertise opener row",
			goos: "linux",
			lookup: func(bin string) (string, error) {
				if bin == "xdg-open" {
					return "/usr/bin/xdg-open", nil
				}
				return "", exec.ErrNotFound
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := detectClaudeDesktopOpener(
				tt.goos,
				func(path string) (os.FileInfo, error) {
					if tt.statErr != nil {
						return nil, tt.statErr
					}
					return fakeDirInfo{}, nil
				},
				tt.lookup,
			)
			if ok != tt.want {
				t.Fatalf("ok = %v, want %v", ok, tt.want)
			}
			if got.Bin != tt.wantBin {
				t.Fatalf("bin = %q, want %q", got.Bin, tt.wantBin)
			}
		})
	}
}

type fakeDirInfo struct{}

func (fakeDirInfo) Name() string       { return "Claude.app" }
func (fakeDirInfo) Size() int64        { return 0 }
func (fakeDirInfo) Mode() os.FileMode  { return os.ModeDir }
func (fakeDirInfo) ModTime() time.Time { return time.Time{} }
func (fakeDirInfo) IsDir() bool        { return true }
func (fakeDirInfo) Sys() any           { return nil }

func TestReadSessionCwd_CopilotFormat(t *testing.T) {
	dir := t.TempDir()
	cwdDir := filepath.Join(dir, "project")
	if err := os.Mkdir(cwdDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cwdJSON, _ := json.Marshal(cwdDir)
	line := `{"type":"session.start","data":{"sessionId":"abc","context":{"cwd":` +
		string(cwdJSON) + `}}}` + "\n"

	sessionFile := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionFile, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readSessionCwd(sessionFile)
	if got != cwdDir {
		t.Errorf("readSessionCwd() = %q, want %q", got, cwdDir)
	}
}

func TestReadCursorLastWorkingDir(t *testing.T) {
	dir := t.TempDir()
	workspaceDir := filepath.Join(dir, "project")
	lastDir := filepath.Join(workspaceDir, "frontend")
	if err := os.MkdirAll(lastDir, 0o755); err != nil {
		t.Fatal(err)
	}

	firstJSON, _ := json.Marshal(workspaceDir)
	lastJSON, _ := json.Marshal(lastDir)
	sessionFile := filepath.Join(dir, "cursor.jsonl")
	content := "" +
		`{"role":"assistant","message":{"content":[{"type":"tool_use","name":"ReadFile","input":{"path":"/tmp/file.txt"}}]}}` + "\n" +
		`{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"pwd","working_directory":` + string(firstJSON) + `}}]}}` + "\n" +
		`{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"pwd","working_directory":"relative/path"}}]}}` + "\n" +
		`{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"pwd","working_directory":` + string(lastJSON) + `}}]}}` + "\n"
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := readCursorLastWorkingDir(sessionFile)
	assertSameDir(t, "readCursorLastWorkingDir()", got, lastDir)
}

func TestCursorProjectDirNameFromTranscriptPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "flat transcript",
			path: filepath.Join(
				"/tmp", ".cursor", "projects",
				"Users-alice-code-my-app",
				"agent-transcripts", "sess.jsonl",
			),
			want: "Users-alice-code-my-app",
		},
		{
			name: "nested transcript",
			path: filepath.Join(
				"/tmp", ".cursor", "projects",
				"Users-alice-code-my-app",
				"agent-transcripts", "sess", "sess.jsonl",
			),
			want: "Users-alice-code-my-app",
		},
		{
			name: "missing agent transcripts ancestor",
			path: filepath.Join(
				"/tmp", ".cursor", "projects",
				"Users-alice-code-my-app", "other", "sess.jsonl",
			),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cursorProjectDirNameFromTranscriptPath(tt.path)
			if got != tt.want {
				t.Errorf(
					"cursorProjectDirNameFromTranscriptPath(%q) = %q, want %q",
					tt.path, got, tt.want,
				)
			}
		})
	}
}

func TestResolveCursorProjectDirNameFromRoot(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(
		root, "Users", "alice", "code", "li",
		"project-cache-hdfs",
	)
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(root, "Users", "alice", "code", "li"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(root, "Users", "alice", "code", "li", "project"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	got := resolveCursorProjectDirNameFromRoot(
		root, "Users-alice-code-li-project-cache-hdfs",
	)
	if got != want {
		t.Errorf(
			"resolveCursorProjectDirNameFromRoot() = %q, want %q",
			got, want,
		)
	}
}

func TestResolveCursorProjectDirNameFromRootMatchesUnderscoreComponents(
	t *testing.T,
) {
	root := t.TempDir()
	want := filepath.Join(
		root, "Users", "alice", "code", "li",
		"project_cache_hdfs",
	)
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveCursorProjectDirNameFromRoot(
		root, "Users-alice-code-li-project-cache-hdfs",
	)
	if got != want {
		t.Errorf(
			"resolveCursorProjectDirNameFromRoot() = %q, want %q",
			got, want,
		)
	}
}

func TestResolveCursorProjectDirFromSessionFileDetectsAmbiguity(
	t *testing.T,
) {
	root := t.TempDir()
	want := filepath.Join(
		root, "Users", "alice", "code", "li-tools",
	)
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(root, "Users", "alice", "code", "li", "tools"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(
		root, ".cursor", "projects",
		"Users-alice-code-li-tools",
		"agent-transcripts", "sess", "sess.jsonl",
	)
	dirName := cursorProjectDirNameFromTranscriptPath(filePath)
	matches := resolveCursorProjectDirNameFromRootMatches(
		root, dirName, "", 2,
	)
	got := ""
	if len(matches) > 0 {
		got = matches[0]
	}
	ambiguous := len(matches) > 1
	if got != want {
		t.Errorf(
			"resolveCursorProjectDirFromSessionFile() = %q, want %q",
			got, want,
		)
	}
	if !ambiguous {
		t.Error("expected ambiguous transcript path")
	}
}

func TestResolveCursorProjectDirFromSessionFileUnambiguous(
	t *testing.T,
) {
	root := t.TempDir()
	want := filepath.Join(
		root, "Users", "alice", "code", "li-openhouse",
	)
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}

	filePath := filepath.Join(
		root, ".cursor", "projects",
		"Users-alice-code-li-openhouse",
		"agent-transcripts", "sess", "sess.jsonl",
	)
	dirName := cursorProjectDirNameFromTranscriptPath(filePath)
	matches := resolveCursorProjectDirNameFromRootMatches(
		root, dirName, "", 2,
	)
	got := ""
	if len(matches) > 0 {
		got = matches[0]
	}
	ambiguous := len(matches) > 1
	if got != want {
		t.Errorf(
			"resolveCursorProjectDirFromSessionFile() = %q, want %q",
			got, want,
		)
	}
	if ambiguous {
		t.Error("expected unambiguous transcript path")
	}
}

func TestResolveCursorProjectDirNameFromRootBacktracksOnDeadEnd(
	t *testing.T,
) {
	root := t.TempDir()
	want := filepath.Join(
		root, "Users", "alice", "code", "li", "tools-app",
	)
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(root, "Users", "alice", "code", "li-tools"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	got := resolveCursorProjectDirNameFromRoot(
		root, "Users-alice-code-li-tools-app",
	)
	if got != want {
		t.Errorf(
			"resolveCursorProjectDirNameFromRoot() = %q, want %q",
			got, want,
		)
	}
}

func TestResolveCursorProjectDirNameFromRootHintPrefersContainingPath(
	t *testing.T,
) {
	root := t.TempDir()
	want := filepath.Join(
		root, "Users", "alice", "code", "li", "tools",
	)
	hint := filepath.Join(want, "frontend")
	if err := os.MkdirAll(hint, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(root, "Users", "alice", "code", "li-tools"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	got := resolveCursorProjectDirNameFromRootHint(
		root, "Users-alice-code-li-tools", hint,
	)
	if got != want {
		t.Errorf(
			"resolveCursorProjectDirNameFromRootHint() = %q, want %q",
			got, want,
		)
	}
}

func TestResolveCursorProjectDirNameFromRootHintStaleReturnsEmpty(
	t *testing.T,
) {
	root := t.TempDir()
	if err := os.MkdirAll(
		filepath.Join(root, "Users", "alice", "code", "li-tools"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(root, "Users", "alice", "code", "li", "tools"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	staleHint := filepath.Join(root, "unrelated")
	if err := os.MkdirAll(staleHint, 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveCursorProjectDirNameFromRootHint(
		root, "Users-alice-code-li-tools", staleHint,
	)
	if got != "" {
		t.Errorf(
			"with stale hint = %q, want empty", got,
		)
	}
}

func TestResolveCursorProjectDirNameFromRootHintSymlinkMatch(
	t *testing.T,
) {
	root := t.TempDir()

	// Real project with a hint subdir.
	realProject := filepath.Join(root, "repos", "li-tools")
	hintDir := filepath.Join(realProject, "src")
	if err := os.MkdirAll(hintDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Second ambiguous path.
	if err := os.MkdirAll(
		filepath.Join(root, "repos", "li", "tools"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	// Symlink: root/code -> root/repos. The DFS walks through
	// the symlink but the hint uses the resolved real path.
	if err := os.Symlink(
		filepath.Join(root, "repos"),
		filepath.Join(root, "code"),
	); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	got := resolveCursorProjectDirNameFromRootHint(
		root, "code-li-tools", hintDir,
	)
	assertSameDir(t, "result", got, realProject)
}

func TestResolveSessionDir(t *testing.T) {
	// Create a real temp directory for the "absolute path" cases.
	tmpDir := t.TempDir()

	// Create a session file with a cwd field.
	sessionFile := filepath.Join(tmpDir, "session.jsonl")
	cwdDir := filepath.Join(tmpDir, "project")
	if err := os.Mkdir(cwdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cwdJSON, _ := json.Marshal(cwdDir)
	content := `{"cwd":` + string(cwdJSON) + `}` + "\n"
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cursorProject := filepath.Join(
		tmpDir, "workspace-root", "li-openhouse",
	)
	if err := os.MkdirAll(cursorProject, 0o755); err != nil {
		t.Fatal(err)
	}
	cursorTranscript := filepath.Join(
		tmpDir, ".cursor", "projects",
		encodeCursorProjectPathForTest(cursorProject),
		"agent-transcripts", "cursor-sess",
		"cursor-sess.jsonl",
	)
	if err := os.MkdirAll(
		filepath.Dir(cursorTranscript), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursorTranscript, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cursorLastDir := filepath.Join(cursorProject, "frontend")
	if err := os.MkdirAll(cursorLastDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cursorLastDirJSON, _ := json.Marshal(cursorLastDir)
	cursorTranscriptWithLastDir := filepath.Join(
		tmpDir, ".cursor", "projects",
		encodeCursorProjectPathForTest(cursorProject),
		"agent-transcripts", "cursor-sess-last",
		"cursor-sess-last.jsonl",
	)
	if err := os.MkdirAll(
		filepath.Dir(cursorTranscriptWithLastDir), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	lastDirContent := `{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"pwd","working_directory":` +
		string(cursorLastDirJSON) + `}}]}}` + "\n"
	if err := os.WriteFile(
		cursorTranscriptWithLastDir, []byte(lastDirContent), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		session *db.Session
		want    string
	}{
		{
			name: "absolute project path",
			session: &db.Session{
				Project: tmpDir,
			},
			want: tmpDir,
		},
		{
			name: "relative project name returns empty",
			session: &db.Session{
				Project: "my-repo",
			},
			want: "",
		},
		{
			name: "nil file_path with relative project",
			session: &db.Session{
				Project:  "my-repo",
				FilePath: nil,
			},
			want: "",
		},
		{
			name: "file_path with cwd in session file",
			session: &db.Session{
				Project:  "my-repo",
				FilePath: &sessionFile,
			},
			want: cwdDir,
		},
		{
			name: "file_path takes precedence over project",
			session: &db.Session{
				Project:  tmpDir,
				FilePath: &sessionFile,
			},
			want: cwdDir,
		},
		{
			name: "nonexistent file_path falls back to project",
			session: func() *db.Session {
				bad := "/nonexistent/session.jsonl"
				return &db.Session{
					Project:  tmpDir,
					FilePath: &bad,
				}
			}(),
			want: tmpDir,
		},
		{
			name: "cursor transcript path resolves workspace dir",
			session: &db.Session{
				Agent:    "cursor",
				Project:  cursorProject,
				FilePath: &cursorTranscript,
			},
			want: cursorProject,
		},
		{
			name: "cursor transcript with last shell dir still resolves workspace",
			session: &db.Session{
				Agent:    "cursor",
				Project:  cursorProject,
				FilePath: &cursorTranscriptWithLastDir,
			},
			want: cursorProject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveSessionDir(tt.session)
			if tt.want == "" {
				if got != "" {
					t.Errorf(
						"resolveSessionDir() = %q, want %q",
						got, tt.want,
					)
				}
				return
			}
			if canonicalTestDir(got) != canonicalTestDir(tt.want) {
				t.Errorf(
					"resolveSessionDir() = %q, want %q",
					canonicalTestDir(got), canonicalTestDir(tt.want),
				)
			}
		})
	}
}

func TestResolveResumeDir(t *testing.T) {
	tmpDir := t.TempDir()

	cursorProject := filepath.Join(
		tmpDir, "workspace-root", "li-openhouse",
	)
	cursorLastDir := filepath.Join(cursorProject, "frontend")
	if err := os.MkdirAll(cursorLastDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cursorLastDirJSON, _ := json.Marshal(cursorLastDir)
	cursorTranscript := filepath.Join(
		tmpDir, ".cursor", "projects",
		encodeCursorProjectPathForTest(cursorProject),
		"agent-transcripts", "cursor-sess-last",
		"cursor-sess-last.jsonl",
	)
	if err := os.MkdirAll(
		filepath.Dir(cursorTranscript), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	lastDirContent := `{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"pwd","working_directory":` +
		string(cursorLastDirJSON) + `}}]}}` + "\n"
	if err := os.WriteFile(
		cursorTranscript, []byte(lastDirContent), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	got := resolveResumeDir(&db.Session{
		Agent:    "cursor",
		Project:  "li_openhouse",
		FilePath: &cursorTranscript,
	})
	assertSameDir(t, "resolveResumeDir()", got, cursorLastDir)
}

func TestResolveCursorWorkspaceDirUsesLastWorkingDirHint(t *testing.T) {
	tmpDir := t.TempDir()

	cursorProject := filepath.Join(
		tmpDir, "workspace-root", "li", "tools",
	)
	cursorLastDir := filepath.Join(cursorProject, "frontend")
	if err := os.MkdirAll(cursorLastDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(tmpDir, "workspace-root", "li-tools"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	cursorLastDirJSON, _ := json.Marshal(cursorLastDir)
	cursorTranscript := filepath.Join(
		tmpDir, ".cursor", "projects",
		encodeCursorProjectPathForTest(cursorProject),
		"agent-transcripts", "cursor-sess-last",
		"cursor-sess-last.jsonl",
	)
	if err := os.MkdirAll(
		filepath.Dir(cursorTranscript), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	lastDirContent := `{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"pwd","working_directory":` +
		string(cursorLastDirJSON) + `}}]}}` + "\n"
	if err := os.WriteFile(
		cursorTranscript, []byte(lastDirContent), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	got := resolveCursorWorkspaceDir(&db.Session{
		Agent:    "cursor",
		Project:  cursorProject,
		FilePath: &cursorTranscript,
	})
	assertSameDir(t, "resolveCursorWorkspaceDir()", got, cursorProject)
}

func TestResolveCursorWorkspaceDirAmbiguousWithoutHintReturnsEmpty(
	t *testing.T,
) {
	tmpDir := t.TempDir()

	// Create two paths that decode from the same encoded name.
	pathA := filepath.Join(tmpDir, "li-tools")
	pathB := filepath.Join(tmpDir, "li", "tools")
	if err := os.MkdirAll(pathA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pathB, 0o755); err != nil {
		t.Fatal(err)
	}

	encoded := encodeCursorProjectPathForTest(pathA)
	cursorTranscript := filepath.Join(
		tmpDir, ".cursor", "projects",
		encoded,
		"agent-transcripts", "cursor-sess",
		"cursor-sess.jsonl",
	)

	got := resolveCursorWorkspaceDir(&db.Session{
		Agent:    "cursor",
		Project:  "li_tools", // Not absolute — no hint.
		FilePath: &cursorTranscript,
	})
	if got != "" {
		t.Errorf(
			"resolveCursorWorkspaceDir() = %q, want empty "+
				"(ambiguous without hint)",
			got,
		)
	}
}

func TestResolveCursorWorkspaceDirStaleHintReturnsEmpty(
	t *testing.T,
) {
	tmpDir := t.TempDir()

	pathA := filepath.Join(tmpDir, "li-tools")
	pathB := filepath.Join(tmpDir, "li", "tools")
	if err := os.MkdirAll(pathA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pathB, 0o755); err != nil {
		t.Fatal(err)
	}

	// Stale hint: exists on disk but not under either candidate.
	staleDir := filepath.Join(tmpDir, "unrelated-project")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}

	encoded := encodeCursorProjectPathForTest(pathA)
	staleDirJSON, _ := json.Marshal(staleDir)
	cursorTranscript := filepath.Join(
		tmpDir, ".cursor", "projects",
		encoded,
		"agent-transcripts", "cursor-sess",
		"cursor-sess.jsonl",
	)
	if err := os.MkdirAll(
		filepath.Dir(cursorTranscript), 0o755,
	); err != nil {
		t.Fatal(err)
	}
	content := `{"role":"assistant","message":{"content":[` +
		`{"type":"tool_use","name":"Shell","input":{` +
		`"command":"pwd","working_directory":` +
		string(staleDirJSON) + `}}]}}` + "\n"
	if err := os.WriteFile(
		cursorTranscript, []byte(content), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	got := resolveCursorWorkspaceDir(&db.Session{
		Agent:    "cursor",
		Project:  "li_tools",
		FilePath: &cursorTranscript,
	})
	if got != "" {
		t.Errorf(
			"resolveCursorWorkspaceDir() with stale hint = %q, "+
				"want empty",
			got,
		)
	}
}

func TestResolveCursorWorkspaceDirWithoutTranscriptContents(
	t *testing.T,
) {
	tmpDir := t.TempDir()

	cursorProject := filepath.Join(
		tmpDir, "workspace-root", "li-openhouse",
	)
	if err := os.MkdirAll(cursorProject, 0o755); err != nil {
		t.Fatal(err)
	}

	cursorTranscript := filepath.Join(
		tmpDir, ".cursor", "projects",
		encodeCursorProjectPathForTest(cursorProject),
		"agent-transcripts", "cursor-sess",
		"cursor-sess.jsonl",
	)

	got := resolveCursorWorkspaceDir(&db.Session{
		Agent:    "cursor",
		Project:  cursorProject,
		FilePath: &cursorTranscript,
	})
	assertSameDir(t, "resolveCursorWorkspaceDir()", got, cursorProject)
}

func TestResolveCursorResumePathsUsesProvidedLastWorkingDir(
	t *testing.T,
) {
	tmpDir := t.TempDir()

	cursorProject := filepath.Join(
		tmpDir, "workspace-root", "li", "tools",
	)
	cursorLastDir := filepath.Join(cursorProject, "frontend")
	if err := os.MkdirAll(cursorLastDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(
		filepath.Join(tmpDir, "workspace-root", "li-tools"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	cursorTranscript := filepath.Join(
		tmpDir, ".cursor", "projects",
		encodeCursorProjectPathForTest(cursorProject),
		"agent-transcripts", "cursor-sess-last",
		"cursor-sess-last.jsonl",
	)

	launchDir, workspaceDir := resolveCursorResumePaths(
		&db.Session{
			Agent:    "cursor",
			Project:  cursorProject,
			FilePath: &cursorTranscript,
		},
		cursorLastDir,
	)
	assertSameDir(t, "launchDir", launchDir, cursorLastDir)
	assertSameDir(t, "workspaceDir", workspaceDir, cursorProject)
}

func TestResolveCursorResumePathsFallbackWorkspaceToLastWorkingDir(
	t *testing.T,
) {
	lastCwd := filepath.Join(t.TempDir(), "frontend")
	if err := os.MkdirAll(lastCwd, 0o755); err != nil {
		t.Fatal(err)
	}

	launchDir, workspaceDir := resolveCursorResumePaths(
		&db.Session{
			Agent:    "cursor",
			Project:  "li_tools",
			FilePath: nil,
		},
		lastCwd,
	)
	assertSameDir(t, "launchDir", launchDir, lastCwd)
	assertSameDir(t, "workspaceDir", workspaceDir, lastCwd)
}

func TestResolveResumeDirCanonicalizesSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	realProject := filepath.Join(tmpDir, "repos", "openhouse")
	if err := os.MkdirAll(realProject, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(tmpDir, "project_cache_hdfs")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkProject := filepath.Join(cacheDir, "openhouse")
	if err := os.Symlink(realProject, linkProject); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	linkJSON, _ := json.Marshal(linkProject)
	sessionFile := filepath.Join(tmpDir, "cursor-symlink.jsonl")
	content := `{"role":"assistant","message":{"content":[{"type":"tool_use","name":"Shell","input":{"command":"pwd","working_directory":` +
		string(linkJSON) + `}}]}}` + "\n"
	if err := os.WriteFile(sessionFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got := resolveResumeDir(&db.Session{
		Agent:    "cursor",
		Project:  "li_openhouse",
		FilePath: &sessionFile,
	})
	assertSameDir(t, "resolveResumeDir()", got, realProject)
}

func TestResolveSessionDirCursorProjectFallbackCanonicalizesSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	realProject := filepath.Join(tmpDir, "repos", "openhouse")
	if err := os.MkdirAll(realProject, 0o755); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(tmpDir, "project_cache_hdfs")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linkProject := filepath.Join(cacheDir, "openhouse")
	if err := os.Symlink(realProject, linkProject); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	got := resolveSessionDir(&db.Session{
		Agent:   "cursor",
		Project: linkProject,
	})
	assertSameDir(t, "resolveSessionDir()", got, realProject)
}

func TestResumeLaunchCwd(t *testing.T) {
	cwd := t.TempDir()

	tests := []struct {
		name     string
		agent    string
		openerID string
		goos     string
		want     string
	}{
		{
			name:     "claude keeps cwd for auto darwin launch",
			agent:    "claude",
			openerID: "auto",
			goos:     "darwin",
			want:     cwd,
		},
		{
			name:     "cursor auto darwin launch keeps cwd",
			agent:    "cursor",
			openerID: "auto",
			goos:     "darwin",
			want:     cwd,
		},
		{
			name:     "cursor iterm2 darwin launch keeps cwd",
			agent:    "cursor",
			openerID: "iterm2",
			goos:     "darwin",
			want:     cwd,
		},
		{
			name:     "cursor terminal darwin launch keeps cwd",
			agent:    "cursor",
			openerID: "terminal",
			goos:     "darwin",
			want:     cwd,
		},
		{
			name:     "cursor ghostty darwin launch keeps cwd",
			agent:    "cursor",
			openerID: "ghostty",
			goos:     "darwin",
			want:     cwd,
		},
		{
			name:     "cursor kitty darwin launch keeps cwd flag",
			agent:    "cursor",
			openerID: "kitty",
			goos:     "darwin",
			want:     cwd,
		},
		{
			name:     "cursor linux launch keeps cwd",
			agent:    "cursor",
			openerID: "ghostty",
			goos:     "linux",
			want:     cwd,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resumeLaunchCwd(
				tt.agent, tt.openerID, tt.goos, cwd,
			)
			if got != tt.want {
				t.Errorf(
					"resumeLaunchCwd() = %q, want %q",
					got, tt.want,
				)
			}
		})
	}
}

func encodeCursorProjectPathForTest(path string) string {
	clean := filepath.Clean(path)
	var tokens []string
	if volume := filepath.VolumeName(clean); volume != "" {
		tokens = append(tokens, strings.TrimSuffix(volume, ":"))
		clean = strings.TrimPrefix(clean, volume)
	}
	parts := strings.SplitSeq(clean, string(filepath.Separator))
	for part := range parts {
		if part == "" {
			continue
		}
		tokens = append(tokens, cursorComponentTokens(part)...)
	}
	return strings.Join(tokens, "-")
}
