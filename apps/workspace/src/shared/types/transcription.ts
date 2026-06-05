export interface TranscriptionResponse {
  text: string;
  provider: string;
  model: string;
  duration_seconds?: number;
}
