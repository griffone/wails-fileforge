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
    return Call.ByID(3302357039, request);
  }

  async getSupportedFormats(): Promise<SupportedFormat[]> {
    return Call.ByID(742994356);
  }
}
