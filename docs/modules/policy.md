# policy module

## Purpose

Decides whether a request is allowed, at which priority class and under which caps; the gateway enforces the decision.

## Owns

- Packages: `internal/policy/` (built by PLT-F-011).
- Tables: none. Rules are stored in `grt_policy_rule` by the grants module, and the policy reads them through its `RuleSource` port.
- Port: `Policy` (`Evaluate(ctx, Input) (Decision, error)`). `Service` implements it: it reads the limits from settings and the rules from a `RuleSource` on every call, then runs `Decide`, the pure rule engine.
- Rule format: version 1, with its JSON schema in `internal/policy/rule_v1.schema.json`. The decoder in `internal/policy/rule.go` is the reference.
- Settings: `policy.request_properties.max_bytes`, `policy.request_properties.max_count` and `policy.rule.max_bytes`, all owned by the host.

### Decision

`Input` holds the `LS-Request-Properties` field lines as received, the engine model name, the time, and the caller's grant after the key's narrowing (id, allowed properties, models with their caps, concurrency). `Decide` checks, in order:

1. The properties header parses within its limits (`invalid_request_properties`).
2. The model is one of the grant's models (`model_not_granted`).
3. Every property is in the grant's allowed properties (`property_not_allowed`). No allowed list means any property, and an empty list means none.
4. Every enabled rule that is global or scoped to this grant, in position order.

A nil error means the request is allowed. `Decision` then holds the priority class and the caps. Every failure is an `*Error` with a `Code`, and its message never quotes a property value, a prompt or a response (D-31, D-40).

Rules combine as follows:

- **Refusals do not depend on order.** A matching rule with `deny` refuses (`policy_denied`). A matching rule with `windows` refuses outside all of its windows (`outside_time_window`), and the error's `Opens` says when that rule's windows next open. The first refusing rule by position is reported.
- **Caps only narrow.** Each cap is the smallest among the grant's value and every matching rule's value.
- **Priority:** the first matching rule by position that sets a priority decides. With none, the request is interactive.
- **A broken rule fails closed.** An enabled rule that does not parse fails every decision it takes part in (`invalid_policy_rule` or `unknown_rule_format_version`). A disabled rule is never parsed.

### Rule format, version 1

A rule is one JSON object. `format_version` is required, and the stored `grt_policy_rule.format_version` must equal it. Every other field is optional, but a rule needs at least one effect.

| Field | Meaning |
|---|---|
| `format_version` | `1`. Any other number is refused with `unknown_rule_format_version`, before the rest of the document is read. |
| `when.properties` | Property name to value: `true`/`false`, an integer, or a string. A string matches a token or a string of the same text. Every pair must hold. |
| `when.models` | Engine model names. The request's model must be one of them. |
| `deny` | `true` refuses every matching request. It cannot be combined with another effect. |
| `windows` + `time_zone` | Daily windows `{"start": "HH:MM", "end": "HH:MM"}`. The start is inclusive and the end exclusive. An end before the start crosses midnight, and an equal start and end are refused. The windows are read as wall-clock time in `time_zone`: an IANA name, `UTC`, or `Local` for the host machine's zone. A matching request outside every window is refused. |
| `priority` | `interactive` or `background`. |
| `caps` | `context_cap`, `max_output_tokens`, `max_concurrency`, each at least 1. |

Leaving out `when` matches every request the rule's scope covers. Its scope is global, or one grant through `grt_policy_rule.grant_id`. The decoder is stricter than JSON itself. It refuses unknown fields, duplicate keys, nulls, keys that are not lower case (which also blocks `encoding/json`'s case-insensitive field matching), invalid UTF-8, trailing data, empty `when`, `properties`, `models`, `windows` or `caps` objects and lists, and documents over `policy.rule.max_bytes`. Lowering that setting below an existing rule's size makes the rule fail closed until it is shortened.

Background-work hours (D-15), "background only 00:00–07:00":

```json
{"format_version": 1, "when": {"properties": {"background": true}},
 "time_zone": "Local", "windows": [{"start": "00:00", "end": "07:00"}], "priority": "background"}
```

**Versioning.** A stored document is never reinterpreted. A change that alters what an existing document means, or adds a field, gets a new `format_version` with its own decoder, and older versions keep their decoders. `ValidateRule` checks a document before it is stored or imported, and returns the version to store next to it.

### Request properties

`LS-Request-Properties` is an RFC 9651 Structured Field Dictionary (api/openapi.yaml). Keys are property names and a bare key means `true`. Values are tokens, strings, integers or booleans. The parser refuses everything else the RFC allows: decimals, byte sequences, dates, display strings, inner lists and parameters. It also refuses a key given twice. Several field lines are combined with commas, and empty lines count as absent. The header may hold at most `policy.request_properties.max_bytes` bytes, counted over every line received, and at most `policy.request_properties.max_count` properties. Zero refuses any properties. `FuzzParseProperties` and `FuzzRuleDocument` fuzz the two parsers (D-60), with seed corpora in `internal/policy/testdata/fuzz/`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters. policy imports settings and nothing else of local-server's. It does not import grants, so the module that wires it adapts `grants.PolicyRuleRepo` to `RuleSource`.

## Decisions

D-15, D-25, D-26, D-33, D-39, D-54, D-60. The module map is in `docs/design_docs/lld.md` section 1.
