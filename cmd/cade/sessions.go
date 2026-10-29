package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type session struct {
	ID    string
	Title string
}

// short is the 8-character ID prefix shown next to titles.
func (s session) short() string { return s.ID[:min(8, len(s.ID))] }

// deleteSessionFiles removes a session's transcript and its per-session folder.
func deleteSessionFiles(root, cwd, id string) error {
	base := filepath.Join(projectDir(root, cwd), id)
	if err := os.Remove(base + ".jsonl"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(base) // tool-results etc., if any
}

// merge keeps the shown order stable (resuming a session bumps its mtime, which
// would otherwise move it to the top): sessions new on disk go first, known ones
// stay in place with fresh titles, cade-started ones not yet on disk are kept.
func merge(shown, loaded []session, pending map[string]bool) []session {
	byID := map[string]session{}
	for _, l := range loaded {
		byID[l.ID] = l
	}
	known := map[string]bool{}
	var kept []session
	for _, s := range shown {
		known[s.ID] = true
		if l, ok := byID[s.ID]; ok {
			kept = append(kept, l)
		} else if pending[s.ID] {
			kept = append(kept, s)
		}
	}
	var fresh []session
	for _, l := range loaded {
		if !known[l.ID] {
			fresh = append(fresh, l)
		}
	}
	return append(fresh, kept...)
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// projectDir mirrors how Claude Code names <root>/projects/<dir>, root being
// ~/.claude or CLAUDE_CONFIG_DIR.
func projectDir(root, cwd string) string {
	return filepath.Join(root, "projects", nonAlnum.ReplaceAllString(cwd, "-"))
}

// loadSessions lists sessions for cwd, newest first.
func loadSessions(root, cwd string) []session {
	files, _ := filepath.Glob(filepath.Join(projectDir(root, cwd), "*.jsonl"))
	sort.Slice(files, func(i, j int) bool { return modTime(files[i]) > modTime(files[j]) })
	out := make([]session, 0, len(files))
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		title := cachedTitle(f)
		if title == "" {
			title = "untitled"
		}
		out = append(out, session{ID: id, Title: title})
	}
	return out
}

func modTime(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.ModTime().UnixNano()
}

type titleEntry struct {
	mod   int64
	title string
}

// titles caches readTitle per file so the periodic refresh only rereads changed files.
var titles = map[string]titleEntry{}

func cachedTitle(path string) string {
	mod := modTime(path)
	if e, ok := titles[path]; ok && e.mod == mod {
		return e.title
	}
	t := readTitle(path)
	titles[path] = titleEntry{mod, t}
	return t
}

// newUUID returns a random v4 UUID for claude --session-id.
func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// readTitle picks the best name in a session file: the last custom-title
// (/rename, -n), else agent-name, else ai-title, else the first typed prompt.
func readTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var custom, agent, ai, prompt string
	// bufio.Reader, not Scanner: session lines can exceed Scanner's 64KB limit.
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		switch {
		case bytes.Contains(line, []byte(`"type":"custom-title"`)),
			bytes.Contains(line, []byte(`"type":"agent-name"`)),
			bytes.Contains(line, []byte(`"type":"ai-title"`)):
			var rec struct {
				Type        string `json:"type"`
				CustomTitle string `json:"customTitle"`
				AgentName   string `json:"agentName"`
				AITitle     string `json:"aiTitle"`
			}
			if json.Unmarshal(line, &rec) == nil {
				switch {
				case rec.Type == "custom-title" && rec.CustomTitle != "":
					custom = rec.CustomTitle
				case rec.Type == "agent-name" && rec.AgentName != "":
					agent = rec.AgentName
				case rec.Type == "ai-title" && rec.AITitle != "":
					ai = rec.AITitle
				}
			}
		case prompt == "" && bytes.Contains(line, []byte(`"type":"user"`)):
			prompt = userPrompt(line)
		}
		if err != nil {
			for _, t := range []string{custom, agent, ai, prompt} {
				if t != "" {
					return t
				}
			}
			return ""
		}
	}
}

// userPrompt returns the text of a typed user message, skipping meta and
// command records (content starting with "<").
func userPrompt(line []byte) string {
	var rec struct {
		Type    string `json:"type"`
		IsMeta  bool   `json:"isMeta"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) != nil || rec.Type != "user" || rec.IsMeta {
		return ""
	}
	var text string
	if json.Unmarshal(rec.Message.Content, &text) != nil {
		var blocks []struct{ Type, Text string }
		json.Unmarshal(rec.Message.Content, &blocks)
		for _, b := range blocks {
			if b.Type == "text" {
				text = b.Text
				break
			}
		}
	}
	text = strings.Join(strings.Fields(text), " ")
	if strings.HasPrefix(text, "<") {
		return ""
	}
	return text
}

// firstPrompts returns up to n typed prompts from a session file, each cut to
// limit runes so a pasted log can't blow up the title request.
func firstPrompts(path string, n, limit int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	r := bufio.NewReader(f) // not Scanner: lines can exceed its 64KB limit
	for len(out) < n {
		line, err := r.ReadBytes('\n')
		if bytes.Contains(line, []byte(`"type":"user"`)) {
			if p := []rune(userPrompt(line)); len(p) > 0 {
				out = append(out, string(p[:min(len(p), limit)]))
			}
		}
		if err != nil {
			break
		}
	}
	return out
}
