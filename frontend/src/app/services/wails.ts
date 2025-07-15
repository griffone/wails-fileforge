import { Injectable } from '@angular/core';
import { Call } from '@wailsio/runtime';

export interface ConversionRequest {
  inputPath: string;
  outputPath?: string;
  format: string;
  options?: Record<string, any>;
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
  options?: Record<string, any>;
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

@Injectable({
  providedIn: 'root',
})
export class Wails {
  async convertFile(request: ConversionRequest): Promise<ConversionResult> {
    try {
      return await Call.ByID(3302357039, request);
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
      return await Call.ByID(2484487663, request);
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
      return await Call.ByID(742994356);
    } catch (error) {
      console.error('Error in getSupportedFormats:', error);
      return [];
    }
  }

  /**
   * Open a native file dialog and return the selected file path
   */
  async openFileDialog(): Promise<string> {
    try {
      const result = await Call.ByID(2246342916);
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
      const result = await Call.ByID(3029276213);
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
      const result = await Call.ByID(261105429);
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
