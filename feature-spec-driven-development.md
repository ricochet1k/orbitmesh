# Feature Spec Driven Development

Welcome to the **OrbitMesh** project! To ensure our codebase is maintainable, our vision remains clear, and our architectures are sound, we follow a **Spec-Driven Development** methodology.

## The Core Rule
**Nothing should be in the codebase unless a feature spec requests it.**

## How It Works

1. **The Vision**: The top-level vision and architecture are documented in the root [`SPEC.md`](SPEC.md) file. This document acts as the entry point and index for all feature specifications.

2. **Feature Specs**:
   - Every new feature, infrastructure change, or major refactor begins as a markdown document in the `specs/features/` or `specs/future/` directory.
   - We maintain a comprehensive, hierarchical list of these features in `SPEC.md`. If a feature is not yet fully designed, it will be marked as "To Be Written" (TBW).
   - Once a feature has been designed and approved, its spec becomes the source of truth for the development team.

3. **Writing a Spec**:
   - All feature specs must be written using the provided [`specs/template.md`](specs/template.md) template.
   - The template ensures that we consistently think through the scope, user experience, system design, testing, and security of every change before a single line of code is written.

4. **Living Documents**:
   - Feature specs are **living documents**. They must be kept up to date.
   - If technical constraints require a change in direction during implementation, the feature spec **must be updated** to reflect this change *before* the code is merged.
   - The spec serves as the long-term documentation for why a feature exists and how it was implemented.

5. **Future Work**:
   - Ideas and designs for features that we do not plan to build immediately should be placed in the `specs/future/` directory. They can still be linked from `SPEC.md`, but they explicitly indicate that they are not currently scheduled for development.

By adhering to this process, we reduce technical debt, prevent scope creep, and ensure that every agent and contributor working on the project shares a single, unified understanding of what we are building.