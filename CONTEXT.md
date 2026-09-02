# Glossary

## Project

A logical namespace for issues and memories that can exist in more than one store. A project keeps its identity when its human-facing name or repository locations change.

## Project key

The immutable, opaque identity of a project across stores and machines.

## Replica key

The immutable, opaque identity of one drops store installation. A replica key orders concurrent record revisions and distinguishes an update from an identifier collision; it is not a project or machine identity.

## Project slug

The human-facing name used to address a project. A slug is unique within a store but is not the project's identity; renaming a project changes its slug without changing its project key.

## Workspace binding

A machine-local association between an existing, canonical absolute directory and a project key. A project can have multiple workspace bindings, but a path names at most one project. A Git-backed binding uses the main repository root so linked worktrees share it. Workspace bindings are not part of the project and do not cross stores or machines.

## Repository locator

A normalized, machine-independent Git remote name used to discover a project from an unbound checkout. A project can have multiple repository locators. A locator is evidence for association, not project identity.

## Reserved project

One of the built-in `global` or `inbox` projects. Each has a well-known project key and a fixed slug and role, so independently created stores converge on the same identity. Reserved projects cannot be renamed or archived.
