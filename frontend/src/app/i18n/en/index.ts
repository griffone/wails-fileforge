import { activityEn } from './activity';
import { categoriesEn } from './categories';
import { commonEn } from './common';
import { fileDropEn } from './file-drop';
import { fileQueueEn } from './file-queue';
import { globalDropEn } from './global-drop';
import { homeEn } from './home';
import { imageAnnotateEn } from './image-annotate';
import { imageBackgroundEn } from './image-background';
import { imageCompressEn } from './image-compress';
import { imageConverterEn } from './image-converter';
import { imageCropEn } from './image-crop';
import { imageOcrEn } from './image-ocr';
import { imageWatermarkEn } from './image-watermark';
import { outputPlanEn } from './output-plan-card';
import { paletteEn } from './palette';
import { recipeRunEn } from './recipe-run';
import { recipesEn } from './recipes';
import { resultCardEn } from './result-card';
import { settingsEn } from './settings';
import { shellEn } from './shell';
import { toastEn } from './toast';
import { toolErrorEn } from './tool-error';
import { toolchainEn } from './toolchain';
import { toolsEn } from './tools';

export const en = {
  ...commonEn,
  ...shellEn,
  ...settingsEn,
  ...categoriesEn,
  ...toolsEn,
  ...paletteEn,
  ...homeEn,
  ...imageConverterEn,
  ...imageCropEn,
  ...imageAnnotateEn,
  ...imageCompressEn,
  ...imageWatermarkEn,
  ...imageOcrEn,
  ...imageBackgroundEn,
  ...recipesEn,
  ...recipeRunEn,
  ...fileDropEn,
  ...fileQueueEn,
  ...outputPlanEn,
  ...resultCardEn,
  ...toolErrorEn,
  ...activityEn,
  ...globalDropEn,
  ...toastEn,
  ...toolchainEn,
} as const;

export type TranslationKey = keyof typeof en;
