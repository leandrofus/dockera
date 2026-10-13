package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"dockera/internal/i18n"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Secrets in config.json (session secret, endpoint tokens, registry
// passwords) are sealed with AES-256-GCM and the access token is stored as
// a hash. The key lives apart from the config, first match wins:
//   - $CREDENTIALS_DIRECTORY/dockera.key (systemd LoadCredential=; the
//     packaged service loads /etc/dockera/secret.key this way)
//   - $DOCKERA_KEY_FILE
//   - /etc/dockera/secret.key for the system instance (/var/lib/dockera)
//   - secret.key in the data directory (created on first use)
// A leaked or backed-up config.json alone reveals nothing; whoever can read
// the key on the host can still decrypt it.

const sealedPrefix = "enc:v1:"

const keyCredential = "dockera.key"

// KeyPath returns where the encryption key is read from.
func KeyPath() string {
	if d := os.Getenv("CREDENTIALS_DIRECTORY"); d != "" {
		if _, err := os.Stat(filepath.Join(d, keyCredential)); err == nil {
			return filepath.Join(d, keyCredential)
		}
	}
	if p := os.Getenv("DOCKERA_KEY_FILE"); p != "" {
		return p
	}
	if Dir() == SystemDir {
		if _, err := os.Stat(SystemKey); err == nil {
			return SystemKey
		}
	}
	return filepath.Join(Dir(), "secret.key")
}

const (
	SystemDir = "/var/lib/dockera"
	SystemKey = "/etc/dockera/secret.key"
)

// loadKey reads the key, creating a random one in the data directory the
// first time. The file holds 32 bytes, raw or hex/base64 encoded.
func loadKey() ([]byte, error) {
	p := KeyPath()
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) && p == filepath.Join(Dir(), "secret.key") {
		// a new key would make existing sealed values unreadable
		if cfg, _ := os.ReadFile(filepath.Join(Dir(), "config.json")); strings.Contains(string(cfg), sealedPrefix) {
			return nil, i18n.Errorf("encryption key not found (%s): the config has encrypted values; set DOCKERA_KEY_FILE or restore the key", p)
		}
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(hex.EncodeToString(key)+"\n"), 0o400); err != nil {
			return nil, err
		}
		FixOwner(p)
		return key, nil
	}
	if err != nil {
		return nil, i18n.Errorf("encryption key %s: %v", p, err)
	}
	return parseKey(p, data)
}

func parseKey(p string, data []byte) ([]byte, error) {
	if len(data) == 32 {
		return data, nil
	}
	s := strings.TrimSpace(string(data))
	if k, err := hex.DecodeString(s); err == nil && len(k) == 32 {
		return k, nil
	}
	if k, err := base64.StdEncoding.DecodeString(s); err == nil && len(k) == 32 {
		return k, nil
	}
	return nil, i18n.Errorf("encryption key %s: must be 32 bytes (raw, hex or base64)", p)
}

func gcm(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// seal encrypts a secret; empty values stay empty.
func seal(key []byte, plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	a, err := gcm(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return sealedPrefix + base64.StdEncoding.EncodeToString(a.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// unseal decrypts a sealed value. legacy reports a plaintext value written
// by an older version (it gets sealed on the next save).
func unseal(key []byte, s string) (plain string, legacy bool, err error) {
	if !strings.HasPrefix(s, sealedPrefix) {
		return s, s != "", nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, sealedPrefix))
	if err != nil {
		return "", false, err
	}
	a, err := gcm(key)
	if err != nil {
		return "", false, err
	}
	if len(data) < a.NonceSize() {
		return "", false, errors.New("short ciphertext")
	}
	out, err := a.Open(nil, data[:a.NonceSize()], data[a.NonceSize():], nil)
	if err != nil {
		return "", false, i18n.Errorf("cannot decrypt the config with the key %s (wrong key?)", KeyPath())
	}
	return string(out), false, nil
}

// HashToken is how the access token is stored: tokens are 192 random bits,
// so a plain SHA-256 is enough to make the stored value useless.
func HashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// CheckToken compares t with the stored access token in constant time.
func (c *Config) CheckToken(t string) bool {
	c.mu.Lock()
	want := c.TokenHash
	c.mu.Unlock()
	if t == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(HashToken(t)), []byte(want)) == 1
}

// SetToken replaces the access token (call inside Update).
func (c *Config) SetToken(t string) {
	c.TokenHash = HashToken(t)
	c.newToken = t
}

// FreshToken returns the access token in clear only when this process just
// generated or migrated it (the config keeps only its hash).
func (c *Config) FreshToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.newToken
}
