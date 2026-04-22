<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan
at specs/001-e2e-bootstrap/plan.md
<!-- SPECKIT END -->

## Testing

Integration tests use Docker Compose + CoreDNS fixtures. All test
helpers live in `testutil/`:

- `testutil/fixture.go` — `NewDNSFixture(t)`, `WriteZone()`, `Reload(t)`, `Probe()`
- `testutil/records.go` — `SOA()`, `NS()`, `A()`, `ZoneFile()` (thin wrappers over `miekg/dns`)
- `testutil/assertions.go` — `AssertGauge()`, `WithLabels()`, `WithValue()`

Read `.specify/memory/constitution.md` Principle VIII for the testing
philosophy. Read `specs/001-e2e-bootstrap/research.md` "Testing Approach"
section for implementation details and examples.
