import { computed, onMounted, onUnmounted, ref, type ComputedRef } from 'vue'

interface ScheduledAccount {
  cookies: string
}

interface ScheduledAccountData {
  signInInfo?: {
    rewardAvailable?: boolean
    nextRewardTs?: number
  }
}

const formatTime = (seconds: number): string => {
  if (seconds <= 0) return 'Доступно сейчас'
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = seconds % 60
  return `${hours.toString().padStart(2, '0')}:${minutes.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`
}

export const useRewardScheduler = <TAccount extends ScheduledAccount>(
  accounts: ComputedRef<TAccount[]>,
  accountsData: Record<string, ScheduledAccountData>,
  refreshAccount: (account: TAccount) => Promise<void>,
  canRefresh: (account: TAccount) => boolean,
) => {
  const now = ref(Math.floor(Date.now() / 1000))
  const scheduledRefreshes = new Set<string>()
  let countdownInterval: number | undefined

  const refreshWhenDue = (account: TAccount): void => {
    if (scheduledRefreshes.has(account.cookies)) return
    scheduledRefreshes.add(account.cookies)
    void refreshAccount(account).finally(() => scheduledRefreshes.delete(account.cookies))
  }

  const tick = (): void => {
    now.value = Math.floor(Date.now() / 1000)
    for (const account of accounts.value) {
      const signInInfo = accountsData[account.cookies]?.signInInfo
      const rewardIsDue = signInInfo?.rewardAvailable === true || (typeof signInInfo?.nextRewardTs === 'number' && signInInfo.nextRewardTs <= now.value)
      if (rewardIsDue && canRefresh(account)) refreshWhenDue(account)
    }
  }

  const nextCoinRewardTime = computed<Record<string, string>>(() => {
    const result: Record<string, string> = {}
    for (const account of accounts.value) {
      const timestamp = accountsData[account.cookies]?.signInInfo?.nextRewardTs
      result[account.cookies] = typeof timestamp === 'number' ? formatTime(timestamp - now.value) : 'Не доступно'
    }
    return result
  })

  onMounted(() => {
    countdownInterval = window.setInterval(tick, 1000)
  })

  onUnmounted(() => {
    if (countdownInterval !== undefined) window.clearInterval(countdownInterval)
  })

  return { nextCoinRewardTime }
}
