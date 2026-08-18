// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package email is the send pipeline and the email log. The pieces:
//
//   - store.go: the emails table, which doubles as the delivery queue
//     (it implements core/queue.Source alongside the proj-scoped CRUD).
//   - service.go: validate -> persist -> wake the worker. Handlers
//     and (later) the machine API go through the Service, never
//     straight to the store.
//   - processor.go: worker-side delivery (pick server, build MIME,
//     send, classify the failure) implementing core/queue.Processor.
//   - endpoint.go: the /api/emails handlers.
package email
