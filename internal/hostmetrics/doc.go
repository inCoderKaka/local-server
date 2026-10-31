// Package hostmetrics is part of the host module; see docs/modules/host.md.
//
// It reports what the host machine is using: the host totals, the share-budget slice (the
// local-server process, the Ollama processes and the models Ollama has loaded) and other apps
// (host use minus the slice), each value labelled with how it was measured (D-31). A Sampler
// takes a reading on a setting-driven ticker, sets the Prometheus gauges, keeps the latest
// reading in memory and writes one row per minute to hst_metric_minute (D-40).
package hostmetrics
