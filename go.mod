module github.com/HelixDevelopment/HelixLLM

go 1.26.1

require (
	digital.vasic.challenges v0.0.0
	digital.vasic.concurrency v0.0.0
	digital.vasic.config v0.0.0
	digital.vasic.containers v0.0.0
	digital.vasic.eventbus v0.0.0
	digital.vasic.lazy v0.0.0
	digital.vasic.middleware v0.0.0
	digital.vasic.observability v0.0.0
	digital.vasic.watcher v0.0.0
	github.com/andybalholm/brotli v1.1.0
	github.com/gin-gonic/gin v1.10.0
	github.com/quic-go/quic-go v0.48.0
)

replace (
	digital.vasic.challenges => ./submodules/Challenges
	digital.vasic.concurrency => ./submodules/Concurrency
	digital.vasic.config => ./submodules/Config
	digital.vasic.containers => ./submodules/Containers
	digital.vasic.eventbus => ./submodules/EventBus
	digital.vasic.lazy => ./submodules/Lazy
	digital.vasic.middleware => ./submodules/Middleware
	digital.vasic.observability => ./submodules/Observability
	digital.vasic.watcher => ./submodules/Watcher
)
