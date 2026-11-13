export interface FileQueueItem {
  path: string;
  name: string;
  meta?: string;
  thumbnailUrl?: string;
  error?: string;
}
