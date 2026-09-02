// Package catalogue records the models that could be run.
//
// Single responsibility: hold each model's capability family, resource
// requirements (memory AND storage, separately), usage terms, streaming-runtime
// roster membership, and expected integrity value.
//
// Roster membership is data, not code: a runtime release that adds a supported
// model family is a catalogue change, never a code change.
package catalogue
