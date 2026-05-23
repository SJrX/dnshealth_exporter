<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at specs/007-stealth-nameservers/plan.md
<!-- SPECKIT END -->

## Testing

Integration tests use in-process `miekg/dns` servers. No Docker needed.
Run with `go test -tags=integration ./...`. All test helpers live in
`testutil/`:

- `testutil/fixture.go` — `NewDNSFixture(t)`, `Server()`, `ReferralServer()`, `ServerWithOptions()`, `Start(t)`, `Stop()`, `Probe()`
- `testutil/records.go` — `SOA()`, `NS()`, `A()` (thin wrappers over `miekg/dns` with defaults-with-override)
- `testutil/assertions.go` — `AssertGauge()`, `AssertGaugeExists()`, `AssertGaugeMissing()`, `AssertGaugeInRange()`, `WithLabels()`, `WithValue()`

Read `.specify/memory/constitution.md` Principle VIII for the testing
philosophy. Read `specs/001-e2e-bootstrap/research.md` "Testing Approach"
section for implementation details and examples.
