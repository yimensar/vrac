# Vrac development

Vrac is a personal Markdown memory workspace with Go, SQLite FTS5, React and shadcn/ui. This branch replaces the previous Rust product at the owner's explicit request.

- Keep business rules in `internal/store`; HTTP and MCP use the same store API.
- Preserve Markdown, stable IDs, original creation dates, source metadata, attachments, and archived state during import. Import must be repeatable without duplicates.
- Use transactions, foreign keys, WAL and explicit schema migrations. Search indexes are rebuildable derived data.
- Never commit personal notes, attachments, credentials, local databases, or private benchmark excerpts. Use synthetic fixtures in Git.
- Check mutation versions to prevent lost updates. Errors must preserve the previous state.
- Keep the UI accessible and responsive. Verify desktop, tablet and phone layouts, including keyboard and touch interactions.
- Prefer simple concrete functions and maintained dependencies. Do not add services without a measured need.
- Run Go tests, the production front-end build, integration checks and relevant benchmarks before committing. Report measurement conditions and limitations.
- Use English for code and repository documentation. The product interface may use French.
- Work on a feature branch. Commit and push validated work; never push directly to main.
