export type Locale = 'en' | 'es';

export const LOCALES: readonly { id: Locale; label: string }[] = [
  { id: 'en', label: 'English' },
  { id: 'es', label: 'Español' },
];

export const DEFAULT_LOCALE: Locale = 'en';

/** Picks Spanish for Spanish-speaking browsers, English everywhere else. */
export function detectLocale(): Locale {
  if (typeof navigator === 'undefined') {
    return DEFAULT_LOCALE;
  }

  const language = (navigator.language ?? '').toLowerCase();
  return language.startsWith('es') ? 'es' : DEFAULT_LOCALE;
}
