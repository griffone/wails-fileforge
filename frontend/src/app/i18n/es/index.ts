import type { TranslationKey } from '../en';

import { categoriesEs } from './categories';
import { paletteEs } from './palette';
import { settingsEs } from './settings';
import { shellEs } from './shell';
import { toolsEs } from './tools';

export const es: Record<TranslationKey, string> = {
  ...shellEs,
  ...settingsEs,
  ...categoriesEs,
  ...toolsEs,
  ...paletteEs,
};
