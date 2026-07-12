export function nameUpdate(value: string): { name: string } | null {
  const name = value.trim();
  return name ? { name } : null;
}

export function descriptionUpdate(value: string): { description: string } {
  return { description: value };
}
