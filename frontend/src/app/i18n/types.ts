export type DictionaryValue = string | readonly string[];

export type Dictionary = Readonly<Record<string, DictionaryValue>>;

export type TranslationParams = Record<string, string | number>;
