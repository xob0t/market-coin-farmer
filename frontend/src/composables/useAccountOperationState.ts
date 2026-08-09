import { reactive, ref } from 'vue'

export interface AccountRuntimeData {
  rewards: any[]
  signInInfo: any
  login?: string
  coinBalance?: string
  blocked?: boolean
}

interface AccountIdentity {
  cookies: string
}

interface CancellableCall {
  cancel: () => void
}

export const useAccountOperationState = () => {
  const accountsData = reactive<Record<string, AccountRuntimeData>>({})
  const loadingAccounts = reactive<Record<string, boolean>>({})
  const cancelledAccounts = reactive<Record<string, boolean>>({})
  const refreshingAll = ref(false)
  const spendingAllCoins = ref(false)

  const inFlightCalls: Record<string, Set<CancellableCall>> = {}

  const trackCall = async <T>(cookies: string, call: Promise<T> & CancellableCall): Promise<T> => {
    const set = (inFlightCalls[cookies] ??= new Set())
    set.add(call)
    try {
      return await call
    } finally {
      set.delete(call)
      if (set.size === 0) delete inFlightCalls[cookies]
    }
  }

  const isCancelled = (cookies: string): boolean => cancelledAccounts[cookies] === true

  const beginRefresh = (cookies: string): void => {
    cancelledAccounts[cookies] = false
  }

  const cancelAccountRefresh = (account: AccountIdentity): void => {
    cancelledAccounts[account.cookies] = true
    const set = inFlightCalls[account.cookies]
    if (set) {
      for (const call of set) call.cancel()
      set.clear()
      delete inFlightCalls[account.cookies]
    }
    loadingAccounts[account.cookies] = false
  }

  return {
    accountsData,
    loadingAccounts,
    cancelledAccounts,
    refreshingAll,
    spendingAllCoins,
    trackCall,
    isCancelled,
    beginRefresh,
    cancelAccountRefresh,
  }
}
