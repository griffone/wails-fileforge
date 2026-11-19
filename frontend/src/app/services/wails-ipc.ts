import { Injectable } from '@angular/core';

import { App } from '../../../bindings/fileforge-desktop/internal/app';
import type * as Models from '../../../bindings/fileforge-desktop/internal/models';
import type * as PreviewModels from '../../../bindings/fileforge-desktop/internal/preview/models';

/**
 * Typed adapter over the generated Wails bindings.
 *
 * Every RPC exposed by the bound `App` service is routed through this class so
 * the rest of the frontend never depends on numeric method IDs. The legacy
 * `Wails` service keeps its public API and fallbacks; phase 6 removes that
 * hand-written DTO layer and calls these bindings directly.
 */
@Injectable({ providedIn: 'root' })
export class WailsIpc {
  // Tools and jobs.
  listToolsV1(): Promise<Models.ListToolsResponseV1> {
    return App.ListToolsV1();
  }

  validateJobV1(
    request: Models.JobRequestV1,
  ): Promise<Models.ValidateJobResponseV1> {
    return App.ValidateJobV1(request);
  }

  planJobV1(request: Models.JobRequestV1): Promise<Models.PlanJobResponseV1> {
    return App.PlanJobV1(request);
  }

  runJobV1(request: Models.JobRequestV1): Promise<Models.RunJobResponseV1> {
    return App.RunJobV1(request);
  }

  cancelJobV1(jobId: string): Promise<Models.CancelJobResponseV1> {
    return App.CancelJobV1(jobId);
  }

  getJobStatusV1(jobId: string): Promise<Models.JobStatusResponseV1> {
    return App.GetJobStatusV1(jobId);
  }

  listJobsV1(limit: number): Promise<Models.JobListResponseV1> {
    return App.ListJobsV1(limit);
  }

  verifyJobV1(jobId: string): Promise<Models.VerificationReportV1> {
    return App.VerifyJobV1(jobId);
  }

  async listJobHistoryV1(): Promise<Models.JobHistoryEntryV1[]> {
    return (await App.ListJobHistoryV1()) ?? [];
  }

  undoJobV1(jobId: string): Promise<Models.UndoResultV1> {
    return App.UndoJobV1(jobId);
  }

  restoreJobV1(jobId: string): Promise<Models.UndoResultV1> {
    return App.RestoreJobV1(jobId);
  }

  emptyTrashV1(): Promise<Models.PathActionResponseV1> {
    return App.EmptyTrashV1();
  }

  // Recipes.
  listRecipesV1(): Promise<Models.ListRecipesResponseV1> {
    return App.ListRecipesV1();
  }

  planRecipeV1(
    request: Models.RecipeRequestV1,
  ): Promise<Models.RecipePlanResponseV1> {
    return App.PlanRecipeV1(request);
  }

  runRecipeV1(
    request: Models.RecipeRequestV1,
  ): Promise<Models.RunRecipeResponseV1> {
    return App.RunRecipeV1(request);
  }

  getRecipeStatusV1(jobId: string): Promise<Models.RecipeStatusResponseV1> {
    return App.GetRecipeStatusV1(jobId);
  }

  async listRecipeRunsV1(limit: number): Promise<Models.RecipeResultV1[]> {
    return (await App.ListRecipeRunsV1(limit)) ?? [];
  }

  cancelRecipeV1(jobId: string): Promise<Models.RecipeStatusResponseV1> {
    return App.CancelRecipeV1(jobId);
  }

  // Automation.
  listAutomationRulesV1(): Promise<Models.ListAutomationRulesResponseV1> {
    return App.ListAutomationRulesV1();
  }

  saveAutomationRuleV1(
    rule: Models.AutomationRuleV1,
  ): Promise<Models.AutomationRuleActionResponseV1> {
    return App.SaveAutomationRuleV1(rule);
  }

  deleteAutomationRuleV1(
    id: string,
  ): Promise<Models.AutomationRuleActionResponseV1> {
    return App.DeleteAutomationRuleV1(id);
  }

  setAutomationRuleEnabledV1(
    id: string,
    enabled: boolean,
  ): Promise<Models.AutomationRuleActionResponseV1> {
    return App.SetAutomationRuleEnabledV1(id, enabled);
  }

  getAutomationStatusV1(): Promise<Models.AutomationStatusResponseV1> {
    return App.GetAutomationStatusV1();
  }

  listAutomationRunsV1(
    limit: number,
  ): Promise<Models.ListAutomationRunsResponseV1> {
    return App.ListAutomationRunsV1(limit);
  }

  clearAutomationRunsV1(): Promise<Models.AutomationRunsActionResponseV1> {
    return App.ClearAutomationRunsV1();
  }

  // Capabilities and toolchain.
  getOcrCapabilitiesV1(): Promise<Models.OCRCapabilitiesResponseV1> {
    return App.GetOCRCapabilitiesV1();
  }

  getTranscriptionCapabilitiesV1(
    whisperPath: string,
    modelPath: string,
  ): Promise<Models.TranscriptionCapabilitiesResponseV1> {
    return App.GetTranscriptionCapabilitiesV1(whisperPath, modelPath);
  }

  getLocalCapabilitiesV1(): Promise<Models.LocalCapabilitiesResponseV1> {
    return App.GetLocalCapabilitiesV1();
  }

  getToolchainStatusV1(): Promise<Models.ToolchainStatusV1> {
    return App.GetToolchainStatusV1();
  }

  getToolchainInstallStateV1(): Promise<Models.ToolchainStatusV1> {
    return App.GetToolchainInstallStateV1();
  }

  startToolchainInstallV1(
    options: Models.ToolchainInstallOptionsV1,
  ): Promise<Models.ToolchainStatusV1> {
    return App.StartToolchainInstallV1(options);
  }

  cancelToolchainInstallV1(): Promise<Models.ToolchainStatusV1> {
    return App.CancelToolchainInstallV1();
  }

  // Inspection, dialogs, and system actions.
  inspectFilesV1(paths: string[]): Promise<Models.InspectFilesResponseV1> {
    return App.InspectFilesV1(paths);
  }

  notifyV1(title: string, body: string): Promise<Models.PathActionResponseV1> {
    return App.NotifyV1(title, body);
  }

  openPath(path: string): Promise<Models.PathActionResponseV1> {
    return App.OpenPath(path);
  }

  revealInFolder(path: string): Promise<Models.PathActionResponseV1> {
    return App.RevealInFolder(path);
  }

  openFileDialog(): Promise<string> {
    return App.OpenFileDialog();
  }

  async openMultipleFilesDialog(): Promise<string[]> {
    return (await App.OpenMultipleFilesDialog()) ?? [];
  }

  openDirectoryDialog(): Promise<string> {
    return App.OpenDirectoryDialog();
  }

  // Preview and media.
  getPdfPreviewSource(
    inputPath: string,
  ): Promise<Models.PDFPreviewSourceResponseV1> {
    return App.GetPDFPreviewSourceV1(inputPath);
  }

  registerMediaPath(path: string): Promise<Models.MediaSourceResponseV1> {
    return App.RegisterMediaPath(path);
  }

  startPreview(
    request: PreviewModels.PreviewRequest,
  ): Promise<PreviewModels.PreviewStartResponse> {
    return App.StartPreview(request);
  }

  getPreviewStatus(jobId: string): Promise<PreviewModels.JobStatus> {
    return App.GetPreviewStatus(jobId);
  }

  getPreview(jobId: string): Promise<PreviewModels.PreviewResult> {
    return App.GetPreview(jobId);
  }

  cancelPreview(jobId: string): Promise<PreviewModels.PreviewStartResponse> {
    return App.CancelPreview(jobId);
  }

  getImagePreviewSource(
    inputPath: string,
  ): Promise<Models.ImagePreviewSourceResponseV1> {
    return App.GetImagePreviewSourceV1(inputPath);
  }

  getImageCropPreview(
    request: Models.ImageCropPreviewRequestV1,
  ): Promise<Models.ImageCropPreviewResponseV1> {
    return App.GetImageCropPreviewV1(request);
  }

  getImageAnnotatePreview(
    request: Models.ImageAnnotatePreviewRequestV1,
  ): Promise<Models.ImageAnnotatePreviewResponseV1> {
    return App.GetImageAnnotatePreviewV1(request);
  }
}
