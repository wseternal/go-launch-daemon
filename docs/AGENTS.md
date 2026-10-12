# Docs Module

The `docs/` directory holds the Architecture Decision Records (ADRs) of the project.

## Conventions

* Location: `docs/adrs/`.
* Naming: `ADR-NNN-short-title.md`. Numbers are sequential.
* Sections: Status, Context, Decision, Alternatives Considered, Consequences, References.
* Lifecycle: Proposed → Accepted → Superseded or Deprecated.
* Dates use ISO format (YYYY-MM-DD).
* The index table in `docs/adrs/README.md` lists every ADR.

## Rules

* Add an ADR for every significant technical decision.
* Never edit an Accepted ADR to change its decision. Write a new ADR that supersedes it.
* Update the index table in the same commit as the ADR.
