import type { Locator, Page } from '@playwright/test'

/**
 * Picks an option of a select found by its accessible name. A native `<select>` (canvas `.fld`)
 * takes the option directly; a Vuetify select is opened through its field wrapper, which Vuetify
 * lays over the combobox input, and the option is clicked in its menu.
 */
export async function chooseOption(
  page: Page,
  scope: Page | Locator,
  label: string,
  option: RegExp,
): Promise<void> {
  const combobox = scope.getByRole('combobox', { name: label })
  if ((await combobox.evaluate((element) => element.tagName)) === 'SELECT') {
    const labels = await combobox.locator('option').allTextContents()
    const match = labels.map((text) => text.trim()).find((text) => option.test(text))
    if (match === undefined) throw new Error(`No option matching ${String(option)} in ${label}`)
    await combobox.selectOption({ label: match })
    return
  }
  await openSelect(scope, label)
  await page.getByRole('option', { name: option }).click()
}

export async function openSelect(scope: Page | Locator, label: string): Promise<void> {
  await scope
    .getByRole('combobox', { name: label })
    .locator('xpath=ancestor::div[contains(concat(" ", @class, " "), " v-field ")][1]')
    .click()
}
