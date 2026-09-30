---
title: "API Reference"
description: "Every endpoint of /api/v1, rendered from the running binary's own OpenAPI description. Authenticate with an API key."
weight: 15
icon: code
layout: reference
openapi: api
toc: false
---

The reference is rendered from the OpenAPI document this instance generates from its own route metadata, so it
describes exactly the binary answering these pages - not a version of the docs written beside it.

**Try it** sends only what you put in the Authorization field, never the console session you are reading this
with - so an answer is the key's own, and a key limited to one permission is refused exactly as it would be from
your code. With the field empty every request answers 401. Keys are minted on the API Keys page.

The same document as a file, for a client generator or Postman:

```bash
mailyard export-api-spec --out openapi.yaml
```
