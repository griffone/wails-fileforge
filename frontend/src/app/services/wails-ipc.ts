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
 * rest of the frontend never depends on numeric method IDs. The `Wails`
 * service adds UI-facing fallbacks and normalizes nullable generated fields.
 */
@Injectable({ providedIn: 'root' })
export class WailsIpc {
  // Tools and jobs.
  listTools(): Promise<Models.ListToolsResponse> {
    return ToolService.ListTools();
  }

  validateJob(
    request: Models.JobRequest,
  ): Promise<Models.ValidateJobResponse> {
    return ToolService.ValidateJob(request);
  }

  planJob(request: Models.JobRequest): Promise<Models.PlanJobResponse> {
    return ToolService.PlanJob(request);
  }

  runJob(request: Models.JobRequest): Promise<Models.RunJobResponse> {
    return ToolService.RunJob(request);
  }

  cancelJob(jobId: string): Promise<Models.CancelJobResponse> {
    return ToolService.CancelJob(jobId);
  }

  getJobStatus(jobId: string): Promise<Models.JobStatusResponse> {
    return ToolService.GetJobStatus(jobId);
  }

  listJobs(limit: number): Promise<Models.JobListResponse> {
    return ToolService.ListJobs(limit);
  }

  verifyJob(jobId: string): Promise<Models.VerificationReport> {
    return ToolService.VerifyJob(jobId);
  }

  async listJobHistory(): Promise<Models.JobHistoryEntry[]> {
    return (await ToolService.ListJobHistory()) ?? [];
  }

  undoJob(jobId: string): Promise<Models.UndoResult> {
    return ToolService.UndoJob(jobId);
  }

  restoreJob(jobId: string): Promise<Models.UndoResult> {
    return ToolService.RestoreJob(jobId);
  }

  emptyTrash(): Promise<Models.PathActionResponse> {
    return ToolService.EmptyTrash();
  }

  trashInfo(): Promise<Models.TrashInfo> {
    return ToolService.TrashInfo();
  }

  // Recipes.
  listRecipes(): Promise<Models.ListRecipesResponse> {
    return RecipeService.ListRecipes();
  }

  planRecipe(
    request: Models.RecipeRequest,
  ): Promise<Models.RecipePlanResponse> {
    return RecipeService.PlanRecipe(request);
  }

  runRecipe(
    request: Models.RecipeRequest,
  ): Promise<Models.RunRecipeResponse> {
    return RecipeService.RunRecipe(request);
  }

  getRecipeStatus(jobId: string): Promise<Models.RecipeStatusResponse> {
    return RecipeService.GetRecipeStatus(jobId);
  }

  async listRecipeRuns(limit: number): Promise<Models.RecipeResult[]> {
    return (await RecipeService.ListRecipeRuns(limit)) ?? [];
  }

  cancelRecipe(jobId: string): Promise<Models.RecipeStatusResponse> {
    return RecipeService.CancelRecipe(jobId);
  }

  // Automation.
  listAutomationRules(): Promise<Models.ListAutomationRulesResponse> {
    return AutomationService.ListAutomationRules();
  }

  saveAutomationRule(
    rule: Models.AutomationRule,
  ): Promise<Models.AutomationRuleActionResponse> {
    return AutomationService.SaveAutomationRule(rule);
  }

  deleteAutomationRule(
    id: string,
  ): Promise<Models.AutomationRuleActionResponse> {
    return AutomationService.DeleteAutomationRule(id);
  }

  setAutomationRuleEnabled(
    id: string,
    enabled: boolean,
  ): Promise<Models.AutomationRuleActionResponse> {
    return AutomationService.SetAutomationRuleEnabled(id, enabled);
  }

  getAutomationStatus(): Promise<Models.AutomationStatusResponse> {
    return AutomationService.GetAutomationStatus();
  }

  listAutomationRuns(
    limit: number,
  ): Promise<Models.ListAutomationRunsResponse> {
    return AutomationService.ListAutomationRuns(limit);
  }

  clearAutomationRuns(): Promise<Models.AutomationRunsActionResponse> {
    return AutomationService.ClearAutomationRuns();
  }

  // Capabilities and toolchain.
  getOcrCapabilities(): Promise<Models.OCRCapabilitiesResponse> {
    return SystemService.GetOCRCapabilities();
  }

  getTranscriptionCapabilities(
    whisperPath: string,
    modelPath: string,
  ): Promise<Models.TranscriptionCapabilitiesResponse> {
    return SystemService.GetTranscriptionCapabilities(whisperPath, modelPath);
  }

  getLocalCapabilities(): Promise<Models.LocalCapabilitiesResponse> {
    return SystemService.GetLocalCapabilities();
  }

  getToolchainStatus(): Promise<Models.ToolchainStatus> {
    return ToolchainService.GetToolchainStatus();
  }

  getToolchainInstallState(): Promise<Models.ToolchainStatus> {
    return ToolchainService.GetToolchainInstallState();
  }

  startToolchainInstall(
    options: Models.ToolchainInstallOptions,
  ): Promise<Models.ToolchainStatus> {
    return ToolchainService.StartToolchainInstall(options);
  }

  cancelToolchainInstall(): Promise<Models.ToolchainStatus> {
    return ToolchainService.CancelToolchainInstall();
  }

  // Inspection, dialogs, and system actions.
  inspectFiles(paths: string[]): Promise<Models.InspectFilesResponse> {
    return SystemService.InspectFiles(paths);
  }

  notify(title: string, body: string): Promise<Models.PathActionResponse> {
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
