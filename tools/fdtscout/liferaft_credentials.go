package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// LifeRaft credentials are a separate, reusable entity from jobs -- one saved login can back any
// number of jobs against the same source (confirmed with the user: "a single set of credentials
// may be used for multiple backup jobs... having them be user selectable to apply to other jobs
// will be useful"). Stored under /etc, same directory class as the Pushbullet and DDNS config --
// config.go's own documented convention is that credential-bearing config lives under /etc, not
// DataDir, and this follows it rather than inventing a new rule.
const (
	lifeRaftCredentialsFile = "/etc/fdtscout/liferaft-credentials.json"
	lifeRaftKeyFile         = "/etc/fdtscout/liferaft.key"
)

// LifeRaftCredential is protocol-agnostic on purpose -- a NAS exposing the same login over both
// SMB and FTP shouldn't need two saved credentials. Domain is SMB-only and simply ignored by an
// FTP job that references this credential.
type LifeRaftCredential struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Username  string    `json:"username"`
	Domain    string    `json:"domain,omitempty"`
	Password  string    `json:"password"` // encrypted-at-rest on disk; see note on masking below
	CreatedAt time.Time `json:"createdAt"`
}

// lifeRaftPasswordPlaceholder mirrors handlePushbulletUpdate/handleDDNSUpdate's own established
// pattern exactly: a saved secret is never echoed back over the API in full, and re-sending this
// exact placeholder on update means "leave it unchanged," not "set the password to this literal
// string." Worth restating plainly what this encryption is actually for, since it's easy to
// overstate: it's not a defense against someone with root on this box -- root already has
// everything, same reasoning already applied to Docker's "no image allow-list" design elsewhere in
// this codebase. It's so the credential never comes back over the API, never shows up in a casual
// `cat` of the config file, and doesn't leak if that file ever ends up in a support bundle.
const lifeRaftPasswordPlaceholder = "••••••••(unchanged)"

// loadOrCreateLifeRaftKey returns the per-device AES-256 key, generating and persisting one
// (mode 0600, same protection level the TLS private key already gets) on first use.
func loadOrCreateLifeRaftKey() ([]byte, error) {
	if data, err := os.ReadFile(lifeRaftKeyFile); err == nil && len(data) == 32 {
		return data, nil
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating encryption key: %w", err)
	}
	if err := os.MkdirAll("/etc/fdtscout", 0755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(lifeRaftKeyFile, key, 0600); err != nil {
		return nil, fmt.Errorf("saving encryption key: %w", err)
	}
	return key, nil
}

// encryptLifeRaftSecret/decryptLifeRaftSecret use stdlib AES-256-GCM rather than pulling in a new
// dependency for this -- golang.org/x/crypto is already in go.mod for other things, but the
// standard library's own crypto/cipher GCM mode is enough here and keeps this auditable without
// needing a second crypto library's own format conventions.
func encryptLifeRaftSecret(plaintext string) (string, error) {
	key, err := loadOrCreateLifeRaftKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func decryptLifeRaftSecret(encoded string) (string, error) {
	key, err := loadOrCreateLifeRaftKey()
	if err != nil {
		return "", err
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("stored credential is corrupt: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("stored credential is corrupt: too short")
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("couldn't decrypt stored credential (key file changed or file corrupt?): %w", err)
	}
	return string(plaintext), nil
}

func loadLifeRaftCredentialsRaw() ([]LifeRaftCredential, error) {
	data, err := os.ReadFile(lifeRaftCredentialsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []LifeRaftCredential{}, nil
		}
		return nil, err
	}
	var creds []LifeRaftCredential
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("couldn't parse %s: %w", lifeRaftCredentialsFile, err)
	}
	return creds, nil
}

func saveLifeRaftCredentialsRaw(creds []LifeRaftCredential) error {
	if err := os.MkdirAll("/etc/fdtscout", 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	tmp := lifeRaftCredentialsFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, lifeRaftCredentialsFile)
}

// ListLifeRaftCredentialsMasked is what the API ever returns -- Password is always the placeholder
// here, never the encrypted blob and never the plaintext.
func ListLifeRaftCredentialsMasked() ([]LifeRaftCredential, error) {
	creds, err := loadLifeRaftCredentialsRaw()
	if err != nil {
		return nil, err
	}
	out := make([]LifeRaftCredential, len(creds))
	for i, c := range creds {
		out[i] = c
		if out[i].Password != "" {
			out[i].Password = lifeRaftPasswordPlaceholder
		}
	}
	return out, nil
}

// DecryptedLifeRaftCredential returns the actual usable password -- for the sync engine's own use
// only (a source connection needs the real credential to authenticate), never exposed over the API.
func DecryptedLifeRaftCredential(id string) (username, domain, password string, err error) {
	creds, err := loadLifeRaftCredentialsRaw()
	if err != nil {
		return "", "", "", err
	}
	for _, c := range creds {
		if c.ID == id {
			pw, decErr := decryptLifeRaftSecret(c.Password)
			if decErr != nil {
				return "", "", "", decErr
			}
			return c.Username, c.Domain, pw, nil
		}
	}
	return "", "", "", fmt.Errorf("no such credential: %s", id)
}

func randomLifeRaftID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%x", prefix, b)
}

// SaveLifeRaftCredential creates a new credential (existing.ID == "") or updates one in place.
// Sending back lifeRaftPasswordPlaceholder as Password leaves the stored password untouched --
// exactly handlePushbulletUpdate's own "didn't retype it" handling, applied here too.
func SaveLifeRaftCredential(incoming LifeRaftCredential) (LifeRaftCredential, error) {
	incoming.Label = strings.TrimSpace(incoming.Label)
	incoming.Username = strings.TrimSpace(incoming.Username)
	if incoming.Label == "" || incoming.Username == "" {
		return LifeRaftCredential{}, fmt.Errorf("label and username are required")
	}

	creds, err := loadLifeRaftCredentialsRaw()
	if err != nil {
		return LifeRaftCredential{}, err
	}

	if incoming.ID == "" {
		if incoming.Password == "" || incoming.Password == lifeRaftPasswordPlaceholder {
			return LifeRaftCredential{}, fmt.Errorf("password is required for a new credential")
		}
		encrypted, err := encryptLifeRaftSecret(incoming.Password)
		if err != nil {
			return LifeRaftCredential{}, err
		}
		incoming.ID = randomLifeRaftID("cred")
		incoming.Password = encrypted
		incoming.CreatedAt = time.Now().UTC()
		creds = append(creds, incoming)
		if err := saveLifeRaftCredentialsRaw(creds); err != nil {
			return LifeRaftCredential{}, err
		}
		return incoming, nil
	}

	for i, existing := range creds {
		if existing.ID != incoming.ID {
			continue
		}
		if incoming.Password == "" || incoming.Password == lifeRaftPasswordPlaceholder {
			incoming.Password = existing.Password // untouched -- still the previously-encrypted value
		} else {
			encrypted, err := encryptLifeRaftSecret(incoming.Password)
			if err != nil {
				return LifeRaftCredential{}, err
			}
			incoming.Password = encrypted
		}
		incoming.CreatedAt = existing.CreatedAt
		creds[i] = incoming
		if err := saveLifeRaftCredentialsRaw(creds); err != nil {
			return LifeRaftCredential{}, err
		}
		return incoming, nil
	}
	return LifeRaftCredential{}, fmt.Errorf("no such credential: %s", incoming.ID)
}

// DeleteLifeRaftCredential is deliberately NOT exposed over the API yet. Deleting a credential
// that a scheduled job still references would break that job silently at its next scheduled run --
// exactly the kind of silent failure this whole feature is trying to design away from (see the
// LifeRaft design notes on why credential deletion needs an in-use check against the job list
// before it's safe to expose). That check belongs here once the job store exists; until then this
// function exists for the data layer to be complete, but handlers_liferaft_credentials.go
// deliberately has no DELETE route calling it.
func DeleteLifeRaftCredential(id string) error {
	creds, err := loadLifeRaftCredentialsRaw()
	if err != nil {
		return err
	}
	out := creds[:0]
	found := false
	for _, c := range creds {
		if c.ID == id {
			found = true
			continue
		}
		out = append(out, c)
	}
	if !found {
		return fmt.Errorf("no such credential: %s", id)
	}
	return saveLifeRaftCredentialsRaw(out)
}
