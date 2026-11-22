import { useI18n } from 'vue-i18n'
import type { Money } from '@/shared/api/contract'
import {
  formatBytes,
  formatDate,
  formatDateTime,
  formatDecimalAmount,
  formatDuration,
  formatInteger,
  formatMoney,
  formatRelativeAge,
} from './formatters'

/** Formatters bound to the current interface language. */
export function useFormat() {
  const { locale } = useI18n()
  return {
    money: (money: Money | null | undefined) => formatMoney(locale.value, money),
    amount: (value: string, currency: string) => formatDecimalAmount(locale.value, value, currency),
    integer: (value: number | null | undefined) => formatInteger(locale.value, value),
    dateTime: (iso: string | null | undefined) => formatDateTime(locale.value, iso),
    date: (iso: string | null | undefined) => formatDate(locale.value, iso),
    age: (iso: string) => formatRelativeAge(locale.value, iso),
    duration: (milliseconds: number) => formatDuration(locale.value, milliseconds),
    bytes: (bytes: number) => formatBytes(locale.value, bytes),
  }
}
