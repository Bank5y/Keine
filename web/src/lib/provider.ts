// A provider configuration (ADR 0008) as the admin page offers it: choosing
// one fills in its address and display name, so only the key and the model
// name are left to type.

export interface Preset { id: string; name: string; format: string; baseUrl: string; displayName: string }

interface Fields { provider: string; baseUrl: string; displayName: string }

// chooseProvider switches the form to another provider (or none): a field
// still blank, or still holding the old provider's default, takes the new
// one's; what the admin typed stays.
export function chooseProvider<F extends Fields>(f: F, next: string, presets: Preset[]): F {
  const before = presets.find((p) => p.id === f.provider);
  const after = presets.find((p) => p.id === next);
  const pick = (value: string, key: 'baseUrl' | 'displayName') => (value === '' || value === before?.[key] ? after?.[key] ?? '' : value);
  return { ...f, provider: next, baseUrl: pick(f.baseUrl, 'baseUrl'), displayName: pick(f.displayName, 'displayName') };
}
