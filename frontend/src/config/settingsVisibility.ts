import { isIntegrationSection, normalizeSettingsSection } from './settingsRoute'

/** Product UI policy, applies to every role including platform administrators. */
export function visibleSettingsSection(section: string, tab?: string | null): string {
  const normalized = normalizeSettingsSection(section, tab)
  return isIntegrationSection(normalized) || normalized.startsWith('integration-')
    ? 'general'
    : normalized
}
