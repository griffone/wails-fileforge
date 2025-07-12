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

  async getSupportedFormats(): Promise<SupportedFormat[]> {
    try {
      return await Call.ByID(742994356);
    } catch (error) {
      console.error('Error in getSupportedFormats:', error);
      return [];
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
