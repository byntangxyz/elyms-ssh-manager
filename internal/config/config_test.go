package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `# global comment
ServerAliveInterval 30

# my web box
Host web
  HostName 10.0.0.1
  User root
  # keep this inner comment
  ForwardAgent yes

Host db
    HostName=10.0.0.2
    Port 2222
    IdentityFile "C:\Users\me\.ssh\id db"

Host *
  Compression yes
`

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestLoadParsesHostsAndDirectives(t *testing.T) {
	c, err := Load(writeTemp(t, sample))
	if err != nil {
		t.Fatal(err)
	}
	hosts := c.Hosts()
	if len(hosts) != 3 {
		t.Fatalf("want 3 hosts, got %d", len(hosts))
	}
	web := hosts[0]
	if web.Alias != "web" || web.HostName != "10.0.0.1" || web.User != "root" {
		t.Errorf("web parsed wrong: %+v", web)
	}
	if web.ExtraDirectives["ForwardAgent"] != "yes" {
		t.Errorf("extra directive lost: %+v", web.ExtraDirectives)
	}
	db := hosts[1]
	if db.HostName != "10.0.0.2" || db.Port != "2222" {
		t.Errorf("db key=value parse wrong: %+v", db)
	}
	if db.IdentityFile != `C:\Users\me\.ssh\id db` {
		t.Errorf("quoted identity file wrong: %q", db.IdentityFile)
	}
	if !hosts[2].IsPattern {
		t.Errorf("Host * must be flagged IsPattern")
	}
	if web.IsPattern || db.IsPattern {
		t.Errorf("plain hosts must not be IsPattern")
	}
}

func TestLoadCreatesMissingDirAndFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nope", ".ssh", "config")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Hosts()) != 0 {
		t.Errorf("want empty hosts")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("config file should have been created: %v", err)
	}
}

func TestAddAppendsWithoutTouchingExisting(t *testing.T) {
	const src = "# top\nHost a\n  HostName x\n"
	p := writeTemp(t, src)
	c, _ := Load(p)
	c.AddOrUpdate(&HostEntry{Alias: "new", HostName: "1.2.3.4", User: "me", Port: "2200"})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, p)
	if want := src + "\nHost new\n  HostName 1.2.3.4\n  User me\n  Port 2200\n"; got != want {
		t.Errorf("got:\n%s", got)
	}
}

func TestAddInsertsBeforeCatchAllHostSoItDoesNotOverrideTheNewBlock(t *testing.T) {
	p := writeTemp(t, sample)
	c, _ := Load(p)
	c.AddOrUpdate(&HostEntry{Alias: "new", HostName: "1.2.3.4", User: "me", Port: "2200"})
	_ = c.Save()
	want := strings.Replace(sample, "Host *\n", "Host new\n  HostName 1.2.3.4\n  User me\n  Port 2200\n\nHost *\n", 1)
	if got := readFile(t, p); got != want {
		t.Errorf("new block must come before Host *.\n%s", got)
	}
}

func TestAddInsertsAboveTheCommentThatBelongsToCatchAll(t *testing.T) {
	p := writeTemp(t, "Host a\n  HostName x\n\n# defaults\nHost *\n  User u\n")
	c, _ := Load(p)
	c.AddOrUpdate(&HostEntry{Alias: "n", HostName: "h"})
	_ = c.Save()
	want := "Host a\n  HostName x\n\nHost n\n  HostName h\n\n# defaults\nHost *\n  User u\n"
	if got := readFile(t, p); got != want {
		t.Errorf("got:\n%s", got)
	}
}

func TestAddWhenCatchAllIsFirstStillGoesBeforeIt(t *testing.T) {
	p := writeTemp(t, "Host *\n  User u\n")
	c, _ := Load(p)
	c.AddOrUpdate(&HostEntry{Alias: "n", HostName: "h"})
	_ = c.Save()
	if got, want := readFile(t, p), "Host n\n  HostName h\n\nHost *\n  User u\n"; got != want {
		t.Errorf("got:\n%s", got)
	}
}

func sshGOutput(t *testing.T, path, host string) string {
	t.Helper()
	exe, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("ssh not installed")
	}
	out, err := exec.Command(exe, "-F", path, "-G", host).CombinedOutput()
	if err != nil {
		t.Fatalf("real ssh rejected the config: %v\n%s\n--- config ---\n%s", err, out, readFile(t, path))
	}
	return strings.ReplaceAll(string(out), "\r\n", "\n")
}

func TestSpecialCharacterValuesRoundTripThroughRealSSH(t *testing.T) {
	for _, val := range []string{"#oops", `a"b`, "my user", `ends\`, "=lead", `C:\dir with space\`, `x#y`} {
		p := writeTemp(t, "Host other\n  HostName 1.1.1.1\n")
		c, _ := Load(p)
		if err := c.AddOrUpdate(&HostEntry{Alias: "box", HostName: "2.2.2.2", User: val}); err != nil {
			t.Fatalf("%q: %v", val, err)
		}
		if err := c.Save(); err != nil {
			t.Fatal(err)
		}
		sshGOutput(t, p, "other")
		if out := sshGOutput(t, p, "box"); !strings.Contains(out, "\nuser "+val+"\n") && !strings.HasPrefix(out, "user "+val+"\n") {
			t.Errorf("%q: ssh resolved a different user:\n%s", val, out)
		}
		c2, _ := Load(p)
		for _, h := range c2.Hosts() {
			if h.Alias == "box" && h.User != val {
				t.Errorf("%q: our parser read back %q", val, h.User)
			}
		}
	}
}

func TestAddOrUpdateRejectsControlCharactersAndLeavesConfigUntouched(t *testing.T) {
	p := writeTemp(t, sample)
	c, _ := Load(p)
	for _, e := range []*HostEntry{
		{Alias: "x", HostName: "h\nHost evil"},
		{Alias: "x", HostName: "h", User: "u\rx"},
		{Alias: "x", HostName: "h", IdentityFile: "k\x00"},
		{Alias: "web", HostName: "h", Port: "22\n"},
	} {
		if err := c.AddOrUpdate(e); err == nil {
			t.Errorf("%+v should be rejected", e)
		}
	}
	if c.String() != sample {
		t.Errorf("rejected entries must not change the config:\n%s", c.String())
	}
}

func TestParserStripsTrailingCommentsLikeOpenSSH(t *testing.T) {
	p := writeTemp(t, "Host web # prod\n  HostName 9.9.9.9 # primary\n  Port 22 # std\n  User \"a b\" # quoted\n")
	c, _ := Load(p)
	h := c.Hosts()
	if len(h) != 1 || h[0].Alias != "web" || h[0].IsPattern {
		t.Fatalf("Host line comment misread: %+v", h)
	}
	if h[0].HostName != "9.9.9.9" || h[0].Port != "22" || h[0].User != "a b" {
		t.Errorf("values wrong: %+v", h[0])
	}
}

func TestUpdateKeepsTrailingCommentOnRewrittenLine(t *testing.T) {
	p := writeTemp(t, "Host web\n  Port 22 # std\n")
	c, _ := Load(p)
	_ = c.AddOrUpdate(&HostEntry{Alias: "web", Port: "2200"})
	_ = c.Save()
	if got, want := readFile(t, p), "Host web\n  Port 2200 # std\n"; got != want {
		t.Errorf("got %q", got)
	}
}

func TestRenameKeepsTrailingCommentOnHostLine(t *testing.T) {
	p := writeTemp(t, "Host web # prod\n  HostName h\n")
	c, _ := Load(p)
	if err := c.Rename("web", "web2"); err != nil {
		t.Fatal(err)
	}
	_ = c.Save()
	if got, want := readFile(t, p), "Host web2 # prod\n  HostName h\n"; got != want {
		t.Errorf("got %q", got)
	}
}

func TestBOMIsReadAndPreserved(t *testing.T) {
	p := writeTemp(t, "\xef\xbb\xbfHost a\n  HostName x\n")
	c, _ := Load(p)
	if h := c.Hosts(); len(h) != 1 || h[0].Alias != "a" {
		t.Fatalf("first host hidden by BOM: %+v", h)
	}
	_ = c.AddOrUpdate(&HostEntry{Alias: "b", HostName: "y"})
	_ = c.Save()
	got := readFile(t, p)
	if !strings.HasPrefix(got, "\xef\xbb\xbfHost a\n") || strings.Count(got, "\xef\xbb\xbf") != 1 {
		t.Errorf("BOM must be kept exactly once:\n%q", got)
	}
}

func TestDuplicateAliasesAreFlagged(t *testing.T) {
	c, _ := Load(writeTemp(t, "Host a\n  HostName 1\nHost b\n  HostName 3\nHost a\n  HostName 2\n"))
	h := c.Hosts()
	if len(h) != 3 || !h[0].Duplicate || h[1].Duplicate || !h[2].Duplicate {
		t.Errorf("Duplicate flags wrong: %v %v %v", h[0].Duplicate, h[1].Duplicate, h[2].Duplicate)
	}
}

func TestBackupKeepsTheOriginalAcrossRepeatedSaves(t *testing.T) {
	p := writeTemp(t, sample)
	for i := 0; i < 3; i++ {
		c, _ := Load(p)
		_ = c.AddOrUpdate(&HostEntry{Alias: "h" + string(rune('a'+i)), HostName: "x"})
		if err := c.Save(); err != nil {
			t.Fatal(err)
		}
	}
	if got := readFile(t, p+".bak"); got != sample {
		t.Errorf("first-of-session backup must survive later saves, got:\n%s", got)
	}
}

func TestUpdatePreservesCommentsAndExtras(t *testing.T) {
	p := writeTemp(t, sample)
	c, _ := Load(p)
	c.AddOrUpdate(&HostEntry{Alias: "web", HostName: "10.0.0.1", User: "admin", Port: "2200"})
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, p)
	for _, must := range []string{
		"# global comment", "# my web box", "# keep this inner comment",
		"ForwardAgent yes", "  User admin", "  Port 2200", "HostName=10.0.0.2",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("missing %q in:\n%s", must, got)
		}
	}
	if strings.Contains(got, "User root") {
		t.Errorf("old user should be replaced:\n%s", got)
	}
}

func TestUpdateWithEmptyFieldRemovesDirective(t *testing.T) {
	p := writeTemp(t, sample)
	c, _ := Load(p)
	c.AddOrUpdate(&HostEntry{Alias: "db", HostName: "10.0.0.2", Port: ""})
	_ = c.Save()
	got := readFile(t, p)
	if strings.Contains(got, "Port 2222") || strings.Contains(got, "id db") {
		t.Errorf("empty fields should remove their directives:\n%s", got)
	}
}

func TestDeleteRemovesOnlyThatBlock(t *testing.T) {
	p := writeTemp(t, sample)
	c, _ := Load(p)
	if !c.Delete("web") {
		t.Fatal("Delete should report true")
	}
	_ = c.Save()
	got := readFile(t, p)
	if strings.Contains(got, "Host web") || strings.Contains(got, "ForwardAgent") {
		t.Errorf("web block should be gone:\n%s", got)
	}
	for _, must := range []string{"# global comment", "Host db", "Host *", "Compression yes"} {
		if !strings.Contains(got, must) {
			t.Errorf("missing %q:\n%s", must, got)
		}
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("delete must not leave stacked blank lines:\n%q", got)
	}
}

func TestDeleteUnknownAliasReturnsFalse(t *testing.T) {
	c, _ := Load(writeTemp(t, sample))
	if c.Delete("ghost") {
		t.Errorf("Delete of unknown alias must return false")
	}
}

func TestSaveWritesBackupOfPreviousContent(t *testing.T) {
	p := writeTemp(t, sample)
	c, _ := Load(p)
	c.Delete("db")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p+".bak"); got != sample {
		t.Errorf("backup should hold the original content, got:\n%s", got)
	}
}

func TestCRLFIsPreserved(t *testing.T) {
	crlf := strings.ReplaceAll(sample, "\n", "\r\n")
	p := writeTemp(t, crlf)
	c, _ := Load(p)
	c.AddOrUpdate(&HostEntry{Alias: "new", HostName: "h"})
	_ = c.Save()
	got := readFile(t, p)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Errorf("file must use CRLF consistently:\n%q", got)
	}
}

func TestMatchBlockIsNotSwallowedByPrecedingHost(t *testing.T) {
	src := "Host a\n  HostName 1.1.1.1\n\nMatch host b\n  User x\n"
	p := writeTemp(t, src)
	c, _ := Load(p)
	c.Delete("a")
	_ = c.Save()
	got := readFile(t, p)
	if !strings.Contains(got, "Match host b\n  User x") {
		t.Errorf("Match block must survive:\n%s", got)
	}
}

func TestMultiAliasHostIsNotConnectable(t *testing.T) {
	c, _ := Load(writeTemp(t, "Host a b\n  HostName x\n"))
	h := c.Hosts()
	if len(h) != 1 || !h[0].IsPattern {
		t.Errorf("multi-alias Host must be flagged IsPattern: %+v", h)
	}
}

func TestEmptyFileAddProducesNoLeadingBlank(t *testing.T) {
	p := writeTemp(t, "")
	c, _ := Load(p)
	c.AddOrUpdate(&HostEntry{Alias: "x", HostName: "h"})
	_ = c.Save()
	if got := readFile(t, p); got != "Host x\n  HostName h\n" {
		t.Errorf("got %q", got)
	}
}

func TestGetPathsUseHomeDir(t *testing.T) {
	dir, err := GetSSHDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := GetConfigPath()
	if filepath.Base(dir) != ".ssh" || filepath.Dir(cfg) != dir || filepath.Base(cfg) != "config" {
		t.Errorf("bad paths: %s %s", dir, cfg)
	}
}

func TestHasReportsExistingAliasOnly(t *testing.T) {
	c, _ := Load(writeTemp(t, sample))
	if !c.Has("web") || c.Has("ghost") {
		t.Errorf("Has wrong")
	}
	if c.Has("*") == false {
		t.Errorf("Has should see the Host * block too")
	}
}

func TestValidateAlias(t *testing.T) {
	bad := []string{"", "  ", "a b", "web*", "a?b", "!x", "tab\tname", "-oProxyCommand=evil",
		`we"b`, "a'b", "a,b", "a;b", "$x", "a=b", "a/b", `a\b`, "a&b", "root@srv", "a#b", "a\nb"}
	for _, a := range bad {
		if ValidateAlias(a) == nil {
			t.Errorf("alias %q should be rejected", a)
		}
	}
	for _, a := range []string{"web", "10.0.0.1", "my-box_1", "prod.example.com"} {
		if err := ValidateAlias(a); err != nil {
			t.Errorf("alias %q should be accepted: %v", a, err)
		}
	}
}

func TestRenameChangesOnlyTheHostLineAndKeepsExtras(t *testing.T) {
	p := writeTemp(t, sample)
	c, _ := Load(p)
	if err := c.Rename("web", "web2"); err != nil {
		t.Fatal(err)
	}
	_ = c.Save()
	got := readFile(t, p)
	want := strings.Replace(sample, "Host web\n", "Host web2\n", 1)
	if got != want {
		t.Errorf("rename should touch only the Host line.\n%s", got)
	}
}

func TestRenameRefusesExistingOrMissingOrInvalid(t *testing.T) {
	c, _ := Load(writeTemp(t, sample))
	if c.Rename("web", "db") == nil {
		t.Errorf("renaming onto an existing alias must fail")
	}
	if c.Rename("ghost", "x") == nil {
		t.Errorf("renaming a missing alias must fail")
	}
	if c.Rename("web", "bad name") == nil {
		t.Errorf("renaming to an invalid alias must fail")
	}
	if !c.Has("web") {
		t.Errorf("failed renames must not change anything")
	}
}

func TestValidateTargetAcceptsExistingRealWorldAliasesButBlocksInjection(t *testing.T) {
	for _, ok := range []string{"prod:22", "fe80::1", "host.example.com", "10.0.0.1", "root@srv", "my-box_1"} {
		if err := ValidateTarget(ok); err != nil {
			t.Errorf("%q should be connectable: %v", ok, err)
		}
	}
	for _, bad := range []string{"", " ", "-oProxyCommand=calc", "-v", "a b", "web*", "a?b", "!x", `a"b`, "a'b", "a\nb", "a\x00b", "a\tb"} {
		if ValidateTarget(bad) == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}
