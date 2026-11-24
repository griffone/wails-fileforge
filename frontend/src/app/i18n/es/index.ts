import type { TranslationKey } from '../en';

import { settingsEs } from './settings';
import { shellEs } from './shell';

export const es: Record<TranslationKey, string> = {
  ...shellEs,
  ...settingsEs,
};
