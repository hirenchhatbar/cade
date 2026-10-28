package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// config is config.yaml, the file the user edits. cade writes only the keys the
// settings dialog changes, keeping the rest of the file as it is.
type config struct {
	Theme  string             `yaml:"theme"`
	Themes map[string]palette `yaml:"themes"` // user themes; empty fields inherit from dracula
	Claude struct {
		Default  string             `yaml:"default"` // may be left out when there is one account
		Accounts map[string]account `yaml:"accounts"`
	} `yaml:"claude"`
	Diff struct {
		Wrap *bool `yaml:"wrap"` // long lines wrap in the F3 diff; false cuts them with …. Default: true
	} `yaml:"diff"`
	Toasts struct {
		Position string `yaml:"position"` // corner, see toastPositions. Default: top-right
	} `yaml:"toasts"`
	Quit struct {
		Confirm *bool `yaml:"confirm"` // ask before quitting while sessions run. Default: true
	} `yaml:"quit"`
	Shell struct {
		Height int `yaml:"height"` // % of the pane the F2 shell takes under the session, see shellMin. Default: 50
	} `yaml:"shell"`
}

// account is one Claude login: how claude is started and where it keeps sessions.
// Values may use ~, $VAR and $PWD (the project directory).
type account struct {
	Command   []string          `yaml:"command"`   // program and flags; cade appends --session-id/--resume. Default: claude
	ConfigDir string            `yaml:"configDir"` // passed as CLAUDE_CONFIG_DIR. Default: ~/.claude
	Env       map[string]string `yaml:"env"`
	Dirs      []string          `yaml:"dirs"` // projects in these directories use this account
}

func configPath() string { return filepath.Join(filepath.Dir(projectsPath()), "config.yaml") }

// parseConfig reads config.yaml; a missing file means defaults. It lists every
// unknown key and a bad default as issues; err is an unreadable file or a
// syntax error, after which nothing was read.
func parseConfig(path string) (cfg config, issues []string, err error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil, nil
	}
	if err != nil {
		return cfg, nil, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true) // a typo like "comand" would silently start plain claude
	var te *yaml.TypeError
	if err := dec.Decode(&cfg); errors.As(err, &te) {
		for _, e := range te.Errors {
			issues = append(issues, didYouMean(e))
		}
	} else if err != nil && err != io.EOF {
		return cfg, nil, err
	}
	c := &cfg.Claude
	if c.Default == "" && len(c.Accounts) == 1 {
		for name := range c.Accounts {
			c.Default = name
		}
	}
	if _, ok := c.Accounts[c.Default]; len(c.Accounts) > 0 && !ok {
		issues = append(issues, fmt.Sprintf("claude.default: no account %q", c.Default))
	}
	if p := cfg.Toasts.Position; p != "" && !slices.Contains(toastPositions, p) {
		issues = append(issues, fmt.Sprintf("toasts.position: %q is not one of %s", p, strings.Join(toastPositions, ", ")))
	}
	if h := cfg.Shell.Height; h != 0 && (h < shellMin || h > shellMax) {
		issues = append(issues, fmt.Sprintf("shell.height: %d is not between %d and %d", h, shellMin, shellMax))
	}
	return cfg, issues, nil
}

// checkConfig lists every problem in config.yaml that cade can find. err is set
// when cade would refuse to start: a bad file, or an account's program not on
// PATH, so it never runs claude without its sandbox or as the wrong account
// quietly. A bad theme or a missing directory is listed but not fatal.
func checkConfig(path, home, cwd string) (config, []string, error) {
	cfg, issues, err := parseConfig(path)
	if err != nil {
		return cfg, []string{err.Error()}, err
	}
	accs := map[string]account{"": {}} // plain claude when no accounts are configured
	if len(cfg.Claude.Accounts) > 0 {
		accs = cfg.Claude.Accounts
	}
	for _, name := range slices.Sorted(maps.Keys(accs)) {
		if bin := accs[name].argv(home, cwd)[0]; !onPath(bin) {
			issues = append(issues, fmt.Sprintf("%s%s not found in PATH", accField(name, "command"), bin))
		}
	}
	if len(issues) > 0 {
		err = errors.New(strings.Join(issues, "\n"))
	}
	if _, terr := loadTheme(cfg); terr != nil {
		issues = append(issues, "theme: "+terr.Error())
	}
	for _, name := range slices.Sorted(maps.Keys(cfg.Claude.Accounts)) {
		acc := cfg.Claude.Accounts[name]
		if acc.ConfigDir != "" && !isDir(acc.sessionsRoot(home)) {
			issues = append(issues, fmt.Sprintf("%s%s is not a directory", accField(name, "configDir"), acc.sessionsRoot(home)))
		}
		for _, d := range acc.Dirs {
			if d = expand(d, home, ""); !isDir(d) {
				issues = append(issues, fmt.Sprintf("%s%s is not a directory", accField(name, "dirs"), d))
			}
		}
	}
	return cfg, issues, err
}

// accField names an account's field in an issue; the implicit plain-claude account has none.
func accField(name, field string) string {
	if name == "" {
		return ""
	}
	return "claude.accounts." + name + "." + field + ": "
}

func onPath(bin string) bool { _, err := exec.LookPath(bin); return err == nil }

func isDir(path string) bool { st, err := os.Stat(path); return err == nil && st.IsDir() }

var unknownKey = regexp.MustCompile(`^(line \d+): field (.+) not found in type`)

// didYouMean rewrites yaml's unknown-field error, naming the closest known key.
func didYouMean(e string) string {
	m := unknownKey.FindStringSubmatch(e)
	if m == nil {
		return e
	}
	out := fmt.Sprintf("%s: unknown key %q", m[1], m[2])
	best, bestD := "", 3 // more than 2 edits away is no suggestion
	for _, k := range yamlKeys(reflect.TypeOf(config{}), nil) {
		if d := editDistance(m[2], k); d < bestD && d < len(m[2]) {
			best, bestD = k, d
		}
	}
	if best != "" {
		out += fmt.Sprintf(" (did you mean %q?)", best)
	}
	return out
}

// yamlKeys lists the yaml keys of t and of the structs nested in it.
func yamlKeys(t reflect.Type, keys []string) []string {
	for t.Kind() == reflect.Map || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return keys
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if k, _, _ := strings.Cut(f.Tag.Get("yaml"), ","); k != "" {
			keys = yamlKeys(f.Type, append(keys, k))
		}
	}
	return keys
}

// editDistance is the Levenshtein distance between a and b, case-insensitive.
func editDistance(a, b string) int {
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := range ra {
		cur := []int{i + 1}
		for j := range rb {
			cost := 1
			if ra[i] == rb[j] {
				cost = 0
			}
			cur = append(cur, min(prev[j]+cost, prev[j+1]+1, cur[j]+1))
		}
		prev = cur
	}
	return prev[len(rb)]
}

// saveConfig sets one key in config.yaml, keys being its path from the top
// (theme, or quit then confirm), creating the file and parent keys if needed.
// It edits the yaml tree rather than re-encoding config, so comments and key
// order survive; indentation is normalised to two spaces.
func saveConfig(path string, val any, keys ...string) error {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real // a dotfiles symlink stays a symlink
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return err
	}
	if doc.Kind == 0 { // empty or missing file
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return errors.New("config.yaml is not a mapping")
	}
	for d, k := range keys {
		i := 0
		for i < len(m.Content) && m.Content[i].Value != k {
			i += 2 // keys and values alternate
		}
		if i == len(m.Content) {
			key := &yaml.Node{}
			key.SetString(k)
			m.Content = append([]*yaml.Node{key, {Kind: yaml.MappingNode}}, m.Content...)
			i = 0
		}
		if v := m.Content[i+1]; d == len(keys)-1 {
			head, line, foot := v.HeadComment, v.LineComment, v.FootComment // Encode resets them
			if err := v.Encode(val); err != nil {
				return err
			}
			v.HeadComment, v.LineComment, v.FootComment = head, line, foot
		} else if m = m.Content[i+1]; m.Kind != yaml.MappingNode {
			*m = yaml.Node{Kind: yaml.MappingNode} // e.g. "quit:" left empty
		}
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return writeFile(path, out.Bytes())
}

// account picks the account whose dirs hold dir most specifically, else the default.
func (cfg config) account(dir, home string) account {
	best, n := cfg.Claude.Accounts[cfg.Claude.Default], -1
	for _, acc := range cfg.Claude.Accounts {
		for _, d := range acc.Dirs {
			d = filepath.Clean(expand(d, home, dir))
			if (dir == d || strings.HasPrefix(dir, d+"/")) && len(d) > n {
				best, n = acc, len(d)
			}
		}
	}
	return best
}

// argv is the command without session flags, expanded for project dir.
func (acc account) argv(home, dir string) []string {
	if len(acc.Command) == 0 {
		return []string{"claude"}
	}
	out := make([]string, len(acc.Command))
	for i, s := range acc.Command {
		out[i] = expand(s, home, dir)
	}
	return out
}

// sessionsRoot is the directory holding projects/<dir>/<id>.jsonl for this account.
func (acc account) sessionsRoot(home string) string {
	if acc.ConfigDir == "" {
		return filepath.Join(home, ".claude")
	}
	return expand(acc.ConfigDir, home, "")
}

func (acc account) cmd(home, dir string, args ...string) *exec.Cmd {
	argv := acc.argv(home, dir)
	cmd := exec.Command(argv[0], append(argv[1:], args...)...)
	cmd.Dir = dir
	// Go only sets PWD itself when Env is nil; wrapper scripts rely on it.
	cmd.Env = append(os.Environ(), "PWD="+dir)
	if acc.ConfigDir != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CONFIG_DIR="+acc.sessionsRoot(home))
	}
	for k, v := range acc.Env {
		cmd.Env = append(cmd.Env, k+"="+expand(v, home, dir))
	}
	return cmd
}

// expand resolves a leading ~ and $VAR/${VAR}, with $PWD as the project dir.
func expand(s, home, dir string) string {
	if s == "~" || strings.HasPrefix(s, "~/") {
		s = home + s[1:]
	}
	return os.Expand(s, func(k string) string {
		if k == "PWD" && dir != "" {
			return dir
		}
		return os.Getenv(k)
	})
}
