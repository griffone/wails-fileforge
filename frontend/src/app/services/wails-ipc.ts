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
  listToolsV1(): Promise<Models.ListToolsResponse> {
    return ToolService.ListTools();
  }

  validateJobV1(
    request: Models.JobRequest,
  ): Promise<Models.ValidateJobResponse> {
    return ToolService.ValidateJob(request);
  }

  planJobV1(request: Models.JobRequest): Promise<Models.PlanJobResponse> {
    return ToolService.PlanJob(request);
  }

  runJobV1(request: Models.JobRequest): Promise<Models.RunJobResponse> {
    return ToolService.RunJob(request);
  }

  cancelJobV1(jobId: string): Promise<Models.CancelJobResponse> {
    return ToolService.CancelJob(jobId);
  }

  getJobStatusV1(jobId: string): Promise<Models.JobStatusResponse> {
    return ToolService.GetJobStatus(jobId);
  }

  listJobsV1(limit: number): Promise<Models.JobListResponse> {
    return ToolService.ListJobs(limit);
  }

  verifyJobV1(jobId: string): Promise<Models.VerificationReport> {
    return ToolService.VerifyJob(jobId);
  }

  async listJobHistoryV1(): Promise<Models.JobHistoryEntry[]> {
    return (await ToolService.ListJobHistory()) ?? [];
  }

  undoJobV1(jobId: string): Promise<Models.UndoResult> {
    return ToolService.UndoJob(jobId);
  }

  restoreJobV1(jobId: string): Promise<Models.UndoResult> {
    return ToolService.RestoreJob(jobId);
  }

  emptyTrashV1(): Promise<Models.PathActionResponse> {
    return ToolService.EmptyTrash();
  }

  // Recipes.
  listRecipesV1(): Promise<Models.ListRecipesResponse> {
    return RecipeService.ListRecipes();
  }

  planRecipeV1(
    request: Models.RecipeRequest,
  ): Promise<Models.RecipePlanResponse> {
    return RecipeService.PlanRecipe(request);
  }

  runRecipeV1(
    request: Models.RecipeRequest,
  ): Promise<Models.RunRecipeResponse> {
    return RecipeService.RunRecipe(request);
  }

  getRecipeStatusV1(jobId: string): Promise<Models.RecipeStatusResponse> {
    return RecipeService.GetRecipeStatus(jobId);
  }

  async listRecipeRunsV1(limit: number): Promise<Models.RecipeResult[]> {
    return (await RecipeService.ListRecipeRuns(limit)) ?? [];
  }

  cancelRecipeV1(jobId: string): Promise<Models.RecipeStatusResponse> {
    return RecipeService.CancelRecipe(jobId);
  }

  // Automation.
  listAutomationRulesV1(): Promise<Models.ListAutomationRulesResponse> {
    return AutomationService.ListAutomationRules();
  }

  saveAutomationRuleV1(
    rule: Models.AutomationRule,
  ): Promise<Models.AutomationRuleActionResponse> {
    return AutomationService.SaveAutomationRule(rule);
  }

  deleteAutomationRuleV1(
    id: string,
  ): Promise<Models.AutomationRuleActionResponse> {
    return AutomationService.DeleteAutomationRule(id);
  }

  setAutomationRuleEnabledV1(
    id: string,
    enabled: boolean,
  ): Promise<Models.AutomationRuleActionResponse> {
    return AutomationService.SetAutomationRuleEnabled(id, enabled);
  }

  getAutomationStatusV1(): Promise<Models.AutomationStatusResponse> {
    return AutomationService.GetAutomationStatus();
  }

  listAutomationRunsV1(
    limit: number,
  ): Promise<Models.ListAutomationRunsResponse> {
    return AutomationService.ListAutomationRuns(limit);
  }

  clearAutomationRunsV1(): Promise<Models.AutomationRunsActionResponse> {
    return AutomationService.ClearAutomationRuns();
  }

  // Capabilities and toolchain.
  getOcrCapabilitiesV1(): Promise<Models.OCRCapabilitiesResponse> {
    return SystemService.GetOCRCapabilities();
  }

  getTranscriptionCapabilitiesV1(
    whisperPath: string,
    modelPath: string,
  ): Promise<Models.TranscriptionCapabilitiesResponse> {
    return SystemService.GetTranscriptionCapabilities(whisperPath, modelPath);
  }

  getLocalCapabilitiesV1(): Promise<Models.LocalCapabilitiesResponse> {
    return SystemService.GetLocalCapabilities();
  }

  getToolchainStatusV1(): Promise<Models.ToolchainStatus> {
    return ToolchainService.GetToolchainStatus();
  }

  getToolchainInstallStateV1(): Promise<Models.ToolchainStatus> {
    return ToolchainService.GetToolchainInstallState();
  }

  startToolchainInstallV1(
    options: Models.ToolchainInstallOptions,
  ): Promise<Models.ToolchainStatus> {
    return ToolchainService.StartToolchainInstall(options);
  }

  cancelToolchainInstallV1(): Promise<Models.ToolchainStatus> {
    return ToolchainService.CancelToolchainInstall();
  }

  // Inspection, dialogs, and system actions.
  inspectFilesV1(paths: string[]): Promise<Models.InspectFilesResponse> {
    return SystemService.InspectFiles(paths);
  }

  notifyV1(title: string, body: string): Promise<Models.PathActionResponse> {
    return SystemService.Notify(title, body);
  }

  openPath(path: string): Promise<Models.PathActionResponse> {
    return SystemService.OpenPath(path);
  }

  revealInFolder(path: string): Promise<Models.PathActionResponse> {
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
  ): Promise<Models.PDFPreviewSourceResponse> {
    return SystemService.GetPDFPreviewSource(inputPath);
  }

  registerMediaPath(path: string): Promise<Models.MediaSourceResponse> {
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
  ): Promise<Models.ImagePreviewSourceResponse> {
    return ToolService.GetImagePreviewSource(inputPath);
  }

  getImageCropPreview(
    request: Models.ImageCropPreviewRequest,
  ): Promise<Models.ImageCropPreviewResponse> {
    return ToolService.GetImageCropPreview(request);
  }

  getImageAnnotatePreview(
    request: Models.ImageAnnotatePreviewRequest,
  ): Promise<Models.ImageAnnotatePreviewResponse> {
    return ToolService.GetImageAnnotatePreview(request);
  }
}
