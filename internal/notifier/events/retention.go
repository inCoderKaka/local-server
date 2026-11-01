package events

import "github.com/inCoderKaka/local-server/internal/settings"

// KeyRetention is the host-owned setting for how long ntf_event rows are kept (D-83).
const KeyRetention = "events.retention"

// Defs are the events settings; register them with settings.Store.Register.
func Defs() []settings.Def {
	return []settings.Def{
		{Key: KeyRetention, Kind: settings.Duration, Default: "720h", Owner: settings.Host,
			Description: "How long domain events are kept before they are deleted. Zero or less keeps them forever."},
	}
}

// Rule prunes ntf_event rows older than KeyRetention (D-83). occurred_at is Unix milliseconds UTC.
//
// PLT-F-030 (outbox pruning) must keep outbox rows from outliving their event: it must not
// leave a row that points at an event this rule has deleted.
var Rule = settings.Rule{Table: "ntf_event", TimeColumn: "occurred_at", Setting: KeyRetention}

// RegisterRetention registers the events.retention setting and the retention rule of ntf_event
// with st.
func RegisterRetention(st *settings.Store) error {
	if err := st.Register(Defs()...); err != nil {
		return err
	}
	return st.RegisterRetention(Rule)
}
