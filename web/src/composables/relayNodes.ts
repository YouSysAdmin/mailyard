// What the two relay-node pages share.
//
// There are two of them - the platform's listing under Admin and a
// project's own under Infrastructure - showing the same machines to
// different readers, and everything below was written out on both.
//
// The three were written out twice, once per page, identical apart from
// the api object and one word in a success message. They are the same
// three decisions either way: a node either may carry mail or may not,
// and removing one costs it its certificate.
//
// The API object is passed in rather than chosen here. A project admin
// acts through their own project's routes and a platform admin through
// the admin ones, and the two answer to different gates - deciding that
// from a flag inside a shared helper would put a permission question in
// the last place anybody looks for one.
import { ref } from 'vue'
import type { RelayNode } from '../api/types'
import { apiErrorMessage } from '../api/client'
import { useNotificationStore } from '../stores/notification'
import { useConfirm } from './useConfirm'

/** What the two api modules have in common, which is all this needs. */
export interface RelayNodeWrites {
  approve: (id: string) => Promise<unknown>
  suspend: (id: string) => Promise<unknown>
  remove: (id: string) => Promise<unknown>
}

export interface RelayNodeActions {
  /** The node whose request is in flight, or ''. */
  busy: ReturnType<typeof ref<string>>
  approve: (node: RelayNode) => Promise<void>
  suspend: (node: RelayNode) => Promise<void>
  remove: (node: RelayNode) => Promise<void>
}

/**
 * @param api      the write half of whichever relay-node module applies
 * @param reload   re-read the list, since every one of these changes it
 * @param carries  what the node is being approved to carry, for the
 *                 success line - "mail" on the platform page, "this
 *                 project's mail" on a project's own
 */
export function useRelayNodeActions(
  api: RelayNodeWrites,
  reload: () => Promise<void>,
  carries: string,
) {
  const notify = useNotificationStore()
  const { confirm } = useConfirm()
  const busy = ref('')

  async function run(node: RelayNode, act: () => Promise<unknown>, ok: string, failed: string) {
    busy.value = node.id
    try {
      await act()
      notify.success(ok)
      await reload()
    } catch (e) {
      notify.error(apiErrorMessage(e, failed))
    } finally {
      busy.value = ''
    }
  }

  async function approve(node: RelayNode) {
    await run(
      node,
      () => api.approve(node.id),
      `${node.name} can now carry ${carries}`,
      'Failed to approve the node',
    )
  }

  async function suspend(node: RelayNode) {
    await run(
      node,
      () => api.suspend(node.id),
      `${node.name} is no longer being given new mail`,
      'Failed to suspend the node',
    )
  }

  // The only one that asks first. Approving and suspending are both
  // reversible from this page; this one is not - the node loses its
  // certificate and has to enrol again.
  async function remove(node: RelayNode) {
    const ok = await confirm({
      title: 'Remove relay node',
      message: `Remove ${node.name}? It loses its certificate and would have to enrol again. Anything already queued on it still delivers.`,
      confirmText: 'Remove',
      variant: 'danger',
    })
    if (!ok) return

    await run(node, () => api.remove(node.id), 'Node removed', 'Failed to remove the node')
  }

  return { busy, approve, suspend, remove }
}

/**
 * The MX record set to publish for the domains whose mail should reach
 * these nodes.
 *
 * Shared because the FORMAT is a correctness detail and it was written
 * on both pages: the trailing dot makes the name absolute, and the equal
 * priorities are the point - the nodes are interchangeable and a sender
 * picking either is the redundancy. Changed on one page only, the two
 * would print different records for the same machines.
 */
export function mxRecordFor(hosts: string[]): string {
  return hosts.map((h) => `IN MX 10 ${h}.`).join('\n')
}
