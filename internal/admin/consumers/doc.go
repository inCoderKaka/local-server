// Package consumers is the business logic behind the admin API's consumer, access key and invite
// operations (docs/modules/admin.md; req_001 D-25, D-38, D-40, D-54). It belongs to the admin
// module: package admin maps the HTTP shapes onto it and onto the error envelope.
//
// # Units of work
//
// Every write runs as one unit of work through the events Notifier's Write, together with its
// audit rows, so a change and its audit row commit or roll back together (req_001 D-07) and
// auth.Service.Revoke may emit access_key.revoked. A change to whether a key may authenticate
// (suspend, reactivate, delete, a key update) tells the auth cache to forget its entries after
// the unit has committed (Auth.Invalidate).
//
// # Audit rows keep no personal data
//
// Audit rows are kept forever, while req_001 D-40 erases a consumer's personal data on deletion.
// So a row's detail holds ids, kinds, key prefixes and field names only: never a display name,
// an email, a key name or an invite note, and never a key or a token.
//
// # Secrets
//
// IssueAccessKey and CreateInvite return their secrets as telemetry.Redacted, so a log line or an
// error that prints the result shows a placeholder. Only the caller that writes the response
// calls Reveal.
//
// # Errors
//
// A refusal is an *Error with a kind (invalid input, not found, conflict), a request field name
// and fixed text. The text never quotes the request. Any other error is an internal failure.
package consumers
