# Contributing

1. Create a focused branch from `main`.
2. Run `gofmt` on changed Go files.
3. Run `go vet ./...` and `go test -race ./...`.
4. Keep production code dependency-free unless a dependency clearly improves safety.
5. Open a pull request describing behavior, tests, and any compatibility impact.

Tests must be deterministic and self-contained. Use `httptest` instead of public network
services, and use contexts or synchronization primitives instead of fixed sleeps where
practical.
