//go:build windows

package keygen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateKeyIsAcceptedByOpenSSHPermissionCheck(t *testing.T) {
	keygenExe, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not installed")
	}
	// Temp dirs often carry extra inherited ACL entries; the key must not.
	dir := filepath.Join(t.TempDir(), "ssh")
	info, err := GenerateKey(dir, "id_acl", TypeEd25519, "acl@test")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(keygenExe, "-y", "-f", info.Path).CombinedOutput()
	if err != nil || strings.Contains(string(out), "bad permissions") {
		t.Fatalf("ssh-keygen rejected the generated key: %v\n%s", err, out)
	}
	if _, err := os.Stat(info.PubPath); err != nil {
		t.Fatal(err)
	}
}
