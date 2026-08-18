// The server's field errors, put back where they belong.
//
// `internal/core/validation` has answered with
// `{"error": "...", "fields": [{"field", "rule", "message"}]}` since it
// was written, and its own comment says the array is there "so the SPA
// can incrementally adopt field-level rendering". The SPA never did:
// apiErrorMessage reads `.error` and nothing ever looked at `.fields`,
// so a refused field reported itself as one line at the top right of the
// screen, well away from the input that caused it, and a body with three
// bad fields reported them as one run-on sentence.
//
// `field` is the JSON name the request actually sent - validation
// registers a tag-name function for exactly that - so a form keyed by
// the same names needs no mapping table.

import { ref } from 'vue'

export interface FieldError {
  field: string
  rule: string
  message: string
}

interface ErrorBody {
  response?: { data?: { error?: string; fields?: FieldError[] } }
}

export function useFieldErrors() {
  const errors = ref<Record<string, string>>({})

  // True when the errors were placed on fields, which is the caller's
  // signal not to raise a toast as well: the message is already on
  // screen, next to the input, and saying it twice reads as two
  // problems.
  //
  // An entry with no `field` is what Humanize produces for a body that
  // did not decode at all. There is no input to blame for that, so it
  // stays a toast.
  function capture(err: unknown): boolean {
    const fields = (err as ErrorBody)?.response?.data?.fields
    if (!Array.isArray(fields)) return false
    const next: Record<string, string> = {}
    for (const fe of fields) {
      if (fe?.field && fe?.message) next[fe.field] = fe.message
    }
    errors.value = next

    return Object.keys(next).length > 0
  }

  // Clear one field as it is edited, or all of them when a request is
  // about to be made. Errors from the previous attempt outliving the
  // next one is how a form ends up refusing a value it already accepted.
  function clear(field?: string) {
    if (!field) {
      errors.value = {}

      return
    }
    if (errors.value[field]) delete errors.value[field]
  }

  return { errors, capture, clear }
}
