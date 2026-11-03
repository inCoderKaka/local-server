package credential

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// Keys of the password settings. All are owned by the host (req_001 D-38, D-54) and read at the
// moment of use, so a change applies to the next hash. A hash that is already stored keeps the
// cost it was made with and still verifies (Verify reads the cost from the hash).
const (
	KeyMemoryKiB   = "auth.password.memory_kib"
	KeyIterations  = "auth.password.iterations"
	KeyParallelism = "auth.password.parallelism"
	KeyMinLength   = "auth.password.min_length"
)

// OWASP's minimum argon2id cost (req_001 D-25): m = 19 MiB, t = 2, p = 1. The settings default
// to it and refuse a value below it.
const (
	MinMemoryKiB   = 19456
	MinIterations  = 2
	MinParallelism = 1
)

// The largest cost this package hashes with or accepts in a stored hash. A host can raise the
// cost freely above the minimum (req_001 D-54), but a value that no machine could verify, or
// that a damaged row could turn into a huge allocation, is refused: a hash that cannot be
// verified would lock the host out.
const (
	MaxMemoryKiB   = 1 << 20 // 1 GiB
	MaxIterations  = 64
	MaxParallelism = 64
)

const (
	saltLen = 16
	keyLen  = 32
)

// Errors returned by this package.
var (
	// ErrMalformedHash means a stored hash is not a PHC argon2id string this package reads.
	ErrMalformedHash = errors.New("credential: malformed password hash")
	// ErrPasswordTooShort means the password is shorter than auth.password.min_length.
	ErrPasswordTooShort = errors.New("credential: password is too short")
)

// Params is the argon2id cost.
type Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// PasswordDefs are the password settings; register them with settings.Store.Register.
func PasswordDefs() []settings.Def {
	h := settings.Host
	return []settings.Def{
		{Key: KeyMemoryKiB, Kind: settings.Int, Default: strconv.Itoa(MinMemoryKiB), Owner: h,
			Description: "Memory, in KiB, that hashing or checking one password uses (argon2id). It cannot be set below 19456 (19 MiB, OWASP's minimum) or above 1048576. Applies to passwords set from now on; a stored password keeps the cost it was made with."},
		{Key: KeyIterations, Kind: settings.Int, Default: strconv.Itoa(MinIterations), Owner: h,
			Description: "Passes over memory when hashing a password (argon2id). It cannot be set below 2 (OWASP's minimum) or above 64. Applies to passwords set from now on."},
		{Key: KeyParallelism, Kind: settings.Int, Default: strconv.Itoa(MinParallelism), Owner: h,
			Description: "Threads hashing one password uses (argon2id). It cannot be set below 1 (OWASP's minimum) or above 64. Applies to passwords set from now on."},
		{Key: KeyMinLength, Kind: settings.Int, Default: "12", Owner: h,
			Description: "Fewest characters a new password must have."},
	}
}

// RegisterPassword registers the password settings and the validators that keep the cost within
// [OWASP's minimum, the safe maximum] and the minimum length at 1 or more.
func RegisterPassword(st *settings.Store) error {
	if err := st.Register(PasswordDefs()...); err != nil {
		return err
	}
	for _, r := range []struct {
		key      string
		min, max int64
	}{
		{KeyMemoryKiB, MinMemoryKiB, MaxMemoryKiB},
		{KeyIterations, MinIterations, MaxIterations},
		{KeyParallelism, MinParallelism, MaxParallelism},
		{KeyMinLength, 1, 1 << 20},
	} {
		key, lo, hi := r.key, r.min, r.max
		err := st.SetValidator(key, func(_ context.Context, v string) error {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			if n < lo || n > hi {
				return fmt.Errorf("must be between %d and %d", lo, hi)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Hasher hashes and checks passwords with the cost from the host settings.
type Hasher struct{ st *settings.Store }

// NewHasher registers the password settings with st and returns the hasher.
func NewHasher(st *settings.Store) (*Hasher, error) {
	if err := RegisterPassword(st); err != nil {
		return nil, err
	}
	return &Hasher{st: st}, nil
}

// CurrentParams reads the cost settings.
func (h *Hasher) CurrentParams(ctx context.Context) (Params, error) {
	m, err := h.st.Int(ctx, KeyMemoryKiB, "")
	if err != nil {
		return Params{}, err
	}
	t, err := h.st.Int(ctx, KeyIterations, "")
	if err != nil {
		return Params{}, err
	}
	p, err := h.st.Int(ctx, KeyParallelism, "")
	if err != nil {
		return Params{}, err
	}
	// A stored value written around the validators is clamped to the safe range, never trusted.
	return Params{
		MemoryKiB:   uint32(min(max(m, MinMemoryKiB), MaxMemoryKiB)),
		Iterations:  uint32(min(max(t, MinIterations), MaxIterations)),
		Parallelism: uint8(min(max(p, MinParallelism), MaxParallelism)),
	}, nil
}

// CheckPolicy reports ErrPasswordTooShort when the password has fewer characters than
// auth.password.min_length. There is no maximum here: the request body limit bounds it.
func (h *Hasher) CheckPolicy(ctx context.Context, password string) error {
	n, err := h.st.Int(ctx, KeyMinLength, "")
	if err != nil {
		return err
	}
	if int64(utf8.RuneCountInString(password)) < n {
		return ErrPasswordTooShort
	}
	return nil
}

// Hash returns the PHC string of password: $argon2id$v=19$m=..,t=..,p=..$salt$hash, with a fresh
// 16-byte random salt and a 32-byte key, at the current cost.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	p, err := h.CurrentParams(ctx)
	if err != nil {
		return "", err
	}
	return HashWith(password, p)
}

// HashWith hashes at an explicit cost. The cost is checked against the safe range.
func HashWith(password string, p Params) (string, error) {
	if err := p.check(); err != nil {
		return "", err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("credential: read random salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.MemoryKiB, p.Iterations, p.Parallelism,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func (p Params) check() error {
	if p.MemoryKiB < 8*uint32(max(p.Parallelism, 1)) || p.MemoryKiB > MaxMemoryKiB ||
		p.Iterations < 1 || p.Iterations > MaxIterations || p.Parallelism < 1 || p.Parallelism > MaxParallelism {
		return ErrMalformedHash
	}
	return nil
}

// Verify reports whether password matches the PHC string. It reads the cost from the string, so
// a hash made before a cost change still verifies, and compares in constant time. A string that
// is not a well-formed argon2id hash, or whose cost is outside the safe range, returns
// ErrMalformedHash without hashing anything.
func Verify(password, encoded string) (bool, error) {
	p, salt, want, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// Burn does the work of one Verify at the current cost and discards it. A login for a name that
// does not exist calls it, so that it takes as long as a wrong password and the response time
// does not reveal which names exist.
func (h *Hasher) Burn(ctx context.Context, password string) error {
	p, err := h.CurrentParams(ctx)
	if err != nil {
		return err
	}
	_ = argon2.IDKey([]byte(password), dummySalt[:], p.Iterations, p.MemoryKiB, p.Parallelism, keyLen)
	return nil
}

var dummySalt = [saltLen]byte{'l', 'o', 'c', 'a', 'l', '-', 's', 'e', 'r', 'v', 'e', 'r', '-', 'n', 'o', 'p'}

func parseHash(encoded string) (p Params, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, nil, nil, ErrMalformedHash
	}
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return p, nil, nil, ErrMalformedHash
	}
	var m, t, par uint64
	fields := strings.Split(parts[3], ",")
	if len(fields) != 3 {
		return p, nil, nil, ErrMalformedHash
	}
	for i, f := range fields {
		name, val, ok := strings.Cut(f, "=")
		if !ok || name != []string{"m", "t", "p"}[i] || val == "" || len(val) > 10 {
			return p, nil, nil, ErrMalformedHash
		}
		n, perr := strconv.ParseUint(val, 10, 32)
		if perr != nil {
			return p, nil, nil, ErrMalformedHash
		}
		switch i {
		case 0:
			m = n
		case 1:
			t = n
		case 2:
			par = n
		}
	}
	if m > MaxMemoryKiB || t > MaxIterations || par > MaxParallelism {
		return p, nil, nil, ErrMalformedHash
	}
	p = Params{MemoryKiB: uint32(m), Iterations: uint32(t), Parallelism: uint8(par)}
	if err := p.check(); err != nil {
		return Params{}, nil, nil, err
	}
	enc := base64.RawStdEncoding.Strict()
	if len(parts[4]) > 128 || len(parts[5]) > 256 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	salt, err = enc.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	key, err = enc.DecodeString(parts[5])
	if err != nil || len(key) < 16 {
		return Params{}, nil, nil, ErrMalformedHash
	}
	return p, salt, key, nil
}
