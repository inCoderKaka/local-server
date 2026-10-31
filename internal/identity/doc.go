// Package identity is the identity module (docs/modules/identity.md): principals, admin
// users, consumers, credentials, invites, access keys, sessions and consent records, with the
// repositories over the idn_* tables (D-25, D-42, D-44, D-53).
//
// Writes take the *storage.Tx of the caller's unit of work, so rows of other modules join
// it; reads take a *storage.Reader. No secret is stored: an access key keeps a prefix and a
// hash, an invite a prefix and a hash, a credential a hash or a public key (D-40). Producing
// the hashes is the auth module's job; this package stores what it is given.
package identity
