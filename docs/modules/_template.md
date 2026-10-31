# <module> module

Copy this file to `docs/modules/<module>.md` when a module is added to the `tools/archcheck` config. `archcheck` fails if a module has no note or a note lacks these four headings.

## Purpose

One or two sentences: what the module does and why it is its own module.

## Owns

- Packages: the directories the module owns (as in the archcheck config).
- Tables: the table prefix it owns (D-44), and its `migrations/` folder.
- Port: the interface it defines, if any, and the adapters behind it.

## Imports

Which other modules it may use, and which it must never touch (core modules import ports, never adapters, D-21).

## Decisions

The `D-NN` rows and ADRs that shape the module.
