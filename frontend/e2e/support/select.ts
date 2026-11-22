import type { Locator, Page } from '@playwright/test'

/**
 * Opens a Vuetify select by its accessible name and picks an option. Vuetify lays its field wrapper
 * over the combobox input, so the click goes to the wrapper, which is what opens the menu.
 */
export async function chooseOption(
  page: Page,
  scope: Page | Locator,
  label: string,
  option: RegExp,
): Promise<void> {
  await openSelect(scope, label)
  await page.getByRole('option', { name: option }).click()
}

export async function openSelect(scope: Page | Locator, label: string): Promise<void> {
  await scope
    .getByRole('combobox', { name: label })
    .locator('xpath=ancestor::div[contains(concat(" ", @class, " "), " v-field ")][1]')
    .click()
}
