import { JobErrorV1 } from './wails';

export type ToolErrorAction =
  | 'retry'
  | 'chooseFolder'
  | 'changeName'
  | 'changeFormat'
  | 'installDependency'
  | 'none';

export interface ToolErrorCopy {
  title: string;
  description: string;
  action: ToolErrorAction;
  technical?: string;
}

type ToolErrorSeed = Omit<ToolErrorCopy, 'technical'>;

const OUTPUT_EXISTS: ToolErrorSeed = {
  title: 'A file with that name already exists.',
  description:
    "FileForge won't overwrite it. Choose another name or folder.",
  action: 'changeName',
};

const OUTPUT_DIR_UNUSABLE: ToolErrorSeed = {
  title: "That folder can't be used.",
  description: 'Choose a folder you can write to.',
  action: 'chooseFolder',
};

const INPUT_NOT_FOUND: ToolErrorSeed = {
  title: "That file couldn't be found.",
  description: 'It may have been moved or deleted. Add it again.',
  action: 'none',
};

const FORMAT_UNSUPPORTED: ToolErrorSeed = {
  title: "That format isn't supported.",
  description: 'Choose a supported format or a different file.',
  action: 'changeFormat',
};

const DEPENDENCY_MISSING: ToolErrorSeed = {
  title: 'A required tool is missing.',
  description: 'Install the missing dependency and try again.',
  action: 'installDependency',
};

const CANCELLED: ToolErrorSeed = {
  title: 'Cancelled.',
  description: 'No files were changed.',
  action: 'none',
};

const GENERIC_DESCRIPTION =
  'Try again. If it keeps happening, open Details.';

const OUTPUT_EXISTS_CODES = [
  '*OUTPUT_EXISTS',
  '*OUTPUT_ALREADY_EXISTS',
  '*OUTPUT_COLLIDES_INPUT',
  'IMAGE_OUTPUT_SAME_AS_INPUT',
  '*OUTPUT_SAME_AS_INPUT',
];

const OUTPUT_DIR_CODES = [
  '*OUTPUT_DIR_NOT_FOUND',
  '*OUTPUT_DIR_NOT_DIRECTORY',
  '*OUTPUT_DIR_NOT_WRITABLE',
  '*OUTPUT_DIR_INVALID',
];

const INPUT_NOT_FOUND_CODES = ['*INPUT_NOT_FOUND', '*NOT_FOUND'];

const FORMAT_CODES = [
  '*UNSUPPORTED',
  '*UNSUPPORTED_FORMAT',
  '*FORMAT_INVALID',
  '*FORMAT_MISMATCH',
];

const DEPENDENCY_CODES = [
  'RUNTIME_DEP_MISSING',
  '*RUNTIME*',
  '*DEPENDENCY*',
];

const CANCELLED_CODES = [
  'CANCELLED_BY_USER',
  '*CANCELLED*',
  '*CANCELED*',
];

function globMatch(value: string, pattern: string): boolean {
  if (pattern.startsWith('*') && pattern.endsWith('*') && pattern.length > 2) {
    return value.includes(pattern.slice(1, -1));
  }
  if (pattern.startsWith('*')) {
    return value.endsWith(pattern.slice(1));
  }
  if (pattern.endsWith('*')) {
    return value.startsWith(pattern.slice(0, -1));
  }
  return value === pattern;
}

function matchesAny(value: string, patterns: string[]): boolean {
  return patterns.some((pattern) => globMatch(value, pattern));
}

function seedForCode(code: string): ToolErrorSeed | null {
  if (matchesAny(code, OUTPUT_EXISTS_CODES)) {
    return OUTPUT_EXISTS;
  }
  if (matchesAny(code, OUTPUT_DIR_CODES)) {
    return OUTPUT_DIR_UNUSABLE;
  }
  if (matchesAny(code, INPUT_NOT_FOUND_CODES)) {
    return INPUT_NOT_FOUND;
  }
  if (matchesAny(code, FORMAT_CODES)) {
    return FORMAT_UNSUPPORTED;
  }
  if (matchesAny(code, DEPENDENCY_CODES)) {
    return DEPENDENCY_MISSING;
  }
  if (matchesAny(code, CANCELLED_CODES)) {
    return CANCELLED;
  }
  return null;
}

function seedForMessage(message: string): ToolErrorSeed | null {
  if (/already exists|overwrite|same as (the )?input/i.test(message)) {
    return OUTPUT_EXISTS;
  }
  if (
    /output (directory|folder)|not a directory|permission denied|access denied|read-only file system|not writable|no such directory/i.test(
      message,
    )
  ) {
    return OUTPUT_DIR_UNUSABLE;
  }
  if (/no such file|not found|does not exist|missing input/i.test(message)) {
    return INPUT_NOT_FOUND;
  }
  if (
    /unsupported|invalid format|format (mismatch|not supported)|unknown format/i.test(
      message,
    )
  ) {
    return FORMAT_UNSUPPORTED;
  }
  if (/cancell?ed|aborted by user/i.test(message)) {
    return CANCELLED;
  }
  if (
    /runtime|dependenc|not installed|ffmpeg not found|libreoffice not found/i.test(
      message,
    )
  ) {
    return DEPENDENCY_MISSING;
  }
  return null;
}

/**
 * Raw OS/ffmpeg/Go messages contain paths, exec output or stack traces. Those
 * should never be the primary explanation; the Details disclosure keeps them.
 */
export function looksTechnicalMessage(message: string): boolean {
  if (!message) {
    return false;
  }
  if (/\b(exec|execve|exit status|ffmpeg|libreoffice|soffice|panic|goroutine)\b/i.test(message)) {
    return true;
  }
  if (/(^|[\s("'`])(?:\/(?:[^\s/]+\/)+|[a-zA-Z]:\\[^\s]*)/.test(message)) {
    return true;
  }
  return message.length > 220;
}

/**
 * Turns a backend JobError into plain-language copy with a recovery action.
 * Resolution order: detail_code, canonical code, then message patterns.
 */
export function describeToolError(
  error?: JobErrorV1 | null,
  fallback?: string,
): ToolErrorCopy {
  const message =
    typeof error?.message === 'string' ? error.message.trim() : '';
  const codes = [error?.detail_code, error?.code].filter(
    (code): code is string =>
      typeof code === 'string' && code.trim().length > 0,
  );

  const technical = [
    ...new Set(
      [...codes, message].filter((part): part is string => !!part),
    ),
  ].join(' · ');

  for (const code of codes) {
    const seed = seedForCode(code.trim().toUpperCase());
    if (seed) {
      return { ...seed, technical: technical || undefined };
    }
  }

  if (message) {
    const seed = seedForMessage(message);
    if (seed) {
      return { ...seed, technical: technical || undefined };
    }
  }

  const fallbackText = typeof fallback === 'string' ? fallback.trim() : '';
  const description =
    message && !looksTechnicalMessage(message)
      ? message
      : fallbackText || GENERIC_DESCRIPTION;

  return {
    title: 'Something went wrong.',
    description,
    action: 'retry',
    technical: technical || fallbackText || undefined,
  };
}
