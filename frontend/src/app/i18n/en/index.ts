import { activityEn } from './activity';
import { audioToolsEn } from './audio-tools';
import { automationsEn } from './automations';
import { categoriesEn } from './categories';
import { commonEn } from './common';
import { contextMenuEn } from './context-menu';
import { destinationPresetsEn } from './destination-presets';
import { docDocxToPdfEn } from './doc-docx-to-pdf';
import { docMdToPdfEn } from './doc-md-to-pdf';
import { duplicatesEn } from './duplicates';
import { fileDropEn } from './file-drop';
import { fileQueueEn } from './file-queue';
import { globalDropEn } from './global-drop';
import { homeEn } from './home';
import { imageAnnotateEn } from './image-annotate';
import { imageBackgroundEn } from './image-background';
import { imageUpscaleEn } from './image-upscale';
import { imageCompressEn } from './image-compress';
import { imageConverterEn } from './image-converter';
import { imageCropEn } from './image-crop';
import { imageOcrEn } from './image-ocr';
import { imageWatermarkEn } from './image-watermark';
import { jobCardEn } from './job-card';
import { outputPlanEn } from './output-plan-card';
import { paletteEn } from './palette';
import { pdfCropEn } from './pdf-crop';
import { pdfFromImagesEn } from './pdf-from-images';
import { pdfMergeEn } from './pdf-merge';
import { pdfOcrEn } from './pdf-ocr';
import { pdfOptimizeEn } from './pdf-optimize';
import { pdfPageOpsEn } from './pdf-page-ops';
import { pdfRendererEn } from './pdf-renderer';
import { pdfSplitEn } from './pdf-split';
import { pdfToDocxEn } from './pdf-to-docx';
import { recipeRunEn } from './recipe-run';
import { recipesEn } from './recipes';
import { resultCardEn } from './result-card';
import { settingsEn } from './settings';
import { shellEn } from './shell';
import { shortcutsEn } from './shortcuts';
import { toastEn } from './toast';
import { toolCatalogEn } from './tool-catalog';
import { toolErrorEn } from './tool-error';
import { toolchainEn } from './toolchain';
import { windowEn } from './window';
import { toolsEn } from './tools';
import { videoConvertEn } from './video-convert';
import { videoMergeEn } from './video-merge';
import { videoTargetEn } from './video-target';
import { videoTranscribeEn } from './video-transcribe';
import { videoTrimEn } from './video-trim';
import { webDownloadEn } from './web-download';

export const en = {
  ...commonEn,
  ...contextMenuEn,
  ...destinationPresetsEn,
  ...audioToolsEn,
  ...automationsEn,
  ...duplicatesEn,
  ...docDocxToPdfEn,
  ...docMdToPdfEn,
  ...shellEn,
  ...shortcutsEn,
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
  ...imageUpscaleEn,
  ...pdfMergeEn,
  ...pdfSplitEn,
  ...pdfCropEn,
  ...pdfPageOpsEn,
  ...pdfOptimizeEn,
  ...pdfOcrEn,
  ...pdfToDocxEn,
  ...pdfFromImagesEn,
  ...recipesEn,
  ...recipeRunEn,
  ...fileDropEn,
  ...fileQueueEn,
  ...outputPlanEn,
  ...jobCardEn,
  ...resultCardEn,
  ...toolErrorEn,
  ...activityEn,
  ...globalDropEn,
  ...toastEn,
  ...toolCatalogEn,
  ...toolchainEn,
  ...videoConvertEn,
  ...videoTrimEn,
  ...videoMergeEn,
  ...videoTargetEn,
  ...videoTranscribeEn,
  ...webDownloadEn,
  ...pdfRendererEn,
  ...windowEn,
} as const;

export type TranslationKey = keyof typeof en;
