// Package grants is the grants module (docs/modules/grants.md): per-consumer grants, their
// immutable versions, the models a version allows and the policy rules, with the
// repositories over the grt_* tables (D-25, D-44, D-48).
//
// A grant version is never changed: a change makes a new version and moves the grant's
// current_version_id to it. The grt_* tables reference idn_consumer and idn_principal, so
// identity.Module() must be registered with storage before Module().
package grants
