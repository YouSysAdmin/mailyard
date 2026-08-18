// What a role selector offers.
//
// The same pair - an empty option naming what happens without a role,
// then one per role - was written out three times: the row selector, the
// add dialog and the invite dialog. The empty option is the part that
// matters and the part that drifted risk lies in: it has to say what
// leaving it alone MEANS, and that depends on whether the project has a
// default at all.
import type { ProjectRole } from '../../../api/types'

export interface RoleOption {
  value: string
  label: string
}

/**
 * The options, with the "no explicit role" one first.
 *
 * A project with no default role gives its members nothing, so the empty
 * option says so rather than reading as a harmless blank.
 */
export function roleOptions(roles: ProjectRole[], fallback: ProjectRole | null): RoleOption[] {
  const empty = fallback ? `project default (${fallback.name})` : 'no role - no access'

  return [{ value: '', label: empty }, ...roles.map((r) => ({ value: r.id, label: r.name }))]
}
