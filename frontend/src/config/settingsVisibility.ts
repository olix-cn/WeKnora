import { isIntegrationSection, normalizeSettingsSection } from './settingsRoute'

/** Product UI policy, applies to every role including platform administrators. */
export function visibleSettingsSection(section: string, tab?: string | null): string {
  const normalized = normalizeSettingsSection(section, tab)
  return isHiddenSettingsSection(normalized)
    ? 'general'
    : normalized
}

export function isHiddenSettingsSection(section: string): boolean {
  return section === 'weknoracloud' || isIntegrationSection(section) || section.startsWith('integration-')
}
