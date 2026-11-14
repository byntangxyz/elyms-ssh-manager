package keygen

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/crypto/ssh"
)

type KeyType string

const (
	TypeEd25519 KeyType = "ed25519"
	TypeRSA     KeyType = "rsa"
)

const rsaBits = 4096

var ErrKeyExists = errors.New("a key with that name already exists")

type KeyInfo struct {
	Name        string
	Path        string
	PubPath     string
	Type        KeyType
	Fingerprint string
	Comment     string
	HasPrivate  bool
}

var reservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// ValidateName accepts a bare file name only: no path parts, no characters
// Windows forbids, no reserved device names, and no ".pub" suffix.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("key name is required")
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 }) {
		return errors.New(`key name must not contain / \ : * ? " < > |`)
	}
	if name != strings.TrimSpace(name) || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return errors.New("key name must not start or end with a dot or space")
	}
	if strings.HasSuffix(strings.ToLower(name), ".pub") {
		return errors.New("key name must not end with .pub")
	}
	base := strings.ToLower(strings.SplitN(name, ".", 2)[0])
	if reservedNames[base] {
		return fmt.Errorf("%q is a reserved Windows name", name)
	}
	return nil
}

// GenerateKey writes an OpenSSH private key and its .pub into dir. It never
// overwrites: if either file exists it returns ErrKeyExists.
func GenerateKey(dir, name string, keyType KeyType, comment string) (*KeyInfo, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	comment = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(comment))

	var raw any
	switch keyType {
	case TypeEd25519:
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		raw = priv
	case TypeRSA:
		priv, err := rsa.GenerateKey(rand.Reader, rsaBits)
		if err != nil {
			return nil, err
		}
		raw = priv
	default:
		return nil, fmt.Errorf("unsupported key type %q", keyType)
	}

	block, err := ssh.MarshalPrivateKey(raw, comment)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		return nil, err
	}
	pubLine := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if comment != "" {
		pubLine += " " + comment
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	privPath := filepath.Join(dir, name)
	pubPath := privPath + ".pub"

	if err := writeNew(privPath, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}
	if err := restrictPrivate(privPath); err != nil {
		os.Remove(privPath)
		return nil, fmt.Errorf("restricting private key permissions: %w", err)
	}
	if err := writeNew(pubPath, []byte(pubLine+"\n"), 0o644); err != nil {
		os.Remove(privPath)
		return nil, err
	}

	return &KeyInfo{
		Name:        name,
		Path:        privPath,
		PubPath:     pubPath,
		Type:        keyType,
		Fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		Comment:     comment,
		HasPrivate:  true,
	}, nil
}

func writeNew(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if errors.Is(err, os.ErrExist) {
		return ErrKeyExists
	}
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return err
	}
	return nil
}

func typeFromSSH(t string) KeyType {
	switch {
	case t == "ssh-ed25519":
		return TypeEd25519
	case t == "ssh-rsa":
		return TypeRSA
	case strings.HasPrefix(t, "ecdsa-"):
		return "ecdsa"
	default:
		return KeyType(t)
	}
}

// ListKeys returns every parsable *.pub in dir, sorted by name. Files that
// are not valid public keys are skipped. A missing dir yields no keys.
func ListKeys(dir string) ([]*KeyInfo, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var keys []*KeyInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".pub") {
			continue
		}
		pubPath := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(pubPath)
		if err != nil {
			continue
		}
		pub, comment, _, _, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".pub")
		privPath := filepath.Join(dir, name)
		_, statErr := os.Stat(privPath)
		keys = append(keys, &KeyInfo{
			Name:        name,
			Path:        privPath,
			PubPath:     pubPath,
			Type:        typeFromSSH(pub.Type()),
			Fingerprint: ssh.FingerprintSHA256(pub),
			Comment:     comment,
			HasPrivate:  statErr == nil,
		})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Name < keys[j].Name })
	return keys, nil
}

// ReadPublicKey returns the public key line without its trailing newline.
func ReadPublicKey(pubPath string) (string, error) {
	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
