# Agent instructions

- Keep RiffHero Linux-first and minimal.
- Domain packages must remain independent of UI/audio/file-format adapters.
- Use sample frames as the authoritative time coordinate.
- Never perform allocations, FFTs, logging or blocking operations in the real-time audio callback.
- Prefer deterministic unit tests and synthetic fixtures before hardware tests.
- Do not add ML, a backend, Electron, React or DAW features without a concrete practice requirement.
- Before adding a dependency, explain why the stdlib/current stack is insufficient.
- Keep commits/changes phase-scoped according to PLAN.md.
