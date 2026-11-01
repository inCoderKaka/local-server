package stream

// The kinds of TimeoutError.
const (
	// TimeoutFirstToken is the time from the engine call until its first chunk.
	TimeoutFirstToken = "first_token"
	// TimeoutIdle is the longest gap between two engine chunks.
	TimeoutIdle = "idle"
	// TimeoutTotal is the whole engine call, from the request to its last chunk.
	TimeoutTotal = "total"
)

// TimeoutError reports that a streaming timeout expired and the engine call was cancelled.
type TimeoutError struct {
	// Kind is one of the Timeout constants.
	Kind string
}

func (e *TimeoutError) Error() string { return "stream: " + e.Kind + " timeout expired" }
