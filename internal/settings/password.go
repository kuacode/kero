package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Host passwords are encrypted in the configuration file, so that what is
// written down is not the password itself: a backup, a dotfiles repository or
// a screen share shows ciphertext instead of what one types.
//
// The key and the salt are fixed in the program, as the user asked. That
// makes this a modest shield, not a secret: anyone with this source (it is
// published) can derive the same key and undo it. It stops a glance and an
// accidental copy, not someone who goes looking. A password that predates
// this is still read as it lies and encrypted the next time it is saved.

const (
	// passwordPrefix marks an encrypted password in the file, so a plain
	// one from an older configuration can be told from ciphertext.
	passwordPrefix = "enc:v1:"

	// passwordKey and passwordSalt derive the file's AES-256 key. They are
	// constants on purpose; see the comment above the block.
	passwordKey  = "kero/host-password"
	passwordSalt = "kero/host-password/salt/v1"

	// passwordNonce is the GCM nonce, fixed like the salt. One password
	// always encrypts to the same value, which keeps the file from
	// changing when nothing about it did. It is 12 bytes, what GCM wants.
	passwordNonce = "kero/nonce01"
)

// passwordAEAD is the file's cipher: AES-256-GCM under the key the constants
// above derive.
var passwordAEAD = sync.OnceValue(func() cipher.AEAD {
	sum := sha256.Sum256([]byte(passwordKey + "\x00" + passwordSalt))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		panic(err) // the key length is fixed at 32 bytes
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return aead
})

// encryptPassword seals a password for the file.
func encryptPassword(plain string) (string, error) {
	aead := passwordAEAD()
	sealed := aead.Seal(nil, passwordNonceBytes(), []byte(plain), nil)
	return passwordPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// decryptPassword opens a password from the file. One without the prefix is
// a plain password an older configuration kept, and is taken as it lies.
func decryptPassword(stored string) (string, error) {
	if !strings.HasPrefix(stored, passwordPrefix) {
		return stored, nil
	}
	sealed, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, passwordPrefix))
	if err != nil {
		return "", fmt.Errorf("bad encrypted password: %w", err)
	}
	plain, err := passwordAEAD().Open(nil, passwordNonceBytes(), sealed, nil)
	if err != nil {
		return "", fmt.Errorf("bad encrypted password: %w", err)
	}
	return string(plain), nil
}

func passwordNonceBytes() []byte { return []byte(passwordNonce) }

// hostJSON is Host as the file holds it: the same fields, the password
// encrypted on the way out and opened on the way in. The memory copy keeps
// the plain password, which is what connecting needs.

type hostJSON struct {
	Name          string `json:"name"`
	Addr          string `json:"addr"`
	RemoteControl bool   `json:"remoteControl,omitempty"`
	Color         string `json:"color,omitempty"`
	Password      string `json:"password,omitempty"`
}

func (h Host) MarshalJSON() ([]byte, error) {
	w := hostJSON{
		Name:          h.Name,
		Addr:          h.Addr,
		RemoteControl: h.RemoteControl,
		Color:         h.Color,
	}
	if h.Password != "" {
		enc, err := encryptPassword(h.Password)
		if err != nil {
			return nil, err
		}
		w.Password = enc
	}
	return json.Marshal(w)
}

func (h *Host) UnmarshalJSON(b []byte) error {
	var w hostJSON
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	plain, err := decryptPassword(w.Password)
	if err != nil {
		return err
	}
	*h = Host{
		Name:          w.Name,
		Addr:          w.Addr,
		RemoteControl: w.RemoteControl,
		Color:         w.Color,
		Password:      plain,
	}
	return nil
}
