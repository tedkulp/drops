# Glossary

## Project

A logical namespace for issues and memories that can exist in more than one store. A project keeps its identity when its human-facing name or repository locations change.

## Project key

The immutable, opaque identity of a project across stores and machines.

## Replica

An exclusive, monotonic lineage of a writable drops store. Processes share a replica only when they operate on the same serialized store; a copy that can be written independently is a different replica.

## Replica key

The immutable, opaque identity of one replica. A replica key orders concurrent record revisions and distinguishes an update from an identifier collision; it is not a project or machine identity.

## Project slug

The human-facing name used to address a project. A slug is unique within a store but is not the project's identity; renaming a project changes its slug without changing its project key.

## Workspace binding

A machine-local association between an existing, canonical absolute directory and a project key. A project can have multiple workspace bindings, but a path names at most one project. A Git-backed binding uses the main repository root so linked worktrees share it. Workspace bindings are not part of the project and do not cross stores or machines.

## Repository locator

A normalized, machine-independent Git remote name used to discover a project from an unbound checkout. A project can have multiple repository locators. A locator is evidence for association, not project identity.

## Reserved project

One of the built-in `global` or `inbox` projects. Each has a well-known project key and a fixed slug and role, so independently created stores converge on the same identity. Reserved projects cannot be renamed or archived.

## Memory

A durable notebook entry that an agent explicitly writes, lists, searches, shows, or edits. Every memory belongs to one project, but a search can span projects. A memory has a permanent opaque identity; it is not ranked or injected into a session automatically.

## Memory provenance

An optional free-text description of where a memory's knowledge came from, such as a skill, document, or migration. Provenance is independent of the project that owns the memory.

## Memory supersession

The replacement of one memory by another while preserving the old memory and its permanent identity. Superseded memories remain directly addressable but are absent from ordinary lists and searches.

## Replicated record

An atomic unit of state merged between stores. An issue, memory, or project is one replicated record; each label membership, dependency, comment, repository locator, and issue-parent relation is another. Concurrent changes do not merge individual fields within a record.

## Record revision

The generation and replica key attached to a replicated record. The generation advances from the version a writer observed; the replica key makes concurrent revisions deterministic without treating wall-clock time as authority.

## Tombstone

The versioned state that records an explicit removal. A tombstone participates in conflict resolution and preserves identity; absence from a mirror never means deletion.

## Issue parent

The single explicit parent relation of an issue. Parentage is independent of the issue's opaque ID, even when newly minted child IDs include their parent's ID for readability.

## ID ownership

The permanent reservation of one opaque ID to either an issue or a memory. Ownership survives tombstoning, so an ID can never change kind or be reused.

## Replica succession

The recorded replacement of one active replica key by another in the same store lineage. Succession affects future authorship only; it never rewrites existing record revisions or creation origins.

## Live issue

An issue that has not been tombstoned, whatever its status. Liveness is independent of whether an issue is open or closed: a closed issue is live, a tombstoned one is not.

## Open issue

A live issue whose status is `open` or `in_progress`. Being open is a claim about whether the work is finished, not about whether it has started, so an issue someone is part-way through is open.

## Ready issue

An open issue with no blocking dependency on another issue that is itself unfinished. Readiness is computed from the dependency graph rather than stored, so it changes when a blocker's status changes and never needs to be maintained.

## Blocked issue

An open issue with at least one blocking dependency on an unfinished issue. Blocked and ready are complementary properties of the same open set; neither is a status an issue can be put into.

## Related dependency

A non-blocking association between two issues. `A related B` is undirected to a reader even though its replicated record has `from` and `to` fields: A and B each read the other under `Related`, and neither end implies origin or precedence.

## Discovered-from dependency

A directed provenance relation between two issues. `A discovered-from B` means A was discovered or derived from B: A reads B under `Discovered from`, while B reads A under `Discovered`.

## Deferral

The convention of marking an issue as deliberately postponed. Deferral is expressed as an ordinary label and carries no meaning to the model: a deferred issue is an ordinary open issue, and nothing computes readiness or ordering differently because of it.
