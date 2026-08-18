// Live project activity over server-sent events.
//
// EventSource authenticates with the session cookie, which the browser
// attaches to same-origin requests automatically. The server does not
// accept a token in the query string on purpose - that would write a
// JWT into every access log and Referer header.
//
// Note the project: EventSource cannot set headers, so the active
// project travels as a query parameter here, which the server
// accepts as a documented fallback to X-Mailyard-Project-Id.

export type StreamEventType =
  | 'system.connected'
  | 'system.reconnect'
  | 'email.sent'
  | 'email.failed'
  | 'email.queued'
  | 'email.inbound.received'
  | 'notification.created'
  | 'campaign.progress'

export interface StreamEvent {
  type: StreamEventType
  at?: string
  data?: Record<string, unknown>
}

export type StreamHandler = (e: StreamEvent) => void

// connectEventStream opens the feed and returns a closer.
//
// The browser reconnects on its own after a drop, so there is no retry
// loop here. The server also recycles a connection every 30 minutes,
// which arrives as a normal reconnect rather than an error.
export function connectEventStream(projectId: string | null, onEvent: StreamHandler): () => void {
  if (typeof window === 'undefined' || typeof EventSource === 'undefined') {
    return () => {}
  }
  const qs = projectId ? `?project_id=${encodeURIComponent(projectId)}` : ''
  const source = new EventSource(`/app/api/events/stream${qs}`, { withCredentials: true })

  const types: StreamEventType[] = [
    'system.connected',
    'system.reconnect',
    'email.sent',
    'email.failed',
    'email.queued',
    'email.inbound.received',
    'notification.created',
    'campaign.progress',
  ]
  for (const t of types) {
    source.addEventListener(t, (ev) => {
      try {
        const parsed = JSON.parse((ev as MessageEvent).data)
        onEvent({ type: t, at: parsed.at, data: parsed.data })
      } catch {
        // A frame we cannot parse is not worth breaking the stream
        // over - the durable tables are the authority either way.
      }
    })
  }

  return () => source.close()
}
