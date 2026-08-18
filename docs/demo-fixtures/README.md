# Demo fixtures

The coat files [`../demo.md`](../demo.md) serves and `cat`s. They are committed
because the demo cannot be regenerated without them, and because a `cat` block
in the document has to reproduce a file byte for byte.

Do not reformat or tidy them. Their contents appear verbatim in the document, so
any edit here shows up as a diff there and needs a regeneration to match:

```sh
scripts/regenerate-demo.sh --check   # what has drifted
scripts/regenerate-demo.sh           # rewrite the document
```

| File | Demonstrates |
|---|---|
| `basic.yaml` | Two coats, GET and POST on one URI |
| `patterns.yaml` | Glob, `**` multi-segment glob, and regex URIs |
| `sequences.yaml` | A cycling `responses` sequence |
| `headers.yaml` | Header matching with a glob value, and an unauthenticated fallthrough |
| `invalid.yaml` | **Deliberately invalid.** The demo runs `trenchcoat validate` against it to show the diagnostics. `trenchcoat validate docs/demo-fixtures/` therefore exits non-zero by design — do not "fix" this file, and do not point a validation check at this directory. |
