// Package stream is step 7 of the gateway pipeline (req_001 D-39): it turns an engine chat
// stream into the OpenAI server-sent-event stream of POST /v1/chat/completions with
// stream: true (D-32, D-33), and bounds the engine call with the three streaming timeouts.
//
// # Wire format
//
// Every chunk is one "data: {json}" event followed by a blank line. The first chunk carries
// the assistant role, each engine chunk with text one content delta, the last one the
// finish_reason. With stream_options.include_usage a chunk with empty choices and the usage
// follows. Then comes the signed receipt as the comment line ": ls-receipt <JWS>", which
// standard SSE clients skip (D-33), and finally "data: [DONE]". An error after streaming has
// started is a "data: {error envelope}" event, followed by the receipt of the failed call and
// "data: [DONE]". When the client has disconnected nothing more is written: its usage is
// metered as aborted and no receipt can be delivered (PLT-B-026).
//
// # Timeouts
//
// First token, idle and total are runtime settings of the host (gateway.stream_*); Open takes
// their values. Each one cancels the engine call and ends the stream with a *TimeoutError. A
// timeout before the first token is returned by Open, so the handler can still answer with an
// HTTP error; later ones become the error event.
//
// # Content
//
// The reply text passes through memory one chunk at a time and is never logged or stored
// (req_001 D-31, D-40). This package does not log at all.
//
// A Stream is used by one goroutine at a time: net/http's response encoder reads it and then
// closes it, in that order.
package stream
