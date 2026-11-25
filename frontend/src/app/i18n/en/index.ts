import { activityEn } from './activity';
import { categoriesEn } from './categories';
import { commonEn } from './common';
import { docDocxToPdfEn } from './doc-docx-to-pdf';
import { docMdToPdfEn } from './doc-md-to-pdf';
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
import { pdfCropEn } from './pdf-crop';
import { pdfFromImagesEn } from './pdf-from-images';
import { pdfMergeEn } from './pdf-merge';
import { pdfOcrEn } from './pdf-ocr';
import { pdfOptimizeEn } from './pdf-optimize';
import { pdfSplitEn } from './pdf-split';
import { pdfToDocxEn } from './pdf-to-docx';
import { recipeRunEn } from './recipe-run';
import { recipesEn } from './recipes';
import { resultCardEn } from './result-card';
import { settingsEn } from './settings';
import { shellEn } from './shell';
import { toastEn } from './toast';
import { toolErrorEn } from './tool-error';
import { toolchainEn } from './toolchain';
import { toolsEn } from './tools';
import { videoConvertEn } from './video-convert';
import { videoMergeEn } from './video-merge';
import { videoTranscribeEn } from './video-transcribe';
import { videoTrimEn } from './video-trim';

export const en = {
  ...commonEn,
  ...docDocxToPdfEn,
  ...docMdToPdfEn,
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
  ...pdfMergeEn,
  ...pdfSplitEn,
  ...pdfCropEn,
  ...pdfOptimizeEn,
  ...pdfOcrEn,
  ...pdfToDocxEn,
  ...pdfFromImagesEn,
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
  ...videoConvertEn,
  ...videoTrimEn,
  ...videoMergeEn,
  ...videoTranscribeEn,
} as const;

export type TranslationKey = keyof typeof en;
