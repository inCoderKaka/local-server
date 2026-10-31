// Package auth is part of the auth module; see docs/modules/auth.md. It issues, revokes and
// authenticates access keys ("ls_<prefix>_<secret>"), resolves key -> consumer -> current grant
// version through a cache that change events clear, and applies a key's narrowing.
package auth
