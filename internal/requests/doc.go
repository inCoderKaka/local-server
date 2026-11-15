// Package requests is the requests module (docs/modules/requests.md, PLT-F-028): the resource
// requests a consumer sends the host, and the comments on them, behind the routes of the consumer
// API and of the admin API.
//
// # States
//
// A request is open until the host approves or rejects it, the consumer withdraws it, or it
// expires; every other state is final (req_001 D-38). A final request refuses a comment and a
// decision with a conflict (HTTP 409). Another consumer's request is not found, the same answer
// as a request that does not exist.
//
// # One unit of work
//
// Create, every change of a request's state and every comment is one unit of work run through the
// events Notifier: the row, the audit row of a decision, the domain event (resource_request.created
// or resource_request.decided) and the syn_change row commit together or not at all. Approve also
// makes the new grant version in that unit, through the ApplyGrant function the admin module
// passes, which writes the version, makes it current and emits grant.changed in the same unit
// (req_001 D-39 F3). This package imports no admin code and repeats none of its checks.
//
// # Settings
//
// requests.ttl, requests.max_open_per_consumer, requests.max_comment_chars (req_001 D-157),
// requests.sweep_interval and requests.page_size are host-owned and read at the moment of use.
// RunSweeper expires the open requests older than the TTL.
//
// # Content
//
// A request's message, its ask and its comments are the consumer's and the host's own text. They
// are stored and shown through the API but never logged, never put in an audit detail, an event
// payload or a syn_change row (req_001 D-31, D-40); every error has fixed text.
package requests
