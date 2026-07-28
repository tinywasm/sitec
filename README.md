# ssr
<img src="docs/img/badges.svg">

SSR asset extractor for TinyWasm: runs components Render methods and collects CSS/JS/HTML/SVG for assetmin.

Module discovery is delegated to [modfind](https://github.com/tinywasm/modfind), ensuring shared and cached module lookups across TinyWasm tools.

## Documentation

- [ARCHITECTURE.md](docs/ARCHITECTURE.md) — what the module is and why: the producer contract, the extraction model, merge and ordering guarantees, and the failure posture.
- [SPECS.md](docs/SPECS.md) — exact detection rules, merge semantics, error conditions, and the author contract.
- [DESIGN.md](docs/DESIGN.md) — the reasoning behind each decision and the alternatives rejected.
- [DOCUMENTATION.md](docs/DOCUMENTATION.md) — documentation standards for this repository.
- [diagrams/EXTRACTION.md](docs/diagrams/EXTRACTION.md) — the extraction pipeline, and where assets are lost today.
