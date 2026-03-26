import { Injectable } from '@angular/core';
import { Call } from '@wailsio/runtime';

export interface ConversionRequest {
  inputPath: string;
  outputPath?: string;
  format: string;
  options?: Record<string, unknown>;
  category: string;
}

export interface ConversionResult {
  success: boolean;
  message: string;
  outputPath?: string;
}

export interface BatchConversionRequest {
  inputPaths: string[];
  outputDir: string;
  format: string;
  options?: Record<string, unknown>;
  category: string;
  keepStructure: boolean;
  workers?: number;
}

export interface FileConversionResult {
  inputPath: string;
  outputPath: string;
  success: boolean;
  message: string;
}

export interface BatchConversionResult {
  success: boolean;
  message: string;
  totalFiles: number;
  successCount: number;
  failureCount: number;
  results: FileConversionResult[];
}

export interface SupportedFormat {
  category: string;
  formats: string[];
}

export type ToolRuntimeStatusV1 = 'enabled' | 'disabled' | 'degraded';
export type JobModeV1 = 'single' | 'batch';
export type JobExecutionStatusV1 =
  | 'queued'
  | 'running'
  | 'completed'
  | 'failed'
  | 'canceled';

export interface ToolManifestV1 {
  toolId: string;
  name: string;
  description: string;
  domain: string;
  capability: string;
  version: string;
  supportsSingle: boolean;
  supportsBatch: boolean;
  inputExtensions: string[];
  outputExtensions: string[];
  runtimeDependencies: string[];
  tags: string[];
}

export interface ToolRuntimeStateV1 {
  status: ToolRuntimeStatusV1;
  reason?: string;
  healthy: boolean;
}

export interface ToolCatalogEntryV1 {
  manifest: ToolManifestV1;
  state: ToolRuntimeStateV1;
}

export interface ListToolsResponseV1 {
  success: boolean;
  message: string;
  tools: ToolCatalogEntryV1[];
}

export interface JobErrorV1 {
  code: string;
  message: string;
  details?: Record<string, unknown>;
}

export interface JobRequestV1 {
  toolId: string;
  mode: JobModeV1;
  inputPaths: string[];
  outputDir: string;
  options: Record<string, unknown>;
  workers?: number;
}

export interface JobProgressV1 {
  current: number;
  total: number;
  stage: string;
  message: string;
}

export interface JobResultItemV1 {
  inputPath: string;
  outputPath: string;
  outputs?: string[];
  outputCount?: number;
  success: boolean;
  message: string;
  error?: JobErrorV1;
}

export interface JobResultV1 {
  jobId: string;
  success: boolean;
  message: string;
  toolId: string;
  status: string;
  progress: JobProgressV1;
  items: JobResultItemV1[];
  error?: JobErrorV1;
  startedAt: number;
  endedAt?: number;
}

export interface ValidateJobResponseV1 {
  success: boolean;
  message: string;
  valid: boolean;
  error?: JobErrorV1;
}

export interface RunJobResponseV1 {
  success: boolean;
  message: string;
  jobId: string;
  status: string;
  error?: JobErrorV1;
}

export interface CancelJobResponseV1 {
  success: boolean;
  message: string;
  jobId: string;
  error?: JobErrorV1;
}

export interface JobStatusResponseV1 {
  success: boolean;
  message: string;
  found: boolean;
  result?: JobResultV1;
  error?: JobErrorV1;
}

export interface PDFPreviewSourceResponseV1 {
  success: boolean;
  message: string;
  dataBase64?: string;
  mimeType?: string;
  error?: JobErrorV1;
}

@Injectable({
  providedIn: 'root',
})
export class Wails {
  private callByID(id: number, ...args: unknown[]): Promise<any> {
    return Call.ByID(id, ...args);
  }

  async convertFile(request: ConversionRequest): Promise<ConversionResult> {
    try {
      return await this.callByID(3302357039, request);
    } catch (error) {
      console.error('Error in convertFile:', error);
      return {
        success: false,
        message: `Conversion failed: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`,
        outputPath: '',
      };
    }
  }

  async convertBatch(
    request: BatchConversionRequest
  ): Promise<BatchConversionResult> {
    try {
      return await this.callByID(2484487663, request);
    } catch (error) {
      console.error('Error in convertBatch:', error);
      return {
        success: false,
        message: `Batch conversion failed: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`,
        totalFiles: 0,
        successCount: 0,
        failureCount: 0,
        results: [],
      };
    }
  }

  async getSupportedFormats(): Promise<SupportedFormat[]> {
    try {
      return await this.callByID(742994356);
    } catch (error) {
      console.error('Error in getSupportedFormats:', error);
      return [];
    }
  }

  async listToolsV1(): Promise<ListToolsResponseV1> {
    try {
      return await this.callByID(517184612);
    } catch (error) {
      console.error('Error in listToolsV1:', error);
      return {
        success: false,
        message: `Failed to list tools: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`,
        tools: [],
      };
    }
  }

  async validateJobV1(request: JobRequestV1): Promise<ValidateJobResponseV1> {
    try {
      return await this.callByID(1505194326, request);
    } catch (error) {
      console.error('Error in validateJobV1:', error);
      return {
        success: false,
        message: `Validation call failed: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`,
        valid: false,
      };
    }
  }

  async runJobV1(request: JobRequestV1): Promise<RunJobResponseV1> {
    try {
      return await this.callByID(162380599, request);
    } catch (error) {
      console.error('Error in runJobV1:', error);
      return {
        success: false,
        message: `Run job failed: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`,
        jobId: '',
        status: 'failed',
      };
    }
  }

  async cancelJobV1(jobId: string): Promise<CancelJobResponseV1> {
    try {
      return await this.callByID(3378080914, jobId);
    } catch (error) {
      console.error('Error in cancelJobV1:', error);
      return {
        success: false,
        message: `Cancel job failed: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`,
        jobId,
      };
    }
  }

  async getJobStatusV1(jobId: string): Promise<JobStatusResponseV1> {
    try {
      return await this.callByID(1961277890, jobId);
    } catch (error) {
      console.error('Error in getJobStatusV1:', error);
      return {
        success: false,
        message: `Get job status failed: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`,
        found: false,
      };
    }
  }

  async getPdfPreviewSource(inputPath: string): Promise<PDFPreviewSourceResponseV1> {
    try {
      return await this.callByID(632882064, inputPath);
    } catch (error) {
      console.error('Error in getPdfPreviewSource:', error);
      return {
        success: false,
        message: `Get PDF preview source failed: ${
          error instanceof Error ? error.message : 'Unknown error'
        }`,
        error: {
          code: 'PDF_PREVIEW_READ_FAILED',
          message: error instanceof Error ? error.message : 'Unknown error',
        },
      };
    }
  }

  /**
   * Open a native file dialog and return the selected file path
   */
  async openFileDialog(): Promise<string> {
    try {
      const result = await this.callByID(2246342916);
      return result ?? '';
    } catch (error) {
      console.error('Error in openFileDialog:', error);
      return '';
    }
  }

  /**
   * Open a native file dialog for multiple files and return the selected file paths
   */
  async openMultipleFilesDialog(): Promise<string[]> {
    try {
      const result = await this.callByID(3029276213);
      return result ?? [];
    } catch (error) {
      console.error('Error in openMultipleFilesDialog:', error);
      return [];
    }
  }

  /**
   * Open a native directory dialog and return the selected directory path
   */
  async openDirectoryDialog(): Promise<string> {
    try {
      const result = await this.callByID(261105429);
      return result ?? '';
    } catch (error) {
      console.error('Error in openDirectoryDialog:', error);
      return '';
    }
  }

  /**
   * Check if the Wails runtime is available (without waiting)
   */
  isRuntimeAvailable(): boolean {
    return (
      typeof window !== 'undefined' &&
      (window as any)._wails &&
      typeof Call?.ByID === 'function'
    );
  }
}
