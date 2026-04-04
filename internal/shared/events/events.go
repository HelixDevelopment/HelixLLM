// Package events provides an EventBus wrapper with HelixLLM-specific topic
// constants, thin constructors, and a simplified Publish API.
package events

import (
	"digital.vasic.eventbus/pkg/bus"
	"digital.vasic.eventbus/pkg/event"
)

// Topic is an alias for event.Type, used to name HelixLLM event topics.
type Topic = event.Type

// HelixLLM event topic constants.
const (
	TopicServerStarted  Topic = "helix.server.started"
	TopicServerStopped  Topic = "helix.server.stopped"
	TopicHealthChanged  Topic = "helix.health.changed"
	TopicConfigReloaded Topic = "helix.config.reloaded"
	TopicModeChanged    Topic = "helix.mode.changed"
)

// Bus wraps bus.EventBus with a simplified API.
type Bus struct {
	inner *bus.EventBus
}

// NewBus creates a new Bus with default configuration.
func NewBus() *Bus {
	return &Bus{inner: bus.New(bus.DefaultConfig())}
}

// Publish constructs an event and synchronously delivers it to all subscribers
// of the given topic.
func (b *Bus) Publish(topic Topic, source string, payload interface{}) {
	b.inner.Publish(event.New(topic, source, payload))
}

// PublishAsync constructs an event and delivers it asynchronously.
func (b *Bus) PublishAsync(topic Topic, source string, payload interface{}) {
	b.inner.PublishAsync(event.New(topic, source, payload))
}

// Subscribe returns a Subscription for the given topic.
// Receive events via sub.Channel; call sub.Cancel() when done.
func (b *Bus) Subscribe(topic Topic) *event.Subscription {
	return b.inner.Subscribe(topic)
}

// SubscribeAll returns a Subscription that receives every event.
func (b *Bus) SubscribeAll() *event.Subscription {
	return b.inner.SubscribeAll()
}

// Close shuts down the bus and closes all subscriber channels.
func (b *Bus) Close() error {
	return b.inner.Close()
}
