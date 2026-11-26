import {
  BYTES_PER_MB,
  DESTINATION_PRESETS,
  mbToTargetBytes,
  presetForTargetMb,
  presetTargetMb,
  targetSizeLabel,
} from './destination-presets';

describe('destination presets', () => {
  it('maps each shipped preset to its exact byte target', () => {
    expect(DESTINATION_PRESETS.map((preset) => preset.id)).toEqual([
      'discord',
      'whatsapp',
      'email',
    ]);

    expect(presetBytes('discord')).toBe(9_961_472);
    expect(presetBytes('whatsapp')).toBe(16_777_216);
    expect(presetBytes('email')).toBe(26_214_400);
  });

  it('uses the app binary MB convention', () => {
    expect(BYTES_PER_MB).toBe(1_048_576);
    expect(mbToTargetBytes(9.5)).toBe(9_961_472);
    expect(mbToTargetBytes(16)).toBe(16_777_216);
    expect(mbToTargetBytes(25)).toBe(26_214_400);
    expect(mbToTargetBytes(2.5)).toBe(2_621_440);
  });

  it('round-trips the fractional Discord preset without precision loss', () => {
    const discord = DESTINATION_PRESETS.find(
      (preset) => preset.id === 'discord',
    )!;
    const mb = presetTargetMb(discord);

    expect(mb).toBe(9.5);
    expect(mbToTargetBytes(mb)).toBe(9_961_472);
    expect(targetSizeLabel(9_961_472)).toBe('9.5 MB');
    expect(mbToTargetBytes(Number(targetSizeLabel(9_961_472).replace(' MB', '')))).toBe(
      9_961_472,
    );
  });

  it('maps a typed value to the same bytes as its matching preset', () => {
    for (const preset of DESTINATION_PRESETS) {
      const typed = presetTargetMb(preset);
      expect(mbToTargetBytes(typed)).toBe(preset.targetBytes);
      expect(presetForTargetMb(typed)?.id).toBe(preset.id);
    }
  });

  it('formats targets in the same MB unit as the field', () => {
    expect(targetSizeLabel(9_961_472)).toBe('9.5 MB');
    expect(targetSizeLabel(16_777_216)).toBe('16 MB');
    expect(targetSizeLabel(26_214_400)).toBe('25 MB');
    expect(targetSizeLabel(2_621_440)).toBe('2.5 MB');
  });

  it('keeps extra typed precision instead of rounding to a different target', () => {
    const bytes = mbToTargetBytes(9.55);
    expect(bytes).toBe(10_013_901);
    expect(targetSizeLabel(bytes)).toBe('9.55 MB');
  });

  it('treats any other value as a custom size', () => {
    expect(presetForTargetMb(12)).toBeNull();
    expect(presetForTargetMb(null)).toBeNull();
    expect(presetForTargetMb(Number.NaN)).toBeNull();
  });

  it('rejects non-positive and over-limit values', () => {
    expect(mbToTargetBytes(0)).toBe(0);
    expect(mbToTargetBytes(-1)).toBe(0);
    expect(mbToTargetBytes(null)).toBe(0);
    expect(mbToTargetBytes(1_000_001)).toBe(0);
  });

  function presetBytes(id: string): number {
    const preset = DESTINATION_PRESETS.find(
      (candidate) => candidate.id === id,
    );
    if (!preset) {
      throw new Error(`missing preset ${id}`);
    }
    return preset.targetBytes;
  }
});
