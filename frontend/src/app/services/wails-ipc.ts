import { Injectable } from '@angular/core';

import {
  AutomationService,
  PreviewService,
  RecipeService,
  SystemService,
  ToolService,
  ToolchainService,
} from '../../../bindings/fileforge-desktop/internal/app';
import type * as Models from '../../../bindings/fileforge-desktop/internal/models';
import type * as PreviewModels from '../../../bindings/fileforge-desktop/internal/preview/models';

/**
 * Typed adapter over the generated Wails bindings.
 *
 * Every RPC exposed by the bound services is routed through this class so the
 * rest of the frontend never depends on numeric method IDs. The legacy `Wails`
 * service keeps its public API and fallbacks; phase 6 removes that hand-written
 * DTO layer and calls these bindings directly.
 */
@Injectable({ providedIn: 'root' })
export class WailsIpc {
  // Tools and jobs.
  listToolsV1(): Promise<Models.ListToolsResponseV1> {
    return ToolService.ListToolsV1();
  }

  validateJobV1(
    request: Models.JobRequestV1,
  ): Promise<Models.ValidateJobResponseV1> {
    return ToolService.ValidateJobV1(request);
  }

  planJobV1(request: Models.JobRequestV1): Promise<Models.PlanJobResponseV1> {
    return ToolService.PlanJobV1(request);
  }

  runJobV1(request: Models.JobRequestV1): Promise<Models.RunJobResponseV1> {
    return ToolService.RunJobV1(request);
  }

  cancelJobV1(jobId: string): Promise<Models.CancelJobResponseV1> {
    return ToolService.CancelJobV1(jobId);
  }

  getJobStatusV1(jobId: string): Promise<Models.JobStatusResponseV1> {
    return ToolService.GetJobStatusV1(jobId);
  }

  listJobsV1(limit: number): Promise<Models.JobListResponseV1> {
    return ToolService.ListJobsV1(limit);
  }

  verifyJobV1(jobId: string): Promise<Models.VerificationReportV1> {
    return ToolService.VerifyJobV1(jobId);
  }

  async listJobHistoryV1(): Promise<Models.JobHistoryEntryV1[]> {
    return (await ToolService.ListJobHistoryV1()) ?? [];
  }

  undoJobV1(jobId: string): Promise<Models.UndoResultV1> {
    return ToolService.UndoJobV1(jobId);
  }

  restoreJobV1(jobId: string): Promise<Models.UndoResultV1> {
    return ToolService.RestoreJobV1(jobId);
  }

  emptyTrashV1(): Promise<Models.PathActionResponseV1> {
    return ToolService.EmptyTrashV1();
  }

  // Recipes.
  listRecipesV1(): Promise<Models.ListRecipesResponseV1> {
    return RecipeService.ListRecipesV1();
  }

  planRecipeV1(
    request: Models.RecipeRequestV1,
  ): Promise<Models.RecipePlanResponseV1> {
    return RecipeService.PlanRecipeV1(request);
  }

  runRecipeV1(
    request: Models.RecipeRequestV1,
  ): Promise<Models.RunRecipeResponseV1> {
    return RecipeService.RunRecipeV1(request);
  }

  getRecipeStatusV1(jobId: string): Promise<Models.RecipeStatusResponseV1> {
    return RecipeService.GetRecipeStatusV1(jobId);
  }

  async listRecipeRunsV1(limit: number): Promise<Models.RecipeResultV1[]> {
    return (await RecipeService.ListRecipeRunsV1(limit)) ?? [];
  }

  cancelRecipeV1(jobId: string): Promise<Models.RecipeStatusResponseV1> {
    return RecipeService.CancelRecipeV1(jobId);
  }

  // Automation.
  listAutomationRulesV1(): Promise<Models.ListAutomationRulesResponseV1> {
    return AutomationService.ListAutomationRulesV1();
  }

  saveAutomationRuleV1(
    rule: Models.AutomationRuleV1,
  ): Promise<Models.AutomationRuleActionResponseV1> {
    return AutomationService.SaveAutomationRuleV1(rule);
  }

  deleteAutomationRuleV1(
    id: string,
  ): Promise<Models.AutomationRuleActionResponseV1> {
    return AutomationService.DeleteAutomationRuleV1(id);
  }

  setAutomationRuleEnabledV1(
    id: string,
    enabled: boolean,
  ): Promise<Models.AutomationRuleActionResponseV1> {
    return AutomationService.SetAutomationRuleEnabledV1(id, enabled);
  }

  getAutomationStatusV1(): Promise<Models.AutomationStatusResponseV1> {
    return AutomationService.GetAutomationStatusV1();
  }

  listAutomationRunsV1(
    limit: number,
  ): Promise<Models.ListAutomationRunsResponseV1> {
    return AutomationService.ListAutomationRunsV1(limit);
  }

  clearAutomationRunsV1(): Promise<Models.AutomationRunsActionResponseV1> {
    return AutomationService.ClearAutomationRunsV1();
  }

  // Capabilities and toolchain.
  getOcrCapabilitiesV1(): Promise<Models.OCRCapabilitiesResponseV1> {
    return SystemService.GetOCRCapabilitiesV1();
  }

  getTranscriptionCapabilitiesV1(
    whisperPath: string,
    modelPath: string,
  ): Promise<Models.TranscriptionCapabilitiesResponseV1> {
    return SystemService.GetTranscriptionCapabilitiesV1(whisperPath, modelPath);
  }

  getLocalCapabilitiesV1(): Promise<Models.LocalCapabilitiesResponseV1> {
    return SystemService.GetLocalCapabilitiesV1();
  }

  getToolchainStatusV1(): Promise<Models.ToolchainStatusV1> {
    return ToolchainService.GetToolchainStatusV1();
  }

  getToolchainInstallStateV1(): Promise<Models.ToolchainStatusV1> {
    return ToolchainService.GetToolchainInstallStateV1();
  }

  startToolchainInstallV1(
    options: Models.ToolchainInstallOptionsV1,
  ): Promise<Models.ToolchainStatusV1> {
    return ToolchainService.StartToolchainInstallV1(options);
  }

  cancelToolchainInstallV1(): Promise<Models.ToolchainStatusV1> {
    return ToolchainService.CancelToolchainInstallV1();
  }

  // Inspection, dialogs, and system actions.
  inspectFilesV1(paths: string[]): Promise<Models.InspectFilesResponseV1> {
    return SystemService.InspectFilesV1(paths);
  }

  notifyV1(title: string, body: string): Promise<Models.PathActionResponseV1> {
    return SystemService.NotifyV1(title, body);
  }

  openPath(path: string): Promise<Models.PathActionResponseV1> {
    return SystemService.OpenPath(path);
  }

  revealInFolder(path: string): Promise<Models.PathActionResponseV1> {
    return SystemService.RevealInFolder(path);
  }

  openFileDialog(): Promise<string> {
    return SystemService.OpenFileDialog();
  }

  async openMultipleFilesDialog(): Promise<string[]> {
    return (await SystemService.OpenMultipleFilesDialog()) ?? [];
  }

  openDirectoryDialog(): Promise<string> {
    return SystemService.OpenDirectoryDialog();
  }

  // Preview and media.
  getPdfPreviewSource(
    inputPath: string,
  ): Promise<Models.PDFPreviewSourceResponseV1> {
    return SystemService.GetPDFPreviewSourceV1(inputPath);
  }

  registerMediaPath(path: string): Promise<Models.MediaSourceResponseV1> {
    return SystemService.RegisterMediaPath(path);
  }

  startPreview(
    request: PreviewModels.PreviewRequest,
  ): Promise<PreviewModels.PreviewStartResponse> {
    return PreviewService.StartPreview(request);
  }

  getPreviewStatus(jobId: string): Promise<PreviewModels.JobStatus> {
    return PreviewService.GetPreviewStatus(jobId);
  }

  getPreview(jobId: string): Promise<PreviewModels.PreviewResult> {
    return PreviewService.GetPreview(jobId);
  }

  cancelPreview(jobId: string): Promise<PreviewModels.PreviewStartResponse> {
    return PreviewService.CancelPreview(jobId);
  }

  getImagePreviewSource(
    inputPath: string,
  ): Promise<Models.ImagePreviewSourceResponseV1> {
    return ToolService.GetImagePreviewSourceV1(inputPath);
  }

  getImageCropPreview(
    request: Models.ImageCropPreviewRequestV1,
  ): Promise<Models.ImageCropPreviewResponseV1> {
    return ToolService.GetImageCropPreviewV1(request);
  }

  getImageAnnotatePreview(
    request: Models.ImageAnnotatePreviewRequestV1,
  ): Promise<Models.ImageAnnotatePreviewResponseV1> {
    return ToolService.GetImageAnnotatePreviewV1(request);
  }
}
