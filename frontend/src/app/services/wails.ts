import { Injectable } from '@angular/core';

declare global {
  interface Window {
    go: {
      app: {
        App: {
          ConvertFile: (req: ConversionRequest) => Promise<ConversionResult>;
          GetSupportedFormats: () => Promise<SupportedFormat[]>;
        };
      };
    };
  }
}

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
    return window.go.app.App.ConvertFile(request);
  }

  async getSupportedFormats(): Promise<SupportedFormat[]> {
    return window.go.app.App.GetSupportedFormats();
  }
}
