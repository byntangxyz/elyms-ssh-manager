package keygen

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateEd25519WritesValidOpenSSHPair(t *testing.T) {
	dir := t.TempDir()
	info, err := GenerateKey(dir, "id_test", TypeEd25519, "me@box")
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != TypeEd25519 || info.Comment != "me@box" || info.Name != "id_test" {
		t.Errorf("info wrong: %+v", info)
	}
	if info.Path != filepath.Join(dir, "id_test") || info.PubPath != info.Path+".pub" {
		t.Errorf("paths wrong: %+v", info)
	}
	if !strings.HasPrefix(info.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint wrong: %q", info.Fingerprint)
	}

	priv, err := os.ReadFile(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(priv), "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Errorf("private key not in OpenSSH format")
	}
	signer, err := ssh.ParsePrivateKey(priv)
	if err != nil {
		t.Fatalf("private key does not parse: %v", err)
	}
	pubLine, err := ReadPublicKey(info.PubPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pubLine, "ssh-ed25519 ") || !strings.HasSuffix(pubLine, " me@box") {
		t.Errorf("public line wrong: %q", pubLine)
	}
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubLine))
	if err != nil {
		t.Fatal(err)
	}
	if string(pub.Marshal()) != string(signer.PublicKey().Marshal()) {
		t.Errorf("public key does not match private key")
	}
}

func TestGenerateRSAIs4096Bits(t *testing.T) {
	if testing.Short() {
		t.Skip("rsa 4096 is slow")
	}
	dir := t.TempDir()
	info, err := GenerateKey(dir, "id_rsa_test", TypeRSA, "")
	if err != nil {
		t.Fatal(err)
	}
	pubLine, _ := ReadPublicKey(info.PubPath)
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pubLine))
	if err != nil || pub.Type() != "ssh-rsa" {
		t.Fatalf("bad rsa public key: %v %v", pub, err)
	}
	cp, ok := pub.(ssh.CryptoPublicKey)
	if !ok {
		t.Fatal("no crypto public key")
	}
	type sizer interface{ Size() int }
	if s, ok := cp.CryptoPublicKey().(sizer); ok && s.Size() != 512 {
		t.Errorf("want 4096-bit key, got %d bytes", s.Size())
	}
}

func TestGenerateRefusesToOverwriteExistingKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := GenerateKey(dir, "id_x", TypeEd25519, ""); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "id_x"))
	_, err := GenerateKey(dir, "id_x", TypeEd25519, "")
	if !errors.Is(err, ErrKeyExists) {
		t.Fatalf("want ErrKeyExists, got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "id_x"))
	if string(before) != string(after) {
		t.Errorf("existing private key was modified")
	}
}

func TestGenerateRefusesWhenOnlyPubFileExists(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "id_y.pub"), []byte("x"), 0o644)
	if _, err := GenerateKey(dir, "id_y", TypeEd25519, ""); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("want ErrKeyExists, got %v", err)
	}
}

func TestGenerateRejectsUnsafeFilenames(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"", "  ", "../evil", `..\evil`, "a/b", `a\b`, "a:b", "id.pub", "con", "x*"} {
		if _, err := GenerateKey(dir, n, TypeEd25519, ""); err == nil {
			t.Errorf("filename %q should be rejected", n)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("nothing should have been written, got %d entries", len(entries))
	}
}

func TestGenerateRejectsUnknownKeyType(t *testing.T) {
	if _, err := GenerateKey(t.TempDir(), "id_z", KeyType("dsa"), ""); err == nil {
		t.Errorf("unknown key type should error")
	}
}

func TestGenerateCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh", ".ssh")
	if _, err := GenerateKey(dir, "id_new", TypeEd25519, ""); err != nil {
		t.Fatal(err)
	}
}

func TestListKeysFindsPairsAndSkipsGarbage(t *testing.T) {
	dir := t.TempDir()
	if _, err := GenerateKey(dir, "id_a", TypeEd25519, "a@x"); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateKey(dir, "id_b", TypeEd25519, "b@x"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "broken.pub"), []byte("not a key"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "config"), []byte("Host x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "lonely.pub"), mustPub(t, dir), 0o644)

	keys, err := ListKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]*KeyInfo{}
	for _, k := range keys {
		names[k.Name] = k
	}
	if len(keys) != 3 || names["id_a"] == nil || names["id_b"] == nil || names["lonely"] == nil {
		t.Fatalf("want id_a, id_b, lonely; got %d: %v", len(keys), names)
	}
	if names["id_a"].Comment != "a@x" || names["id_a"].Type != TypeEd25519 {
		t.Errorf("id_a parsed wrong: %+v", names["id_a"])
	}
	if names["id_a"].Path != filepath.Join(dir, "id_a") {
		t.Errorf("path wrong: %q", names["id_a"].Path)
	}
	if !names["id_a"].HasPrivate || names["lonely"].HasPrivate {
		t.Errorf("HasPrivate wrong: a=%v lonely=%v", names["id_a"].HasPrivate, names["lonely"].HasPrivate)
	}
}

func mustPub(t *testing.T, dir string) []byte {
	t.Helper()
	i, err := GenerateKey(t.TempDir(), "tmp", TypeEd25519, "")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(i.PubPath)
	return b
}

func TestListKeysOnMissingDirIsEmptyNotError(t *testing.T) {
	keys, err := ListKeys(filepath.Join(t.TempDir(), "absent"))
	if err != nil || len(keys) != 0 {
		t.Errorf("want empty/nil, got %v %v", keys, err)
	}
}

func TestReadPublicKeyTrimsNewline(t *testing.T) {
	p := filepath.Join(t.TempDir(), "k.pub")
	_ = os.WriteFile(p, []byte("ssh-ed25519 AAAA c\r\n"), 0o644)
	got, err := ReadPublicKey(p)
	if err != nil || got != "ssh-ed25519 AAAA c" {
		t.Errorf("got %q %v", got, err)
	}
}
