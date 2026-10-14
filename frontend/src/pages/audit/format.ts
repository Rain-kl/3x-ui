export function formatAuditBytes(value: number): string {
  if (value < 1024) return `${value} B`;
  const units = ['KiB', 'MiB', 'GiB', 'TiB'];
  let scaled = value;
  let index = -1;
  do {
    scaled /= 1024;
    index++;
  } while (scaled >= 1024 && index < units.length - 1);
  return `${scaled.toFixed(1)} ${units[index]}`;
}
