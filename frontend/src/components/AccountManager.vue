<script setup lang="ts">
import { ref, onMounted, computed, onUnmounted, reactive } from 'vue'
import { ConfigService, YaApiService } from "../../bindings/backend";
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { ScrollArea } from '@/components/ui/scroll-area'
import Coin from '@/components/ui/svg/Coin.vue'
import { toast } from 'vue-sonner'
import { RefreshCw, Pencil, Trash2, Frown, Dices, ShieldAlert } from 'lucide-vue-next'
import { Account } from "../../bindings/backend";
import { Clipboard } from "@wailsio/runtime";
import { onKeyStroke } from '@vueuse/core'
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card'

const config = ref<any>(null)
const cookies = ref('')
const accountsData = reactive<Record<string, {
  rewards: any[],
  signInInfo: any,
  login?: string,
  coinBalance?: string,
  blocked?: boolean
}>>({})
const name = ref('')
const proxy = ref('')
const editingAccountCookies = ref<string | null>(null)
const editName = ref('')
const editProxy = ref('')
const editCookies = ref('')
const now = ref(Math.floor(Date.now() / 1000))
let countdownInterval: number | null = null

const loadingAccounts = reactive<Record<string, boolean>>({})
const refreshingAll = ref(false)
const hideJunk = ref(false)
const spendingAllCoins = ref(false)

onMounted(async () => {
  await claimAndUpdateAccountInfo()
  startCountdownTimer()
})

onUnmounted(() => {
  if (countdownInterval) {
    clearInterval(countdownInterval)
  }
})

const startCountdownTimer = () => {
  if (countdownInterval) {
    clearInterval(countdownInterval)
  }
  countdownInterval = window.setInterval(() => {
    now.value = Math.floor(Date.now() / 1000)
  }, 1000)
}

const formatTime = (seconds: number): string => {
  if (seconds <= 0) return "Доступно сейчас"
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = seconds % 60
  return `${hours.toString().padStart(2, '0')}:${minutes.toString().padStart(2, '0')}:${secs.toString().padStart(2, '0')}`
}

const nextCoinRewardTime = computed(() => {
  const result: Record<string, string> = {}
  if (!config.value?.accounts) return result

  for (const account of config.value.accounts) {
    const accountData = accountsData[account.cookies]
    if (accountData?.signInInfo?.rewardAvailable) {
      claimDailyCoins(account)
      claimDailyGameRewards(account)
      getRewards(account)
    }

    if (!accountData?.signInInfo?.nextRewardTs) {
      result[account.cookies] = "Не доступно"
      continue
    }

    const timestamp = accountData.signInInfo.nextRewardTs
    const diff = timestamp - now.value

    result[account.cookies] = formatTime(diff)

    if (diff <= 0) {
      claimDailyCoins(account)
      claimDailyGameRewards(account)
      getRewards(account)
    }
  }
  return result
})

// A 403 VPN/proxy block is persistent account state, not a transient failure:
// it goes on the account card instead of a toast.
const reportAccountError = (account: Account, description: string, err: unknown): void => {
  const message = err instanceof Error ? err.message : String(err)
  if (message.includes('ERR_IP_BLOCKED')) {
    accountsData[account.cookies] = {
      ...accountsData[account.cookies],
      blocked: true
    }
    return
  }
  toast.error(account.name || accountsData[account.cookies]?.login || "Аккаунт", {
    description: `${description} ${message}`,
  })
}

const decodeBase64 = (str: string): string => {
  try {
    return atob(str)
  } catch (e) {
    console.error("Ошибка декодирования base64:", e)
    return str
  }
}

const getConfig = async (): Promise<void> => {
  try {
    const resultValue = await ConfigService.GetConfig({})
    config.value = resultValue

    // Initialize account data for new accounts
    if (config.value?.accounts) {
      for (const account of config.value.accounts) {
        if (!accountsData[account.cookies]) {
          accountsData[account.cookies] = {
            rewards: [],
            signInInfo: {}
          }
        }
      }

      // Clean up data for removed accounts
      const currentAccountCookies = config.value.accounts.map((a: Account) => a.cookies)
      Object.keys(accountsData).forEach(cookies => {
        if (!currentAccountCookies.includes(cookies)) {
          delete accountsData[cookies]
        }
      })
    }
  } catch (err) {
    console.error(err)
    toast.error("Ошибка получения конфигурации", {
      description: err instanceof Error ? err.message : String(err),
    })
  }
}

const addAccount = async (): Promise<void> => {
  if (!cookies.value.trim()) {
    toast.error("Требуется cookies")
    return
  }

  try {
    const account = new Account()
    account.cookies = cookies.value.trim()
    account.proxy = proxy.value.trim()
    account.name = name.value.trim() || ''

    await ConfigService.AddAccountToConfig(account)
    toast.success(account.name || accountsData[account.cookies]?.login || '', {
      description: "Аккаунт успешно добавлен",
    })

    // Initialize account data
    accountsData[account.cookies] = {
      rewards: [],
      signInInfo: {}
    }

    await getConfig()

    name.value = ''
    cookies.value = ''
    proxy.value = ''

    // Find the updated account in config
    const newAccount = config.value?.accounts?.find((a: Account) => a.cookies === btoa(account.cookies))
    if (newAccount) {
      console.log(newAccount)
      await claimDailyGameRewards(newAccount)
      if (newAccount?.signInInfo?.rewardAvailable) {
        await claimDailyCoins(newAccount)
      }
      await getRewards(newAccount)
    }

  } catch (err) {
    console.error(err)
    toast.error("Ошибка добавления аккаунта", {
      description: err instanceof Error ? err.message : String(err),
    })
  }
}

const removeAccount = async (account: Account): Promise<void> => {
  try {
    await ConfigService.RemoveAccountFromConfig(account)
    delete accountsData[account.cookies]
    delete loadingAccounts[account.cookies]
    toast.success(account.name || accountsData[account.cookies]?.login || '', {
      description: "Аккаунт успешно удален",
    })
    await getConfig()
  } catch (err) {
    console.error(err)
    toast.error("Ошибка удаления аккаунта", {
      description: err instanceof Error ? err.message : String(err),
    })
  }
}

const getRewards = async (account: Account): Promise<void> => {
  loadingAccounts[account.cookies] = true
  try {
    const [rewardsJson, login, coinBalance] = await YaApiService.GetRewardsJson(account)
    const parsedData = JSON.parse(rewardsJson)
    const rewardsResult = parsedData?.result ?? parsedData?.results?.[0]?.data?.result
    // Create a new object reference to force reactivity
    accountsData[account.cookies] = {
      ...accountsData[account.cookies],
      rewards: rewardsResult?.userRewards ?? rewardsResult?.user_rewards ?? [],
      login,
      coinBalance,
      blocked: false
    }
  } catch (err) {
    console.error(err)
    reportAccountError(account, "Ошибка получения наград", err)
  } finally {
    loadingAccounts[account.cookies] = false
  }
}
const claimAndUpdateAccountInfo = async (): Promise<void> => {
  refreshingAll.value = true
  try {
    await getConfig()
    if (!config.value?.accounts) return

    await Promise.all(config.value.accounts.map(async (account: Account) => {
      await claimDailyCoins(account)
      await claimDailyGameRewards(account)
      await getRewards(account)
    }))
  } finally {
    refreshingAll.value = false
  }
}

const claimDailyCoins = async (account: Account): Promise<void> => {
  loadingAccounts[account.cookies] = true
  try {
    const signInInfo = await YaApiService.ClaimDailyCoins(account)
    const parsedData = JSON.parse(signInInfo)
    const dailyResult = parsedData?.result ?? parsedData?.results?.[0]?.data?.result
    // Create a new object reference
    accountsData[account.cookies] = {
      ...accountsData[account.cookies],
      signInInfo: dailyResult?.info ?? dailyResult?.dailySignInInfo ?? {}
    }
    const dailyStatus = dailyResult?.shortInfo?.status
    if (dailyStatus === "SUCCESS" || (dailyResult?.rewardInfo?.reward && dailyStatus !== "REWARD_NOT_AVAILABLE")) {
      toast.success(account.name || accountsData[account.cookies]?.login || '', {
        description: "Сегодняшние монеты успешно получены",
      })
    }
  } catch (err) {
    console.error(err)
    reportAccountError(account, "Ошибка получения награды", err)
  } finally {
    loadingAccounts[account.cookies] = false
  }
}

const claimDailyGameRewards = async (account: Account): Promise<void> => {
  loadingAccounts[account.cookies] = true
  try {
    const gameRewardStatus = await YaApiService.ClaimGameRewards(account)
    const summary = JSON.parse(gameRewardStatus)
    const rewardCount = (summary.claimedLevels ?? 0) + (summary.completedChallenges ?? 0)
    if (rewardCount > 0) {
      toast.success(account.name || accountsData[account.cookies]?.login || '', {
        description: `Получено игровых наград: ${rewardCount}`,
      })
    } else if (summary.challengeEvents > 0) {
      toast.success(account.name || accountsData[account.cookies]?.login || '', {
        description: `Обработано ежедневных заданий: ${summary.challengeEvents}`,
      })
    }
    const failures = summary.games?.filter((game: any) => game.error) ?? []
    if (failures.length > 0 && failures.length === summary.games?.length && summary.challengeEvents === 0) {
      toast.error(account.name || accountsData[account.cookies]?.login || "Аккаунт", {
        description: "Не удалось обработать игровые награды",
      })
    }
  } catch (err) {
    console.error(err)
    reportAccountError(account, "Ошибка получения игровых наград", err)
  } finally {
    loadingAccounts[account.cookies] = false
  }
}

const roll = async (account: Account): Promise<void> => {
  loadingAccounts[account.cookies] = true
  try {
    const rollStatus = await YaApiService.Roll(account)
    const parsedData = JSON.parse(rollStatus)
    const spinResult = parsedData?.result?.spinResponse ?? parsedData?.results?.[0]?.data?.result
    const status = spinResult?.type
    if (status === "not_enough_coins") {
      toast.error(account.name || accountsData[account.cookies]?.login || '', {
        description: "Не хватает монет!",
      })
      return
    }
    if (status === "success") {
      toast.success(account.name || accountsData[account.cookies]?.login || '', {
        description: "Награда из колеса получена!",
      })
      await getRewards(account)
      return
    }
    toast.error(account.name || accountsData[account.cookies]?.login || "Аккаунт", {
      description: "Ошибка получения награды. Неизвестный статус " + status,
    })
  } catch (err) {
    console.error(err)
    reportAccountError(account, "Ошибка получения награды", err)
  } finally {
    loadingAccounts[account.cookies] = false
  }
}

const spendAllCoins = async (): Promise<void> => {
  if (!config.value?.accounts) return;

  spendingAllCoins.value = true;
  try {
    await Promise.all(
      config.value.accounts.map(async (account) => {
        while (canRoll(account.cookies)) {
          await roll(account);
        }
      })
    );
    toast.success("Все монеты потрачены на всех аккаунтах");
  } catch (err) {
    console.error(err);
    toast.error("Ошибка при трате монет", {
      description: err instanceof Error ? err.message : String(err),
    });
  } finally {
    spendingAllCoins.value = false;
  }
};

const startEditing = (account: Account): void => {
  editingAccountCookies.value = account.cookies
  editName.value = account.name || ''
  editProxy.value = account.proxy || ''
  editCookies.value = decodeBase64(account.cookies || '')
}

const cancelEditing = (): void => {
  editingAccountCookies.value = null
  editName.value = ''
  editProxy.value = ''
  editCookies.value = ''
}

const saveEditing = async (oldAccount: Account): Promise<void> => {
  if (!editCookies.value.trim()) {
    toast.error("Требуется cookies")
    return
  }

  try {
    await ConfigService.RemoveAccountFromConfig(oldAccount)

    const account = new Account()
    account.cookies = editCookies.value.trim()
    account.proxy = editProxy.value.trim()
    account.name = editName.value.trim() || "" // Keep as undefined if empty

    await ConfigService.AddAccountToConfig(account)

    // Update data reference if cookies changed
    if (oldAccount.cookies !== account.cookies && accountsData[oldAccount.cookies]) {
      accountsData[account.cookies] = accountsData[oldAccount.cookies]
      delete accountsData[oldAccount.cookies]
      delete loadingAccounts[oldAccount.cookies]
    }

    toast.success("Аккаунт успешно обновлен")
    editingAccountCookies.value = null

    // Refresh config to get the properly stored account instance
    await getConfig()

    console.log(account)

    // Find the updated account in config
    const updatedAccount = config.value?.accounts?.find((a: Account) => a.cookies === btoa(account.cookies))
    if (updatedAccount) {
      console.log(updatedAccount)
      await getRewards(updatedAccount)
    }
  } catch (err) {
    console.error(err)
    toast.error("Ошибка обновления аккаунта", {
      description: err instanceof Error ? err.message : String(err),
    })
  }
}

onKeyStroke('Escape', (e) => {
  if (editingAccountCookies.value) {
    cancelEditing()
    e.preventDefault()
  }
})

const canRoll = (cookies: string): boolean => {
  if (loadingAccounts[cookies]) return false
  const balance = accountsData[cookies]?.coinBalance
  if (!balance) return false
  return parseInt(balance) >= 10
}

const copyPromocode = (promocode: string) => {
  Clipboard.SetText(promocode)
  toast.success(`Промокод ${promocode} скопирован`)
}

const sortedRewards = (rewards: any[]) => {
  if (!rewards) return []
  return [...rewards].sort((a, b) => {
    const aHasPromo = a.actions?.some((act: any) => act.promocode)
    const bHasPromo = b.actions?.some((act: any) => act.promocode)
    return bHasPromo ? 1 : aHasPromo ? -1 : 0
  }).filter(reward => reward.subtitle !== "Больше не действует")
}

const filteredRewards = (rewards: any[]) => {
  if (!rewards) return []
  return rewards.filter(reward => {
    if (hideJunk.value) {
      return !reward.title.startsWith("Скидка")
    }
    return true
  })
}

const getAccountDisplayName = (account: Account): string => {
  const data = accountsData[account.cookies]
  return account.name
    ? `${account.name}${data?.login ? ` (${data.login})` : ''}`
    : data?.login || "Аккаунт"
}
</script>

<template>
  <div class="rounded-xl border border-border/60 bg-card/50 p-3">
    <div class="grid grid-cols-1 md:grid-cols-6 gap-2">
      <Input v-model="name" placeholder="Имя (опционально)" title="Имя аккаунта" />
      <Textarea v-model="cookies" placeholder="Cookies (Netscape) *" required
        class="md:col-span-2 h-9 min-h-9 resize-none py-2 leading-tight" title="Cookies аккаунта" />
      <Input v-model="proxy" placeholder="proxytype://username:password@server:port" class="md:col-span-2"
        title="Прокси для аккаунта" />
      <Button @click="addAccount" class="cursor-pointer h-9" title="Добавить новый аккаунт">Добавить</Button>
    </div>
  </div>

  <div v-if="config?.accounts.length > 0" class="mt-4 space-y-3">
    <div class="flex items-center gap-3 select-none">
      <Button @click="claimAndUpdateAccountInfo" variant="ghost" size="icon"
        class="cursor-pointer size-9 shrink-0 text-muted-foreground hover:text-foreground"
        title="Обновить все аккаунты и получить монетки">
        <RefreshCw class="size-4" :class="{ 'animate-spin': refreshingAll }" />
      </Button>
      <Button @click="spendAllCoins" :disabled="spendingAllCoins" class="cursor-pointer" title="Потратить все монеты"
        variant="outline">
        Потратить все монеты
        <RefreshCw v-if="spendingAllCoins" class="size-4 ml-1 animate-spin" />
      </Button>
      <Label for="hideJunk" class="cursor-pointer gap-2 text-muted-foreground">
        Скрыть скидки
        <Switch id="hideJunk" v-model="hideJunk" />
      </Label>
      <p class="ml-auto text-sm">
        <span class="text-muted-foreground">Монетки за вход:</span>
        <span class="ml-1.5 font-medium tabular-nums">{{ nextCoinRewardTime[config.accounts[0].cookies] || 'Н/Д'
        }}</span>
      </p>
    </div>

    <ScrollArea class="-mr-3 pr-3">
      <div v-for="account in config.accounts" :key="account.cookies"
        class="relative mb-2 overflow-hidden rounded-lg border bg-card p-4 transition-colors"
        :class="accountsData[account.cookies]?.blocked
          ? 'border-destructive/50'
          : 'border-border/60 hover:border-border'">
        <div v-if="loadingAccounts[account.cookies] || spendingAllCoins"
          class="absolute inset-0 z-10 flex items-center justify-center bg-background/60 backdrop-blur-[2px]">
          <RefreshCw class="size-6 animate-spin text-primary" />
        </div>

        <div class="flex justify-between items-start gap-3">
          <div v-if="editingAccountCookies !== account.cookies" class="flex-grow min-w-0 space-y-2">
            <div class="flex items-center gap-2.5">
              <p class="font-medium truncate">
                {{ getAccountDisplayName(account) }}
              </p>
              <span
                class="inline-flex shrink-0 items-center gap-1 rounded-full bg-primary/10 px-2 py-0.5 text-sm font-medium tabular-nums text-primary"
                title="Баланс монет">
                {{ accountsData[account.cookies]?.coinBalance || 'Н/Д' }}
                <Coin class="inline" />
              </span>
            </div>
            <div v-if="accountsData[account.cookies]?.blocked"
              class="flex items-center gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              <ShieldAlert class="size-4 shrink-0" />
              <span>Яндекс заблокировал доступ (403): IP определён как VPN/прокси. Смените прокси или IP и обновите.</span>
            </div>
            <div v-if="accountsData[account.cookies]?.signInInfo?.plan" class="flex gap-1.5">
              <div v-for="(day, dayIndex) in accountsData[account.cookies].signInInfo.plan"
                :title="`День ${dayIndex + 1}: ${day.reward.amount} монет`" :key="dayIndex"
                class="size-2.5 rounded-full transition-colors" :class="{
                  'bg-primary': day.received,
                  'bg-muted-foreground/20': !day.received
                }">
              </div>
            </div>

            <p class="text-sm truncate">
              <span class="text-muted-foreground select-none">Прокси: </span>
              <span :class="account.proxy ? '' : 'text-muted-foreground/60'">{{ account.proxy || 'Нет' }}</span>
            </p>
          </div>

          <div v-else class="flex flex-col gap-2 w-full">
            <Input v-model="editName" placeholder="Имя (опционально)" />
            <Input v-model="editProxy" placeholder="Прокси" />
            <Textarea v-model="editCookies" placeholder="Cookies (Netscape)" required class="h-24" />
            <div class="flex gap-2">
              <Button @click="saveEditing(account)" class="cursor-pointer flex-1" title="Сохранить изменения">
                Сохранить
              </Button>
              <Button @click="cancelEditing()" variant="outline" class="cursor-pointer flex-1"
                title="Отменить редактирование">
                Отмена
              </Button>
            </div>
          </div>

          <div class="flex gap-1 flex-col">
            <div class="flex gap-1.5 flex-shrink-0 self-end">
              <template v-if="editingAccountCookies !== account.cookies">
                <Button @click="roll(account)" variant="ghost"
                  :disabled="!canRoll(account.cookies) || loadingAccounts[account.cookies] || spendingAllCoins"
                  class="cursor-pointer p-0 size-8 text-primary hover:text-primary"
                  title="Вращать колесо (стоимость 10 монет)">
                  <Dices class="size-4" />
                </Button>
                <Button @click="startEditing(account)" variant="ghost"
                  class="cursor-pointer size-8 text-muted-foreground hover:text-foreground"
                  :disabled="loadingAccounts[account.cookies] || spendingAllCoins" title="Редактировать аккаунт">
                  <Pencil class="size-4" />
                </Button>
                <Button @click="removeAccount(account)" variant="ghost"
                  class="cursor-pointer size-8 text-muted-foreground hover:text-destructive"
                  :disabled="loadingAccounts[account.cookies] || spendingAllCoins" title="Удалить аккаунт">
                  <Trash2 class="size-4" />
                </Button>
              </template>

            </div>
            <div v-if="filteredRewards(accountsData[account.cookies]?.rewards)?.length > 0" class="mt-2 self-end">
              <div class="flex flex-wrap justify-end gap-1.5">
                <HoverCard
                  v-for="(reward, rewardIndex) in sortedRewards(filteredRewards(accountsData[account.cookies].rewards))"
                  :key="rewardIndex">
                  <HoverCardTrigger>
                    <div
                      class="rounded-md border border-transparent p-0.5 transition-colors hover:border-border hover:bg-accent/50">
                      <img v-if="reward.rewardImage || reward.reward_image"
                        :src="reward.rewardImage || reward.reward_image" alt="Награда"
                        class="w-13 h-13 object-contain cursor-pointer">
                    </div>
                  </HoverCardTrigger>
                  <HoverCardContent class="w-64">
                    <h4 class="font-medium">{{ reward.title }}</h4>
                    <p v-if="reward.subtitle && reward.subtitle !== 'Больше не действует'"
                      class="text-sm text-muted-foreground mt-1">{{
                        reward.subtitle }}</p>
                    <div v-if="reward.actions?.some(a => a.promocode)" class="mt-2">
                      <p class="text-xs font-medium text-muted-foreground">Промокод:</p>
                      <div class="flex flex-wrap gap-1 mt-1">
                        <Button v-for="(action, actionIndex) in reward.actions.filter(a => a.promocode)"
                          :key="actionIndex" @click="copyPromocode(action.promocode)" size="sm" variant="outline"
                          class="h-6" :title="`Скопировать промокод: ${action.promocode}`">
                          {{ action.text }}
                        </Button>
                      </div>
                    </div>
                  </HoverCardContent>
                </HoverCard>
              </div>
            </div>
          </div>

        </div>
      </div>
    </ScrollArea>
  </div>

  <div v-if="config?.accounts.length === 0"
    class="flex h-[calc(100%-4rem)] w-full flex-col items-center justify-center gap-3 text-muted-foreground">
    <Frown class="size-20 text-muted-foreground/30 wrench" stroke-width="1.5" />
    <p class="text-sm">
      Аккаунты не найдены
    </p>
  </div>
</template>
