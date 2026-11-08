package snapshot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Sealer seals a secret with the host master key; *hostkeys.Keys is one (req_001 D-40).
type Sealer interface {
	SealSecret(plaintext, aad []byte) ([]byte, error)
	UnsealSecret(sealed, aad []byte) ([]byte, error)
}

// ErrNoCredentials means no bucket access key is stored.
var ErrNoCredentials = errors.New("snapshot: no bucket access key is stored; set it with local-server backup-credentials")

const sealedPrefix = "sealed:v1:"

// credentialsAAD binds the sealed value to its setting, so it cannot be copied into another.
var credentialsAAD = []byte("hst_setting:" + KeyS3SealedSecret)

func looksSealed(v string) bool {
	raw, ok := strings.CutPrefix(v, sealedPrefix)
	if !ok {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	return err == nil && len(b) >= sealOver+1
}

type storedCredentials struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

// SetCredentials seals the bucket access key with the host master key and stores it in the
// setting backup.s3.sealed_secret (req_001 D-40). The plain key is never stored or logged, and
// no error here carries it.
func SetCredentials(ctx context.Context, st *settings.Store, sealer Sealer, c Credentials) error {
	if c.AccessKeyID == "" || c.SecretAccessKey == "" {
		return errors.New("snapshot: the access key id and the secret are both required")
	}
	raw, err := json.Marshal(storedCredentials{AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey})
	if err != nil {
		return err
	}
	sealed, err := sealer.SealSecret(raw, credentialsAAD)
	if err != nil {
		return fmt.Errorf("snapshot: seal the bucket access key: %w", err)
	}
	return st.Set(ctx, settings.Host, KeyS3SealedSecret, "", sealedPrefix+base64.RawURLEncoding.EncodeToString(sealed))
}

// LoadCredentials opens the stored bucket access key. ErrNoCredentials means none is stored; any
// other error means the value cannot be opened with this host's master key.
func LoadCredentials(ctx context.Context, st *settings.Store, sealer Sealer) (Credentials, error) {
	v, err := st.String(ctx, KeyS3SealedSecret, "")
	if err != nil {
		return Credentials{}, err
	}
	if v == "" {
		return Credentials{}, ErrNoCredentials
	}
	raw, ok := strings.CutPrefix(v, sealedPrefix)
	if !ok {
		return Credentials{}, errors.New("snapshot: the stored bucket access key is not sealed; set it again with local-server backup-credentials")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return Credentials{}, errors.New("snapshot: the stored bucket access key is damaged; set it again with local-server backup-credentials")
	}
	plain, err := sealer.UnsealSecret(sealed, credentialsAAD)
	if err != nil {
		return Credentials{}, errors.New("snapshot: the stored bucket access key cannot be opened with this host's master key; set it again with local-server backup-credentials")
	}
	var sc storedCredentials
	if err := json.Unmarshal(plain, &sc); err != nil || sc.AccessKeyID == "" || sc.SecretAccessKey == "" {
		return Credentials{}, errors.New("snapshot: the stored bucket access key is damaged; set it again with local-server backup-credentials")
	}
	return Credentials{AccessKeyID: sc.AccessKeyID, SecretAccessKey: sc.SecretAccessKey}, nil
}
