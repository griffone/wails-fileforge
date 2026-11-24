import { settingsEn } from './settings';
import { shellEn } from './shell';

export const en = {
  ...shellEn,
  ...settingsEn,
} as const;

export type TranslationKey = keyof typeof en;
