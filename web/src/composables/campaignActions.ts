// What can be done to a campaign, from either page that offers it.
//
// The list and the detail page both carry all six, written out twice -
// including three confirmations worded twice, which is three chances for
// the same act to be described two ways.
//
// TWO of them genuinely differ and the difference is where the reader
// ends up, not what happens: duplicating from the list leaves you on the
// list, duplicating from the detail page opens the copy, and deleting
// from the detail page has to leave the page it deleted. So those two
// ANSWER rather than reload, and the caller decides where to go.
import { ref } from 'vue'
import { campaignsApi } from '../api/campaigns'
import { apiErrorMessage } from '../api/client'
import type { Campaign } from '../api/types'
import { useNotificationStore } from '../stores/notification'
import { useConfirm } from './useConfirm'

/** Enough of a campaign to act on it and to name it in a question. */
export interface CampaignRef {
  id: string
  name: string
}

export function useCampaignActions(reload: () => Promise<unknown>) {
  const notify = useNotificationStore()
  const { confirm } = useConfirm()

  // One flag for all of them: these are page-level acts and two cannot
  // sensibly be in flight at once.
  const busy = ref(false)

  async function run(act: () => Promise<unknown>, ok: string, failed: string): Promise<boolean> {
    busy.value = true
    try {
      await act()
      notify.success(ok)

      return true
    } catch (e) {
      notify.error(apiErrorMessage(e, failed))

      return false
    } finally {
      busy.value = false
      await reload()
    }
  }

  async function send(c: CampaignRef) {
    const ok = await confirm({
      title: 'Send Campaign',
      message: `Send "${c.name}" now? This will start delivering emails to all subscribers in the list.`,
      confirmText: 'Send',
      variant: 'info',
    })
    if (!ok) return

    await run(() => campaignsApi.send(c.id), 'Campaign is sending', 'Failed to send campaign')
  }

  /** @param at a datetime-local value, converted to UTC here. */
  async function schedule(c: CampaignRef, at: string) {
    await run(
      () => campaignsApi.send(c.id, { scheduled_at: new Date(at).toISOString() }),
      'Campaign scheduled',
      'Failed to schedule campaign',
    )
  }

  // Pausing and resuming ask nothing: both are reversible by the control
  // beside them, and a confirmation for an act you can undo in one click
  // teaches people to dismiss confirmations.
  async function pause(c: CampaignRef) {
    await run(() => campaignsApi.pause(c.id), 'Campaign paused', 'Failed to pause campaign')
  }

  async function resume(c: CampaignRef) {
    await run(() => campaignsApi.resume(c.id), 'Campaign resumed', 'Failed to resume campaign')
  }

  async function cancel(c: CampaignRef) {
    const ok = await confirm({
      title: 'Cancel Campaign',
      message: `Cancel "${c.name}"? Unsent messages will be skipped.`,
      confirmText: 'Cancel Campaign',
      variant: 'danger',
    })
    if (!ok) return

    await run(() => campaignsApi.cancel(c.id), 'Campaign cancelled', 'Failed to cancel campaign')
  }

  /** @returns the copy, so a caller that wants to open it can. */
  async function duplicate(c: CampaignRef): Promise<Campaign | null> {
    busy.value = true
    try {
      const res = await campaignsApi.duplicate(c.id)
      notify.success('Campaign duplicated as draft')

      return res.data.campaign
    } catch (e) {
      notify.error(apiErrorMessage(e, 'Failed to duplicate campaign'))

      return null
    } finally {
      busy.value = false
      await reload()
    }
  }

  /** @returns whether it went, so a detail page knows to leave. */
  async function remove(c: CampaignRef): Promise<boolean> {
    const ok = await confirm({
      title: 'Delete Campaign',
      message: `Delete "${c.name}"? This action cannot be undone.`,
      confirmText: 'Delete',
      variant: 'danger',
    })
    if (!ok) return false

    return run(() => campaignsApi.remove(c.id), 'Campaign deleted', 'Failed to delete campaign')
  }

  return { busy, send, schedule, pause, resume, cancel, duplicate, remove }
}
