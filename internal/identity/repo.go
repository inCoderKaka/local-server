package identity

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("identity: not found")

// Clock returns the current time; a repository calls it for every created_at and updated_at.
type Clock func() time.Time

func newID() (string, error) {
	u, err := uuid.NewV7()
	return u.String(), err
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func msp(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.UnixMilli()
	return &v
}

func tm(v int64) time.Time { return time.UnixMilli(v).UTC() }

func tmp(v *int64) *time.Time {
	if v == nil {
		return nil
	}
	t := tm(*v)
	return &t
}

// Repos holds every identity repository.
type Repos struct {
	Principals  *PrincipalRepo
	AdminUsers  *AdminUserRepo
	Consumers   *ConsumerRepo
	Credentials *CredentialRepo
	Invites     *InviteRepo
	AccessKeys  *AccessKeyRepo
	Sessions    *SessionRepo
	Consents    *ConsentRepo
}

// NewRepos builds the repositories. A nil clock means time.Now.
func NewRepos(clock Clock) *Repos {
	if clock == nil {
		clock = time.Now
	}
	return &Repos{
		Principals: &PrincipalRepo{clock}, AdminUsers: &AdminUserRepo{clock},
		Consumers: &ConsumerRepo{clock}, Credentials: &CredentialRepo{clock},
		Invites: &InviteRepo{clock}, AccessKeys: &AccessKeyRepo{clock},
		Sessions: &SessionRepo{clock}, Consents: &ConsentRepo{clock},
	}
}
