package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"
)

type HostEntry struct {
	Alias           string
	HostName        string
	User            string
	Port            string
	IdentityFile    string
	ExtraDirectives map[string]string
	IsPattern       bool
	// Duplicate is set when another Host block uses the same alias.
	Duplicate bool
}

// Config keeps the raw lines of an ssh_config file and edits only the lines
// it has to, so comments, unknown directives and line endings survive.
type Config struct {
	path  string
	eol   string
	bom   bool
	lines []string
}

type block struct {
	start, end int
	kind       string
	value      string
}

const utf8BOM = "\xef\xbb\xbf"

// backedUp records config paths whose original content this process has
// already copied to .bak, so later saves never overwrite that copy.
var backedUp sync.Map

func GetSSHDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh"), nil
}

func GetConfigPath() (string, error) {
	dir, err := GetSSHDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config"), nil
}

// Load reads the config at path, creating the directory and an empty file
// when they do not exist.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			return nil, err
		}
		data = nil
	} else if err != nil {
		return nil, err
	}

	c := &Config{path: path, eol: "\n"}
	text := string(data)
	if strings.HasPrefix(text, utf8BOM) {
		c.bom = true
		text = strings.TrimPrefix(text, utf8BOM)
	}
	if strings.Contains(text, "\r\n") {
		c.eol = "\r\n"
	}
	if text != "" {
		parts := strings.Split(text, "\n")
		if parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		for i, p := range parts {
			parts[i] = strings.TrimSuffix(p, "\r")
		}
		c.lines = parts
	}
	return c, nil
}

func (c *Config) Path() string { return c.path }

// scanValue splits the text after a keyword into its value and a trailing
// comment, following OpenSSH's argv_split: a '#' starts a comment only at the
// start of a token, quotes group, and backslash escapes \ " ' only.
func scanValue(s string) (raw, trailing string) {
	end, i := 0, 0
	for i < len(s) {
		if s[i] == ' ' || s[i] == '\t' {
			i++
			continue
		}
		if s[i] == '#' {
			return s[:end], s[end:]
		}
		var quote byte
		for ; i < len(s); i++ {
			c := s[i]
			switch {
			case c == '\\':
				if i+1 < len(s) && (s[i+1] == '\'' || s[i+1] == '"' || s[i+1] == '\\' || (quote == 0 && s[i+1] == ' ')) {
					i++
				}
			case quote == 0 && (c == ' ' || c == '\t'):
				goto tokenDone
			case quote == 0 && (c == '"' || c == '\''):
				quote = c
			case quote != 0 && c == quote:
				quote = 0
			}
		}
	tokenDone:
		end = i
	}
	return s[:end], ""
}

// splitArgs returns the unescaped, unquoted tokens of raw.
func splitArgs(raw string) []string {
	var out []string
	i := 0
	for i < len(raw) {
		if raw[i] == ' ' || raw[i] == '\t' {
			i++
			continue
		}
		var tok strings.Builder
		var quote byte
	token:
		for ; i < len(raw); i++ {
			c := raw[i]
			switch {
			case c == '\\':
				if i+1 < len(raw) && (raw[i+1] == '\'' || raw[i+1] == '"' || raw[i+1] == '\\' || (quote == 0 && raw[i+1] == ' ')) {
					i++
				}
				tok.WriteByte(raw[i])
			case quote == 0 && (c == ' ' || c == '\t'):
				break token
			case quote == 0 && (c == '"' || c == '\''):
				quote = c
			case quote != 0 && c == quote:
				quote = 0
			default:
				tok.WriteByte(c)
			}
		}
		out = append(out, tok.String())
	}
	return out
}

func firstArg(raw string) string {
	if t := splitArgs(raw); len(t) > 0 {
		return t[0]
	}
	return ""
}

// parseLine returns the keyword, the raw value text, and any trailing
// comment (with its leading whitespace) of a directive line.
func parseLine(line string) (key, raw, trailing string, ok bool) {
	t := strings.TrimLeft(line, " \t")
	if t == "" || strings.HasPrefix(t, "#") {
		return "", "", "", false
	}
	i := strings.IndexAny(t, " \t=")
	if i < 0 {
		return t, "", "", true
	}
	key = t[:i]
	rest := strings.TrimLeft(t[i:], " \t")
	if strings.HasPrefix(rest, "=") {
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	raw, trailing = scanValue(rest)
	return key, raw, trailing, true
}

func hasRiskyBackslash(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && (i+1 == len(v) || strings.IndexByte("\\\"'", v[i+1]) >= 0) {
			return true
		}
	}
	return false
}

// formatValue quotes and escapes v only when OpenSSH would otherwise read it
// differently: whitespace, quotes, a leading # or =, or a backslash that
// would act as an escape.
func formatValue(v string) string {
	if !strings.ContainsAny(v, " \t\"'") && !strings.HasPrefix(v, "#") && !strings.HasPrefix(v, "=") && !hasRiskyBackslash(v) {
		return v
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(v); i++ {
		switch {
		case v[i] == '"':
			b.WriteString(`\"`)
		case v[i] == '\\' && (i+1 == len(v) || strings.IndexByte("\\\"'", v[i+1]) >= 0):
			b.WriteString(`\\`)
		default:
			b.WriteByte(v[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

func isBlankOrComment(line string) bool {
	t := strings.TrimSpace(line)
	return t == "" || strings.HasPrefix(t, "#")
}

func indentOf(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func aliasOf(raw string) string {
	if t := splitArgs(raw); len(t) == 1 {
		return t[0]
	}
	return raw
}

func (c *Config) blocks() []block {
	var out []block
	cur := -1
	closeAt := func(limit int) {
		if cur < 0 {
			return
		}
		e := limit
		for e > out[cur].start+1 && isBlankOrComment(c.lines[e-1]) {
			e--
		}
		out[cur].end = e
	}
	for i, l := range c.lines {
		k, raw, _, ok := parseLine(l)
		if !ok {
			continue
		}
		kind := strings.ToLower(k)
		if kind != "host" && kind != "match" {
			continue
		}
		closeAt(i)
		out = append(out, block{start: i, kind: kind, value: aliasOf(raw)})
		cur = len(out) - 1
	}
	closeAt(len(c.lines))
	return out
}

func (c *Config) findHost(alias string) (block, bool) {
	for _, b := range c.blocks() {
		if b.kind == "host" && b.value == alias {
			return b, true
		}
	}
	return block{}, false
}

func isPatternAlias(a string) bool {
	return a == "" || strings.ContainsAny(a, "*?! \t")
}

func (c *Config) entryFrom(b block) *HostEntry {
	e := &HostEntry{Alias: b.value, IsPattern: isPatternAlias(b.value), ExtraDirectives: map[string]string{}}
	for i := b.start + 1; i < b.end; i++ {
		k, raw, _, ok := parseLine(c.lines[i])
		if !ok {
			continue
		}
		switch strings.ToLower(k) {
		case "hostname":
			if e.HostName == "" {
				e.HostName = firstArg(raw)
			}
		case "user":
			if e.User == "" {
				e.User = firstArg(raw)
			}
		case "port":
			if e.Port == "" {
				e.Port = firstArg(raw)
			}
		case "identityfile":
			if e.IdentityFile == "" {
				e.IdentityFile = firstArg(raw)
			}
		default:
			v := raw
			if t := splitArgs(raw); len(t) == 1 {
				v = t[0]
			}
			if prev, dup := e.ExtraDirectives[k]; dup {
				v = prev + ", " + v
			}
			e.ExtraDirectives[k] = v
		}
	}
	return e
}

// Hosts returns every Host block in file order, wildcard blocks included.
func (c *Config) Hosts() []*HostEntry {
	var out []*HostEntry
	count := map[string]int{}
	for _, b := range c.blocks() {
		if b.kind == "host" {
			out = append(out, c.entryFrom(b))
			count[b.value]++
		}
	}
	for _, e := range out {
		e.Duplicate = count[e.Alias] > 1
	}
	return out
}

func (c *Config) Has(alias string) bool {
	_, ok := c.findHost(alias)
	return ok
}

// ValidateAlias accepts letters, digits and . _ + - only, and not a leading
// '-'. Anything else either changes meaning in ssh_config, breaks quoting for
// the whole file, or cannot be passed to ssh as a bare host name.
func ValidateAlias(alias string) error {
	if strings.TrimSpace(alias) == "" {
		return errors.New("alias is required")
	}
	if strings.HasPrefix(alias, "-") {
		return errors.New("alias must not start with '-'")
	}
	for _, r := range alias {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._+-", r)) {
			return errors.New("alias may only contain letters, digits and . _ + -")
		}
	}
	return nil
}

// ValidateTarget checks an alias that already exists in the config before it
// is handed to ssh. It is looser than ValidateAlias, so hosts written by
// other tools stay connectable, but it still refuses anything ssh could read
// as an option or that the shell-free exec path could mangle.
func ValidateTarget(alias string) error {
	if strings.TrimSpace(alias) == "" {
		return errors.New("alias is required")
	}
	if strings.HasPrefix(alias, "-") {
		return errors.New("alias must not start with '-'")
	}
	for _, r := range alias {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("*?!\"'", r) {
			return errors.New("alias contains characters that cannot be used as an ssh target")
		}
	}
	return nil
}

func validateValue(field, v string) error {
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s must not contain control characters or line breaks", field)
		}
	}
	return nil
}

func validateEntry(e *HostEntry) error {
	for _, f := range []struct{ name, v string }{
		{"hostname", e.HostName}, {"user", e.User}, {"port", e.Port}, {"identity file", e.IdentityFile},
	} {
		if err := validateValue(f.name, f.v); err != nil {
			return err
		}
	}
	for k, v := range e.ExtraDirectives {
		if err := validateValue(k, k+v); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) setDirective(alias, key, value string) {
	b, ok := c.findHost(alias)
	if !ok {
		return
	}
	for i := b.start + 1; i < b.end; i++ {
		k, raw, trailing, ok := parseLine(c.lines[i])
		if !ok || !strings.EqualFold(k, key) {
			continue
		}
		switch {
		case value == "":
			c.lines = append(c.lines[:i], c.lines[i+1:]...)
		case firstArg(raw) != value:
			c.lines[i] = indentOf(c.lines[i]) + k + " " + formatValue(value) + trailing
		}
		return
	}
	if value == "" {
		return
	}
	indent := "  "
	for i := b.start + 1; i < b.end; i++ {
		if _, _, _, ok := parseLine(c.lines[i]); ok {
			indent = indentOf(c.lines[i])
			break
		}
	}
	line := indent + key + " " + formatValue(value)
	c.lines = append(c.lines[:b.end], append([]string{line}, c.lines[b.end:]...)...)
}

// insertionPoint returns where a new block should go: just above the first
// "Host *" block (and the comment lines attached to it), because OpenSSH
// uses the first value it finds and a catch-all above would override the new
// block. Without one, it is the end of the file.
func (c *Config) insertionPoint() int {
	for _, b := range c.blocks() {
		if b.kind == "host" && b.value == "*" {
			p := b.start
			for p > 0 && strings.HasPrefix(strings.TrimSpace(c.lines[p-1]), "#") {
				p--
			}
			return p
		}
	}
	return len(c.lines)
}

// AddOrUpdate updates the known fields of an existing Host block in place
// (empty fields remove their directive), or adds a new block. It rejects
// values that would corrupt the file and then changes nothing.
func (c *Config) AddOrUpdate(e *HostEntry) error {
	if err := validateEntry(e); err != nil {
		return err
	}
	if _, exists := c.findHost(e.Alias); exists {
		c.setDirective(e.Alias, "HostName", e.HostName)
		c.setDirective(e.Alias, "User", e.User)
		c.setDirective(e.Alias, "Port", e.Port)
		c.setDirective(e.Alias, "IdentityFile", e.IdentityFile)
		return nil
	}
	if err := ValidateAlias(e.Alias); err != nil {
		return err
	}

	block := []string{"Host " + e.Alias}
	for _, f := range []struct{ k, v string }{
		{"HostName", e.HostName}, {"User", e.User}, {"Port", e.Port}, {"IdentityFile", e.IdentityFile},
	} {
		if f.v != "" {
			block = append(block, "  "+f.k+" "+formatValue(f.v))
		}
	}
	extras := make([]string, 0, len(e.ExtraDirectives))
	for k := range e.ExtraDirectives {
		extras = append(extras, k)
	}
	sort.Strings(extras)
	for _, k := range extras {
		block = append(block, "  "+k+" "+formatValue(e.ExtraDirectives[k]))
	}

	at := c.insertionPoint()
	var ins []string
	if at > 0 && strings.TrimSpace(c.lines[at-1]) != "" {
		ins = append(ins, "")
	}
	ins = append(ins, block...)
	if at < len(c.lines) {
		ins = append(ins, "")
	}
	c.lines = append(c.lines[:at], append(ins, c.lines[at:]...)...)
	return nil
}

// Rename changes the alias on a Host line, leaving the block body untouched.
func (c *Config) Rename(oldAlias, newAlias string) error {
	if err := ValidateAlias(newAlias); err != nil {
		return err
	}
	b, ok := c.findHost(oldAlias)
	if !ok {
		return errors.New("host not found: " + oldAlias)
	}
	if c.Has(newAlias) {
		return errors.New("a host with this alias already exists")
	}
	k, _, trailing, _ := parseLine(c.lines[b.start])
	c.lines[b.start] = indentOf(c.lines[b.start]) + k + " " + newAlias + trailing
	return nil
}

// Delete removes the first Host block with this alias and reports whether
// one was found. Comments outside the block are kept.
func (c *Config) Delete(alias string) bool {
	b, ok := c.findHost(alias)
	if !ok {
		return false
	}
	c.lines = append(c.lines[:b.start], c.lines[b.end:]...)
	at := b.start
	if at < len(c.lines) && strings.TrimSpace(c.lines[at]) == "" &&
		(at == 0 || strings.TrimSpace(c.lines[at-1]) == "") {
		c.lines = append(c.lines[:at], c.lines[at+1:]...)
	}
	if at >= len(c.lines) {
		for len(c.lines) > 0 && strings.TrimSpace(c.lines[len(c.lines)-1]) == "" {
			c.lines = c.lines[:len(c.lines)-1]
		}
	}
	return true
}

func (c *Config) String() string {
	prefix := ""
	if c.bom {
		prefix = utf8BOM
	}
	if len(c.lines) == 0 {
		return prefix
	}
	return prefix + strings.Join(c.lines, c.eol) + c.eol
}

// Save writes the config atomically. The first save by this process copies
// the file then on disk to path+".bak"; later saves leave that copy alone, so
// the backup always holds what the user had before elyms touched it.
func (c *Config) Save() error {
	if _, done := backedUp.Load(c.path); !done {
		if prev, err := os.ReadFile(c.path); err == nil {
			if err := os.WriteFile(c.path+".bak", prev, 0o600); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		backedUp.Store(c.path, true)
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".config-*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(c.String())
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), c.path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
