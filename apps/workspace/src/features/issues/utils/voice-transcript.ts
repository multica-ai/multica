export interface VoiceTranscriptDraft {
  title: string;
  description: string;
  titleNeedsManualConfirmation: boolean;
}

const MIN_TITLE_LENGTH = 4;
const MAX_TITLE_LENGTH = 120;

// Extracts a deterministic issue title candidate without using AI.
export function extractVoiceTitleCandidate(transcript: string): string {
  const normalized = transcript.replace(/\s+/g, " ").trim();
  if (!normalized) return "";

  const firstLine = transcript
    .split(/\r?\n/)
    .map((line) => line.trim())
    .find(Boolean);
  const source = firstLine || normalized;
  const sentenceMatch = source.match(/^(.+?[.!?。！？])(?:\s|$)/);
  const candidate = (sentenceMatch?.[1] ?? source).trim();
  if (candidate.length <= MAX_TITLE_LENGTH) return candidate;
  return candidate.slice(0, MAX_TITLE_LENGTH).trim();
}

// Merges transcript text into the existing create-issue draft.
export function applyVoiceTranscriptToDraft(args: {
  title: string;
  description: string;
  transcript: string;
}): VoiceTranscriptDraft {
  const title = args.title.trim();
  const description = args.description.trim();
  const transcript = args.transcript.trim();

  if (!transcript) {
    return { title, description, titleNeedsManualConfirmation: !title };
  }

  const nextDescription = description ? `${description}\n\n${transcript}` : transcript;
  if (title) {
    return {
      title,
      description: nextDescription,
      titleNeedsManualConfirmation: false,
    };
  }

  const candidate = extractVoiceTitleCandidate(transcript);
  const usableTitle = candidate.length >= MIN_TITLE_LENGTH;
  return {
    title: usableTitle ? candidate : "",
    description: nextDescription,
    titleNeedsManualConfirmation: !usableTitle,
  };
}
